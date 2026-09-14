package platform

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
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

func describePad(evs []Event) string {
	var s []string
	for _, e := range evs {
		switch e.Kind {
		case KeyDown:
			s = append(s, "down "+e.Code)
		case KeyUp:
			s = append(s, "up "+e.Code)
		case Close:
			s = append(s, "close")
		default:
			s = append(s, "other")
		}
	}
	return strings.Join(s, ",")
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
				record(size, evKey, 0x13b, 1),      // Start
				record(size, evKey, btnTrigger, 1), // a joystick-style pad's first button
			))
			want := "down Space,up Space,down Enter,down Space"
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
	if want := "down ArrowLeft,up ArrowLeft,down ArrowDown,up ArrowDown"; got != want {
		t.Fatalf("hat: %s\n want: %s", got, want)
	}
	// A stick rests in the middle of a wide range: only a real push counts.
	got = describePad(decodeAll(t, size,
		record(size, evAbs, absX, 300),   // inside the dead zone
		record(size, evAbs, absX, 30000), // pushed right
		record(size, evAbs, absX, 0),     // let go
	))
	if want := "down ArrowRight,up ArrowRight"; got != want {
		t.Fatalf("stick: %s\n  want: %s", got, want)
	}
	// Moving from one side to the other releases before it presses.
	got = describePad(decodeAll(t, size,
		record(size, evAbs, absHat0X, -1),
		record(size, evAbs, absHat0X, 1),
	))
	if want := "down ArrowLeft,up ArrowLeft,down ArrowRight"; got != want {
		t.Fatalf("reversal: %s\n  want: %s", got, want)
	}
	// Some pads report the D-pad as four buttons instead.
	got = describePad(decodeAll(t, size, record(size, evKey, 0x222, 1), record(size, evKey, 0x222, 0)))
	if want := "down ArrowLeft,up ArrowLeft"; got != want {
		t.Fatalf("buttons: %s\n   want: %s", got, want)
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
	if want := "down Space,down ArrowLeft,up Space,up ArrowLeft"; got != want {
		t.Fatalf("dropped: %s\n   want: %s", got, want)
	}
}

// TestPadExitChord: Select and Start together close the window, which is the only way off
// a console with no keyboard, and it releases what was held first. It has to work on both
// kinds of pad the table knows: a gamepad, whose buttons start at BTN_SOUTH, and a
// joystick-style pad, whose buttons start at BTN_TRIGGER.
func TestPadExitChord(t *testing.T) {
	const size = 24
	for _, c := range []struct {
		name          string
		a, sel, start uint16
	}{
		{"gamepad", btnSouth, 0x13a, 0x13b},                                // BTN_SELECT, BTN_START
		{"joystick-style pad", btnTrigger, btnTrigger + 6, btnTrigger + 7}, // what the table calls Select and Start
	} {
		got := describePad(decodeAll(t, size,
			record(size, evKey, c.a, 1),
			record(size, evKey, c.sel, 1),
			record(size, evKey, c.start, 1),
		))
		if want := "down Space,down Tab,up Space,up Tab,close"; got != want {
			t.Errorf("%s chord: %s\n want: %s", c.name, got, want)
		}
		// One of the two alone is an ordinary button.
		got = describePad(decodeAll(t, size, record(size, evKey, c.start, 1)))
		if want := "down Enter"; got != want {
			t.Errorf("%s start alone: %s, want %s", c.name, got, want)
		}
	}
}

// TestPadExitChordsFollowTheTable: the chords are the buttons the table turns into Tab and
// Enter, so a table corrected against a real pad cannot leave the exit behind. Every
// button called Select or Start belongs to a chord, and every chord is two real buttons.
func TestPadExitChordsFollowTheTable(t *testing.T) {
	inChord := map[uint16]bool{}
	for _, c := range padExitChords {
		if c[0] == 0 || c[1] == 0 || c[0] == c[1] {
			t.Errorf("exit chord %#x is not two buttons", c)
		}
		if padButtons[c[0]] != "Tab" || padButtons[c[1]] != "Enter" {
			t.Errorf("exit chord %#x is %q+%q, want Tab+Enter", c, padButtons[c[0]], padButtons[c[1]])
		}
		inChord[c[0]], inChord[c[1]] = true, true
	}
	for code, name := range padButtons {
		if (name == "Tab" || name == "Enter") && !inChord[code] {
			t.Errorf("button %#x is %s but in no exit chord", code, name)
		}
	}
}

// TestKeyboardKeys: a console with no pad is driven from a keyboard, so the kernel's own
// key codes have to reach the game as W3C codes. Without this the dashboard draws and then
// answers nothing at all.
func TestKeyboardKeys(t *testing.T) {
	const size = 24
	got := describePad(decodeAll(t, size,
		record(size, evKey, 103, 1), // KEY_UP
		record(size, evKey, 103, 0),
		record(size, evKey, 28, 1), // KEY_ENTER
		record(size, evKey, 57, 1), // KEY_SPACE
		record(size, evKey, keyEsc, 1),
		record(size, evKey, 17, 1),  // KEY_W
		record(size, evKey, 190, 1), // a key with no W3C name: ignored, not guessed
	))
	want := "down ArrowUp,up ArrowUp,down Enter,down Space,down Escape,down KeyW"
	if got != want {
		t.Fatalf("keyboard: %s\n     want: %s", got, want)
	}
}

// TestKeyboardExit: a keyboard needs its own way out, since the pad's chord does not exist
// on one — and it must not be Escape, which the dashboard and games already use. Either
// Ctrl will do, as it does for Ctrl+Q anywhere else, and in either order.
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
		}, "down KeyW,down ControlLeft,up KeyW,up ControlLeft,close"},
		{"right Ctrl+Q", [][]byte{
			record(size, evKey, 17, 1),
			record(size, evKey, keyRightCtrl, 1),
			record(size, evKey, keyQ, 1),
		}, "down KeyW,down ControlRight,up KeyW,up ControlRight,close"},
		{"Q, then Ctrl", [][]byte{
			record(size, evKey, keyQ, 1),
			record(size, evKey, keyRightCtrl, 1),
		}, "down KeyQ,up KeyQ,close"},
		// Q belongs to both chords, so holding it through a change of Ctrl has to count
		// for the second one too, as letting go of Start and pressing it again does on a pad.
		{"Q held from one Ctrl to the other", [][]byte{
			record(size, evKey, keyLeftCtrl, 1),
			record(size, evKey, keyQ, 1),
			record(size, evKey, keyLeftCtrl, 0),
			record(size, evKey, keyRightCtrl, 1),
		}, "down ControlLeft,up ControlLeft,close,close"},
	} {
		if got := describePad(decodeAll(t, size, c.recs...)); got != c.want {
			t.Errorf("%s: %s\n  want: %s", c.name, got, c.want)
		}
	}
	// Escape alone is an ordinary key, not a way out of the player.
	if got := describePad(decodeAll(t, size, record(size, evKey, keyEsc, 1))); got != "down Escape" {
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
	if got, want := poll(), "down Space,down ArrowLeft"; got != want {
		t.Fatalf("poll = %q, want %q", got, want)
	}
	// More than one read's worth: the source keeps reading until the kernel has no more.
	var many [][]byte
	for i := 0; i < 70; i++ {
		many = append(many, record(eventSize, evKey, btnSouth+1, int32(1-i%2)))
	}
	write(many...)
	if got := poll(); strings.Count(got, "down Escape") != 35 || strings.Count(got, "up Escape") != 35 {
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
