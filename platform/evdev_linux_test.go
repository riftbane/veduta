package platform

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/riftbane/veduta/v2/sim"
)

// record builds one evdev record of the given size, as the kernel writes them.
func record(size int, typ, code uint16, value int32) []byte {
	b := make([]byte, size)
	binary.LittleEndian.PutUint16(b[size-8:], typ)
	binary.LittleEndian.PutUint16(b[size-6:], code)
	binary.LittleEndian.PutUint32(b[size-4:], uint32(value))
	return b
}

func decodeAll(t *testing.T, size int, recs ...[]byte) []Event {
	t.Helper()
	d := newPadDecoder()
	d.size = size
	var data []byte
	for _, r := range recs {
		data = append(data, r...)
	}
	out, err := d.decode(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestPadDecoder covers both record sizes, because a 32-bit board writes 16-byte records
// and a 64-bit one 24-byte records.
func TestPadDecoder(t *testing.T) {
	for _, size := range []int{16, 24} {
		t.Run(map[int]string{16: "32-bit", 24: "64-bit"}[size], func(t *testing.T) {
			got := describePad(decodeAll(t, size,
				record(size, evKey, btnSouth, 1),   // A down
				record(size, evKey, btnSouth, 2),   // auto-repeat: ignored
				record(size, evKey, btnSouth, 1),   // already down: ignored
				record(size, evKey, btnSouth, 0),   // A up
				record(size, evKey, btnSouth+1, 1), // B
				record(size, evKey, btnSelect, 1),  // Select
				record(size, evKey, btnSouth+3, 1), // X: not a console button, ignored
				record(size, evKey, btnTrigger, 1), // a joystick-style pad's first button
				record(size, evKey, keyBack, 1),    // the handheld's Cancel
			))
			want := "down a,up a,down b,down select,down a,down cancel"
			if got != want {
				t.Fatalf("buttons: %s\n    want: %s", got, want)
			}
		})
	}
}

func TestPadDPad(t *testing.T) {
	const size = 24
	// A hat reports -1, 0 and 1.
	got := describePad(decodeAll(t, size,
		record(size, evAbs, absHat0X, -1),
		record(size, evAbs, absHat0X, 0),
		record(size, evAbs, absHat0Y, 1),
		record(size, evAbs, absHat0Y, 0),
	))
	if want := "down left,up left,down down,up down"; got != want {
		t.Fatalf("hat: %s\n want: %s", got, want)
	}
	// A stick rests in the middle of a wide range: only a real push counts.
	got = describePad(decodeAll(t, size,
		record(size, evAbs, absX, 300),   // inside the dead zone
		record(size, evAbs, absX, 30000), // pushed right
		record(size, evAbs, absX, 0),     // let go
	))
	if want := "down right,up right"; got != want {
		t.Fatalf("stick: %s\n  want: %s", got, want)
	}
	// Moving from one side to the other releases before it presses.
	got = describePad(decodeAll(t, size,
		record(size, evAbs, absHat0X, -1),
		record(size, evAbs, absHat0X, 1),
	))
	if want := "down left,up left,down right"; got != want {
		t.Fatalf("reversal: %s\n  want: %s", got, want)
	}
	// Some pads, and the handheld, report the D-pad as four buttons instead.
	got = describePad(decodeAll(t, size, record(size, evKey, btnDPadUp+2, 1), record(size, evKey, btnDPadUp+2, 0)))
	if want := "down left,up left"; got != want {
		t.Fatalf("buttons: %s\n   want: %s", got, want)
	}
	// On such a pad the sticks are sticks, which the console does not have.
	d := newPadDecoder()
	d.size, d.stickDPad = size, false
	out, err := d.decode(record(size, evAbs, absX, 30000), nil)
	if err != nil || len(out) != 0 {
		t.Fatalf("a stick beside D-pad buttons: %v %v", describePad(out), err)
	}
}

// TestPadAxisRanges: an axis is read against the range its device reports (EVIOCGABS), not
// against one assumed centred on zero. Many pads report the D-pad on ABS_X and ABS_Y from 0
// to 255 and rest at 127 or 128: read as ±32767, left and right were dead and a push to 1,
// nearly full left, came out as right.
func TestPadAxisRanges(t *testing.T) {
	const size = 24
	for _, c := range []struct {
		name     string
		min, max int32
		values   []int32
		want     string
	}{
		{"0..255 at rest", 0, 255, []int32{127, 128, 110, 150}, ""},
		{"0..255 left and right", 0, 255, []int32{0, 127, 255, 128}, "down left,up left,down right,up right"},
		{"0..255 nearly full left", 0, 255, []int32{1, 127}, "down left,up left"},
		{"0..255 straight across", 0, 255, []int32{0, 255, 127}, "down left,up left,down right,up right"},
		{"-128..127", -128, 127, []int32{-128, 0, 127, 10, -127, -20}, "down left,up left,down right,up right,down left,up left"},
		{"-32768..32767", -32768, 32767, []int32{300, 30000, 0, -1, 0, -32768, 0}, "down right,up right,down left,up left"},
		{"a hat, -1..1", -1, 1, []int32{-1, 0, 1, 0}, "down left,up left,down right,up right"},
	} {
		d := newPadDecoder()
		d.size = size
		d.setRange(absX, c.min, c.max)
		var data []byte
		for _, v := range c.values {
			data = append(data, record(size, evAbs, absX, v)...)
		}
		out, err := d.decode(data, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := describePad(out); got != c.want {
			t.Errorf("%s: %v gave %q\n  want %q", c.name, c.values, got, c.want)
		}
	}
	// A range that says nothing (a device that answered zeros for the axis, or none at
	// all) is ignored, and the axis is read as it was before ranges were known.
	d := newPadDecoder()
	d.size = size
	d.setRange(absY, 0, 0)
	d.setRange(absHat0Y, 5, -5)
	out, err := d.decode(append(append(record(size, evAbs, absY, 30000), record(size, evAbs, absY, 0)...),
		append(record(size, evAbs, absHat0Y, -1), record(size, evAbs, absHat0Y, 0)...)...), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := describePad(out), "down down,up down,down up,up up"; got != want {
		t.Errorf("no usable range: %q, want %q", got, want)
	}
}

// TestPadDroppedEvents: after SYN_DROPPED what is held is no longer known, so everything
// is released rather than left stuck down.
func TestPadDroppedEvents(t *testing.T) {
	const size = 24
	got := describePad(decodeAll(t, size,
		record(size, evKey, btnSouth, 1),
		record(size, evAbs, absHat0X, -1),
		record(size, evSyn, synDropped, 0),
	))
	if want := "down a,down left,up a,up left"; got != want {
		t.Fatalf("dropped: %s\n   want: %s", got, want)
	}
	// Half of an exit chord is forgotten too: its release may be among the lost events, and
	// the other button alone must not then quit the game.
	for _, c := range []struct {
		name  string
		first uint16
		then  uint16
		want  string
	}{
		{"Select, then Start", padExitChords[0][0], padExitChords[0][1], "down select,up select,down cancel"},
		{"Ctrl, then Q", keyLeftCtrl, keyQ, ""},
	} {
		got := describePad(decodeAll(t, size,
			record(size, evKey, c.first, 1),
			record(size, evSyn, synDropped, 0),
			record(size, evKey, c.then, 1),
		))
		if got != c.want {
			t.Errorf("%s across dropped events: %s\n  want: %s", c.name, got, c.want)
		}
	}
	// After the drop a chord pressed again in full still closes, and only once.
	got = describePad(decodeAll(t, size,
		record(size, evKey, keyLeftCtrl, 1),
		record(size, evKey, keyQ, 1),
		record(size, evSyn, synDropped, 0),
		record(size, evKey, keyLeftCtrl, 1),
		record(size, evKey, keyQ, 1),
	))
	if want := "close,close"; got != want {
		t.Errorf("a chord pressed again after the drop: %s\n  want: %s", got, want)
	}
}

// TestPadExitChord: Select and Start together close the window, which is the only way off
// a pad with no Home button, and it releases what was held first. It has to work on both
// kinds of pad the table knows: a gamepad, whose buttons start at BTN_SOUTH, and a
// joystick-style pad, whose buttons start at BTN_TRIGGER.
func TestPadExitChord(t *testing.T) {
	const size = 24
	for _, c := range []struct {
		name          string
		a, sel, start uint16
	}{
		{"gamepad", btnSouth, btnSelect, btnStart},
		{"joystick-style pad", btnTrigger, btnTrigger + 6, btnTrigger + 7},
	} {
		got := describePad(decodeAll(t, size,
			record(size, evKey, c.a, 1),
			record(size, evKey, c.sel, 1),
			record(size, evKey, c.start, 1),
		))
		if want := "down a,down select,up a,up select,close"; got != want {
			t.Errorf("%s chord: %s\n want: %s", c.name, got, want)
		}
		// One of the two alone is an ordinary button: Start is Cancel.
		got = describePad(decodeAll(t, size, record(size, evKey, c.start, 1)))
		if want := "down cancel"; got != want {
			t.Errorf("%s start alone: %s, want %s", c.name, got, want)
		}
	}
}

// TestPadExitChordsFollowTheTable: the chords are the buttons the table calls Select and
// Start (Cancel), and every chord is two real buttons.
func TestPadExitChordsFollowTheTable(t *testing.T) {
	for _, c := range padExitChords {
		if c[0] == 0 || c[1] == 0 || c[0] == c[1] {
			t.Errorf("exit chord %#x is not two buttons", c)
		}
		if b0, b1 := padButtons[c[0]], padButtons[c[1]]; b0 != sim.ButtonSelect || b1 != sim.ButtonCancel {
			t.Errorf("exit chord %#x is %v+%v, want select+cancel", c, b0, b1)
		}
	}
}

// TestKeyboardKeys: a console with no pad is driven from a keyboard, so the kernel's key
// codes have to reach the game as buttons. Without this the dashboard draws and then
// answers nothing at all.
func TestKeyboardKeys(t *testing.T) {
	const size = 24
	got := describePad(decodeAll(t, size,
		record(size, evKey, 103, 1), // KEY_UP
		record(size, evKey, 103, 0),
		record(size, evKey, 17, 1), // KEY_W: up as well
		record(size, evKey, 30, 1), // KEY_A: left
		record(size, evKey, 57, 1), // KEY_SPACE: A
		record(size, evKey, 45, 1), // KEY_X: B
		record(size, evKey, 28, 1), // KEY_ENTER: Select
		record(size, evKey, keyEsc, 1),
		record(size, evKey, 190, 1), // a key with no meaning on the console: ignored
		record(size, evSyn, synDropped, 0),
	))
	// Lost events release in the order of the kernel's codes: Escape, W, Enter, A, X, Space.
	want := "down up,up up,down up,down left,down a,down b,down select,down cancel," +
		"up cancel,up up,up select,up left,up b,up a"
	if got != want {
		t.Fatalf("keyboard: %s\n     want: %s", got, want)
	}
}

// TestPadHome: Home closes the player on its own, as Select and Start do together, and
// never reaches the game: BTN_MODE on a gamepad, KEY_HOMEPAGE on the handheld.
func TestPadHome(t *testing.T) {
	const size = 24
	for _, home := range []uint16{btnMode, keyHomePage} {
		got := describePad(decodeAll(t, size,
			record(size, evKey, btnSouth, 1),
			record(size, evKey, home, 1),
			record(size, evKey, home, 2), // auto-repeat
			record(size, evKey, home, 0),
			record(size, evKey, home, 1), // pressed again: closes again
		))
		if want := "down a,up a,close,close"; got != want {
			t.Errorf("Home %#x: %s\n want: %s", home, got, want)
		}
	}
}

// TestKeyboardExit: a keyboard needs its own way out, since the pad's Home does not exist
// on one — and it must not be Escape, which is Cancel. Either Ctrl will do, as it does for
// Ctrl+Q anywhere else, and in either order.
func TestKeyboardExit(t *testing.T) {
	const size = 24
	for _, c := range []struct {
		name string
		recs [][]byte
		want string
	}{
		{"left Ctrl+Q", [][]byte{
			record(size, evKey, 17, 1), // something held, to be released first
			record(size, evKey, keyLeftCtrl, 1),
			record(size, evKey, keyQ, 1),
		}, "down up,up up,close"},
		{"right Ctrl+Q", [][]byte{
			record(size, evKey, 17, 1),
			record(size, evKey, keyRightCtrl, 1),
			record(size, evKey, keyQ, 1),
		}, "down up,up up,close"},
		{"Q, then Ctrl", [][]byte{
			record(size, evKey, keyQ, 1),
			record(size, evKey, keyRightCtrl, 1),
		}, "close"},
		// Q belongs to both chords, so holding it through a change of Ctrl has to count
		// for the second one too, as letting go of Start and pressing it again does on a pad.
		{"Q held from one Ctrl to the other", [][]byte{
			record(size, evKey, keyLeftCtrl, 1),
			record(size, evKey, keyQ, 1),
			record(size, evKey, keyLeftCtrl, 0),
			record(size, evKey, keyRightCtrl, 1),
		}, "close,close"},
	} {
		if got := describePad(decodeAll(t, size, c.recs...)); got != c.want {
			t.Errorf("%s: %s\n  want: %s", c.name, got, c.want)
		}
	}
	// Escape alone is Cancel, not a way out of the player.
	if got := describePad(decodeAll(t, size, record(size, evKey, keyEsc, 1))); got != "down cancel" {
		t.Fatalf("Escape: %s", got)
	}
}

func TestPadPartialRecord(t *testing.T) {
	d := newPadDecoder()
	d.size = 24
	if _, err := d.decode(make([]byte, 30), nil); err == nil {
		t.Fatal("a partial record was accepted")
	}
}

// TestEvdevSourceReadsWithoutWaiting reads a FIFO, which the Go runtime handles as it
// handles an event device: registered with its poller and non-blocking. What was written
// must come out of the next poll, and a poll with nothing to read must return at once
// rather than wait for the player to press something. A source that gave each read a
// deadline already in the past failed the first half: the runtime refuses such a read
// without asking the kernel, so nothing was ever read from a real pad.
func TestEvdevSourceReadsWithoutWaiting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "event0")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("cannot make a FIFO here: %v", err)
	}
	// The writing end first, and read-write, which on Linux opens a FIFO without waiting
	// for the other end.
	w, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	src, err := openEvdev(path)
	if err != nil {
		t.Fatal(err)
	}
	defer src.close()

	poll := func() string {
		t.Helper()
		type result struct {
			evs []Event
			err error
		}
		done := make(chan result, 1)
		go func() { evs, err := src.poll(); done <- result{append([]Event(nil), evs...), err} }()
		select {
		case r := <-done:
			if r.err != nil {
				t.Fatalf("poll: %v", r.err)
			}
			return describePad(r.evs)
		case <-time.After(10 * time.Second):
			t.Fatal("poll is waiting for input instead of returning")
			return ""
		}
	}
	write := func(recs ...[]byte) {
		t.Helper()
		var data []byte
		for _, r := range recs {
			data = append(data, r...)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}

	if got := poll(); got != "" {
		t.Fatalf("nothing written, poll = %q", got)
	}
	write(record(eventSize, evKey, btnSouth, 1), record(eventSize, evAbs, absHat0X, -1))
	if got, want := poll(), "down a,down left"; got != want {
		t.Fatalf("poll = %q, want %q", got, want)
	}
	// More than one read's worth: the source keeps reading until the kernel has no more.
	var many [][]byte
	for i := 0; i < 70; i++ {
		many = append(many, record(eventSize, evKey, btnSouth+1, int32(1-i%2)))
	}
	write(many...)
	if got := poll(); strings.Count(got, "down b") != 35 || strings.Count(got, "up b") != 35 {
		t.Fatalf("70 records gave %q", got)
	}
	if n := testing.AllocsPerRun(100, func() { src.poll() }); n != 0 {
		t.Errorf("an idle poll allocates %v times", n)
	}
}

// TestEventSizeMatchesTheWord pins the arithmetic the decoder depends on.
func TestEventSizeMatchesTheWord(t *testing.T) {
	switch eventSize {
	case 16, 24:
	default:
		t.Fatalf("eventSize = %d, want 16 on a 32-bit kernel or 24 on a 64-bit one", eventSize)
	}
}
