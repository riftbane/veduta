package platform

import (
	"math"
	"testing"
)

// TestStickValue: an axis reading becomes -1…1 against the range the device reports, with
// rest in the dead zone reading exactly 0 (never -0, which a game could store and print),
// the ends reading ±1, and the two sides mirroring each other exactly.
func TestStickValue(t *testing.T) {
	for _, c := range []struct {
		v, lo, hi int64
		want      float64
	}{
		{127, 0, 255, 0}, {128, 0, 255, 0}, {0, 0, 255, -1}, {255, 0, 255, 1},
		{0, -32768, 32767, 0}, {-32768, -32768, 32767, -1}, {32767, -32768, 32767, 1},
		{400, -400, 400, 1}, {60, -400, 400, 0}, {200, -400, 400, (0.5 - 0.15) / 0.85},
		{5, 5, 5, 0}, // a range with nothing in it
	} {
		got := stickValue(c.v, c.lo, c.hi)
		if math.Abs(float64(got)-c.want) > 1e-6 || math.Signbit(float64(got)) && got == 0 {
			t.Errorf("stickValue(%d, %d, %d) = %v, want %v", c.v, c.lo, c.hi, got, c.want)
		}
	}
	prev := float32(-2)
	for v := int64(0); v <= 255; v++ {
		got := stickValue(v, 0, 255)
		if got < prev || got != -stickValue(255-v, 0, 255) || got < -1 || got > 1 {
			t.Fatalf("stickValue(%d, 0, 255) = %v after %v, mirror %v", v, got, prev, stickValue(255-v, 0, 255))
		}
		prev = got
	}
}

// TestPadStick: ABS_X and ABS_Y are the stick. Up on the device is down its axis, and up for
// the game; a move inside the dead zone is no move at all. A pad whose D-pad is buttons does
// not also get the arrows from its stick.
func TestPadStick(t *testing.T) {
	const size = 24
	recs := [][]byte{
		record(size, evAbs, absX, 3000),   // inside the dead zone: nothing
		record(size, evAbs, absX, 32767),  // right, to the end
		record(size, evAbs, absY, -32768), // up, to the end
		record(size, evAbs, absX, 0),      // x back to rest
		record(size, evAbs, absY, 100),    // y back to rest
	}
	for _, c := range []struct {
		name   string
		arrows bool
		want   string
	}{
		{"a pad whose D-pad may be its stick", true, "down ArrowRight,stick 1,0,down ArrowUp,stick 1,1,up ArrowRight,stick 0,1,up ArrowUp,stick 0,0"},
		{"a pad with D-pad buttons", false, "stick 1,0,stick 1,1,stick 0,1,stick 0,0"},
	} {
		d := newPadDecoder()
		d.size, d.stickArrows = size, c.arrows
		d.setRange(absX, -32768, 32767)
		d.setRange(absY, -32768, 32767)
		var data []byte
		for _, r := range recs {
			data = append(data, r...)
		}
		out, err := d.decode(data, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := describeAll(out); got != c.want {
			t.Errorf("%s: %s\n  want: %s", c.name, got, c.want)
		}
	}
	// A hat is a D-pad, never the stick.
	if got := describeAll(decodeAll(t, size, record(size, evAbs, absHat0X, 1))); got != "down ArrowRight" {
		t.Errorf("hat: %s", got)
	}
}

// TestMouseStick: a mouse is a stick that stays where it is left. Its movement adds up,
// mouseReach counts from rest is the end of the travel and further is still the end, down
// is down, and a click of any button brings it back to rest.
func TestMouseStick(t *testing.T) {
	const size = 24
	got := describeAll(decodeAll(t, size,
		record(size, evRel, relX, mouseReach),   // right, to the end
		record(size, evRel, relX, 2*mouseReach), // further: still the end
		record(size, evRel, relX, -mouseReach),  // back from where it stopped: rest
		record(size, evRel, relX, -30),          // a nudge inside the dead zone
		record(size, evRel, relY, mouseReach),   // down
		record(size, evKey, btnLeft, 1),         // a click: rest
		record(size, evKey, btnLeft, 0),
		record(size, evRel, relX, 60),              // counted from rest again: inside the dead zone
		record(size, evRel, relX, mouseReach-60),   // the end, only if the click forgot the nudge
		record(size, evKey, btnLeft+1, 1),          // the right button recentres too
		record(size, evRel, relY, -(mouseReach/2)), // up, half way
	))
	want := "stick 1,0,stick 0,0,stick 0,-1,stick 0,0,stick 1,0,stick 0,0,stick 0,0.4117647"
	if got != want {
		t.Fatalf("mouse: %s\n want: %s", got, want)
	}
}

// TestTabletStick: an absolute pointer, such as QEMU's usb-tablet, is the stick directly: the
// middle of its area is rest and its edges are the ends. It presses no arrows, and its
// clicks move nothing.
func TestTabletStick(t *testing.T) {
	const size = 24
	d := newPadDecoder()
	d.size, d.stickArrows = size, false
	d.setRange(absX, 0, 32767)
	d.setRange(absY, 0, 32767)
	var data []byte
	for _, r := range [][]byte{
		record(size, evAbs, absY, 16384), // the middle: rest
		record(size, evAbs, absX, 32767), // the right edge
		record(size, evAbs, absY, 0),     // the top edge
		record(size, evKey, btnLeft, 1),  // a click
		record(size, evAbs, absX, 16383), // back to the middle
	} {
		data = append(data, r...)
	}
	out, err := d.decode(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := describeAll(out), "stick 1,0,stick 1,1,stick 0,1"; got != want {
		t.Fatalf("tablet: %s\n  want: %s", got, want)
	}
}

// TestStickDroppedEvents: after lost events the stick's position is no longer known either,
// so it goes back to rest with the keys, and a mouse counts from rest again.
func TestStickDroppedEvents(t *testing.T) {
	const size = 24
	got := describeAll(decodeAll(t, size,
		record(size, evKey, btnSouth, 1),
		record(size, evRel, relX, mouseReach),
		record(size, evSyn, synDropped, 0),
		record(size, evRel, relX, mouseReach/2),
	))
	if want := "down Space,stick 1,0,up Space,stick 0,0,stick 0.4117647,0"; got != want {
		t.Fatalf("dropped: %s\n   want: %s", got, want)
	}
}
