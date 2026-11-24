//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"

	"mctunnel/internal/applog"
	"mctunnel/internal/proxy"
	"mctunnel/internal/secret"
	"mctunnel/internal/serverprobe"
	"mctunnel/internal/settings"
	"mctunnel/internal/tunnel"
)

// FormValues is what the page sends back on start-up: exactly the four fields
// the form holds.
type FormValues struct {
	Server string `json:"server"`
	Proxy  string `json:"proxy"`
	Kind   string `json:"kind"`
	Port   int    `json:"port"`
}

// StartResult tells the page whether the proxy line overrode the radio buttons
// and which local addresses Minecraft can use.
type StartResult struct {
	Kind  string   `json:"kind"`
	Addrs []string `json:"addrs"`
}

// State is polled by the page twice a second.
type State struct {
	Running   bool     `json:"running"`
	StartedAt int64    `json:"startedAt"`
	Sessions  int64    `json:"sessions"`
	Active    int64    `json:"active"`
	Up        int64    `json:"up"`
	Down      int64    `json:"down"`
	UptimeSec int      `json:"uptimeSec"`
	Addrs     []string `json:"addrs"`
}

// LogLine is one streamed log entry.
type LogLine struct {
	Time  string `json:"time"`
	Level string `json:"level"`
	Msg   string `json:"msg"`
}

// App is the object Wails exposes to JavaScript as window.go.main.App.
type App struct {
	ctx       context.Context
	log       *applog.Logger
	manager   *tunnel.Manager
	protector settings.Protector

	path      string
	current   settings.Settings
	startedAt time.Time

	mu       sync.Mutex
	lastEmit time.Time
	testing  bool
	signal   chan struct{}
}

func newApp() *App {
	a := &App{
		log:       applog.New(1000),
		protector: dpapiProtector{},
		signal:    make(chan struct{}, 1),
	}
	a.manager = tunnel.New(a.log)
	return a
}

// startup runs once the window exists: it loads the saved form and starts the
// log pump. Settings load before the page asks for them, so Load never races.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.log.SetHook(a.wake)
	go a.pump()
	go shapeWindow()

	path, err := settings.ResolvePath()
	if err != nil {
		a.log.Errorf("settings: %v", err)
		return
	}
	a.path = path
	loaded, err := settings.Load(path, a.protector)
	if err != nil {
		a.log.Warnf("settings: %v", err)
		loaded = settings.Default()
	}
	a.current = loaded
	a.log.Infof("settings loaded from %s", path)
}

// beforeClose stops the tunnel and remembers the form before the window goes.
func (a *App) beforeClose(context.Context) bool {
	a.manager.Stop()
	a.save()
	return false
}

// Load hands the saved form to the page.
func (a *App) Load() FormValues {
	s := a.current
	return FormValues{
		Server: s.Server,
		Proxy:  proxyLine(s),
		Kind:   s.ProxyKind,
		Port:   s.LocalPort,
	}
}

// Start validates the form and brings the tunnel up.
func (a *App) Start(server, proxyText, kind string, port int) (StartResult, error) {
	if a.manager.Running() {
		return StartResult{}, fmt.Errorf("the tunnel is already running")
	}
	target, localPort, err := resolveTarget(server, port)
	if err != nil {
		return StartResult{}, err
	}
	dialer, forced, notes, err := resolveDialer(proxyText, kind)
	if err != nil {
		return StartResult{}, err
	}
	for _, note := range notes {
		a.log.Infof("%s", note)
	}
	a.log.Infof("using proxy %s", dialer.Describe())

	addrs, err := a.manager.Start(tunnel.Config{
		Server: target,
		Listen: fmt.Sprintf("127.0.0.1:%d", localPort),
		Proxy:  dialer,
	})
	if err != nil {
		a.log.Errorf("start tunnel: %v", err)
		return StartResult{}, err
	}

	a.mu.Lock()
	a.startedAt = time.Now()
	a.mu.Unlock()

	a.log.Infof("connect Minecraft to %s", strings.Join(addrs, " or "))
	a.remember(server, proxyText, kind, localPort)
	a.emitState()
	return StartResult{Kind: string(forced), Addrs: addrs}, nil
}

// Stop tears the tunnel down, including live sessions.
func (a *App) Stop() error {
	if !a.manager.Running() {
		return fmt.Errorf("the tunnel is not running")
	}
	a.manager.Stop()
	a.emitState()
	return nil
}

// Test checks the proxy and, when a server is filled in, whether that server
// answers through it. Both results are logged; the page only spins the button.
func (a *App) Test(server, proxyText, kind string) error {
	a.mu.Lock()
	if a.testing {
		a.mu.Unlock()
		return fmt.Errorf("a proxy check is already running")
	}
	a.testing = true
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.testing = false
		a.mu.Unlock()
	}()

	dialer, forced, notes, err := resolveDialer(proxyText, kind)
	if err != nil {
		a.log.Errorf("%v", err)
		return err
	}
	for _, note := range notes {
		a.log.Infof("%s", note)
	}
	_ = forced

	target, _, targetErr := resolveTarget(server, 0)
	a.log.Infof("checking proxy %s…", dialer.Describe())

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	report := proxy.Check(ctx, dialer)
	if report.OK {
		a.log.Infof("proxy check: %s", report)
	} else {
		a.log.Errorf("proxy check: %s", report)
	}

	if server == "" {
		a.log.Warnf("skipping the server check: no server given")
	} else if targetErr != nil {
		a.log.Warnf("skipping the server check: %v", targetErr)
	} else {
		probe := serverprobe.Status(ctx, dialer, target)
		if probe.OK() {
			a.log.Infof("server %s through the proxy: %s", target, probe)
		} else {
			a.log.Errorf("server %s through the proxy: %s", target, probe)
		}
	}
	return nil
}

// State is polled by the page for the traffic card.
func (a *App) State() State {
	stats := a.manager.Stats()
	out := State{
		Running:  a.manager.Running(),
		Sessions: stats.Total,
		Active:   stats.Active,
		Up:       stats.Up,
		Down:     stats.Down,
	}
	if out.Running {
		a.mu.Lock()
		started := a.startedAt
		a.mu.Unlock()
		out.StartedAt = started.UnixMilli()
		out.UptimeSec = int(time.Since(started).Seconds())
		out.Addrs = a.manager.LocalAddrs()
	}
	return out
}

// SaveLog writes what the page has in the log pane to a file the user picks.
func (a *App) SaveLog(text string) (string, error) {
	path, err := wailsrt.SaveFileDialog(a.ctx, wailsrt.SaveDialogOptions{
		Title:           "Сохранить лог",
		DefaultFilename: "mc-tunnel-log.txt",
		Filters: []wailsrt.FileFilter{
			{DisplayName: "Текстовые файлы (*.txt)", Pattern: "*.txt"},
			{DisplayName: "Все файлы (*.*)", Pattern: "*.*"},
		},
	})
	if err != nil || path == "" {
		return "", err
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		a.log.Errorf("save log: %v", err)
		return "", err
	}
	a.log.Infof("log saved to %s", path)
	return path, nil
}

// remember stores a form that is known to work.
func (a *App) remember(server, proxyText, kind string, port int) {
	cfg, err := proxy.Parse(proxyText)
	if err != nil {
		a.log.Warnf("settings not saved: %v", err)
		return
	}
	if cfg.Kind == "" {
		cfg.Kind, _ = proxy.ParseKind(kind)
	}
	a.current = settings.Settings{
		Server:        server,
		ProxyKind:     string(cfg.Kind),
		ProxyAddr:     cfg.Addr,
		ProxyUser:     cfg.User,
		ProxyPassword: cfg.Pass,
		LocalPort:     port,
	}
	a.save()
}

// save writes the remembered form; failures are logged, never fatal.
func (a *App) save() {
	if a.path == "" {
		return
	}
	if err := settings.Save(a.path, a.current, a.protector); err != nil {
		a.log.Warnf("settings not saved: %v", err)
		return
	}
	a.log.Debugf("settings saved to %s", a.path)
}

// proxyLine renders the saved proxy back into the single line the form shows.
func proxyLine(s settings.Settings) string {
	if s.ProxyAddr == "" {
		return ""
	}
	return proxy.Config{
		Kind: proxy.Kind(s.ProxyKind),
		Addr: s.ProxyAddr,
		User: s.ProxyUser,
		Pass: s.ProxyPassword,
	}.Line()
}

// wake pokes the pump; a single pending signal is enough.
func (a *App) wake() {
	select {
	case a.signal <- struct{}{}:
	default:
	}
}

// pump forwards new log entries to the page as Wails events.
func (a *App) pump() {
	for range a.signal {
		ctx := a.ctx
		if ctx == nil {
			continue
		}
		a.mu.Lock()
		last := a.lastEmit
		a.mu.Unlock()
		for _, e := range a.log.Entries() {
			if !e.Time.After(last) {
				continue
			}
			last = e.Time
			wailsrt.EventsEmit(ctx, "log", LogLine{
				Time:  e.Time.Format("15:04:05.000"),
				Level: e.Level.String(),
				Msg:   e.Msg,
			})
		}
		a.mu.Lock()
		a.lastEmit = last
		a.mu.Unlock()
	}
}

// emitState pushes the current state so the page does not wait for its poll.
func (a *App) emitState() {
	if a.ctx == nil {
		return
	}
	wailsrt.EventsEmit(a.ctx, "state", a.State())
}

// dpapiProtector stores the proxy password with Windows DPAPI.
type dpapiProtector struct{}

// Protect implements settings.Protector.
func (dpapiProtector) Protect(plaintext string) (string, error) { return secret.Protect(plaintext) }

// Unprotect implements settings.Protector.
func (dpapiProtector) Unprotect(blob string) (string, error) { return secret.Unprotect(blob) }
