//go:build windows

package platform

import (
	"fmt"
	"syscall"
	"unsafe"
)

// Win32 entry points, resolved lazily from the system DLLs. user32, gdi32 and kernel32
// are KnownDLLs: Windows always loads them from System32, never from the search path.
var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procAdjustWindowRectEx = user32.NewProc("AdjustWindowRectEx")
	procBeginPaint         = user32.NewProc("BeginPaint")
	procCreateWindowExW    = user32.NewProc("CreateWindowExW")
	procDefWindowProcW     = user32.NewProc("DefWindowProcW")
	procDestroyWindow      = user32.NewProc("DestroyWindow")
	procDispatchMessageW   = user32.NewProc("DispatchMessageW")
	procEndPaint           = user32.NewProc("EndPaint")
	procFillRect           = user32.NewProc("FillRect")
	procGetClientRect      = user32.NewProc("GetClientRect")
	procGetDC              = user32.NewProc("GetDC")
	procGetKeyState        = user32.NewProc("GetKeyState")
	procGetMessageTime     = user32.NewProc("GetMessageTime")
	procGetWindowRect      = user32.NewProc("GetWindowRect")
	procLoadCursorW        = user32.NewProc("LoadCursorW")
	procMapVirtualKeyW     = user32.NewProc("MapVirtualKeyW")
	procPeekMessageW       = user32.NewProc("PeekMessageW")
	procRegisterClassExW   = user32.NewProc("RegisterClassExW")
	procReleaseCapture     = user32.NewProc("ReleaseCapture")
	procReleaseDC          = user32.NewProc("ReleaseDC")
	procSetCapture         = user32.NewProc("SetCapture")
	procSetWindowPos       = user32.NewProc("SetWindowPos")
	procShowWindow         = user32.NewProc("ShowWindow")
	procTranslateMessage   = user32.NewProc("TranslateMessage")
	procUnregisterClassW   = user32.NewProc("UnregisterClassW")
	procUpdateWindow       = user32.NewProc("UpdateWindow")

	// Optional: missing on older Windows versions, checked with Find before use.
	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext") // Windows 10 1703
	procSetProcessDPIAware            = user32.NewProc("SetProcessDPIAware")            // Vista

	procGetStockObject = gdi32.NewProc("GetStockObject")
	procStretchDIBits  = gdi32.NewProc("StretchDIBits")

	procGetCurrentThreadId = kernel32.NewProc("GetCurrentThreadId")
	procGetModuleHandleW   = kernel32.NewProc("GetModuleHandleW")
)

// requiredProcs are resolved before the first window so a missing export fails Open with
// an error instead of panicking inside LazyProc.Call.
var requiredProcs = []*syscall.LazyProc{
	procAdjustWindowRectEx, procBeginPaint, procCreateWindowExW, procDefWindowProcW,
	procDestroyWindow, procDispatchMessageW, procEndPaint, procFillRect, procGetClientRect,
	procGetDC, procGetKeyState, procGetMessageTime, procGetWindowRect, procLoadCursorW,
	procMapVirtualKeyW, procPeekMessageW, procRegisterClassExW, procReleaseCapture,
	procReleaseDC, procSetCapture, procSetWindowPos, procShowWindow, procTranslateMessage,
	procUnregisterClassW, procUpdateWindow,
	procGetStockObject, procStretchDIBits,
	procGetCurrentThreadId, procGetModuleHandleW,
}

// loadWin32 resolves requiredProcs.
func loadWin32() error {
	for _, p := range requiredProcs {
		if err := p.Find(); err != nil {
			return fmt.Errorf("platform: resolve Win32 function %s: %w", p.Name, err)
		}
	}
	return nil
}

// setDPIAware makes the process per-monitor DPI aware so the client area is measured in
// physical pixels and Windows does not bitmap-stretch the frames on scaled displays.
// Failure (older Windows, or awareness already set by the executable's manifest) leaves
// the process as it was.
func setDPIAware() {
	if procSetProcessDpiAwarenessContext.Find() == nil {
		if r, _, _ := procSetProcessDpiAwarenessContext.Call(dpiAwarenessContextPerMonitorAwareV2); uint32(r) != 0 {
			return
		}
	}
	if procSetProcessDPIAware.Find() == nil {
		procSetProcessDPIAware.Call()
	}
}

// currentThreadID returns the calling OS thread's id.
func currentThreadID() uint32 {
	r, _, _ := syscall.SyscallN(procGetCurrentThreadId.Addr())
	return uint32(r)
}

// clientRect returns the client area size of hwnd.
func clientRect(hwnd uintptr) (w, h int32, err error) {
	var r rect
	if ok, _, e := procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); uint32(ok) == 0 {
		return 0, 0, fmt.Errorf("GetClientRect: %w", e)
	}
	return r.right - r.left, r.bottom - r.top, nil
}

// user32OS is the winOS of a real window.
//
// Its calls run once per message, so they use syscall.SyscallN, whose variadic arguments
// stay on the stack, instead of LazyProc.Call, which moves them (and anything a pointer
// argument points at) to the heap: one allocation per call. SyscallN keeps pointer
// arguments alive but does not pin them, and a call that sends messages (PeekMessageW,
// BeginPaint) re-enters Go through the window procedure, which can grow and move the
// goroutine stack. So every pointer passed through SyscallN points into the
// heap-allocated window or image, never at a local variable; calls that pass locals (in
// open and clientRect) use LazyProc.Call. The procs are resolved by loadWin32 before any
// window exists, so Addr cannot panic. Win32 functions returning a 32-bit value (BOOL,
// UINT, LONG, SHORT) leave the upper half of the return register undefined on 64-bit
// Windows, so results are truncated before use.
type user32OS struct{}

func (user32OS) defWindowProc(hwnd uintptr, m uint32, wp, lp uintptr) uintptr {
	r, _, _ := syscall.SyscallN(procDefWindowProcW.Addr(), hwnd, uintptr(m), wp, lp)
	return r
}

func (user32OS) setCapture(hwnd uintptr) { syscall.SyscallN(procSetCapture.Addr(), hwnd) }

func (user32OS) releaseCapture() { syscall.SyscallN(procReleaseCapture.Addr()) }

// peekNext requires m to point into the heap (it is &window.peekMsg).
func (user32OS) peekNext(m *msg) bool {
	r, _, _ := syscall.SyscallN(procPeekMessageW.Addr(), uintptr(unsafe.Pointer(m)), 0, 0, 0, pmNoRemove)
	return uint32(r) != 0
}

func (user32OS) messageTime() uint32 {
	r, _, _ := syscall.SyscallN(procGetMessageTime.Addr())
	return uint32(r)
}

func (user32OS) keyPressed(vk uint32) bool {
	r, _, _ := syscall.SyscallN(procGetKeyState.Addr(), uintptr(vk))
	return uint16(r)&0x8000 != 0
}

func (user32OS) scanFromVK(vk uint32) uint32 {
	r, _, _ := syscall.SyscallN(procMapVirtualKeyW.Addr(), uintptr(vk), mapvkVKToVSCEx)
	return uint32(r)
}

func (user32OS) paint(w *window) {
	hdc, _, _ := syscall.SyscallN(procBeginPaint.Addr(), w.hwnd, uintptr(unsafe.Pointer(&w.ps)))
	if hdc == 0 {
		return
	}
	w.blit(hdc, w.last)
	syscall.SyscallN(procEndPaint.Addr(), w.hwnd, uintptr(unsafe.Pointer(&w.ps)))
}
