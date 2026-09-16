//go:build windows

package platform

import (
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/riftbane/veduta/gfx"
)

// The simulator: on Windows the player draws in a window that shows the console's panel. The
// game renders its 320×240 frame as on the console; the window shows it at a whole scale
// (VEDUTA_SCALE, 3 by default, then the largest that fits when the window is resized),
// centered on black, in the panel's 16-bit colors (VEDUTA_PANEL=0 shows the frame's own).
// The keyboard presses the console's buttons (sim.go), and closing the window or Ctrl+Q
// leaves the game as Home does.
//
// One window class and one window procedure; messages are pumped only by Poll, on the
// thread that created the window (the main thread, see init). Calls made per message or
// per frame use syscall.SyscallN rather than LazyProc.Call, which moves its arguments to
// the heap; every pointer they pass points into the heap-allocated window.

var (
	win32Once       sync.Once
	win32Err        error
	wndProcCallback uintptr

	regMu     sync.Mutex
	theWindow *window // the one simulator window of the process
	procPanic error   // first panic recovered in the window procedure
)

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

// wndProc routes a message to the window. Windows calls it from DispatchMessageW, or from a
// Win32 call that sends a message, on the window's thread.
func wndProc(hwnd, m, wp, lp uintptr) (result uintptr) {
	id := uint32(m) // a UINT: the upper half of the register is undefined on 64-bit Windows
	defer func() {
		if r := recover(); r != nil {
			regMu.Lock()
			if procPanic == nil {
				procPanic = fmt.Errorf("platform: window procedure panicked on message %#x: %v\n%s", id, r, debug.Stack())
			}
			regMu.Unlock()
			result = 0
		}
	}()
	regMu.Lock()
	w := theWindow
	regMu.Unlock()
	if w == nil || (w.hwnd != 0 && w.hwnd != hwnd) {
		r, _, _ := syscall.SyscallN(procDefWindowProcW.Addr(), hwnd, uintptr(id), wp, lp)
		return r
	}
	if w.hwnd == 0 {
		w.hwnd = hwnd // the messages sent inside CreateWindowExW come before its handle
	}
	return w.handle(id, wp, lp)
}

// window is the simulator window.
type window struct {
	hwnd     uintptr
	instance uintptr
	class    []uint16
	dc       uintptr
	brush    uintptr

	fw, fh int // the frame the game renders
	cw, ch int // the client area, from WM_SIZE (0×0 while minimized)
	panel  bool

	keys  keyboard
	queue []Event
	out   []Event

	last  *gfx.Image // last presented frame, repainted on WM_PAINT
	shown []uint32   // the frame in the panel's colors
	bmi   bitmapInfo
	ps    paintStruct
	pump  msg
	fill  [4]rect

	closed    bool
	destroyed bool
}

// open creates the simulator window on the calling OS thread.
func open(o Options) (Window, error) {
	if err := initWin32(); err != nil {
		return nil, err
	}
	scale := 3
	if s := os.Getenv(renderScaleEnv); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 8 {
			return nil, fmt.Errorf("platform: %s %q (want 1 to 8)", renderScaleEnv, s)
		}
		scale = n
	}
	regMu.Lock()
	if theWindow != nil {
		regMu.Unlock()
		return nil, errors.New("platform: the simulator window is already open")
	}
	w := &window{fw: o.Width, fh: o.Height, panel: os.Getenv(panelEnv) != "0"}
	theWindow = w
	regMu.Unlock()
	fail := func(err error) (Window, error) {
		w.Close()
		return nil, err
	}

	title, err := syscall.UTF16FromString(strings.ReplaceAll(o.Title+" — Veduta simulator", "\x00", ""))
	if err != nil {
		return fail(fmt.Errorf("platform: window title: %w", err))
	}
	w.instance, _, _ = procGetModuleHandleW.Call(0)
	w.class, _ = syscall.UTF16FromString(fmt.Sprintf("Veduta.Simulator.%d", os.Getpid()))
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
		w.class = nil
		return fail(fmt.Errorf("platform: RegisterClassExW: %w", e))
	}

	r := rect{right: int32(w.fw * scale), bottom: int32(w.fh * scale)}
	procAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&r)), wsOverlappedWindow, 0, 0)
	hwnd, _, e := procCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(&w.class[0])), uintptr(unsafe.Pointer(&title[0])),
		wsOverlappedWindow,
		cwUseDefault, cwUseDefault, // y must be CW_USEDEFAULT too, or it is taken as nCmdShow
		uintptr(r.right-r.left), uintptr(r.bottom-r.top),
		0, 0, w.instance, 0)
	if hwnd == 0 {
		w.hwnd, w.destroyed = 0, true
		return fail(fmt.Errorf("platform: CreateWindowExW: %w", e))
	}
	w.hwnd = hwnd
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
	w.shown = make([]uint32, w.fw*w.fh)
	procShowWindow.Call(hwnd, swShowDefault)
	procUpdateWindow.Call(hwnd)
	cw, ch, err := clientRect(hwnd)
	if err != nil {
		return fail(fmt.Errorf("platform: %w", err))
	}
	w.cw, w.ch = int(cw), int(ch)
	w.queue = w.queue[:0]
	return w, nil
}

// handle turns one message into events and returns its result.
func (w *window) handle(m uint32, wp, lp uintptr) uintptr {
	switch m {
	case wmKeyDown, wmKeyUp, wmSysKeyDown, wmSysKeyUp:
		w.key(m, wp, lp)
		if m == wmSysKeyDown || m == wmSysKeyUp {
			break // DefWindowProc still sees them, so Alt+F4 closes the window
		}
		return 0
	case wmSysCommand:
		if wp&0xFFF0 == scKeyMenu {
			return 0 // the keyboard menu loop would freeze the game inside DispatchMessage
		}
	case wmSysChar:
		return 0 // Alt+letter is a menu mnemonic; DefWindowProc would beep
	case wmSize:
		w.cw, w.ch = int(uint16(lp)), int(uint16(lp>>16))
		return 0
	case wmClose:
		w.queue = append(w.keys.releaseAll(w.queue), Event{Kind: Close}) // destroyed only by Close
		return 0
	case wmKillFocus:
		w.queue = append(w.keys.releaseAll(w.queue), Event{Kind: FocusLost})
		return 0
	case wmPaint:
		hdc, _, _ := syscall.SyscallN(procBeginPaint.Addr(), w.hwnd, uintptr(unsafe.Pointer(&w.ps)))
		if hdc != 0 {
			w.blit(hdc)
			syscall.SyscallN(procEndPaint.Addr(), w.hwnd, uintptr(unsafe.Pointer(&w.ps)))
		}
		return 0
	case wmEraseBkgnd:
		return 1 // WM_PAINT covers the whole client area
	case wmDestroy:
		if !w.closed && !w.destroyed {
			w.destroyed = true
			w.queue = append(w.queue, Event{Kind: Close})
		}
		return 0
	}
	r, _, _ := syscall.SyscallN(procDefWindowProcW.Addr(), w.hwnd, uintptr(m), wp, lp)
	return r
}

// key handles a key message: bits 16–23 of lParam are the scan code, bit 24 the extended
// flag, bit 31 the release.
func (w *window) key(m uint32, wp, lp uintptr) {
	vk := uint32(wp)
	if vk == vkPacket || vk == vkNone {
		return // not a physical key
	}
	scan := int(lp>>16) & 0xFF
	if scan == 0 { // some synthesized messages carry no scan code
		s, _, _ := syscall.SyscallN(procMapVirtualKeyW.Addr(), uintptr(vk), mapvkVKToVSCEx)
		scan = int(s) & 0xFF
		if s>>8 == 0xE0 || s>>8 == 0xE1 {
			scan |= extendedScan
		}
	} else if lp>>24&1 != 0 {
		scan |= extendedScan
	}
	if scan == extendedScan|0x2A {
		return // the fake Shift keyboards send around the gray navigation keys
	}
	if scan == extendedScan|0x36 {
		scan = scanRShift // CJK IMEs set the extended flag on Right Shift
	}
	w.queue = w.keys.key(w.queue, scan, m == wmKeyDown || m == wmSysKeyDown)
}

// Poll dispatches every pending message without blocking and returns the events they
// produced; the slice is valid until the next call.
func (w *window) Poll() ([]Event, error) {
	if w.closed {
		return nil, errors.New("platform: Poll on a closed window")
	}
	for i := 0; i < 4096; i++ { // bounded, so a window that keeps posting to itself cannot block Poll
		r, _, _ := syscall.SyscallN(procPeekMessageW.Addr(), uintptr(unsafe.Pointer(&w.pump)), 0, 0, 0, pmRemove)
		if uint32(r) == 0 {
			break
		}
		if w.pump.message == wmQuit {
			w.queue = append(w.queue, Event{Kind: Close})
			continue
		}
		syscall.SyscallN(procDispatchMessageW.Addr(), uintptr(unsafe.Pointer(&w.pump)))
	}
	regMu.Lock()
	err := procPanic
	procPanic = nil
	regMu.Unlock()
	if err != nil {
		return nil, err
	}
	w.out, w.queue = w.queue, w.out[:0]
	if len(w.out) == 0 {
		return nil, nil
	}
	return w.out, nil
}

// Present shows a frame; it is referenced, not copied, and repainted on WM_PAINT.
func (w *window) Present(img *gfx.Image) error {
	if w.closed || w.destroyed {
		return errors.New("platform: Present on a closed window")
	}
	if img == nil || img.W != w.fw || img.H != w.fh || len(img.Pix) < w.fw*w.fh {
		return fmt.Errorf("platform: the simulator takes %dx%d frames", w.fw, w.fh)
	}
	w.last = img
	if w.cw > 0 && w.ch > 0 {
		w.blit(w.dc)
	}
	return nil
}

// blit draws the last frame scaled into hdc and clears the borders to black. StretchDIBits'
// result is not checked: a frame that did not reach a hidden window is no reason to stop.
func (w *window) blit(hdc uintptr) {
	scale, x, y := fitScale(w.cw, w.ch, w.fw, w.fh)
	dw, dh := w.fw*scale, w.fh*scale
	if img := w.last; img != nil {
		pix := img.Pix
		if w.panel {
			panelColors(w.shown, img.Pix[:w.fw*w.fh])
			pix = w.shown
		}
		w.bmi.header.width = int32(w.fw)
		w.bmi.header.height = -int32(w.fh) // top-down: the first row is the top one
		// 0xAARRGGBB little-endian is B, G, R, A in memory, the order of a 32-bit BI_RGB DIB.
		syscall.SyscallN(procStretchDIBits.Addr(), hdc,
			uintptr(x), uintptr(y), uintptr(dw), uintptr(dh),
			0, 0, uintptr(w.fw), uintptr(w.fh),
			uintptr(unsafe.Pointer(&pix[0])), uintptr(unsafe.Pointer(&w.bmi)),
			dibRGBColors, srcCopy)
	}
	cw, ch := int32(w.cw), int32(w.ch)
	x0, y0, x1, y1 := int32(max(x, 0)), int32(max(y, 0)), int32(min(x+dw, w.cw)), int32(min(y+dh, w.ch))
	w.fill = [4]rect{
		{0, 0, cw, y0},   // above
		{0, y1, cw, ch},  // below
		{0, y0, x0, y1},  // left
		{x1, y0, cw, y1}, // right
	}
	for i := range w.fill {
		if f := &w.fill[i]; f.right > f.left && f.bottom > f.top {
			syscall.SyscallN(procFillRect.Addr(), hdc, uintptr(unsafe.Pointer(f)), w.brush)
		}
	}
}

// Size is the frame the game renders, whatever the window's size.
func (w *window) Size() (int, int) {
	if w.closed || w.destroyed {
		return 0, 0
	}
	return w.fw, w.fh
}

// Close destroys the window. It is safe to call more than once.
func (w *window) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	var errs []error
	if w.hwnd != 0 && !w.destroyed {
		if w.dc != 0 {
			procReleaseDC.Call(w.hwnd, w.dc)
		}
		if ok, _, e := procDestroyWindow.Call(w.hwnd); uint32(ok) == 0 {
			errs = append(errs, fmt.Errorf("platform: DestroyWindow: %w", e))
		}
	}
	regMu.Lock()
	if theWindow == w {
		theWindow = nil
	}
	regMu.Unlock()
	if len(w.class) > 0 {
		if ok, _, e := procUnregisterClassW.Call(uintptr(unsafe.Pointer(&w.class[0])), w.instance); uint32(ok) == 0 {
			errs = append(errs, fmt.Errorf("platform: UnregisterClassW: %w", e))
		}
	}
	w.dc, w.last = 0, nil
	return errors.Join(errs...)
}
