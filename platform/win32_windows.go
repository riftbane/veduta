//go:build windows

package platform

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

// A window belongs to the OS thread that creates it, and Windows delivers its messages only
// to that thread: the player runs on the main goroutine, which this keeps on the main
// thread.
func init() { runtime.LockOSThread() }

// Win32 entry points, resolved lazily from the system DLLs. user32, gdi32 and kernel32 are
// KnownDLLs: Windows always loads them from System32, never from the search path.
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
	procLoadCursorW        = user32.NewProc("LoadCursorW")
	procMapVirtualKeyW     = user32.NewProc("MapVirtualKeyW")
	procPeekMessageW       = user32.NewProc("PeekMessageW")
	procRegisterClassExW   = user32.NewProc("RegisterClassExW")
	procReleaseDC          = user32.NewProc("ReleaseDC")
	procShowWindow         = user32.NewProc("ShowWindow")
	procUnregisterClassW   = user32.NewProc("UnregisterClassW")
	procUpdateWindow       = user32.NewProc("UpdateWindow")

	// Optional: missing on older Windows versions, checked with Find before use.
	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext") // Windows 10 1703
	procSetProcessDPIAware            = user32.NewProc("SetProcessDPIAware")            // Vista

	procGetStockObject = gdi32.NewProc("GetStockObject")
	procStretchDIBits  = gdi32.NewProc("StretchDIBits")

	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
)

// requiredProcs are resolved before the first window so a missing export fails Open with an
// error instead of panicking inside a call.
var requiredProcs = []*syscall.LazyProc{
	procAdjustWindowRectEx, procBeginPaint, procCreateWindowExW, procDefWindowProcW,
	procDestroyWindow, procDispatchMessageW, procEndPaint, procFillRect, procGetClientRect,
	procGetDC, procLoadCursorW, procMapVirtualKeyW, procPeekMessageW, procRegisterClassExW,
	procReleaseDC, procShowWindow, procUnregisterClassW, procUpdateWindow,
	procGetStockObject, procStretchDIBits, procGetModuleHandleW,
}

func loadWin32() error {
	for _, p := range requiredProcs {
		if err := p.Find(); err != nil {
			return fmt.Errorf("platform: resolve Win32 function %s: %w", p.Name, err)
		}
	}
	return nil
}

// setDPIAware makes the process per-monitor DPI aware, so the client area is measured in
// physical pixels and Windows does not blur the frames on scaled displays.
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

// clientRect returns the client area size of hwnd.
func clientRect(hwnd uintptr) (w, h int32, err error) {
	var r rect
	if ok, _, e := procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); uint32(ok) == 0 {
		return 0, 0, fmt.Errorf("GetClientRect: %w", e)
	}
	return r.right - r.left, r.bottom - r.top, nil
}
