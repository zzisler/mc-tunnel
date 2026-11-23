//go:build windows

package ui

import (
	"syscall"
	"unsafe"

	"mctunnel/internal/applog"
	"mctunnel/internal/tunnel"
)

// Control identifiers. Values above 100 keep clear of the ids Windows reserves.
const (
	idServerEdit = 101 + iota
	idProxyEdit
	idPortEdit
	idKindSOCKS5
	idKindHTTP
	idStart
	idStop
	idTest
	idLogToggle
	idLogClear
	idStatusLabel
	idStatsLabel
	idLogEdit
)

// Window geometry. The left column is fixed; the log column appears on the
// right when the log panel is expanded.
const (
	windowTitle    = "MC Tunnel"
	windowHeight   = 430
	collapsedWidth = 446
	expandedWidth  = 946

	leftX       = 14
	leftContent = 392 // left column width

	logX     = 424
	logWidth = 500

	timerID       = 1
	refreshMillis = 500
)

// App holds the window handles and everything the handlers need.
type App struct {
	hwnd syscall.Handle

	lblServer, edServer       syscall.Handle
	lblProxy, edProxy         syscall.Handle
	grpKind, rbSOCKS5, rbHTTP syscall.Handle
	lblPort, edPort           syscall.Handle
	btnStart, btnStop         syscall.Handle
	btnTest, btnLogToggle     syscall.Handle
	lblStatus, lblStats       syscall.Handle
	lblLog, edLog, btnClear   syscall.Handle

	log     *applog.Logger
	manager *tunnel.Manager

	logsVisible bool
	logCursor   int
	testing     bool
}

// control creates one child control owned by the window.
func (a *App) control(class, text string, style uint32, x, y, w, h int32, id int) syscall.Handle {
	ex := uintptr(0)
	if class == "EDIT" {
		ex = wsExClientEdge
	}
	r, _, _ := procCreateWindowEx.Call(ex,
		uintptr(unsafe.Pointer(utf16(class))),
		uintptr(unsafe.Pointer(utf16(text))),
		uintptr(uint32(wsChild)|uint32(wsVisible)|style),
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		uintptr(a.hwnd), uintptr(id), 0, 0)
	handle := syscall.Handle(r)
	if handle == 0 {
		return 0
	}
	font, _, _ := procGetStockObject.Call(defaultGUIFont)
	sendMessage(handle, wmSetFont, font, 1)
	return handle
}

// createControls builds the form.
func (a *App) createControls() {
	// The server field starts empty on purpose: a placeholder value there would
	// be probed by the Test button and produce a confusing error.
	a.lblServer = a.control("STATIC", "Minecraft server (host or host:port)", ssLeft, leftX, 14, leftContent, 18, 0)
	a.edServer = a.control("EDIT", "", esAutoHScroll|wsTabStop, leftX, 34, leftContent, 26, idServerEdit)

	a.lblProxy = a.control("STATIC", "Proxy (paste socks5://user:pass@host:port or host:port)", ssLeft, leftX, 68, leftContent+20, 18, 0)
	a.edProxy = a.control("EDIT", "", esAutoHScroll|wsTabStop, leftX, 88, leftContent, 26, idProxyEdit)

	a.grpKind = a.control("BUTTON", "Proxy type", bsGroupBox, leftX, 122, leftContent, 60, 0)
	a.rbSOCKS5 = a.control("BUTTON", "SOCKS5", bsAutoRadioButton|wsTabStop|wsGroup, leftX+14, 150, 120, 20, idKindSOCKS5)
	a.rbHTTP = a.control("BUTTON", "HTTP CONNECT", bsAutoRadioButton|wsTabStop, leftX+150, 150, 150, 20, idKindHTTP)
	sendMessage(a.rbSOCKS5, bmSetCheck, 1, 0)

	a.lblPort = a.control("STATIC", "Local port", ssLeft, leftX, 190, 90, 18, 0)
	a.edPort = a.control("EDIT", "25565", esAutoHScroll|wsTabStop, leftX+96, 186, 90, 26, idPortEdit)

	a.btnStart = a.control("BUTTON", "Start tunnel", bsPushButton|wsTabStop, leftX, 224, 120, 32, idStart)
	a.btnStop = a.control("BUTTON", "Stop", bsPushButton|wsTabStop, leftX+128, 224, 120, 32, idStop)
	a.btnTest = a.control("BUTTON", "Test proxy", bsPushButton|wsTabStop, leftX+256, 224, 136, 32, idTest)
	enableWindow(a.btnStop, false)

	a.btnLogToggle = a.control("BUTTON", "Show logs »", bsPushButton|wsTabStop, leftX, 268, 140, 28, idLogToggle)

	a.lblStatus = a.control("STATIC", "stopped", ssLeft, leftX, 302, leftContent, 18, idStatusLabel)
	a.lblStats = a.control("STATIC", "no connections yet", ssLeft, leftX, 322, leftContent, 18, idStatsLabel)

	a.lblLog = a.control("STATIC", "Log", ssLeft, logX, 14, logWidth, 18, 0)
	a.edLog = a.control("EDIT", "",
		esMultiline|esAutoVScroll|esReadOnly|wsVScroll|wsBorder|wsTabStop,
		logX, 34, logWidth, 300, idLogEdit)
	sendMessage(a.edLog, emSetLimitText, 1<<20, 0)
	a.btnClear = a.control("BUTTON", "Clear log", bsPushButton|wsTabStop, logX, 342, 120, 28, idLogClear)

	a.setLogsVisible(false)
}

// layout hides or shows the log column. The left column never moves, so this
// is the only layout work the fixed-size window needs.
func (a *App) layout() {
	if a.logsVisible {
		showWindow(a.lblLog, true)
		showWindow(a.edLog, true)
		showWindow(a.btnClear, true)
		setWindowSize(a.hwnd, expandedWidth, windowHeight)
		setText(a.btnLogToggle, "« Hide logs")
		return
	}
	showWindow(a.lblLog, false)
	showWindow(a.edLog, false)
	showWindow(a.btnClear, false)
	setWindowSize(a.hwnd, collapsedWidth, windowHeight)
	setText(a.btnLogToggle, "Show logs »")
}

// setLogsVisible expands or collapses the log panel.
func (a *App) setLogsVisible(visible bool) {
	a.logsVisible = visible
	a.layout()
	if visible {
		a.flushLog()
	}
}

// selectedKind reads the protocol radio buttons.
func (a *App) selectedKind() string {
	if sendMessage(a.rbHTTP, bmGetCheck, 0, 0) == 1 {
		return "http"
	}
	return "socks5"
}

// setKind checks the radio button matching kind.
func (a *App) setKind(kind string) {
	if kind == "http" {
		sendMessage(a.rbHTTP, bmSetCheck, 1, 0)
		sendMessage(a.rbSOCKS5, bmSetCheck, 0, 0)
		return
	}
	sendMessage(a.rbSOCKS5, bmSetCheck, 1, 0)
	sendMessage(a.rbHTTP, bmSetCheck, 0, 0)
}

// flushLog appends every log entry the panel has not shown yet. A shorter log
// than last time means applog dropped old lines, so the panel is rewritten.
func (a *App) flushLog() {
	if a.edLog == 0 || a.log == nil {
		return
	}
	entries := a.log.Entries()
	if a.logCursor > len(entries) {
		a.logCursor = 0
		replaceEditText(a.edLog, "")
	}
	pending := entries[a.logCursor:]
	if len(pending) == 0 {
		return
	}
	a.logCursor = len(entries)
	var text string
	for _, entry := range pending {
		text += entry.Format() + "\r\n"
	}
	appendEditText(a.edLog, text)
}

// clearLog empties the panel and skips everything logged so far.
func (a *App) clearLog() {
	a.logCursor = a.log.Len()
	replaceEditText(a.edLog, "")
}
