//go:build windows

package platform

import (
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/sim"
)

// winOS is the part of user32 the message handler calls back into. A real window uses
// user32OS; tests substitute a fake so message translation is testable without a desktop.
type winOS interface {
	// defWindowProc runs the default window procedure (DefWindowProcW).
	defWindowProc(hwnd uintptr, m uint32, wp, lp uintptr) uintptr
	// setCapture routes mouse input to hwnd while a button is held (SetCapture).
	setCapture(hwnd uintptr)
	// releaseCapture ends mouse capture (ReleaseCapture); the system then sends
	// WM_CAPTURECHANGED synchronously.
	releaseCapture()
	// setCursorPos moves the cursor to a position in screen pixels (SetCursorPos).
	setCursorPos(x, y int32)
	// showCursor shows or hides this application's cursor (ShowCursor).
	showCursor(show bool)
	// clientToScreen turns a position in the client area into a screen position
	// (ClientToScreen).
	clientToScreen(hwnd uintptr, x, y int32) (int32, int32)
	// peekNext copies the next queued message to m without removing it (PeekMessageW
	// with PM_NOREMOVE) and reports whether there was one.
	peekNext(m *msg) bool
	// messageTime is the timestamp of the message being handled (GetMessageTime).
	messageTime() uint32
	// keyPressed reports whether virtual key vk is down as of the last message retrieved
	// from the queue (GetKeyState).
	keyPressed(vk uint32) bool
	// scanFromVK maps a virtual key to its scan code, 0xE0xx for extended keys
	// (MapVirtualKeyW with MAPVK_VK_TO_VSC_EX).
	scanFromVK(vk uint32) uint32
	// paint answers WM_PAINT by drawing the last presented image.
	paint(w *window)
}

// window is the Win32 player window. It is touched only by the OS thread that created it:
// the window procedure runs on that thread, inside Poll.
type window struct {
	os       winOS
	hwnd     uintptr
	tid      uint32   // OS thread that created the window and owns its message queue
	instance uintptr  // module handle the window class is registered with
	class    []uint16 // NUL-terminated UTF-16 window class name
	dc       uintptr  // private device context (CS_OWNDC), valid while the window lives
	brush    uintptr  // stock black brush for the client area an image does not cover

	w, h int // client area size, from WM_SIZE

	queue []Event // events received since the last Poll
	out   []Event // the slice the last Poll returned; its array is reused by the next

	down      [len(scanCodes)]bool // keys reported down, by scanCodes index
	buttons   sim.ButtonSet        // mouse buttons reported down
	mouseX    int32
	mouseY    int32
	mouseSeen bool

	locked       bool    // pointer locked: the cursor is hidden and kept centered
	lockMoved    bool    // it moved since the last recentering
	virtX, virtY float32 // virtual cursor position reported while locked
	surrogate    utf16Decoder

	last    *gfx.Image // last presented image, redrawn on WM_PAINT (referenced, not copied)
	bmi     bitmapInfo // StretchDIBits header, reused every frame
	ps      paintStruct
	pumpMsg msg // PeekMessageW(PM_REMOVE) target in Poll
	peekMsg msg // PeekMessageW(PM_NOREMOVE) target for the AltGr check
	fill    [2]rect

	closed    bool // Close was called
	destroyed bool // the window is gone (WM_DESTROY seen before Close)
}

// push queues an event. Consecutive MouseMove events and consecutive Resize events
// collapse into the latest one: only the final position or size matters, and a window
// being dragged larger sends one WM_SIZE per pixel.
func (w *window) push(e Event) {
	if n := len(w.queue); n > 0 && (e.Kind == MouseMove || e.Kind == Resize) && w.queue[n-1].Kind == e.Kind {
		w.queue[n-1] = e
		return
	}
	w.queue = append(w.queue, e)
}

// takeEvents returns the queued events and starts a new queue in the array the previous
// call returned, so steady-state polling allocates nothing. The result is valid until the
// next call.
func (w *window) takeEvents() []Event {
	w.out, w.queue = w.queue, w.out[:0]
	if len(w.out) == 0 {
		return nil
	}
	return w.out
}

// handle is the window procedure of w: it turns one message into events and returns the
// message's result.
func (w *window) handle(m uint32, wp, lp uintptr) uintptr {
	switch m {
	case wmKeyDown, wmKeyUp:
		w.key(m, wp, lp)
		return 0
	case wmSysKeyDown, wmSysKeyUp:
		// Keys pressed with Alt held, and F10. DefWindowProc still sees them so Alt+F4
		// closes the window; the keyboard menu loop they would start (which would freeze
		// the game inside DispatchMessage) is refused in WM_SYSCOMMAND below.
		w.key(m, wp, lp)
		return w.os.defWindowProc(w.hwnd, m, wp, lp)
	case wmSysCommand:
		if wp&0xFFF0 == scKeyMenu {
			return 0
		}
	case wmChar:
		if r, ok := w.surrogate.feed(uint16(wp)); ok {
			w.text(r)
		}
		return 0
	case wmSysChar:
		// Alt+letter is a menu mnemonic, not typing; DefWindowProc would beep.
		return 0
	case wmUniChar:
		if wp == unicodeNoChar {
			return 1 // yes, this window accepts WM_UNICHAR
		}
		if wp <= unicode.MaxRune { // a UTF-32 code point; rune(wp) alone would truncate
			w.text(rune(wp))
		}
		return 0
	case wmMouseMove:
		x, y := mouseLParam(lp)
		w.move(x, y)
		return 0
	case wmLButtonDown, wmLButtonUp, wmRButtonDown, wmRButtonUp, wmMButtonDown, wmMButtonUp:
		b, down := mouseButton(m)
		x, y := mouseLParam(lp)
		w.button(b, down, x, y)
		return 0
	case wmCaptureChanged:
		if lp != w.hwnd {
			w.captureLost()
		}
		return 0
	case wmSize:
		cw, ch := sizeLParam(lp)
		w.resize(cw, ch)
		return 0
	case wmClose:
		// Report it; the window is destroyed only by Close.
		w.push(Event{Kind: Close})
		return 0
	case wmKillFocus:
		w.focusLost()
		return 0
	case wmPaint:
		w.os.paint(w)
		return 0
	case wmEraseBkgnd:
		return 1 // WM_PAINT covers the whole client area
	case wmDestroy:
		// Only reached when something other than Close destroyed the window (Close
		// unregisters it first): tell the game it is gone.
		if !w.closed && !w.destroyed {
			w.destroyed = true
			w.push(Event{Kind: Close})
		}
		return 0
	}
	return w.os.defWindowProc(w.hwnd, m, wp, lp)
}

// key handles WM_KEYDOWN, WM_KEYUP, WM_SYSKEYDOWN and WM_SYSKEYUP.
func (w *window) key(m uint32, wp, lp uintptr) {
	vk := uint32(wp)
	if vk == vkPacket || vk == vkNone {
		// VK_PACKET carries a character in the scan code field (its text arrives as
		// WM_CHAR); VK__none_ is a fake key. Neither is a physical key.
		return
	}
	k := decodeKeyLParam(lp)
	if k.scan == 0 {
		// Some synthesized key messages carry no scan code: recover it from the
		// virtual key.
		s := w.os.scanFromVK(vk)
		k.scan, k.extended = s&0xFF, s>>8 == 0xE0 || s>>8 == 0xE1
	}
	i, ok := keyIndex(k.scan, k.extended)
	if !ok {
		return
	}
	if vk == vkControl && !k.extended && w.altGrFollows() {
		return
	}
	code := scanCodes[i]
	if m == wmKeyUp || m == wmSysKeyUp {
		w.down[i] = false
		w.push(Event{Kind: KeyUp, Code: code})
		return
	}
	if k.repeat && w.down[i] {
		return // auto-repeat
	}
	w.down[i] = true
	w.push(Event{Kind: KeyDown, Code: code})
}

// altGrFollows reports whether the left-Control key message being handled is the fake
// one Windows sends with AltGr on layouts that have it: the next queued message is the
// Right Alt key message with the same timestamp. AltGr is then reported once, as AltRight.
func (w *window) altGrFollows() bool {
	return w.os.peekNext(&w.peekMsg) && isAltGrPair(&w.peekMsg, w.os.messageTime())
}

// isAltGrPair reports whether next is a Right Alt key message sent at time t.
func isAltGrPair(next *msg, t uint32) bool {
	switch next.message {
	case wmKeyDown, wmKeyUp, wmSysKeyDown, wmSysKeyUp:
	default:
		return false
	}
	return uint32(next.wParam) == vkMenu && decodeKeyLParam(next.lParam).extended && next.time == t
}

// stuckKeys are keys whose release Windows may not report: with both Shift keys held the
// first one released sends no WM_KEYUP, and the Windows keys lose their key-up to some
// shell hotkeys (Win+V). Poll compares them against GetKeyState.
var stuckKeys = [...]struct {
	vk    uint32
	index int
}{
	{vkLShift, 0x2A},
	{vkRShift, 0x36},
	{vkLWin, extendedKey | 0x5B},
	{vkRWin, extendedKey | 0x5C},
}

// releaseStuckKeys reports as released the stuckKeys that are down for the game but up
// for the system. Poll calls it after the queue is drained, when GetKeyState is current.
func (w *window) releaseStuckKeys() {
	for _, k := range stuckKeys {
		if w.down[k.index] && !w.os.keyPressed(k.vk) {
			w.down[k.index] = false
			w.push(Event{Kind: KeyUp, Code: scanCodes[k.index]})
		}
	}
}

// asciiText holds the one-character strings of ASCII so typing them allocates nothing.
var asciiText = func() (t [utf8.RuneSelf]string) {
	for i := range t {
		t[i] = string(rune(i))
	}
	return t
}()

// text queues a typed character; control characters (Backspace, Enter, Tab, Escape,
// Ctrl+letter, DEL, C1 controls) and invalid code points are not text.
func (w *window) text(r rune) {
	if !utf8.ValidRune(r) || unicode.IsControl(r) {
		return
	}
	s := asciiText[r&0x7F]
	if r >= utf8.RuneSelf {
		s = string(r)
	}
	w.push(Event{Kind: Text, Text: s})
}

// utf16Decoder reassembles WM_CHAR code units, which arrive one UTF-16 unit per message:
// a character outside the Basic Multilingual Plane is a high surrogate message followed by
// a low surrogate message.
type utf16Decoder struct {
	high uint16 // pending high surrogate, 0 when none
}

// feed consumes one code unit and returns the completed character, if any. Unpaired
// surrogates are dropped.
func (d *utf16Decoder) feed(u uint16) (rune, bool) {
	switch {
	case u >= 0xD800 && u < 0xDC00: // high surrogate: wait for its pair
		d.high = u
		return 0, false
	case u >= 0xDC00 && u < 0xE000: // low surrogate
		h := d.high
		d.high = 0
		if h == 0 {
			return 0, false
		}
		return utf16.DecodeRune(rune(h), rune(u)), true
	default:
		d.high = 0
		return rune(u), true
	}
}

// mouseLParam returns the client coordinates of a mouse message. They are signed: a
// captured mouse dragged left of or above the window reports negative values.
func mouseLParam(lp uintptr) (x, y int32) {
	return int32(int16(uint16(lp))), int32(int16(uint16(lp >> 16)))
}

// sizeLParam returns the client size of WM_SIZE.
func sizeLParam(lp uintptr) (w, h int) {
	return int(uint16(lp)), int(uint16(lp >> 16))
}

// mouseButton maps a button message to its button and direction.
func mouseButton(m uint32) (b sim.ButtonSet, down bool) {
	switch m {
	case wmLButtonDown:
		return sim.ButtonLeft, true
	case wmLButtonUp:
		return sim.ButtonLeft, false
	case wmMButtonDown:
		return sim.ButtonMiddle, true
	case wmMButtonUp:
		return sim.ButtonMiddle, false
	case wmRButtonDown:
		return sim.ButtonRight, true
	case wmRButtonUp:
		return sim.ButtonRight, false
	}
	return 0, false
}

// move handles WM_MOUSEMOVE, which Windows also sends when the cursor did not move.
func (w *window) move(x, y int32) {
	if w.mouseSeen && x == w.mouseX && y == w.mouseY {
		return
	}
	var dx, dy int32
	if w.mouseSeen {
		dx, dy = x-w.mouseX, y-w.mouseY
	}
	w.mouseX, w.mouseY, w.mouseSeen = x, y, true
	if w.locked {
		// Only the movement matters: the cursor itself goes back to the middle of the
		// client area at the end of Poll.
		w.virtX, w.virtY = w.virtX+float32(dx), w.virtY+float32(dy)
		w.lockMoved = true
		w.push(Event{Kind: MouseMove, X: w.virtX, Y: w.virtY})
		return
	}
	w.push(Event{Kind: MouseMove, X: float32(x), Y: float32(y)})
}

// button handles a button message. The mouse is captured while any button is held so the
// release is seen even outside the window.
func (w *window) button(b sim.ButtonSet, down bool, x, y int32) {
	w.mouseX, w.mouseY, w.mouseSeen = x, y, true
	ex, ey := float32(x), float32(y)
	if w.locked { // the cursor sits in the middle; the game knows the virtual position
		ex, ey = w.virtX, w.virtY
	}
	if down {
		if w.buttons == 0 {
			w.os.setCapture(w.hwnd)
		}
		w.buttons |= b
		w.push(Event{Kind: ButtonDown, Button: b, X: ex, Y: ey})
		return
	}
	held := w.buttons
	w.buttons &^= b
	w.push(Event{Kind: ButtonUp, Button: b, X: ex, Y: ey})
	if held != 0 && w.buttons == 0 {
		w.os.releaseCapture()
	}
}

// captureLost releases the held buttons when another window took the mouse capture: the
// button-up messages will not come here.
func (w *window) captureLost() {
	for _, b := range [...]sim.ButtonSet{sim.ButtonLeft, sim.ButtonMiddle, sim.ButtonRight} {
		if w.buttons&b != 0 {
			w.push(Event{Kind: ButtonUp, Button: b, X: float32(w.mouseX), Y: float32(w.mouseY)})
		}
	}
	w.buttons = 0
}

// resize handles WM_SIZE (0×0 while minimized).
func (w *window) resize(cw, ch int) {
	if cw == w.w && ch == w.h {
		return
	}
	w.w, w.h = cw, ch
	w.push(Event{Kind: Resize, W: cw, H: ch})
}

// focusLost handles WM_KILLFOCUS. The game releases everything on FocusLost, so the key
// and button state is simply forgotten: a key still held when focus returns is reported
// down again by its next auto-repeat.
func (w *window) focusLost() {
	w.down = [len(scanCodes)]bool{}
	w.surrogate = utf16Decoder{}
	if w.buttons != 0 {
		w.buttons = 0
		w.os.releaseCapture()
	}
	w.push(Event{Kind: FocusLost})
}
