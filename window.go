//go:build windows

package main

import (
	"syscall"
	"time"
	"unsafe"
)

// Window shape. Windows 11 rounds every window in the compositor, with proper
// anti-aliasing; Windows 10 has no such API, so the window is clipped to a
// rounded region instead. The region has hard pixel edges, which is the price
// of doing it on an older compositor.
const (
	windowClass = "MCTunnelWindow"

	dwmwaCornerPreference = 33 // DWMWA_WINDOW_CORNER_PREFERENCE
	dwmwcpRound           = 2  // DWMWCP_ROUND

	cornerRadiusDip = 10
)

var (
	user32                 = syscall.NewLazyDLL("user32.dll")
	gdi32                  = syscall.NewLazyDLL("gdi32.dll")
	dwmapi                 = syscall.NewLazyDLL("dwmapi.dll")
	procFindWindow         = user32.NewProc("FindWindowW")
	procGetWindowRect      = user32.NewProc("GetWindowRect")
	procGetDpiForWindow    = user32.NewProc("GetDpiForWindow")
	procSetWindowRgn       = user32.NewProc("SetWindowRgn")
	procCreateRoundRectRgn = gdi32.NewProc("CreateRoundRectRgn")
	procDwmSetAttribute    = dwmapi.NewProc("DwmSetWindowAttribute")
)

type windowRect struct{ left, top, right, bottom int32 }

func utf16Ptr(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		return nil
	}
	return p
}

// shapeWindow rounds the corners of the main window. It runs after the window
// exists, so it waits for it instead of racing the framework.
func shapeWindow() {
	var hwnd uintptr
	for i := 0; i < 100; i++ {
		hwnd, _, _ = procFindWindow.Call(
			uintptr(unsafe.Pointer(utf16Ptr(windowClass))), 0)
		if hwnd != 0 {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if hwnd == 0 {
		return
	}

	if roundWithDWM(hwnd) {
		return
	}
	clipWithRegion(hwnd)
}

// roundWithDWM asks the compositor to round the corners. It reports whether
// the running Windows knows the attribute; Windows 10 answers with a failure.
func roundWithDWM(hwnd uintptr) bool {
	want := int32(dwmwcpRound)
	ret, _, _ := procDwmSetAttribute.Call(
		hwnd,
		dwmwaCornerPreference,
		uintptr(unsafe.Pointer(&want)),
		unsafe.Sizeof(want),
	)
	return ret == 0
}

// clipWithRegion cuts the window to a rounded rectangle. GDI regions are
// pixel masks, so the corners come out stepped rather than anti-aliased.
func clipWithRegion(hwnd uintptr) {
	var r windowRect
	if ret, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); ret == 0 {
		return
	}
	width := r.right - r.left
	height := r.bottom - r.top
	if width <= 0 || height <= 0 {
		return
	}

	radius := int32(cornerRadiusDip)
	if dpi, _, _ := procGetDpiForWindow.Call(hwnd); dpi > 96 {
		radius = radius * int32(dpi) / 96
	}

	// The +1 keeps the right and bottom edges from being clipped away, which is
	// the usual off-by-one of CreateRoundRectRgn.
	rgn, _, _ := procCreateRoundRectRgn.Call(
		0, 0, uintptr(width+1), uintptr(height+1),
		uintptr(radius*2), uintptr(radius*2),
	)
	if rgn == 0 {
		return
	}
	// SetWindowRgn takes ownership of the region, so it must not be deleted.
	procSetWindowRgn.Call(hwnd, rgn, 1)
}
