//go:build windows

package platform

import (
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/riftbane/veduta/gfx"
)

// The Win32 window: one window class per window, one window procedure for all of them
// (syscall.NewCallback slots are limited and never freed), a private device context, and
// frames drawn with StretchDIBits from a top-down 32-bit DIB header that points at the
// gfx.Image pixels directly. Messages are pumped only by Poll, on the thread that created
// the window. Resizing or moving the window by its frame runs a modal loop inside
// DispatchMessageW, so the game pauses while the frame is dragged (WM_PAINT keeps the last
// frame on screen meanwhile).
//
// Calls made per message or per frame use syscall.SyscallN rather than LazyProc.Call,
// which moves its arguments to the heap (one allocation per call); see user32OS for the
// rule that makes this safe.

const (
	// maxPollMessages bounds the messages one Poll dispatches, so a window that keeps
	// posting to itself cannot make Poll block; the rest wait for the next Poll.
	maxPollMessages = 4096
	// maxSide bounds the requested client size (window sizes travel in 16-bit fields).
	maxSide = 0x7FFF
)

var (
	win32Once       sync.Once
	win32Err        error
	wndProcCallback uintptr // the window procedure of every Veduta window class

	classSeq atomic.Uint32 // makes window class names unique within the process

	createMu    sync.Mutex              // one CreateWindowExW at a time: creating is process-wide
	regMu       sync.Mutex              // guards liveWindows, creating and procPanic
	liveWindows = map[uintptr]*window{} // by HWND
	creating    *window                 // inside CreateWindowExW, handle not known yet
	procPanic   error                   // first panic recovered in the window procedure
)

// initWin32 resolves the Win32 functions, makes the process DPI aware and creates the
// window procedure callback, once per process.
func initWin32() error {
	win32Once.Do(func() {
		if win32Err = loadWin32(); win32Err != nil {
			return
		}
		setDPIAware()
		wndProcCallback = syscall.NewCallback(wndProc)
	})
	return win32Err
}

// wndProc is the window procedure of every Veduta window class. Windows calls it on the
// thread that owns the window, from DispatchMessageW or from a Win32 call that sends a
// message synchronously; it routes the message to its window.
func wndProc(hwnd, m, wp, lp uintptr) (result uintptr) {
	// The message is a UINT: on 64-bit Windows the upper half of its register is undefined.
	id := uint32(m)
	defer func() {
		if r := recover(); r != nil {
			// A panic must not unwind through Windows' frames; Poll reports it.
			regMu.Lock()
			if procPanic == nil {
				procPanic = fmt.Errorf("platform: window procedure panicked on message %#x: %v\n%s", id, r, debug.Stack())
			}
			regMu.Unlock()
			result = 0
		}
	}()
	w := lookupWindow(hwnd)
	if w == nil {
		r, _, _ := syscall.SyscallN(procDefWindowProcW.Addr(), hwnd, uintptr(id), wp, lp)
		return r
	}
	result = w.handle(id, wp, lp)
	if id == wmNCDestroy { // the last message a window receives
		forgetWindow(hwnd, w)
	}
	return result
}

// lookupWindow returns the window hwnd belongs to, or nil.
func lookupWindow(hwnd uintptr) *window {
	regMu.Lock()
	defer regMu.Unlock()
	if w := liveWindows[hwnd]; w != nil {
		return w
	}
	if creating != nil && creating.hwnd == 0 {
		// The first messages of a new window (WM_GETMINMAXINFO, WM_NCCREATE, ...) arrive
		// before CreateWindowExW returns its handle. Each window has its own class and
		// creation is serialized, so an unknown handle reaching this procedure then is
		// the window being created.
		creating.hwnd = hwnd
		liveWindows[hwnd] = creating
		return creating
	}
	return nil
}

// forgetWindow removes w from the registry; later messages for hwnd get default handling.
func forgetWindow(hwnd uintptr, w *window) {
	regMu.Lock()
	if liveWindows[hwnd] == w {
		delete(liveWindows, hwnd)
	}
	regMu.Unlock()
}

// takeProcPanic returns and clears the panic recovered in the window procedure.
func takeProcPanic() error {
	regMu.Lock()
	defer regMu.Unlock()
	err := procPanic
	procPanic = nil
	return err
}

// open creates the window on the calling OS thread, which must be the one that later
// calls Poll, Present and Close.
func open(o Options) (Window, error) {
	if err := initWin32(); err != nil {
		return nil, err
	}
	cw, ch := min(o.Width, maxSide), min(o.Height, maxSide)
	title, err := syscall.UTF16FromString(strings.ReplaceAll(o.Title, "\x00", ""))
	if err != nil {
		return nil, fmt.Errorf("platform: window title: %w", err)
	}

	w := &window{os: user32OS{}, tid: currentThreadID()}
	w.instance, _, _ = procGetModuleHandleW.Call(0)
	name := fmt.Sprintf("Veduta.Window.%d.%d", os.Getpid(), classSeq.Add(1))
	if w.class, err = syscall.UTF16FromString(name); err != nil {
		return nil, fmt.Errorf("platform: window class %s: %w", name, err)
	}
	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
	wc := wndClassEx{
		style:     csOwnDC | csHRedraw | csVRedraw,
		wndProc:   wndProcCallback,
		instance:  w.instance,
		cursor:    cursor,
		className: &w.class[0],
	}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	if atom, _, e := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); uint16(atom) == 0 {
		return nil, fmt.Errorf("platform: RegisterClassExW %s: %w", name, e)
	}
	fail := func(err error) (Window, error) {
		w.Close()
		return nil, err
	}

	// Outer size for the requested client size.
	r := rect{right: int32(cw), bottom: int32(ch)}
	procAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&r)), wsOverlappedWindow, 0, 0)

	createMu.Lock()
	regMu.Lock()
	creating = w
	regMu.Unlock()
	hwnd, _, e := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(&w.class[0])),
		uintptr(unsafe.Pointer(&title[0])),
		wsOverlappedWindow,
		cwUseDefault, cwUseDefault, // y must be CW_USEDEFAULT too, or it is taken as nCmdShow
		uintptr(r.right-r.left), uintptr(r.bottom-r.top),
		0, 0, w.instance, 0)
	regMu.Lock()
	creating = nil
	if hwnd != 0 {
		w.hwnd = hwnd
		liveWindows[hwnd] = w
	}
	regMu.Unlock()
	createMu.Unlock()
	if hwnd == 0 {
		if w.hwnd != 0 { // adopted, then destroyed during creation
			forgetWindow(w.hwnd, w)
		}
		w.hwnd, w.destroyed = 0, true
		return fail(fmt.Errorf("platform: CreateWindowExW: %w", e))
	}

	// AdjustWindowRectEx ignores per-monitor DPI and some frame styles: measure the
	// client area and correct the outer size by the difference.
	if gw, gh, err := clientRect(hwnd); err == nil && (gw != int32(cw) || gh != int32(ch)) {
		var wr rect
		if ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&wr))); uint32(ok) != 0 {
			nw := wr.right - wr.left + int32(cw) - gw
			nh := wr.bottom - wr.top + int32(ch) - gh
			if nw > 0 && nh > 0 {
				procSetWindowPos.Call(hwnd, 0, 0, 0, uintptr(nw), uintptr(nh), swpNoMove|swpNoZOrder|swpNoActivate)
			}
		}
	}

	dc, _, e := procGetDC.Call(hwnd)
	if dc == 0 {
		return fail(fmt.Errorf("platform: GetDC: %w", e))
	}
	w.dc = dc
	w.brush, _, _ = procGetStockObject.Call(blackBrush)
	w.bmi.header = bitmapInfoHeader{
		size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		planes:      1,
		bitCount:    32,
		compression: biRGB,
	}

	procShowWindow.Call(hwnd, swShowDefault)
	procUpdateWindow.Call(hwnd)
	gw, gh, err := clientRect(hwnd)
	if err != nil {
		return fail(fmt.Errorf("platform: %w", err))
	}
	w.w, w.h = int(gw), int(gh)
	// Creation-time messages (the first WM_SIZE, activation) are not input; Size has the
	// initial size.
	w.queue = w.queue[:0]
	return w, nil
}

// checkThread fails when the caller is not on the thread that owns the window: Windows
// delivers a window's messages only to that thread, so input would be lost silently.
func (w *window) checkThread(op string) error {
	if t := currentThreadID(); t != w.tid {
		return fmt.Errorf("platform: %s on OS thread %d, but the window belongs to thread %d; "+
			"use the window from the main goroutine (package platform locks it to the main thread)", op, t, w.tid)
	}
	return nil
}

// Poll dispatches every pending message without blocking and returns the events they
// produced. The returned slice is reused: it is valid until the next call to Poll.
func (w *window) Poll() ([]Event, error) {
	if w.closed {
		return nil, errors.New("platform: Poll on a closed window")
	}
	if err := w.checkThread("Poll"); err != nil {
		return nil, err
	}
	for i := 0; i < maxPollMessages; i++ {
		r, _, _ := syscall.SyscallN(procPeekMessageW.Addr(), uintptr(unsafe.Pointer(&w.pumpMsg)), 0, 0, 0, pmRemove)
		if uint32(r) == 0 {
			break
		}
		if w.pumpMsg.message == wmQuit {
			// PostQuitMessage somewhere in the process: the application is asked to exit.
			w.push(Event{Kind: Close})
			continue
		}
		// TranslateMessage posts WM_CHAR for WM_KEYDOWN; this loop dispatches it too.
		syscall.SyscallN(procTranslateMessage.Addr(), uintptr(unsafe.Pointer(&w.pumpMsg)))
		syscall.SyscallN(procDispatchMessageW.Addr(), uintptr(unsafe.Pointer(&w.pumpMsg)))
	}
	if err := takeProcPanic(); err != nil {
		return nil, err
	}
	if !w.destroyed {
		w.releaseStuckKeys()
	}
	if w.locked && w.lockMoved && !w.destroyed {
		w.recenterPointer()
	}
	return w.takeEvents(), nil
}

// Present draws img at the top-left corner of the client area, clipped, and keeps it for
// repainting. The image is referenced, not copied: WM_PAINT (handled inside Poll) redraws
// whatever it holds then, which in the player loop is the last presented frame.
func (w *window) Present(img *gfx.Image) error {
	if w.closed || w.destroyed {
		return errors.New("platform: Present on a closed window")
	}
	if img == nil {
		return errors.New("platform: Present: nil image")
	}
	if !validImage(img) {
		return fmt.Errorf("platform: Present: invalid image %dx%d with %d pixels", img.W, img.H, len(img.Pix))
	}
	if err := w.checkThread("Present"); err != nil {
		return err
	}
	w.last = img
	if w.w > 0 && w.h > 0 { // not minimized
		w.blit(w.dc, img)
	}
	return nil
}

// validImage reports whether img can be handed to StretchDIBits as a W×H DIB.
func validImage(img *gfx.Image) bool {
	return img.W > 0 && img.H > 0 && img.W <= 1<<16 && img.H <= 1<<16 &&
		int64(len(img.Pix)) >= int64(img.W)*int64(img.H)
}

// blit draws img (nil: nothing) 1:1 at the top-left corner of hdc and clears the rest of
// the client area to black. StretchDIBits' result is not checked: it returns 0 when no
// scan line was drawn, which also happens for a window being hidden, and a frame that
// did not reach the screen is not a reason to stop the game.
func (w *window) blit(hdc uintptr, img *gfx.Image) {
	cw, ch := int32(w.w), int32(w.h)
	var iw, ih int32
	if img != nil && validImage(img) { // revalidated: WM_PAINT may come long after Present
		iw, ih = int32(img.W), int32(img.H)
		w.bmi.header.width = iw
		w.bmi.header.height = -ih // top-down: gfx.Image stores the top row first
		// 0xAARRGGBB little-endian is B, G, R, A in memory: the byte order of a 32-bit
		// BI_RGB DIB, which ignores the fourth byte. The whole image is the source
		// rectangle; the device context clips what falls outside the client area.
		syscall.SyscallN(procStretchDIBits.Addr(), hdc,
			0, 0, uintptr(iw), uintptr(ih),
			0, 0, uintptr(iw), uintptr(ih),
			uintptr(unsafe.Pointer(&img.Pix[0])), uintptr(unsafe.Pointer(&w.bmi)),
			dibRGBColors, srcCopy)
	}
	n := 0
	if iw < cw {
		w.fill[n] = rect{left: iw, right: cw, bottom: ch}
		n++
	}
	if ih < ch {
		w.fill[n] = rect{top: ih, right: min(iw, cw), bottom: ch}
		n++
	}
	for i := 0; i < n; i++ {
		syscall.SyscallN(procFillRect.Addr(), hdc, uintptr(unsafe.Pointer(&w.fill[i])), w.brush)
	}
}

// Size returns the client area size; 0×0 while minimized and after Close.
func (w *window) Size() (int, int) {
	if w.closed || w.destroyed {
		return 0, 0
	}
	return w.w, w.h
}

// Close destroys the window and unregisters its class. It is safe to call more than
// once; it must be called from the thread that created the window.
// SetPointerLock hides the cursor and keeps putting it back in the middle of the client
// area, so looking around never runs out of screen. See Window.
func (w *window) SetPointerLock(on bool) error {
	if w.closed || w.destroyed {
		return errors.New("platform: SetPointerLock on a closed window")
	}
	if on == w.locked {
		return nil
	}
	w.locked = on
	w.virtX, w.virtY = 0, 0
	w.os.showCursor(!on) // ShowCursor counts: once per change of state
	if on {
		w.recenterPointer()
	}
	w.lockMoved = false
	return nil
}

// recenterPointer moves the cursor to the middle of the client area; the next movement is
// measured from there.
func (w *window) recenterPointer() {
	if w.w <= 0 || w.h <= 0 {
		return
	}
	cx, cy := int32(w.w/2), int32(w.h/2)
	sx, sy := w.os.clientToScreen(w.hwnd, cx, cy)
	w.os.setCursorPos(sx, sy)
	w.mouseX, w.mouseY, w.mouseSeen = cx, cy, true
	w.lockMoved = false
}

func (w *window) Close() error {
	if w.closed {
		return nil
	}
	if err := w.checkThread("Close"); err != nil {
		return err
	}
	w.closed = true
	if w.hwnd != 0 {
		// Messages sent during DestroyWindow get default handling.
		forgetWindow(w.hwnd, w)
	}
	var errs []error
	if w.hwnd != 0 && !w.destroyed {
		if w.dc != 0 {
			procReleaseDC.Call(w.hwnd, w.dc) // a no-op for a CS_OWNDC context, kept for symmetry
		}
		if ok, _, e := procDestroyWindow.Call(w.hwnd); uint32(ok) == 0 {
			errs = append(errs, fmt.Errorf("platform: DestroyWindow: %w", e))
		}
	}
	if len(w.class) > 0 {
		if ok, _, e := procUnregisterClassW.Call(uintptr(unsafe.Pointer(&w.class[0])), w.instance); uint32(ok) == 0 {
			errs = append(errs, fmt.Errorf("platform: UnregisterClassW: %w", e))
		}
	}
	w.dc, w.last = 0, nil
	w.queue, w.out = nil, nil
	return errors.Join(errs...)
}
