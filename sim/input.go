package sim

import (
	"fmt"
	"strings"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/gmath"
)

// Button is one of the console's eight game buttons. Home is not one: it returns to the
// console's home and never reaches a game.
type Button uint8

// The buttons, in the order of asset.ButtonNames.
const (
	ButtonUp     Button = iota // D-pad up
	ButtonDown                 // D-pad down
	ButtonLeft                 // D-pad left
	ButtonRight                // D-pad right
	ButtonA                    // A: the main action, confirm
	ButtonB                    // B: the second action
	ButtonSelect               // Select: the game's menu
	ButtonCancel               // Cancel: back, close a menu

	NumButtons = 8
)

func init() {
	if len(asset.ButtonNames) != NumButtons {
		panic("sim: asset.ButtonNames and the Button constants disagree")
	}
}

// String returns the button's name (asset.ButtonNames).
func (b Button) String() string {
	if b < NumButtons {
		return asset.ButtonNames[b]
	}
	return fmt.Sprintf("Button(%d)", uint8(b))
}

// ParseButton returns the button called name (up, down, left, right, a, b, select, cancel).
func ParseButton(name string) (Button, error) {
	if i := asset.ButtonIndex(name); i >= 0 {
		return Button(i), nil
	}
	return 0, fmt.Errorf("unknown button %q (want one of %s)", name, strings.Join(asset.ButtonNames, ", "))
}

// Buttons is a set of buttons. The zero value is empty.
type Buttons uint8

// Of returns the set of the given buttons.
func Of(bs ...Button) Buttons {
	var s Buttons
	for _, b := range bs {
		s |= 1 << b
	}
	return s
}

// Has reports whether b is in the set.
func (s Buttons) Has(b Button) bool { return b < NumButtons && s&(1<<b) != 0 }

// Names returns the names of the buttons in the set, in button order.
func (s Buttons) Names() []string {
	var out []string
	for b := Button(0); b < NumButtons; b++ {
		if s.Has(b) {
			out = append(out, b.String())
		}
	}
	return out
}

// Input is what the player did with the buttons during one tick. It is a value type and is
// identical whether it comes from the console or from a scenario script.
type Input struct {
	Pressed  Buttons // buttons that went down this tick
	Held     Buttons // buttons down at the end of this tick (a tap within the tick is only in Pressed and Released)
	Released Buttons // buttons that went up this tick
}

// Down reports whether b is held.
func (in Input) Down(b Button) bool { return in.Held.Has(b) }

// JustPressed reports whether b went down this tick.
func (in Input) JustPressed(b Button) bool { return in.Pressed.Has(b) }

// JustReleased reports whether b went up this tick.
func (in Input) JustReleased(b Button) bool { return in.Released.Has(b) }

// DPad returns the D-pad as a direction: X is -1 (left), 0 or +1 (right), Y is -1 (down),
// 0 or +1 (up). Opposite directions held together cancel out. A diagonal is (±1, ±1):
// normalize it when moving diagonally must not be faster.
func (in Input) DPad() gmath.Vec2 {
	axis := func(neg, pos Button) float32 {
		var v float32
		if in.Down(neg) {
			v--
		}
		if in.Down(pos) {
			v++
		}
		return v
	}
	return gmath.V2(axis(ButtonLeft, ButtonRight), axis(ButtonDown, ButtonUp))
}

// InputState turns button presses and releases (from the player or a script) into one
// Input per tick.
type InputState struct {
	held     Buttons
	pressed  Buttons
	released Buttons
}

// Press records b going down. A press of a button already held is ignored.
func (s *InputState) Press(b Button) {
	if b >= NumButtons || s.held.Has(b) {
		return
	}
	s.pressed |= 1 << b
	s.held |= 1 << b
}

// Release records b going up. A release of a button not held is ignored.
func (s *InputState) Release(b Button) {
	if !s.held.Has(b) {
		return
	}
	s.released |= 1 << b
	s.held &^= 1 << b
}

// ReleaseAll releases every held button (for example when the player loses its input).
func (s *InputState) ReleaseAll() {
	s.released |= s.held
	s.held = 0
}

// Next returns the Input of the tick that just ended and starts a new tick.
func (s *InputState) Next() Input {
	in := Input{Pressed: s.pressed, Held: s.held, Released: s.released}
	s.pressed, s.released = 0, 0
	return in
}
