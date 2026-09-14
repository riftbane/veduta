package sim

import (
	"math"
	"strings"
	"testing"

	"github.com/riftbane/veduta/gmath"
)

func TestKeySet(t *testing.T) {
	var k KeySet
	if !k.Add("KeyW") || !k.Add("Space") || k.Add("w") {
		t.Fatal("Add results wrong")
	}
	if !k.Has("KeyW") || k.Has("KeyA") || k.Len() != 2 {
		t.Fatal("Has/Len wrong")
	}
	if got := strings.Join(k.Codes(), ","); got != "KeyW,Space" {
		t.Fatalf("Codes = %s", got)
	}
	k.Remove("KeyW")
	if k.Has("KeyW") || k.Empty() {
		t.Fatal("Remove wrong")
	}
}

func TestInputStateEdges(t *testing.T) {
	var s InputState
	s.KeyDown("KeyW")
	in := s.Next()
	if !in.JustPressed("KeyW") || !in.Down("KeyW") {
		t.Fatal("press edge missing")
	}
	s.KeyDown("KeyW") // auto-repeat while held: not a new press
	in = s.Next()
	if in.JustPressed("KeyW") || !in.Down("KeyW") {
		t.Fatal("repeat must not re-press")
	}
	s.KeyUp("KeyW")
	in = s.Next()
	if !in.JustReleased("KeyW") || in.Down("KeyW") {
		t.Fatal("release edge missing")
	}
	s.KeyDown("Space")
	s.KeyUp("Space") // tap within one tick
	in = s.Next()
	if !in.JustPressed("Space") || !in.JustReleased("Space") || in.Down("Space") {
		t.Fatal("tap within a tick must be visible as press+release")
	}
	s.ButtonDown(ButtonLeft)
	s.MouseMove(10, 20)
	s.TypeText("hi")
	in = s.Next()
	if !in.Button("left") || in.ButtonsPressed != ButtonLeft || in.Mouse != gmath.V2(10, 20) || in.Text != "hi" {
		t.Fatalf("mouse/text: %+v", in)
	}
	if in = s.Next(); in.Text != "" || in.ButtonsPressed != 0 || !in.Button("left") {
		t.Fatal("per-tick fields not cleared")
	}
	if in.Axis("KeyA", "KeyD") != 0 {
		t.Fatal("axis")
	}
}

func TestMouseDelta(t *testing.T) {
	var s InputState
	if in := s.Next(); in.MouseDelta != (gmath.Vec2{}) {
		t.Fatalf("first tick delta %v, want zero", in.MouseDelta)
	}
	s.MouseMove(100, 40)
	in := s.Next()
	if in.Mouse != gmath.V2(100, 40) || in.MouseDelta != gmath.V2(100, 40) {
		t.Fatalf("moved tick: mouse %v delta %v", in.Mouse, in.MouseDelta)
	}
	if in = s.Next(); in.Mouse != gmath.V2(100, 40) || in.MouseDelta != (gmath.Vec2{}) {
		t.Fatalf("still tick: mouse %v delta %v, want zero delta", in.Mouse, in.MouseDelta)
	}
	// Several moves within one tick are one delta.
	s.MouseMove(110, 40)
	s.MouseMove(90, 60)
	if in = s.Next(); in.MouseDelta != gmath.V2(-10, 20) {
		t.Fatalf("merged delta %v, want (-10, 20)", in.MouseDelta)
	}
	// Releasing everything (lost focus) is not a mouse jump.
	s.ReleaseAll()
	if in = s.Next(); in.MouseDelta != (gmath.Vec2{}) {
		t.Fatalf("delta after ReleaseAll %v, want zero", in.MouseDelta)
	}
}

// TestStick: the stick is a position, not an edge. It holds until moved, is copied into
// every tick's Input, and goes back to rest when input is lost.
func TestStick(t *testing.T) {
	var s InputState
	if in := s.Next(); in.Stick != (gmath.Vec2{}) {
		t.Fatalf("stick at start %v, want rest", in.Stick)
	}
	s.SetStick(0.25, -1)
	s.SetStick(1, 0.5) // the last position of a tick is the one seen
	if in := s.Next(); in.Stick != gmath.V2(1, 0.5) {
		t.Fatalf("moved stick %v", in.Stick)
	}
	if in := s.Next(); in.Stick != gmath.V2(1, 0.5) {
		t.Fatalf("held stick %v, want it where it was left", in.Stick)
	}
	s.ReleaseAll()
	if in := s.Next(); in.Stick != (gmath.Vec2{}) {
		t.Fatalf("stick after ReleaseAll %v, want rest", in.Stick)
	}
}

func TestScriptStick(t *testing.T) {
	right, back := gmath.V2(1, 0), gmath.V2(0, 0)
	sc, err := NewScript([]InputEvent{{Tick: 3, Stick: &right}, {Tick: 6, Stick: &back}})
	if err != nil {
		t.Fatal(err)
	}
	for tick := uint64(1); tick <= 4; tick++ {
		in := sc.Input(tick)
		if want := map[bool]gmath.Vec2{true: right}[tick >= 3]; in.Stick != want {
			t.Fatalf("tick %d: stick %v, want %v", tick, in.Stick, want)
		}
	}
	// A snapshot between ticks resumes with the stick where it was.
	st := sc.State()
	resumed, _ := NewScript(sc.Events())
	resumed.SetState(st)
	for tick := uint64(5); tick <= 6; tick++ {
		a, b := sc.Input(tick), resumed.Input(tick)
		if a.Stick != b.Stick {
			t.Fatalf("tick %d: resumed stick %v, original %v", tick, b.Stick, a.Stick)
		}
		if want := map[bool]gmath.Vec2{true: right, false: back}[tick < 6]; a.Stick != want {
			t.Fatalf("tick %d: stick %v, want %v", tick, a.Stick, want)
		}
	}
	nan := gmath.V2(float32(math.NaN()), 0)
	for _, bad := range []gmath.Vec2{gmath.V2(1.5, 0), gmath.V2(0, -1.01), nan} {
		if _, err := NewScript([]InputEvent{{Tick: 1, Stick: &bad}}); err == nil {
			t.Errorf("stick %v accepted", bad)
		}
	}
}

func TestScriptTimeline(t *testing.T) {
	mouse := gmath.V2(5, 6)
	sc, err := NewScript([]InputEvent{
		{Tick: 0, Press: []string{"KeyD"}},
		{Tick: 10, Press: []string{"KeyW"}},
		{Tick: 70, Release: []string{"KeyW"}},
		{Tick: 80, Press: []string{"Space"}, Mouse: &mouse, Buttons: []string{"right"}},
		{Tick: 81, Release: []string{"Space"}, Buttons: []string{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for tick := uint64(1); tick <= 100; tick++ {
		in := sc.Input(tick)
		switch tick {
		case 1:
			if !in.JustPressed("KeyD") || in.Axis("KeyA", "KeyD") != 1 {
				t.Fatal("tick-0 events must appear at tick 1")
			}
		case 10:
			if !in.JustPressed("KeyW") {
				t.Fatal("KeyW not pressed at 10")
			}
		case 69:
			if !in.Down("KeyW") || in.JustPressed("KeyW") {
				t.Fatal("KeyW should be held at 69")
			}
		case 70:
			if !in.JustReleased("KeyW") || in.Down("KeyW") {
				t.Fatal("KeyW not released at 70")
			}
		case 80:
			if !in.JustPressed("Space") || !in.Button("right") || in.Mouse != mouse {
				t.Fatalf("tick 80: %+v", in)
			}
		case 81:
			if in.Button("right") || in.ButtonsReleased != ButtonRight {
				t.Fatal("empty buttons list must release all")
			}
		}
	}
	for _, bad := range [][]InputEvent{
		{{Tick: 5}, {Tick: 4}},
		{{Tick: 1, Press: []string{"w"}}},
		{{Tick: 1, Buttons: []string{"side"}}},
	} {
		if _, err := NewScript(bad); err == nil {
			t.Errorf("script %+v accepted", bad)
		}
	}
}

func TestScriptStateRoundTrip(t *testing.T) {
	events := []InputEvent{{Tick: 2, Press: []string{"KeyA"}}, {Tick: 5, Release: []string{"KeyA"}}, {Tick: 6, Press: []string{"KeyB"}}}
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
