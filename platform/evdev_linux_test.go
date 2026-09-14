package platform

import (
	"encoding/binary"
	"strings"
	"testing"
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
// a console with no keyboard, and it releases what was held first.
func TestPadExitChord(t *testing.T) {
	const size = 24
	got := describePad(decodeAll(t, size,
		record(size, evKey, btnSouth, 1),
		record(size, evKey, exitChord[0], 1),
		record(size, evKey, exitChord[1], 1),
	))
	if want := "down Space,down Tab,up Space,up Tab,close"; got != want {
		t.Fatalf("chord: %s\n want: %s", got, want)
	}
	// One of the two alone is an ordinary button.
	got = describePad(decodeAll(t, size, record(size, evKey, exitChord[1], 1)))
	if want := "down Enter"; got != want {
		t.Fatalf("start alone: %s, want %s", got, want)
	}
}

func TestPadPartialRecord(t *testing.T) {
	d := newPadDecoder()
	d.size = 24
	if _, err := d.decode(make([]byte, 30), nil); err == nil {
		t.Fatal("a partial record was accepted")
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
