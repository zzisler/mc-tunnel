//go:build windows

// Package ui is the Win32 control panel. It is written straight against
// user32/gdi32 so the program stays a single unsigned executable with no
// runtime and no extra dependency.
package ui

import (
	"syscall"
	"unsafe"
)

// Window styles and messages. Names mirror the Win32 SDK; only what is used
// here is declared.
const (
	wsOverlappedWindow = 0x00CF0000
	wsThickFrame       = 0x00040000
	wsMaximizeBox      = 0x00010000
	wsChild            = 0x40000000
	wsVisible          = 0x10000000
	wsTabStop          = 0x00010000
	wsGroup            = 0x00020000
	wsBorder           = 0x00800000
	wsVScroll          = 0x00200000
	wsExClientEdge     = 0x00000200

	esAutoHScroll = 0x0080
	esMultiline   = 0x0004
	esAutoVScroll = 0x0040
	esReadOnly    = 0x0800

	bsPushButton      = 0x00000000
	bsAutoRadioButton = 0x00000009
	bsGroupBox        = 0x00000007

	ssLeft = 0x00000000

	cwUseDefault = 0x80000000

	swHide = 0
	swShow = 5

	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010

	colorWindow = 5

	defaultGUIFont = 17

	wmDestroy = 0x0002
	wmClose   = 0x0010
	wmSetFont = 0x0030
	wmCommand = 0x0111
	wmTimer   = 0x0113
	wmApp     = 0x8000

	emSetSel       = 0x00B1
	emReplaceSel   = 0x00C2
	emSetLimitText = 0x00C5
	emSetReadOnly  = 0x00CF

	bmSetCheck = 0x00F1
	bmGetCheck = 0x00F0

	mbOK        = 0x00000000
	mbIconError = 0x00000010
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassEx = user32.NewProc("RegisterClassExW")
	procCreateWindowEx  = user32.NewProc("CreateWindowExW")
	procDefWindowProc   = user32.NewProc("DefWindowProcW")
	procDestroyWindow   = user32.NewProc("DestroyWindow")
	procShowWindow      = user32.NewProc("ShowWindow")
	procUpdateWindow    = user32.NewProc("UpdateWindow")
	procGetMessage      = user32.NewProc("GetMessageW")
	procTranslateMsg    = user32.NewProc("TranslateMessage")
	procDispatchMsg     = user32.NewProc("DispatchMessageW")
	procPostQuitMessage = user32.NewProc("PostQuitMessage")
	procSetWindowText   = user32.NewProc("SetWindowTextW")
	procGetWindowText   = user32.NewProc("GetWindowTextW")
	procSendMessage     = user32.NewProc("SendMessageW")
	procPostMessage     = user32.NewProc("PostMessageW")
	procMoveWindow      = user32.NewProc("MoveWindow")
	procSetWindowPos    = user32.NewProc("SetWindowPos")
	procEnableWindow    = user32.NewProc("EnableWindow")
	procSetTimer        = user32.NewProc("SetTimer")
	procKillTimer       = user32.NewProc("KillTimer")
	procMessageBox      = user32.NewProc("MessageBoxW")

	procGetStockObject  = gdi32.NewProc("GetStockObject")
	procGetModuleHandle = kernel32.NewProc("GetModuleHandleW")
)

// wndclassex mirrors WNDCLASSEXW.
type wndclassex struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   syscall.Handle
	icon       syscall.Handle
	cursor     syscall.Handle
	background syscall.Handle
	menuName   *uint16
	className  *uint16
	iconSm     syscall.Handle
}

// point mirrors POINT.
type point struct{ x, y int32 }

// msg mirrors MSG.
type msg struct {
	hwnd    syscall.Handle
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
}

// utf16 returns a NUL terminated UTF-16 copy of s.
func utf16(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		// The only failure is an embedded NUL, which cannot happen for text that
		// comes from a window control.
		return &[]uint16{0}[0]
	}
	return p
}

func setText(h syscall.Handle, s string) {
	if h == 0 {
		return
	}
	procSetWindowText.Call(uintptr(h), uintptr(unsafe.Pointer(utf16(s))))
}

func getText(h syscall.Handle) string {
	if h == 0 {
		return ""
	}
	buf := make([]uint16, 1024)
	n, _, _ := procGetWindowText.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}

func sendMessage(h syscall.Handle, msg uint32, wp, lp uintptr) uintptr {
	r, _, _ := procSendMessage.Call(uintptr(h), uintptr(msg), wp, lp)
	return r
}

func postMessage(h syscall.Handle, msg uint32, wp, lp uintptr) {
	if h == 0 {
		return
	}
	procPostMessage.Call(uintptr(h), uintptr(msg), wp, lp)
}

func moveWindow(h syscall.Handle, x, y, w, hgt int32) {
	if h == 0 {
		return
	}
	procMoveWindow.Call(uintptr(h), uintptr(x), uintptr(y), uintptr(w), uintptr(hgt), 1)
}

func setWindowSize(h syscall.Handle, w, hgt int32) {
	procSetWindowPos.Call(uintptr(h), 0, 0, 0, uintptr(w), uintptr(hgt),
		uintptr(swpNoMove|swpNoZOrder|swpNoActivate))
}

func showWindow(h syscall.Handle, visible bool) {
	if h == 0 {
		return
	}
	cmd := uintptr(swHide)
	if visible {
		cmd = swShow
	}
	procShowWindow.Call(uintptr(h), cmd)
}

func enableWindow(h syscall.Handle, enabled bool) {
	if h == 0 {
		return
	}
	flag := uintptr(0)
	if enabled {
		flag = 1
	}
	procEnableWindow.Call(uintptr(h), flag)
}

func setTimer(h syscall.Handle, id, millis uintptr) {
	procSetTimer.Call(uintptr(h), id, millis, 0)
}

func killTimer(h syscall.Handle, id uintptr) {
	procKillTimer.Call(uintptr(h), id)
}

// messageBox shows a modal error and is only used before the window exists.
func messageBox(owner syscall.Handle, text string) {
	procMessageBox.Call(uintptr(owner),
		uintptr(unsafe.Pointer(utf16(text))),
		uintptr(unsafe.Pointer(utf16("MC Tunnel"))),
		uintptr(mbOK|mbIconError))
}

// appendEditText appends s to a multi-line edit control and keeps the caret at
// the end. The read-only style is toggled around the insert because
// EM_REPLACESEL is ignored by read-only controls.
func appendEditText(h syscall.Handle, s string) {
	if h == 0 || s == "" {
		return
	}
	sendMessage(h, emSetReadOnly, 0, 0)
	// Select the whole text and move the caret to the end of it.
	length := int32(sendMessage(h, 0x000E /* WM_GETTEXTLENGTH */, 0, 0))
	sendMessage(h, emSetSel, uintptr(length), uintptr(length))
	sendMessage(h, emReplaceSel, 1, uintptr(unsafe.Pointer(utf16(s))))
	sendMessage(h, emSetReadOnly, 1, 0)
}

// replaceEditText swaps the whole content of an edit control.
func replaceEditText(h syscall.Handle, s string) {
	if h == 0 {
		return
	}
	sendMessage(h, emSetReadOnly, 0, 0)
	setText(h, s)
	sendMessage(h, emSetReadOnly, 1, 0)
}
