package platform

import "github.com/riftbane/veduta/v2/sim"

// Pads on Windows, read by the simulator: XInput for Xbox controllers, WinMM's joystick API
// for any other pad (the SNES-style USB pads among them). The tables below turn what each
// API reports into the console's buttons, the same buttons a pad presses on the console:
// the D-pad, A and B, Start as the game's menu (Select), and a pad's own Select to leave the
// game, as Home does. They are in a file every GOOS builds, so they are tested everywhere.

// XInput gamepad buttons (XINPUT_GAMEPAD.wButtons).
const (
	xinputDPadUp    = 0x0001
	xinputDPadDown  = 0x0002
	xinputDPadLeft  = 0x0004
	xinputDPadRight = 0x0008
	xinputStart     = 0x0010
	xinputBack      = 0x0020
	xinputA         = 0x1000
	xinputB         = 0x2000
)

// xinputButtons maps an XInput button mask to the console's buttons, and whether Back
// (Home) is held.
func xinputButtons(mask uint16) (sim.Buttons, bool) {
	var b sim.Buttons
	for bit, button := range map[uint16]sim.Button{
		xinputDPadUp: sim.ButtonUp, xinputDPadDown: sim.ButtonDown, xinputDPadLeft: sim.ButtonLeft,
		xinputDPadRight: sim.ButtonRight, xinputA: sim.ButtonA, xinputB: sim.ButtonB,
		xinputStart: sim.ButtonSelect,
	} {
		if mask&bit != 0 {
			b |= sim.Of(button)
		}
	}
	return b, mask&xinputBack != 0
}

// joystick is what joyGetPosEx reports: the button bits (button 1 is bit 0, the order HID
// numbers them), the X and Y axes in 0…65535, and the point-of-view hat in hundredths of
// a degree clockwise from up, or 0xFFFF when centered or absent.
type joystick struct {
	buttons uint32
	x, y    uint32
	pov     uint32
}

// The buttons of a pad read through WinMM, by bit, and of a Linux joystick-style pad, from
// BTN_TRIGGER (padmap_linux.go): the HID order, so a pad plays the same on both. It is the
// order of the cheap SNES-style USB pads: X, A, B, Y, L, R, then Select and Start.
const (
	joyA      = 1
	joyB      = 2
	joySelect = 8
	joyStart  = 9
)

// joystickButtons maps a WinMM report to the console's buttons, and whether Select (Home) is
// held. The D-pad is the hat when it points somewhere, else the X and Y axes,
// which many cheap pads use for it: an axis counts as a direction a quarter of the way from
// the middle to an end.
func joystickButtons(j joystick) (sim.Buttons, bool) {
	var b sim.Buttons
	for bit, button := range map[uint32]sim.Button{joyA: sim.ButtonA, joyB: sim.ButtonB, joyStart: sim.ButtonSelect} {
		if j.buttons&(1<<bit) != 0 {
			b |= sim.Of(button)
		}
	}
	if j.pov != 0xFFFF && j.pov < 36000 {
		// Eight directions, 45° apart; a diagonal presses two buttons.
		switch d := (j.pov + 2250) / 4500 % 8; d {
		case 0:
			b |= sim.Of(sim.ButtonUp)
		case 1:
			b |= sim.Of(sim.ButtonUp, sim.ButtonRight)
		case 2:
			b |= sim.Of(sim.ButtonRight)
		case 3:
			b |= sim.Of(sim.ButtonDown, sim.ButtonRight)
		case 4:
			b |= sim.Of(sim.ButtonDown)
		case 5:
			b |= sim.Of(sim.ButtonDown, sim.ButtonLeft)
		case 6:
			b |= sim.Of(sim.ButtonLeft)
		case 7:
			b |= sim.Of(sim.ButtonUp, sim.ButtonLeft)
		}
	} else {
		const mid, dead = 32767, 16384 // a quarter of the travel
		switch {
		case j.x <= mid-dead:
			b |= sim.Of(sim.ButtonLeft)
		case j.x >= mid+dead:
			b |= sim.Of(sim.ButtonRight)
		}
		switch {
		case j.y <= mid-dead:
			b |= sim.Of(sim.ButtonUp) // Windows' Y grows downwards
		case j.y >= mid+dead:
			b |= sim.Of(sim.ButtonDown)
		}
	}
	return b, j.buttons&(1<<joySelect) != 0
}

// padTracker turns successive reports of one pad into events: a button reported down that
// was up is pressed, and the other way round; Home closes, once per press, after
// releasing what the pad held.
type padTracker struct {
	held sim.Buttons
	home bool
}

func (t *padTracker) update(out []Event, now sim.Buttons, home bool) []Event {
	if home && !t.home {
		t.home = true
		return append(t.update(out, 0, true), Event{Kind: Close})
	}
	t.home = home
	if home {
		now = 0 // nothing held with Home reaches the game
	}
	for b := sim.Button(0); b < sim.NumButtons; b++ {
		switch was, is := t.held.Has(b), now.Has(b); {
		case is && !was:
			out = append(out, Event{Kind: Press, Button: b})
		case was && !is:
			out = append(out, Event{Kind: Release, Button: b})
		}
	}
	t.held = now
	return out
}

// buttonMerge combines the events of several sources — the keyboard and each pad — so that
// a button two of them hold is pressed once and released with the last.
type buttonMerge struct {
	holds [sim.NumButtons]int
}

func (m *buttonMerge) add(out []Event, e Event) []Event {
	switch e.Kind {
	case Press:
		if m.holds[e.Button]++; m.holds[e.Button] == 1 {
			out = append(out, e)
		}
	case Release:
		if m.holds[e.Button] == 0 {
			return out
		}
		if m.holds[e.Button]--; m.holds[e.Button] == 0 {
			out = append(out, e)
		}
	default:
		out = append(out, e)
	}
	return out
}
