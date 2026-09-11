//go:build windows

package platform

import (
	"fmt"
	"slices"
	"testing"
	"unsafe"

	"github.com/riftbane/veduta/sim"
)

// fakeOS records what the handler asks of user32.
type fakeOS struct {
	w        *window
	def      []uint32          // messages passed to DefWindowProc
	captures int               // SetCapture calls
	releases int               // ReleaseCapture calls
	next     *msg              // what PeekMessage(PM_NOREMOVE) returns; nil: empty queue
	time     uint32            // GetMessageTime
	up       map[uint32]bool   // virtual keys GetKeyState reports released
	vsc      map[uint32]uint32 // MapVirtualKey results
	paints   int
}

func (f *fakeOS) defWindowProc(_ uintptr, m uint32, _, _ uintptr) uintptr {
	f.def = append(f.def, m)
	return 0
}
func (f *fakeOS) setCapture(uintptr) { f.captures++ }
func (f *fakeOS) releaseCapture() {
	f.releases++
	f.w.handle(wmCaptureChanged, 0, 0) // sent synchronously, as by the system
}
func (f *fakeOS) peekNext(m *msg) bool {
	if f.next == nil {
		return false
	}
	*m = *f.next
	return true
}
func (f *fakeOS) messageTime() uint32         { return f.time }
func (f *fakeOS) keyPressed(vk uint32) bool   { return !f.up[vk] }
func (f *fakeOS) scanFromVK(vk uint32) uint32 { return f.vsc[vk] }
func (f *fakeOS) paint(*window)               { f.paints++ }

func newFakeWindow() (*window, *fakeOS) {
	f := &fakeOS{up: map[uint32]bool{}, vsc: map[uint32]uint32{}}
	w := &window{hwnd: 0x1234, os: f}
	f.w = w
	return w, f
}

// describe renders events compactly for comparison.
func describe(evs []Event) []string {
	var s []string
	for _, e := range evs {
		switch e.Kind {
		case KeyDown:
			s = append(s, "down "+e.Code)
		case KeyUp:
			s = append(s, "up "+e.Code)
		case MouseMove:
			s = append(s, fmt.Sprintf("move %g,%g", e.X, e.Y))
		case ButtonDown:
			s = append(s, fmt.Sprintf("bdown %d %g,%g", e.Button, e.X, e.Y))
		case ButtonUp:
			s = append(s, fmt.Sprintf("bup %d %g,%g", e.Button, e.X, e.Y))
		case Text:
			s = append(s, "text "+e.Text)
		case Resize:
			s = append(s, fmt.Sprintf("resize %dx%d", e.W, e.H))
		case Close:
			s = append(s, "close")
		case FocusLost:
			s = append(s, "focuslost")
		default:
			s = append(s, fmt.Sprintf("kind %d", e.Kind))
		}
	}
	return s
}

func expect(t *testing.T, w *window, want ...string) {
	t.Helper()
	if got := describe(w.takeEvents()); !slices.Equal(got, want) {
		t.Errorf("events:\n got  %q\n want %q", got, want)
	}
}

func TestKeyDownRepeatUp(t *testing.T) {
	w, f := newFakeWindow()
	w.handle(wmKeyDown, 'W', keyLP(0x11, false, false, false))
	w.handle(wmKeyDown, 'W', keyLP(0x11, false, true, false)) // auto-repeat
	w.handle(wmKeyDown, 'W', keyLP(0x11, false, true, false))
	w.handle(wmKeyUp, 'W', keyLP(0x11, false, false, true))
	w.handle(wmKeyDown, 0x0D, keyLP(0x1C, false, false, false))
	w.handle(wmKeyDown, 0x0D, keyLP(0x1C, true, false, false))
	w.handle(wmKeyDown, 0x11, keyLP(0x1D, true, false, false))
	w.handle(wmKeyDown, 0x26, keyLP(0x48, true, false, false))
	w.handle(wmKeyDown, 0x26, keyLP(0x48, false, false, false)) // NumLock off: VK_UP from Numpad8
	w.handle(wmKeyDown, 0x90, keyLP(0x45, true, false, false))  // NumLock: unmapped
	expect(t, w, "down KeyW", "up KeyW", "down Enter", "down NumpadEnter", "down ControlRight",
		"down ArrowUp", "down Numpad8", "down ")
	if len(f.def) != 0 {
		t.Errorf("WM_KEYDOWN reached DefWindowProc: %#x", f.def)
	}
}

func TestSysKeysAndMenu(t *testing.T) {
	w, f := newFakeWindow()
	w.handle(wmSysKeyDown, vkMenu, keyLP(0x38, false, false, false)|1<<29)
	w.handle(wmSysKeyDown, 0x73, keyLP(0x3E, false, false, false)|1<<29) // Alt+F4
	w.handle(wmSysKeyUp, vkMenu, keyLP(0x38, false, false, true))
	w.handle(wmSysKeyDown, 0x79, keyLP(0x44, false, false, false)) // F10
	expect(t, w, "down AltLeft", "down F4", "up AltLeft", "down F10")
	if want := []uint32{wmSysKeyDown, wmSysKeyDown, wmSysKeyUp, wmSysKeyDown}; !slices.Equal(f.def, want) {
		t.Errorf("DefWindowProc got %#x, want %#x (Alt+F4 needs it)", f.def, want)
	}
	f.def = nil
	if r := w.handle(wmSysCommand, scKeyMenu, 0); r != 0 || len(f.def) != 0 {
		t.Errorf("SC_KEYMENU not swallowed: r=%d def=%#x", r, f.def)
	}
	w.handle(wmSysCommand, scKeyMenu|0x3, ' ') // low four bits are reserved
	w.handle(wmSysCommand, 0xF060, 0)          // SC_CLOSE goes on to DefWindowProc
	if want := []uint32{wmSysCommand}; !slices.Equal(f.def, want) {
		t.Errorf("DefWindowProc got %#x, want %#x", f.def, want)
	}
	f.def = nil
	w.handle(wmSysChar, 'x', 0)
	if len(f.def) != 0 {
		t.Error("WM_SYSCHAR reached DefWindowProc (beeps)")
	}
	expect(t, w)
}

func TestAltGrFakeControl(t *testing.T) {
	w, f := newFakeWindow()
	f.time = 1000
	f.next = &msg{message: wmKeyDown, wParam: vkMenu, lParam: keyLP(0x38, true, false, false), time: 1000}
	w.handle(wmKeyDown, vkControl, keyLP(0x1D, false, false, false)) // fake: dropped
	f.next = nil
	w.handle(wmSysKeyDown, vkMenu, keyLP(0x38, true, false, false))
	f.next = &msg{message: wmKeyUp, wParam: vkMenu, lParam: keyLP(0x38, true, false, true), time: 1000}
	w.handle(wmKeyUp, vkControl, keyLP(0x1D, false, false, true))
	f.next = nil
	w.handle(wmSysKeyUp, vkMenu, keyLP(0x38, true, false, true))
	expect(t, w, "down AltRight", "up AltRight")

	// A real Left Ctrl: next message is later, or is not Right Alt.
	f.next = &msg{message: wmKeyDown, wParam: vkMenu, lParam: keyLP(0x38, true, false, false), time: 1001}
	w.handle(wmKeyDown, vkControl, keyLP(0x1D, false, false, false))
	f.next = &msg{message: wmKeyDown, wParam: vkMenu, lParam: keyLP(0x38, false, false, false), time: 1000}
	w.handle(wmKeyUp, vkControl, keyLP(0x1D, false, false, true))
	f.next = &msg{message: wmChar, wParam: vkMenu, lParam: keyLP(0x38, true, false, false), time: 1000}
	w.handle(wmKeyDown, vkControl, keyLP(0x1D, false, false, false))
	expect(t, w, "down ControlLeft", "up ControlLeft", "down ControlLeft")
}

func TestSyntheticKeys(t *testing.T) {
	w, f := newFakeWindow()
	w.handle(wmKeyDown, 0x10, keyLP(0x2A, true, false, false)) // fake Shift
	w.handle(wmKeyDown, vkPacket, keyLP(0x42, false, false, false))
	w.handle(wmKeyDown, vkNone, keyLP(0x11, false, false, false))
	expect(t, w)
	f.vsc[0x26] = 0xE048 // scan code 0: recovered from the virtual key
	f.vsc['A'] = 0x1E
	w.handle(wmKeyDown, 0x26, 1)
	w.handle(wmKeyDown, 'A', 1)
	w.handle(wmKeyDown, 0xE5, keyLP(0x11, false, false, false)) // VK_PROCESSKEY: IME, still KeyW
	w.handle(wmKeyDown, 0x10, keyLP(0x36, true, false, false))  // CJK IME Right Shift
	expect(t, w, "down ArrowUp", "down KeyA", "down KeyW", "down ShiftRight")
}

func TestStuckKeysReleased(t *testing.T) {
	w, f := newFakeWindow()
	w.handle(wmKeyDown, 0x10, keyLP(0x2A, false, false, false))
	w.handle(wmKeyDown, 0x10, keyLP(0x36, false, false, false))
	w.handle(wmKeyDown, vkLWin, keyLP(0x5B, true, false, false))
	w.releaseStuckKeys()
	expect(t, w, "down ShiftLeft", "down ShiftRight", "down MetaLeft")
	// Left Shift released first: Windows sends nothing. Then Right Shift's WM_KEYUP.
	f.up[vkLShift] = true
	f.up[vkRShift] = true
	w.handle(wmKeyUp, 0x10, keyLP(0x36, false, false, true))
	w.releaseStuckKeys()
	expect(t, w, "up ShiftRight", "up ShiftLeft")
	f.up[vkLWin] = true // Win+V swallowed the key-up
	w.releaseStuckKeys()
	w.releaseStuckKeys()
	expect(t, w, "up MetaLeft")
}

func TestUTF16Decoder(t *testing.T) {
	for _, c := range []struct {
		units []uint16
		want  []rune
	}{
		{[]uint16{'a', 0xE9, 0x20AC}, []rune{'a', 'é', '€'}},
		{[]uint16{0xD83D, 0xDE00}, []rune{0x1F600}},           // 😀
		{[]uint16{0xD834, 0xDD1E, 'x'}, []rune{0x1D11E, 'x'}}, // 𝄞 x
		{[]uint16{0xDE00, 'b'}, []rune{'b'}},                  // lone low surrogate
		{[]uint16{0xD83D, 'c'}, []rune{'c'}},                  // high surrogate, no pair
		{[]uint16{0xD83D, 0xD83D, 0xDE00}, []rune{0x1F600}},   // first high replaced
		{[]uint16{0xDBFF, 0xDFFF}, []rune{0x10FFFF}},          // last code point
		{[]uint16{0xFFFF, 0xE000}, []rune{0xFFFF, 0xE000}},    // BMP edges pass through
	} {
		var d utf16Decoder
		var got []rune
		for _, u := range c.units {
			if r, ok := d.feed(u); ok {
				got = append(got, r)
			}
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("feed(%#x) = %U, want %U", c.units, got, c.want)
		}
	}
}

func TestTextEvents(t *testing.T) {
	w, _ := newFakeWindow()
	for _, u := range []uint16{'h', 0xE9, 0x08, 0x0D, 0x09, 0x1B, 0x01, 0x7F, 0x85, 0xD83D, 0xDE00, ' '} {
		w.handle(wmChar, uintptr(u), 0)
	}
	expect(t, w, "text h", "text é", "text 😀", "text  ")
	if r := w.handle(wmUniChar, unicodeNoChar, 0); r != 1 {
		t.Errorf("WM_UNICHAR probe returned %d, want 1", r)
	}
	w.handle(wmUniChar, 0x1F600, 0)
	w.handle(wmUniChar, 0xD800, 0)   // surrogate code point: not a character
	w.handle(wmUniChar, 0x110000, 0) // out of range
	expect(t, w, "text 😀")
	// ASCII text reuses static strings: no allocation per character.
	if n := testing.AllocsPerRun(100, func() {
		w.handle(wmChar, 'q', 0)
		w.takeEvents()
	}); n != 0 {
		t.Errorf("typing ASCII allocates %v times per character", n)
	}
}

func TestMouseDecoding(t *testing.T) {
	for _, c := range []struct {
		lp   uintptr
		x, y int32
	}{
		{0x00140010, 16, 20},
		{0xFFF6FFFB, -5, -10},
		{0x7FFF8000, -32768, 32767},
	} {
		if x, y := mouseLParam(c.lp); x != c.x || y != c.y {
			t.Errorf("mouseLParam(%#x) = %d,%d, want %d,%d", c.lp, x, y, c.x, c.y)
		}
	}
	if w, h := sizeLParam(0x02D00500); w != 1280 || h != 720 {
		t.Errorf("sizeLParam = %dx%d", w, h)
	}
	if w, h := sizeLParam(0xFFFF0001); w != 1 || h != 65535 {
		t.Errorf("sizeLParam = %dx%d", w, h)
	}
	for _, c := range []struct {
		m    uint32
		b    sim.ButtonSet
		down bool
	}{
		{wmLButtonDown, sim.ButtonLeft, true}, {wmLButtonUp, sim.ButtonLeft, false},
		{wmMButtonDown, sim.ButtonMiddle, true}, {wmMButtonUp, sim.ButtonMiddle, false},
		{wmRButtonDown, sim.ButtonRight, true}, {wmRButtonUp, sim.ButtonRight, false},
	} {
		if b, down := mouseButton(c.m); b != c.b || down != c.down {
			t.Errorf("mouseButton(%#x) = %d,%v, want %d,%v", c.m, b, down, c.b, c.down)
		}
	}
}

func TestMouseEventsAndCapture(t *testing.T) {
	w, f := newFakeWindow()
	w.handle(wmMouseMove, 0, 0x00140010)
	w.handle(wmMouseMove, 0, 0x00140010) // no movement
	w.handle(wmMouseMove, 0, 0x00150011)
	w.handle(wmLButtonDown, 1, 0x00150011)
	w.handle(wmMouseMove, 0, 0xFFF6FFFB) // captured, outside the window
	w.handle(wmRButtonDown, 3, 0xFFF6FFFB)
	w.handle(wmLButtonUp, 2, 0xFFF6FFFB)
	w.handle(wmRButtonUp, 0, 0x00020001)
	expect(t, w, "move 17,21", "bdown 1 17,21", "move -5,-10", "bdown 4 -5,-10",
		"bup 1 -5,-10", "bup 4 1,2")
	if f.captures != 1 || f.releases != 1 {
		t.Errorf("SetCapture %d, ReleaseCapture %d; want 1, 1", f.captures, f.releases)
	}
	// Another window takes the capture while the middle button is held.
	w.handle(wmMButtonDown, 0x10, 0x00030003)
	w.handle(wmCaptureChanged, 0, 0x9999)
	w.handle(wmMButtonUp, 0, 0x00030003) // arrives anyway: reported, no second release
	expect(t, w, "bdown 2 3,3", "bup 2 3,3", "bup 2 3,3")
	if f.captures != 2 || f.releases != 1 {
		t.Errorf("SetCapture %d, ReleaseCapture %d; want 2, 1", f.captures, f.releases)
	}
	// Our own SetCapture's WM_CAPTURECHANGED names this window: nothing is released.
	w.handle(wmLButtonDown, 1, 0x00030003)
	w.handle(wmCaptureChanged, 0, w.hwnd)
	expect(t, w, "bdown 1 3,3")
}

func TestResizeCloseFocusPaint(t *testing.T) {
	w, f := newFakeWindow()
	w.w, w.h = 320, 240
	w.handle(wmSize, 0, 0x00F00140) // unchanged
	w.handle(wmSize, 0, 0x00F10141)
	w.handle(wmSize, 0, 0x00F20142)
	w.handle(wmKeyDown, 'W', keyLP(0x11, false, false, false))
	w.handle(wmSize, 1, 0) // minimized
	if cw, ch := w.w, w.h; cw != 0 || ch != 0 {
		t.Errorf("size while minimized = %dx%d", cw, ch)
	}
	w.handle(wmClose, 0, 0)
	w.handle(wmKillFocus, 0, 0)
	// Focus is back and W is still held: its next auto-repeat reports it down again.
	w.handle(wmKeyDown, 'W', keyLP(0x11, false, true, false))
	w.handle(wmKeyDown, 'W', keyLP(0x11, false, true, false))
	expect(t, w, "resize 322x242", "down KeyW", "resize 0x0", "close", "focuslost", "down KeyW")
	if len(f.def) != 0 {
		t.Errorf("DefWindowProc got %#x (WM_CLOSE would destroy the window)", f.def)
	}
	if r := w.handle(wmEraseBkgnd, 0, 0); r != 1 {
		t.Errorf("WM_ERASEBKGND returned %d, want 1", r)
	}
	w.handle(wmPaint, 0, 0)
	if f.paints != 1 {
		t.Errorf("paints = %d", f.paints)
	}
	// Focus lost with a button held releases the capture.
	w.handle(wmLButtonDown, 1, 0)
	w.handle(wmKillFocus, 0, 0)
	if f.releases != 1 || w.buttons != 0 {
		t.Errorf("focus loss: releases=%d buttons=%d", f.releases, w.buttons)
	}
	expect(t, w, "bdown 1 0,0", "focuslost")
}

func TestDestroyedBySystem(t *testing.T) {
	w, _ := newFakeWindow()
	w.handle(wmDestroy, 0, 0)
	w.handle(wmDestroy, 0, 0)
	if !w.destroyed {
		t.Error("not marked destroyed")
	}
	expect(t, w, "close")
}

func TestEventBuffersReused(t *testing.T) {
	w, _ := newFakeWindow()
	w.handle(wmKeyDown, 'A', keyLP(0x1E, false, false, false))
	first := w.takeEvents()
	w.handle(wmKeyUp, 'A', keyLP(0x1E, false, false, true)) // arrives before the next Poll
	if got := describe(first); !slices.Equal(got, []string{"down KeyA"}) {
		t.Fatalf("returned slice overwritten before the next Poll: %q", got)
	}
	expect(t, w, "up KeyA")
	if got := w.takeEvents(); got != nil {
		t.Errorf("empty poll = %v, want nil", got)
	}
	// Steady state: no allocation per event once the buffers have grown.
	n := testing.AllocsPerRun(100, func() {
		for i := 0; i < 8; i++ {
			w.handle(wmKeyDown, 'A', keyLP(0x1E, false, false, false))
			w.handle(wmKeyUp, 'A', keyLP(0x1E, false, false, true))
			w.handle(wmMouseMove, 0, uintptr(i))
		}
		w.takeEvents()
	})
	if n != 0 {
		t.Errorf("event handling allocates %v times per poll", n)
	}
}

func TestWin32StructLayout(t *testing.T) {
	const ptr = unsafe.Sizeof(uintptr(0))
	pick := func(p64, p32 uintptr) uintptr {
		if ptr == 8 {
			return p64
		}
		return p32
	}
	for _, c := range []struct {
		name      string
		got, want uintptr
	}{
		{"POINT", unsafe.Sizeof(point{}), 8},
		{"RECT", unsafe.Sizeof(rect{}), 16},
		{"MSG.message", unsafe.Offsetof(msg{}.message), pick(8, 4)},
		{"MSG.wParam", unsafe.Offsetof(msg{}.wParam), pick(16, 8)},
		{"MSG.lParam", unsafe.Offsetof(msg{}.lParam), pick(24, 12)},
		{"MSG.time", unsafe.Offsetof(msg{}.time), pick(32, 16)},
		{"MSG.pt", unsafe.Offsetof(msg{}.pt), pick(36, 20)},
		{"MSG", unsafe.Sizeof(msg{}), pick(48, 32)}, // C: 48 / 28 (+ lPrivate)
		{"WNDCLASSEXW.lpfnWndProc", unsafe.Offsetof(wndClassEx{}.wndProc), 8},
		{"WNDCLASSEXW.hInstance", unsafe.Offsetof(wndClassEx{}.instance), pick(24, 20)},
		{"WNDCLASSEXW.lpszClassName", unsafe.Offsetof(wndClassEx{}.className), pick(64, 40)},
		{"WNDCLASSEXW", unsafe.Sizeof(wndClassEx{}), pick(80, 48)},
		{"PAINTSTRUCT.rcPaint", unsafe.Offsetof(paintStruct{}.paint), pick(12, 8)},
		{"PAINTSTRUCT", unsafe.Sizeof(paintStruct{}), pick(72, 64)},
		{"BITMAPINFOHEADER", unsafe.Sizeof(bitmapInfoHeader{}), 40},
		{"BITMAPINFOHEADER.biCompression", unsafe.Offsetof(bitmapInfoHeader{}.compression), 16},
		{"BITMAPINFO", unsafe.Sizeof(bitmapInfo{}), 44},
	} {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
}
