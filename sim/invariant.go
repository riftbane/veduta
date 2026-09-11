package sim

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/riftbane/veduta/gmath"
	"github.com/riftbane/veduta/scene"
)

// Built-in invariant names (spec §6.5). Parameterized ones are written name:args.
const (
	InvFinitePositions = "finite_positions"
	InvWithinBounds    = "within_bounds"
	InvEntityCountMax  = "entity_count_max" // entity_count_max:N
	InvNoOverlap       = "no_overlap"       // no_overlap:tagA,tagB
)

// Invariant is a named predicate over the scene, evaluated after every tick. Check
// returns "" when the invariant holds, else a short description of the first violation.
type Invariant struct {
	Name  string
	Check func(s *scene.Scene) string
}

// Violation is an invariant that failed at a tick.
type Violation struct {
	Tick   uint64 `json:"tick"`
	Name   string `json:"name"`
	Detail string `json:"detail"`
}

// ParseInvariant resolves an invariant spec: a built-in (with arguments where needed)
// or the name of a game-registered predicate in custom. bounds is the project bounds
// used by within_bounds.
func ParseInvariant(spec string, bounds gmath.AABB, custom map[string]func() bool) (Invariant, error) {
	name, arg, hasArg := strings.Cut(spec, ":")
	switch name {
	case InvFinitePositions:
		if hasArg {
			break
		}
		return Invariant{Name: spec, Check: finitePositions}, nil
	case InvWithinBounds:
		if hasArg {
			break
		}
		return Invariant{Name: spec, Check: func(s *scene.Scene) string { return withinBounds(s, bounds) }}, nil
	case InvEntityCountMax:
		n, err := strconv.Atoi(arg)
		if !hasArg || err != nil || n <= 0 {
			return Invariant{}, fmt.Errorf("invariant %q: want %s:N with N > 0", spec, InvEntityCountMax)
		}
		return Invariant{Name: spec, Check: func(s *scene.Scene) string {
			if c := s.Len(); c > n {
				return fmt.Sprintf("%d entities, max %d", c, n)
			}
			return ""
		}}, nil
	case InvNoOverlap:
		a, b, ok := strings.Cut(arg, ",")
		if !hasArg || !ok || a == "" || b == "" {
			return Invariant{}, fmt.Errorf("invariant %q: want %s:tagA,tagB", spec, InvNoOverlap)
		}
		return Invariant{Name: spec, Check: func(s *scene.Scene) string { return noOverlap(s, a, b) }}, nil
	default:
		if pred, ok := custom[spec]; ok && !hasArg {
			return Invariant{Name: spec, Check: func(*scene.Scene) string {
				if pred() {
					return ""
				}
				return "predicate returned false"
			}}, nil
		}
		return Invariant{}, fmt.Errorf("unknown invariant %q", spec)
	}
	return Invariant{}, fmt.Errorf("invariant %q takes no arguments", spec)
}

func finitePositions(s *scene.Scene) string {
	for _, e := range s.Entities() {
		if !e.Alive() {
			continue
		}
		if p := e.WorldPosition(); !p.IsFinite() {
			return fmt.Sprintf("entity %s position %s", e.Name, fmtVec(p))
		}
		if !e.AABB.IsEmpty() && !(e.AABB.Min.IsFinite() && e.AABB.Max.IsFinite()) {
			return fmt.Sprintf("entity %s bounds are not finite", e.Name)
		}
	}
	return ""
}

func withinBounds(s *scene.Scene, b gmath.AABB) string {
	for _, e := range s.Entities() {
		if !e.Alive() {
			continue
		}
		if p := e.WorldPosition(); !b.Contains(p) {
			return fmt.Sprintf("entity %s at %s outside %s", e.Name, fmtVec(p), fmtBox(b))
		}
	}
	return ""
}

func noOverlap(s *scene.Scene, a, b string) string {
	for _, x := range s.Entities() {
		if !x.Alive() || !x.HasTag(a) || x.AABB.IsEmpty() {
			continue
		}
		for _, y := range s.Entities() {
			if y == x || !y.Alive() || !y.HasTag(b) || y.AABB.IsEmpty() {
				continue
			}
			if x.AABB.Overlaps(y.AABB) {
				return fmt.Sprintf("%s (%s) overlaps %s (%s)", x.Name, a, y.Name, b)
			}
		}
	}
	return ""
}

func fmtVec(v gmath.Vec3) string {
	b, _ := AppendCanonical(nil, v)
	return string(b)
}

func fmtBox(b gmath.AABB) string { return "[" + fmtVec(b.Min) + "," + fmtVec(b.Max) + "]" }

// Monitor evaluates invariants after each tick. It reports a violation when an
// invariant starts failing (not again while it keeps failing) and remembers the first
// violation of each invariant.
type Monitor struct {
	list    []Invariant
	failing []bool
	first   []Violation // first violation per invariant, in list order
	total   []int       // ticks spent failing
}

// NewMonitor returns a monitor for the given invariants, checked in order.
func NewMonitor(list []Invariant) *Monitor {
	return &Monitor{list: list, failing: make([]bool, len(list)), first: make([]Violation, len(list)), total: make([]int, len(list))}
}

// Check evaluates every invariant and returns the ones that started failing this tick.
func (m *Monitor) Check(tick uint64, s *scene.Scene) []Violation {
	var out []Violation
	for i, inv := range m.list {
		d := inv.Check(s)
		if d == "" {
			m.failing[i] = false
			continue
		}
		m.total[i]++
		if m.failing[i] {
			continue
		}
		m.failing[i] = true
		v := Violation{Tick: tick, Name: inv.Name, Detail: d}
		if m.first[i].Name == "" {
			m.first[i] = v
		}
		out = append(out, v)
	}
	return out
}

// First returns the first violation of every invariant that failed, in list order.
func (m *Monitor) First() []Violation {
	var out []Violation
	for _, v := range m.first {
		if v.Name != "" {
			out = append(out, v)
		}
	}
	return out
}

// FailingTicks returns how many ticks invariant i (list order) failed.
func (m *Monitor) FailingTicks(i int) int { return m.total[i] }

// Names returns the invariant names in list order.
func (m *Monitor) Names() []string {
	out := make([]string, len(m.list))
	for i, inv := range m.list {
		out[i] = inv.Name
	}
	return out
}

// FailingState returns which invariants are currently failing, for snapshots.
func (m *Monitor) FailingState() []bool { return append([]bool(nil), m.failing...) }

// SetFailingState restores the state returned by FailingState.
func (m *Monitor) SetFailingState(f []bool) { copy(m.failing, f) }
