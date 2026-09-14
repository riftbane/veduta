package sim

import (
	"fmt"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/gmath"
)

// InputEvent is one entry of an input script: at Tick, press and release keys, move the
// mouse, set the held mouse buttons (nil: unchanged, empty: release all), type text and
// move the stick (nil: unchanged).
type InputEvent struct {
	Tick    uint64
	Press   []string
	Release []string
	Mouse   *gmath.Vec2
	Buttons []string
	Text    string
	Stick   *gmath.Vec2
}

// Script replays input events deterministically. Events are applied at the start of
// their tick, so an event at tick t shows up in the Input of tick t; events at tick 0
// are applied before the first update and appear in the Input of tick 1.
type Script struct {
	events []InputEvent
	pos    int
	st     InputState
	last   uint64
}

// NewScript validates events (ticks non-decreasing, known keys and buttons, stick axes in
// -1…1) and returns a script positioned before tick 1.
func NewScript(events []InputEvent) (*Script, error) {
	var prev uint64
	for i, e := range events {
		if e.Tick < prev {
			return nil, fmt.Errorf("input event %d: tick %d before tick %d", i, e.Tick, prev)
		}
		prev = e.Tick
		for _, k := range append(append([]string(nil), e.Press...), e.Release...) {
			if !asset.IsKeyCode(k) {
				return nil, fmt.Errorf("input event %d: unknown key %q", i, k)
			}
		}
		for _, b := range e.Buttons {
			if _, err := ParseButton(b); err != nil {
				return nil, fmt.Errorf("input event %d: %w", i, err)
			}
		}
		if st := e.Stick; st != nil && !(st.X >= -1 && st.X <= 1 && st.Y >= -1 && st.Y <= 1) {
			return nil, fmt.Errorf("input event %d: stick %v outside -1…1", i, *st)
		}
	}
	return &Script{events: events}, nil
}

// Input returns the Input of tick (which must increase from call to call, starting at 1).
func (s *Script) Input(tick uint64) Input {
	for s.pos < len(s.events) && s.events[s.pos].Tick <= tick {
		e := &s.events[s.pos]
		for _, k := range e.Press {
			s.st.KeyDown(k)
		}
		for _, k := range e.Release {
			s.st.KeyUp(k)
		}
		if e.Mouse != nil {
			s.st.MouseMove(e.Mouse.X, e.Mouse.Y)
		}
		if e.Buttons != nil {
			var set ButtonSet
			for _, b := range e.Buttons {
				x, _ := ParseButton(b)
				set |= x
			}
			s.st.SetButtons(set)
		}
		if e.Stick != nil {
			s.st.SetStick(e.Stick.X, e.Stick.Y)
		}
		s.st.TypeText(e.Text)
		s.pos++
	}
	s.last = tick
	return s.st.Next()
}

// Events returns the script's events.
func (s *Script) Events() []InputEvent { return s.events }

// ScriptState is the resumable position of a script, for snapshots.
type ScriptState struct {
	Pos             int
	Held            KeySet
	Buttons         ButtonSet
	Mouse           gmath.Vec2
	LastMouse       gmath.Vec2 // cursor at the end of the previous tick (MouseDelta)
	LastTick        uint64
	PendingPressed  KeySet
	PendingReleased KeySet
	Stick           gmath.Vec2
}

// State returns the script position (between ticks).
func (s *Script) State() ScriptState {
	return ScriptState{Pos: s.pos, Held: s.st.held, Buttons: s.st.buttons, Mouse: s.st.mouse, LastMouse: s.st.lastMouse, LastTick: s.last, PendingPressed: s.st.pressed, PendingReleased: s.st.released, Stick: s.st.stick}
}

// SetState restores a position returned by State.
func (s *Script) SetState(st ScriptState) {
	s.pos = st.Pos
	s.last = st.LastTick
	s.st = InputState{held: st.Held, buttons: st.Buttons, mouse: st.Mouse, lastMouse: st.LastMouse, pressed: st.PendingPressed, released: st.PendingReleased, stick: st.Stick}
}
