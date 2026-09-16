package sim

import (
	"strings"
	"testing"

	"github.com/riftbane/veduta/v2/gmath"
)

func TestButtons(t *testing.T) {
	s := Of(ButtonA, ButtonUp)
	if !s.Has(ButtonA) || !s.Has(ButtonUp) || s.Has(ButtonB) || s.Has(NumButtons) {
		t.Fatal("Has wrong")
	}
	if got := strings.Join(s.Names(), ","); got != "up,a" {
		t.Fatalf("Names = %s", got)
	}
	for b := Button(0); b < NumButtons; b++ {
		if p, err := ParseButton(b.String()); err != nil || p != b {
			t.Errorf("ParseButton(%q) = %v, %v", b.String(), p, err)
		}
	}
	if _, err := ParseButton("start"); err == nil {
		t.Error("ParseButton accepted start")
	}
}

func TestInputStateEdges(t *testing.T) {
	var s InputState
	s.Press(ButtonUp)
	in := s.Next()
	if !in.JustPressed(ButtonUp) || !in.Down(ButtonUp) {
		t.Fatal("press edge missing")
	}
	s.Press(ButtonUp) // a second press while held is not a new press
	in = s.Next()
	if in.JustPressed(ButtonUp) || !in.Down(ButtonUp) {
		t.Fatal("repeat must not re-press")
	}
	s.Release(ButtonUp)
	in = s.Next()
	if !in.JustReleased(ButtonUp) || in.Down(ButtonUp) {
		t.Fatal("release edge missing")
	}
	s.Press(ButtonA)
	s.Release(ButtonA) // tap within one tick
	in = s.Next()
	if !in.JustPressed(ButtonA) || !in.JustReleased(ButtonA) || in.Down(ButtonA) {
		t.Fatal("tap within a tick must be visible as press+release")
	}
	if in = s.Next(); in != (Input{}) {
		t.Fatalf("per-tick fields not cleared: %+v", in)
	}
	s.Press(ButtonB)
	s.Press(ButtonLeft)
	s.Next()
	s.ReleaseAll()
	if in = s.Next(); in.Held != 0 || in.Released != Of(ButtonB, ButtonLeft) {
		t.Fatalf("ReleaseAll: %+v", in)
	}
}

func TestDPad(t *testing.T) {
	cases := []struct {
		held Buttons
		want gmath.Vec2
	}{
		{0, gmath.V2(0, 0)},
		{Of(ButtonUp), gmath.V2(0, 1)},
		{Of(ButtonDown, ButtonRight), gmath.V2(1, -1)},
		{Of(ButtonLeft, ButtonRight, ButtonUp), gmath.V2(0, 1)},
	}
	for _, c := range cases {
		if got := (Input{Held: c.held}).DPad(); got != c.want {
			t.Errorf("DPad(%v) = %v, want %v", c.held.Names(), got, c.want)
		}
	}
}

func TestScriptTimeline(t *testing.T) {
	sc, err := NewScript([]InputEvent{
		{Tick: 0, Press: Of(ButtonRight)},
		{Tick: 10, Press: Of(ButtonUp)},
		{Tick: 70, Release: Of(ButtonUp)},
		{Tick: 80, Press: Of(ButtonA)},
		{Tick: 81, Release: Of(ButtonA, ButtonRight)},
	})
	if err != nil {
		t.Fatal(err)
	}
	for tick := uint64(1); tick <= 100; tick++ {
		in := sc.Input(tick)
		switch tick {
		case 1:
			if !in.JustPressed(ButtonRight) || in.DPad().X != 1 {
				t.Fatal("tick-0 events must appear at tick 1")
			}
		case 10:
			if !in.JustPressed(ButtonUp) {
				t.Fatal("up not pressed at 10")
			}
		case 69:
			if !in.Down(ButtonUp) || in.JustPressed(ButtonUp) {
				t.Fatal("up should be held at 69")
			}
		case 70:
			if !in.JustReleased(ButtonUp) || in.Down(ButtonUp) {
				t.Fatal("up not released at 70")
			}
		case 80:
			if !in.JustPressed(ButtonA) {
				t.Fatalf("tick 80: %+v", in)
			}
		case 81:
			if in.Held != 0 || in.Released != Of(ButtonA, ButtonRight) {
				t.Fatalf("tick 81: %+v", in)
			}
		}
	}
	if _, err := NewScript([]InputEvent{{Tick: 5}, {Tick: 4}}); err == nil {
		t.Error("script out of tick order accepted")
	}
}

func TestScriptStateRoundTrip(t *testing.T) {
	events := []InputEvent{{Tick: 2, Press: Of(ButtonLeft)}, {Tick: 5, Release: Of(ButtonLeft)}, {Tick: 6, Press: Of(ButtonB)}}
	a, _ := NewScript(events)
	for tick := uint64(1); tick <= 3; tick++ {
		a.Input(tick)
	}
	st := a.State()
	var want []Input
	for tick := uint64(4); tick <= 8; tick++ {
		want = append(want, a.Input(tick))
	}
	b, _ := NewScript(events)
	b.SetState(st)
	for i, tick := 0, uint64(4); tick <= 8; i, tick = i+1, tick+1 {
		if got := b.Input(tick); got != want[i] {
			t.Fatalf("tick %d after restore: %+v, want %+v", tick, got, want[i])
		}
	}
}
