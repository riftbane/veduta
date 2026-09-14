package sim

import (
	"fmt"
	"math/bits"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/gmath"
)

// keyIndex maps a W3C KeyboardEvent.code to its bit in a KeySet.
var keyIndex = func() map[string]int {
	if len(asset.KeyCodes) > 128 {
		panic("sim: more than 128 key codes")
	}
	m := make(map[string]int, len(asset.KeyCodes))
	for i, k := range asset.KeyCodes {
		m[k] = i
	}
	return m
}()

// KeySet is a set of keys identified by W3C KeyboardEvent.code names (see
// asset.KeyCodes). The zero value is empty.
type KeySet [2]uint64

// Add inserts code and reports whether it is a known key.
func (k *KeySet) Add(code string) bool {
	i, ok := keyIndex[code]
	if ok {
		k[i>>6] |= 1 << (i & 63)
	}
	return ok
}

// Remove deletes code.
func (k *KeySet) Remove(code string) {
	if i, ok := keyIndex[code]; ok {
		k[i>>6] &^= 1 << (i & 63)
	}
}

// Has reports whether code is in the set.
func (k KeySet) Has(code string) bool {
	i, ok := keyIndex[code]
	return ok && k[i>>6]&(1<<(i&63)) != 0
}

// Empty reports whether the set is empty.
func (k KeySet) Empty() bool { return k[0]|k[1] == 0 }

// Len returns the number of keys in the set.
func (k KeySet) Len() int { return bits.OnesCount64(k[0]) + bits.OnesCount64(k[1]) }

// Codes returns the keys in asset.KeyCodes order.
func (k KeySet) Codes() []string {
	var out []string
	for i, c := range asset.KeyCodes {
		if k[i>>6]&(1<<(i&63)) != 0 {
			out = append(out, c)
		}
	}
	return out
}

func (k KeySet) union(o KeySet) KeySet   { return KeySet{k[0] | o[0], k[1] | o[1]} }
func (k KeySet) without(o KeySet) KeySet { return KeySet{k[0] &^ o[0], k[1] &^ o[1]} }

// ButtonSet is a set of mouse buttons.
type ButtonSet uint8

// Mouse buttons.
const (
	ButtonLeft ButtonSet = 1 << iota
	ButtonMiddle
	ButtonRight
)

// ParseButton returns the button named left, middle or right.
func ParseButton(name string) (ButtonSet, error) {
	switch name {
	case "left":
		return ButtonLeft, nil
	case "middle":
		return ButtonMiddle, nil
	case "right":
		return ButtonRight, nil
	}
	return 0, fmt.Errorf("unknown mouse button %q (want left, middle or right)", name)
}

// Has reports whether name (left, middle, right) is in the set.
func (b ButtonSet) Has(name string) bool {
	x, err := ParseButton(name)
	return err == nil && b&x != 0
}

// Input is everything the player did during one tick. It is a value type and is
// identical whether it comes from the console or from a scenario script.
type Input struct {
	Pressed         KeySet     // keys that went down this tick
	Held            KeySet     // keys down at the end of this tick (a tap within the tick is only in Pressed and Released)
	Released        KeySet     // keys that went up this tick
	Mouse           gmath.Vec2 // cursor position in frame pixels (origin top-left)
	MouseDelta      gmath.Vec2 // cursor movement during this tick, in pixels (mouse look)
	Buttons         ButtonSet  // mouse buttons held
	ButtonsPressed  ButtonSet
	ButtonsReleased ButtonSet
	Text            string     // characters typed this tick
	Stick           gmath.Vec2 // the analog stick: each axis -1…1, +X right, +Y up, zero at rest
}

// Down reports whether key code is held.
func (in Input) Down(code string) bool { return in.Held.Has(code) }

// JustPressed reports whether key code went down this tick.
func (in Input) JustPressed(code string) bool { return in.Pressed.Has(code) }

// JustReleased reports whether key code went up this tick.
func (in Input) JustReleased(code string) bool { return in.Released.Has(code) }

// Button reports whether mouse button name (left, middle, right) is held.
func (in Input) Button(name string) bool { return in.Buttons.Has(name) }

// Axis returns -1 when only neg is held, +1 when only pos is held, else 0. For example
// in.Axis("KeyA", "KeyD") for strafing.
func (in Input) Axis(neg, pos string) float32 {
	var v float32
	if in.Down(neg) {
		v--
	}
	if in.Down(pos) {
		v++
	}
	return v
}

// InputState turns raw events (from the player or a script) into one Input per tick.
type InputState struct {
	held            KeySet
	pressed         KeySet
	released        KeySet
	buttons         ButtonSet
	buttonsPressed  ButtonSet
	buttonsReleased ButtonSet
	mouse           gmath.Vec2
	lastMouse       gmath.Vec2 // cursor at the end of the previous tick, for MouseDelta
	stick           gmath.Vec2
	text            []byte
}

// KeyDown records a key press; unknown codes are ignored and reported as false.
func (s *InputState) KeyDown(code string) bool {
	var k KeySet
	if !k.Add(code) {
		return false
	}
	if !s.held.Has(code) {
		s.pressed = s.pressed.union(k)
	}
	s.held = s.held.union(k)
	return true
}

// KeyUp records a key release.
func (s *InputState) KeyUp(code string) {
	if s.held.Has(code) {
		var k KeySet
		k.Add(code)
		s.released = s.released.union(k)
		s.held = s.held.without(k)
	}
}

// MouseMove records the cursor position.
func (s *InputState) MouseMove(x, y float32) { s.mouse = gmath.V2(x, y) }

// ButtonDown records a mouse button press.
func (s *InputState) ButtonDown(b ButtonSet) {
	s.buttonsPressed |= b &^ s.buttons
	s.buttons |= b
}

// ButtonUp records a mouse button release.
func (s *InputState) ButtonUp(b ButtonSet) {
	s.buttonsReleased |= b & s.buttons
	s.buttons &^= b
}

// SetButtons makes exactly the buttons in b held.
func (s *InputState) SetButtons(b ButtonSet) {
	s.ButtonUp(s.buttons &^ b)
	s.ButtonDown(b &^ s.buttons)
}

// SetStick records the stick's position: each axis -1…1, +X right, +Y up.
func (s *InputState) SetStick(x, y float32) { s.stick = gmath.V2(x, y) }

// TypeText appends typed characters.
func (s *InputState) TypeText(t string) { s.text = append(s.text, t...) }

// ReleaseAll releases every key and button and lets the stick go back to rest (for example
// when the player loses its input).
func (s *InputState) ReleaseAll() {
	s.released = s.released.union(s.held)
	s.held = KeySet{}
	s.ButtonUp(s.buttons)
	s.stick = gmath.Vec2{}
}

// Next returns the Input of the tick that just ended and starts a new tick.
func (s *InputState) Next() Input {
	in := Input{
		Pressed: s.pressed, Held: s.held, Released: s.released,
		Mouse: s.mouse, MouseDelta: s.mouse.Sub(s.lastMouse), Buttons: s.buttons,
		ButtonsPressed: s.buttonsPressed, ButtonsReleased: s.buttonsReleased,
		Text: string(s.text), Stick: s.stick,
	}
	s.pressed, s.released = KeySet{}, KeySet{}
	s.buttonsPressed, s.buttonsReleased = 0, 0
	s.lastMouse = s.mouse
	s.text = s.text[:0]
	return in
}
