package platform

import (
	"testing"

	"github.com/riftbane/veduta/v2/sim"
)

func TestXInputButtons(t *testing.T) {
	b, home := xinputButtons(xinputDPadUp | xinputA | xinputBack | 0x4000 /* X: nothing */)
	if b != sim.Of(sim.ButtonUp, sim.ButtonA, sim.ButtonSelect) || home {
		t.Fatalf("buttons %v home %v", b.Names(), home)
	}
	if _, home := xinputButtons(xinputBack | xinputStart); !home {
		t.Fatal("Back+Start is not Home")
	}
}

func TestJoystickButtons(t *testing.T) {
	for _, c := range []struct {
		name string
		j    joystick
		want sim.Buttons
		home bool
	}{
		{"at rest", joystick{x: 32767, y: 32767, pov: 0xFFFF}, 0, false},
		{"axes left and down", joystick{x: 0, y: 65535, pov: 0xFFFF}, sim.Of(sim.ButtonLeft, sim.ButtonDown), false},
		{"axes inside the dead zone", joystick{x: 40000, y: 25000, pov: 0xFFFF}, 0, false},
		{"hat right", joystick{x: 32767, y: 32767, pov: 9000}, sim.Of(sim.ButtonRight), false},
		{"hat down-left", joystick{x: 32767, y: 32767, pov: 22500}, sim.Of(sim.ButtonDown, sim.ButtonLeft), false},
		{"hat beats the axes", joystick{x: 0, y: 32767, pov: 0}, sim.Of(sim.ButtonUp), false},
		{"A, B, Select, Start", joystick{buttons: 1<<joyA | 1<<joyB | 1<<2 | 1<<joySelect, x: 32767, y: 32767, pov: 0xFFFF},
			sim.Of(sim.ButtonA, sim.ButtonB, sim.ButtonSelect), false},
		{"Select+Start", joystick{buttons: 1<<joySelect | 1<<joyStart, x: 32767, y: 32767, pov: 0xFFFF},
			sim.Of(sim.ButtonSelect, sim.ButtonCancel), true},
	} {
		if b, home := joystickButtons(c.j); b != c.want || home != c.home {
			t.Errorf("%s: %v home %v, want %v home %v", c.name, b.Names(), home, c.want.Names(), c.home)
		}
	}
}

func TestPadTrackerAndMerge(t *testing.T) {
	var pad padTracker
	var m buttonMerge
	merge := func(evs []Event) string {
		var out []Event
		for _, e := range evs {
			out = m.add(out, e)
		}
		return describePad(out)
	}
	if got := merge(pad.update(nil, sim.Of(sim.ButtonLeft, sim.ButtonA), false)); got != "down left,down a" {
		t.Fatalf("pad presses: %s", got)
	}
	if got := merge([]Event{{Kind: Press, Button: sim.ButtonLeft}}); got != "" { // the keyboard too
		t.Fatalf("keyboard on a held button: %s", got)
	}
	if got := merge(pad.update(nil, sim.Of(sim.ButtonA), false)); got != "" {
		t.Fatalf("pad lets go while the keyboard holds: %s", got)
	}
	if got := merge([]Event{{Kind: Release, Button: sim.ButtonLeft}}); got != "up left" {
		t.Fatalf("keyboard lets go: %s", got)
	}
	if got := merge(pad.update(nil, sim.Of(sim.ButtonA, sim.ButtonSelect, sim.ButtonCancel), true)); got != "up a,close" {
		t.Fatalf("Home chord: %s", got)
	}
	if got := merge(pad.update(nil, sim.Of(sim.ButtonSelect, sim.ButtonCancel), true)); got != "" {
		t.Fatalf("chord still held: %s", got)
	}
	if got := merge(pad.update(nil, 0, false)); got != "" {
		t.Fatalf("chord let go: %s", got)
	}
}
