//go:build windows

package platform

// Win32 constants and structure layouts used by the window. The structures mirror the C
// declarations of winuser.h and wingdi.h field by field; Go's natural alignment matches
// the C ABI on windows/386, windows/amd64 and windows/arm64 (TestWin32StructLayout).

// Window messages.
const (
	wmDestroy        = 0x0002
	wmSize           = 0x0005
	wmKillFocus      = 0x0008
	wmPaint          = 0x000F
	wmClose          = 0x0010
	wmQuit           = 0x0012
	wmEraseBkgnd     = 0x0014
	wmNCDestroy      = 0x0082
	wmKeyDown        = 0x0100
	wmKeyUp          = 0x0101
	wmChar           = 0x0102
	wmSysKeyDown     = 0x0104
	wmSysKeyUp       = 0x0105
	wmSysChar        = 0x0106
	wmUniChar        = 0x0109
	wmSysCommand     = 0x0112
	wmMouseMove      = 0x0200
	wmLButtonDown    = 0x0201
	wmLButtonUp      = 0x0202
	wmRButtonDown    = 0x0204
	wmRButtonUp      = 0x0205
	wmMButtonDown    = 0x0207
	wmMButtonUp      = 0x0208
	wmCaptureChanged = 0x0215
)

// Virtual keys the handler looks at.
const (
	vkControl = 0x11
	vkMenu    = 0x12 // Alt
	vkLWin    = 0x5B
	vkRWin    = 0x5C
	vkLShift  = 0xA0
	vkRShift  = 0xA1
	vkPacket  = 0xE7 // a character injected by SendInput(KEYEVENTF_UNICODE)
	vkNone    = 0xFF // no key (fake keys some layouts and drivers send)
)

// Other Win32 constants.
const (
	csVRedraw = 0x0001
	csHRedraw = 0x0002
	csOwnDC   = 0x0020

	wsOverlappedWindow = 0x00CF0000
	cwUseDefault       = 0x80000000

	swShowDefault = 10

	swpNoMove     = 0x0002
	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010

	idcArrow = 32512 // MAKEINTRESOURCE(32512)

	pmNoRemove = 0x0000
	pmRemove   = 0x0001

	scKeyMenu     = 0xF100 // WM_SYSCOMMAND: menu activated from the keyboard (Alt, F10)
	unicodeNoChar = 0xFFFF // WM_UNICHAR probe

	mapvkVKToVSCEx = 4

	biRGB        = 0
	dibRGBColors = 0
	srcCopy      = 0x00CC0020
	blackBrush   = 4

	// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2, ((DPI_AWARENESS_CONTEXT)-4).
	dpiAwarenessContextPerMonitorAwareV2 = ^uintptr(3)
)

// point is POINT.
type point struct {
	x, y int32
}

// rect is RECT.
type rect struct {
	left, top, right, bottom int32
}

// msg is MSG. lPrivate is declared only for _MAC in winuser.h; keeping it makes the Go
// struct at least as large as the C one on every architecture (Windows writes into it).
type msg struct {
	hwnd     uintptr
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       point
	lPrivate uint32
}

// wndClassEx is WNDCLASSEXW.
type wndClassEx struct {
	cbSize     uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   uintptr
	icon       uintptr
	cursor     uintptr
	background uintptr
	menuName   *uint16
	className  *uint16
	iconSm     uintptr
}

// paintStruct is PAINTSTRUCT.
type paintStruct struct {
	hdc       uintptr
	erase     int32
	paint     rect
	restore   int32
	incUpdate int32
	reserved  [32]byte
}

// bitmapInfoHeader is BITMAPINFOHEADER.
type bitmapInfoHeader struct {
	size          uint32
	width         int32
	height        int32 // negative: top-down DIB, first row is the top row
	planes        uint16
	bitCount      uint16
	compression   uint32
	sizeImage     uint32
	xPelsPerMeter int32
	yPelsPerMeter int32
	clrUsed       uint32
	clrImportant  uint32
}

// bitmapInfo is BITMAPINFO with its one-entry color table (unused for 32-bit BI_RGB).
type bitmapInfo struct {
	header bitmapInfoHeader
	colors [1]uint32
}
