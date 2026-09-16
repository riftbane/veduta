package platform

import (
	"fmt"
	"strings"
	"testing"
)

// describePad describes buttons and closing as "down a,up left,close".
func describePad(evs []Event) string {
	var s []string
	for _, e := range evs {
		switch e.Kind {
		case Press:
			s = append(s, "down "+e.Button.String())
		case Release:
			s = append(s, "up "+e.Button.String())
		case Close:
			s = append(s, "close")
		case Tool:
			s = append(s, fmt.Sprintf("tool %d", e.Tool))
		default:
			s = append(s, "other")
		}
	}
	return strings.Join(s, ",")
}

func TestFitScale(t *testing.T) {
	for _, c := range []struct{ cw, ch, scale, x, y int }{
		{320, 240, 1, 0, 0},
		{960, 720, 3, 0, 0},
		{1000, 720, 3, 20, 0},
		{960, 800, 3, 0, 40},
		{640, 720, 2, 0, 120},
		{100, 100, 1, -110, -70}, // smaller than the frame: scale 1, cropped around the middle
	} {
		if s, x, y := fitScale(c.cw, c.ch, 320, 240); s != c.scale || x != c.x || y != c.y {
			t.Errorf("fitScale(%d, %d) = %d, %d, %d; want %d, %d, %d", c.cw, c.ch, s, x, y, c.scale, c.x, c.y)
		}
	}
}

func TestPanelColors(t *testing.T) {
	src := []uint32{0xffffffff, 0x00000000, 0xff123456, 0x80f0f0f0}
	dst := make([]uint32, len(src))
	panelColors(dst, src)
	want := []uint32{0xffffffff, 0xff000000, 0xff103452, 0xfff7f3f7}
	for i := range want {
		if dst[i] != want[i] {
			t.Errorf("pixel %d: %#08x, want %#08x", i, dst[i], want[i])
		}
	}
}

func TestSimKeyboard(t *testing.T) {
	var k keyboard
	var out []Event
	out = k.key(out, scanUp, true)
	out = k.key(out, scanUp, true) // repeat
	out = k.key(out, scanW, true)  // up held twice
	out = k.key(out, scanUp, false)
	out = k.key(out, scanSpace, true)
	out = k.key(out, 0x3b, true) // F1: a tool key
	out = k.key(out, 0x3b, false)
	out = k.key(out, 0x44, true) // F10: nothing
	if got, want := describePad(out), "down up,down a,tool 1"; got != want {
		t.Fatalf("keys: %s, want %s", got, want)
	}
	out = k.key(out[:0], scanW, false)
	out = k.key(out, scanRCtrl, true)
	out = k.key(out, scanQ, true)
	if got, want := describePad(out), "up up,up a,close"; got != want {
		t.Fatalf("Ctrl+Q: %s, want %s", got, want)
	}
	if out = k.key(out[:0], scanSpace, false); len(out) != 0 {
		t.Fatalf("a release after Close: %s", describePad(out))
	}
}
