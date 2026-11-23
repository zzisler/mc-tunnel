//go:build windows

package ui

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"mctunnel/internal/addr"
	"mctunnel/internal/applog"
	"mctunnel/internal/proxy"
	"mctunnel/internal/serverprobe"
	"mctunnel/internal/tunnel"
)

// Messages posted to the window from worker goroutines.
const (
	wmLogArrived = wmApp + 1
	wmTestDone   = wmApp + 2
)

// application is the single window of this process. Win32 window procedures are
// free functions, so the instance is reached through this variable instead of a
// user-data pointer.
var application *App

func wndProc(hwnd syscall.Handle, message uint32, wp, lp uintptr) uintptr {
	a := application
	switch message {
	case wmCommand:
		if a == nil {
			break
		}
		// Only notifications with code 0 are clicks; EDIT controls also send
		// EN_CHANGE and friends, which must be ignored here.
		if uint16(wp>>16) != 0 {
			return 0
		}
		switch int(uint16(wp)) {
		case idStart:
			a.onStart()
		case idStop:
			a.onStop()
		case idTest:
			a.onTest()
		case idLogToggle:
			a.setLogsVisible(!a.logsVisible)
		case idLogClear:
			a.clearLog()
		}
		return 0
	case wmLogArrived:
		if a != nil {
			a.flushLog()
		}
		return 0
	case wmTestDone:
		if a != nil {
			a.onTestDone()
		}
		return 0
	case wmTimer:
		if a != nil {
			a.refreshStatus()
		}
		return 0
	case wmClose:
		if a != nil {
			a.log.SetHook(nil)
			a.manager.Stop()
		}
		killTimer(hwnd, timerID)
		procDestroyWindow.Call(uintptr(hwnd))
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProc.Call(uintptr(hwnd), uintptr(message), wp, lp)
	return r
}

// Run creates the window and runs the message loop until it is closed.
func Run() {
	// A Win32 message loop must stay on the thread that owns the windows, and
	// the Go runtime is otherwise free to migrate the goroutine.
	runtime.LockOSThread()

	// The executable is linked as a GUI program, so a panic would otherwise
	// vanish without a trace: there is no console and stderr goes nowhere.
	defer func() {
		if recovered := recover(); recovered != nil {
			messageBox(0, fmt.Sprintf("MC Tunnel stopped with an internal error:\n\n%v\n\n%s",
				recovered, debug.Stack()))
		}
	}()

	logger := applog.New(4000)
	a := &App{
		log:     logger,
		manager: tunnel.New(logger),
	}
	application = a

	if err := a.create(); err != nil {
		messageBox(0, err.Error())
		return
	}
	logger.SetHook(func() {
		// Posted, never sent: the hook runs on network goroutines and must not
		// block on the UI thread.
		postMessage(a.hwnd, wmLogArrived, 0, 0)
	})

	logger.Infof("MC Tunnel ready")
	logger.Infof("1) fill in the server, 2) paste the proxy, 3) Test proxy, 4) Start tunnel")
	logger.Infof("then connect Minecraft Java to 127.0.0.1:%d", defaultPort)

	var m msg
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMsg.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMsg.Call(uintptr(unsafe.Pointer(&m)))
	}
	a.manager.Stop()
}

const defaultPort = 25565

// create registers the window class and builds the form.
func (a *App) create() error {
	className := utf16("MCTunnelWindow")
	instance, _, _ := procGetModuleHandle.Call(0)
	class := wndclassex{
		wndProc:    syscall.NewCallback(wndProc),
		instance:   syscall.Handle(instance),
		background: syscall.Handle(colorWindow + 1),
		className:  className,
	}
	class.size = uint32(unsafe.Sizeof(class))
	if r, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&class))); r == 0 {
		return fmt.Errorf("cannot register the window class: %v", err)
	}
	style := uint32(wsOverlappedWindow &^ wsThickFrame &^ wsMaximizeBox)
	r, _, err := procCreateWindowEx.Call(0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(utf16(windowTitle))),
		uintptr(style), cwUseDefault, cwUseDefault,
		collapsedWidth, windowHeight, 0, 0, instance, 0)
	if r == 0 {
		return fmt.Errorf("cannot create the window: %v", err)
	}
	a.hwnd = syscall.Handle(r)
	a.createControls()
	showWindow(a.hwnd, true)
	procUpdateWindow.Call(uintptr(a.hwnd))
	setTimer(a.hwnd, timerID, refreshMillis)
	a.refreshStatus()
	return nil
}

// onStart validates the form and starts the tunnel.
func (a *App) onStart() {
	if a.manager.Running() {
		a.log.Warnf("the tunnel is already running")
		return
	}
	s, err := resolveForm(a.readForm())
	if err != nil {
		a.log.Errorf("%v", err)
		return
	}
	a.applyNotes(s)
	addrs, err := a.manager.Start(tunnel.Config{
		Server: s.target,
		Listen: net.JoinHostPort("127.0.0.1", strconv.Itoa(s.port)),
		Proxy:  s.dialer,
	})
	if err != nil {
		a.log.Errorf("start tunnel: %v", err)
		return
	}
	a.updateButtons()
	a.log.Infof("connect Minecraft to %s", strings.Join(addrs, " or "))
}

// onStop tears the tunnel down, including live sessions.
func (a *App) onStop() {
	if !a.manager.Running() {
		a.log.Warnf("the tunnel is not running")
		return
	}
	a.manager.Stop()
	a.updateButtons()
}

// onTest checks the proxy and, when a server is filled in, whether that server
// actually answers through the proxy. A proxy can accept a CONNECT and still
// drop the stream, which otherwise looks like a mysterious client timeout.
func (a *App) onTest() {
	if a.testing {
		return
	}
	form := a.readForm()
	dialer, kind, notes, err := resolveProxy(form)
	if err != nil {
		a.log.Errorf("%v", err)
		return
	}
	a.applyProxyNotes(kind, notes)
	// The server is optional here: checking the proxy alone is a valid test.
	target, targetErr := addr.Normalize(form.Server, defaultPort)

	a.testing = true
	enableWindow(a.btnTest, false)
	enableWindow(a.btnStart, false)
	a.log.Infof("checking proxy %s…", dialer.Describe())

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		report := proxy.Check(ctx, dialer)
		if report.OK {
			a.log.Infof("proxy check: %s", report)
		} else {
			a.log.Errorf("proxy check: %s", report)
		}

		if targetErr != nil {
			a.log.Warnf("skipping the server check: %v", targetErr)
		} else {
			probe := serverprobe.Status(ctx, dialer, target)
			if probe.OK() {
				a.log.Infof("server %s through the proxy: %s", target, probe)
			} else {
				a.log.Errorf("server %s through the proxy: %s", target, probe)
			}
		}
		postMessage(a.hwnd, wmTestDone, 0, 0)
	}()
}

// onTestDone re-enables the controls after a proxy check.
func (a *App) onTestDone() {
	a.testing = false
	enableWindow(a.btnTest, true)
	enableWindow(a.btnStart, true)
	a.updateButtons()
}

// formValues is the raw text the user typed, straight from the controls.
type formValues struct {
	Server string
	Proxy  string
	Kind   string
	Port   string
}

// settings is a validated form, ready to be handed to the tunnel manager.
type settings struct {
	target string
	port   int
	dialer proxy.Dialer
	// kind is set when the proxy line overrode the radio buttons.
	kind  proxy.Kind
	notes []string
}

// readForm reads the controls into a formValues.
func (a *App) readForm() formValues {
	return formValues{
		Server: getText(a.edServer),
		Proxy:  getText(a.edProxy),
		Kind:   a.selectedKind(),
		Port:   getText(a.edPort),
	}
}

// applyProxyNotes syncs the UI with what the proxy parser decided and logs it.
func (a *App) applyProxyNotes(kind proxy.Kind, notes []string) {
	if kind != "" {
		a.setKind(string(kind))
	}
	for _, note := range notes {
		a.log.Infof("%s", note)
	}
}

// applyNotes syncs the UI with what the parser decided and logs it.
func (a *App) applyNotes(s settings) {
	a.applyProxyNotes(s.kind, s.notes)
	a.log.Infof("using proxy %s", s.dialer.Describe())
}

// resolveProxy parses the proxy line and builds a dialer for it. It reports the
// kind the line forced and any notes worth showing the user.
func resolveProxy(in formValues) (proxy.Dialer, proxy.Kind, []string, error) {
	cfg, err := proxy.Parse(in.Proxy)
	if err != nil {
		return nil, "", nil, err
	}
	selected, err := proxy.ParseKind(in.Kind)
	if err != nil {
		return nil, "", nil, err
	}
	var kind proxy.Kind
	var notes []string
	switch {
	case cfg.Kind == "":
		// No scheme in the line: the radio buttons decide.
		cfg.Kind = selected
	case cfg.Kind != selected:
		// A scheme always wins, and the radio buttons follow it.
		kind = cfg.Kind
		notes = append(notes,
			fmt.Sprintf("proxy type taken from the line: %s (radio button updated)", cfg.Kind))
	}
	dialer, err := proxy.New(cfg)
	if err != nil {
		return nil, "", nil, err
	}
	return dialer, kind, notes, nil
}

// resolveForm turns the typed text into a tunnel configuration. It holds the
// whole validation story of the form, which keeps it testable without a window.
func resolveForm(in formValues) (settings, error) {
	var out settings

	target, err := addr.Normalize(in.Server, defaultPort)
	if err != nil {
		return out, fmt.Errorf("minecraft server: %w", err)
	}
	port, err := parseLocalPort(in.Port)
	if err != nil {
		return out, err
	}
	dialer, kind, notes, err := resolveProxy(in)
	if err != nil {
		return out, err
	}

	out.target = target
	out.port = port
	out.dialer = dialer
	out.kind = kind
	out.notes = notes
	return out, nil
}

// updateButtons keeps the controls in sync with the tunnel state.
func (a *App) updateButtons() {
	running := a.manager.Running()
	enableWindow(a.btnStart, !running && !a.testing)
	enableWindow(a.btnStop, running)
	if running {
		setText(a.lblStatus, "running -> "+strings.Join(a.manager.LocalAddrs(), ", "))
		return
	}
	setText(a.lblStatus, "stopped")
}

// refreshStatus is the timer tick: state plus live counters.
func (a *App) refreshStatus() {
	a.updateButtons()
	stats := a.manager.Stats()
	switch {
	case a.manager.Running():
		setText(a.lblStats, fmt.Sprintf("sessions %d active / %d total, up %s, down %s",
			stats.Active, stats.Total, applog.Bytes(stats.Up), applog.Bytes(stats.Down)))
	case stats.Total > 0:
		setText(a.lblStats, fmt.Sprintf("last run: %d sessions, up %s, down %s",
			stats.Total, applog.Bytes(stats.Up), applog.Bytes(stats.Down)))
	default:
		setText(a.lblStats, "no connections yet")
	}
}

// parseLocalPort parses the local port field, defaulting to 25565.
func parseLocalPort(text string) (int, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return defaultPort, nil
	}
	port, err := strconv.Atoi(text)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("local port %q is not a valid port", text)
	}
	return port, nil
}
