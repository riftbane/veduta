package sim

import "fmt"

// InputEvent is one entry of an input script: at Tick, press and release buttons.
type InputEvent struct {
	Tick    uint64
	Press   Buttons
	Release Buttons
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

// NewScript validates events (ticks non-decreasing) and returns a script positioned before
// tick 1.
func NewScript(events []InputEvent) (*Script, error) {
	var prev uint64
	for i, e := range events {
		if e.Tick < prev {
			return nil, fmt.Errorf("input event %d: tick %d before tick %d", i, e.Tick, prev)
		}
		prev = e.Tick
	}
	return &Script{events: events}, nil
}

// Input returns the Input of tick (which must increase from call to call, starting at 1).
func (s *Script) Input(tick uint64) Input {
	for s.pos < len(s.events) && s.events[s.pos].Tick <= tick {
		e := &s.events[s.pos]
		for b := Button(0); b < NumButtons; b++ {
			if e.Press.Has(b) {
				s.st.Press(b)
			}
		}
		for b := Button(0); b < NumButtons; b++ {
			if e.Release.Has(b) {
				s.st.Release(b)
			}
		}
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
	Held            Buttons
	LastTick        uint64
	PendingPressed  Buttons
	PendingReleased Buttons
}

// State returns the script position (between ticks).
func (s *Script) State() ScriptState {
	return ScriptState{Pos: s.pos, Held: s.st.held, LastTick: s.last, PendingPressed: s.st.pressed, PendingReleased: s.st.released}
}

// SetState restores a position returned by State.
func (s *Script) SetState(st ScriptState) {
	s.pos = st.Pos
	s.last = st.LastTick
	s.st = InputState{held: st.Held, pressed: st.PendingPressed, released: st.PendingReleased}
}
