package asset

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/riftbane/veduta/gmath"
)

// MaxTicks is the longest scenario, in ticks (about 13.9 hours at 20 Hz).
const MaxTicks = 1_000_000

// Scenario is a compiled scenario test (tests/scenarios/<name>.scenario.json): a scene
// simulated for Ticks ticks from Seed with scripted inputs, checked by expectations and
// invariants, with screenshots at chosen ticks.
//
// Tick 0 is the scene just loaded; ticks 1…Ticks each run one Update. State "at tick t"
// is the state after the Update of tick t (tick 0: the initial state).
type Scenario struct {
	Name        string
	Scene       string        // scene asset name
	Seed        uint64        // seed of the simulation RNG
	Ticks       int           // number of ticks to simulate, 1…MaxTicks
	Inputs      []Input       // in non-decreasing tick order (file order)
	Expect      []Expectation // file order
	Invariants  []string      // invariant specs, see ParseInvariant
	Screenshots []int         // ticks to capture, strictly increasing
}

// Input is one scripted input event. Keys are W3C KeyboardEvent.code names (KeyCodes).
// Events of tick t are applied to the Update of tick t, in file order; events at tick 0
// are applied to the first Update (tick 1), since no Update runs at tick 0.
type Input struct {
	Tick    int
	Press   []string    // keys that go down at this tick and stay held until released
	Release []string    // held keys that go up at this tick
	Buttons []string    // nil: unchanged; otherwise the mouse buttons held from now on (left, right, middle)
	Mouse   *gmath.Vec2 // nil: unchanged; otherwise the mouse position in frame pixels
	Text    string      // characters typed during this tick
}

// Expectation is one check of a scenario. It is either an entity comparison (Entity,
// Path, Op, Value set; Trace empty) or a trace event count (Trace and at least one of
// CountMin/CountMax set; the others empty).
type Expectation struct {
	Tick int // evaluated on the state at this tick
	// Entity comparison: Path of the entity summary of Entity, compared with Value by Op
	// (one of ExpectOps). Value is a float64, string, bool, or []any of those.
	Entity string
	Path   string
	Op     string
	Value  any
	// Trace count: the number of trace events named Trace emitted from tick 0 through
	// Tick must be ≥ *CountMin and ≤ *CountMax (nil bounds are not checked).
	Trace    string
	CountMin *int
	CountMax *int
}

// IsTrace reports whether e is a trace event count rather than an entity comparison.
func (e *Expectation) IsTrace() bool { return e.Trace != "" }

// ExpectOps are the comparison operators of entity expectations.
var ExpectOps = []string{"<", "<=", "==", "!=", ">=", ">", "contains"}

// BuiltinInvariants are the invariant names the engine evaluates itself.
var BuiltinInvariants = []string{"entity_count_max", "finite_positions", "no_overlap", "within_bounds"}

// Invariant is a parsed invariant spec such as "entity_count_max:500".
type Invariant struct {
	// Name is finite_positions, within_bounds, entity_count_max, no_overlap, or the name of
	// an invariant the game registers with ctx.Invariant.
	Name string
	Max  int       // entity_count_max: the maximum number of entities
	Tags [2]string // no_overlap: no entity tagged Tags[0] may overlap one tagged Tags[1]
}

// Builtin reports whether the invariant is evaluated by the engine (not the game).
func (iv Invariant) Builtin() bool { return indexOf(BuiltinInvariants, iv.Name) >= 0 }

// String returns the canonical spec of iv.
func (iv Invariant) String() string {
	switch iv.Name {
	case "entity_count_max":
		return iv.Name + ":" + strconv.Itoa(iv.Max)
	case "no_overlap":
		return iv.Name + ":" + iv.Tags[0] + "," + iv.Tags[1]
	}
	return iv.Name
}

// ParseInvariant parses an invariant spec: "finite_positions", "within_bounds",
// "entity_count_max:N" (N > 0), "no_overlap:tagA,tagB" (two valid tag names, may be
// equal), or a plain valid name for a game-registered invariant.
func ParseInvariant(s string) (Invariant, error) {
	name, arg, hasArg := strings.Cut(s, ":")
	iv := Invariant{Name: name}
	switch name {
	case "finite_positions", "within_bounds":
		if hasArg {
			return iv, fmt.Errorf("invariant %q: %s takes no argument", s, name)
		}
	case "entity_count_max":
		if !hasArg || arg == "" {
			return iv, fmt.Errorf("invariant %q: want entity_count_max:N with N > 0", s)
		}
		for _, r := range arg {
			if r < '0' || r > '9' {
				return iv, fmt.Errorf("invariant %q: N must be a positive integer", s)
			}
		}
		n, err := strconv.Atoi(arg)
		if err != nil || n <= 0 || n > 1<<31-1 {
			return iv, fmt.Errorf("invariant %q: N must be in [1, %d]", s, 1<<31-1)
		}
		iv.Max = n
	case "no_overlap":
		a, b, ok := strings.Cut(arg, ",")
		if !hasArg || !ok || strings.Contains(b, ",") {
			return iv, fmt.Errorf("invariant %q: want no_overlap:tagA,tagB (two tags, no spaces)", s)
		}
		for _, t := range []string{a, b} {
			if err := ValidName(t); err != nil {
				return iv, fmt.Errorf("invariant %q: tag %v", s, err)
			}
		}
		iv.Tags = [2]string{a, b}
	default:
		if hasArg {
			return iv, fmt.Errorf("invariant %q: unknown built-in %q (built-ins with arguments: entity_count_max:N, no_overlap:tagA,tagB); game invariants are plain names", s, name)
		}
		if err := ValidName(name); err != nil {
			return iv, fmt.Errorf("invariant %v", err)
		}
	}
	return iv, nil
}

// checkInvariants validates a list of invariant specs at path (no duplicates).
func checkInvariants(c *Checker, path string, list []string) []string {
	if len(list) == 0 {
		return nil
	}
	out := make([]string, 0, len(list))
	for i, s := range list {
		p := Path(path, i)
		iv, err := ParseInvariant(s)
		if err != nil {
			c.Errorf(p, "%v", err)
			continue
		}
		canon := iv.String()
		if j := indexOf(out, canon); j >= 0 {
			c.Errorf(p, "duplicate invariant %q", s)
			continue
		}
		out = append(out, canon)
	}
	return out
}

// ParseScenario decodes and compiles the scenario source file (for example
// "tests/scenarios/move.scenario.json"). The scenario name is the file name without its
// ".scenario.json" suffix and must be a valid asset name.
func ParseScenario(file string, data []byte) (*Scenario, error) {
	var src ScenarioSource
	loc, err := Decode(file, data, TypeScenario, &src)
	if err != nil {
		return nil, err
	}
	name, err := nameFromFile(KindScenario, file, loc)
	if err != nil {
		return nil, err
	}
	return CompileScenario(name, &src, loc)
}

// CompileScenario validates src and returns the compiled scenario called name. Every
// problem is reported, located through loc (nil reports positions as unknown), in one
// Errors value. Entity names and the scene are checked for syntax only; they are
// resolved when the scenario runs.
func CompileScenario(name string, src *ScenarioSource, loc *Locator) (*Scenario, error) {
	c := NewChecker(ensureLoc(loc))
	if err := ValidName(name); err != nil {
		c.Errorf("", "scenario %v", err)
	}
	sc := &Scenario{Name: name, Scene: src.Scene, Seed: src.Seed, Ticks: src.Ticks}
	if src.Scene == "" {
		c.Errorf("scene", "is required (name of a scene in assets/scenes)")
	} else {
		c.Name("scene", src.Scene)
	}
	maxTick := src.Ticks // upper bound for tick fields; -1 when unknown
	switch {
	case src.Ticks == 0:
		c.Errorf("ticks", "is required (number of ticks to simulate, 1 to %d)", MaxTicks)
		maxTick = -1
	case src.Ticks < 1 || src.Ticks > MaxTicks:
		c.Errorf("ticks", "%d out of range [1, %d]", src.Ticks, MaxTicks)
		maxTick = -1
	}
	tick := func(path string, t int) {
		switch {
		case t < 0:
			c.Errorf(path, "%d must not be negative", t)
		case maxTick >= 0 && t > maxTick:
			c.Errorf(path, "%d is after the last tick (ticks = %d)", t, maxTick)
		}
	}
	sc.Inputs = compileInputs(c, src.Inputs, tick)
	sc.Expect = compileExpect(c, src.Expect, tick)
	sc.Invariants = checkInvariants(c, "invariants", src.Invariants)
	if len(src.Screenshots) > 0 {
		sc.Screenshots = append([]int(nil), src.Screenshots...)
		for i, t := range src.Screenshots {
			p := Path("screenshots", i)
			tick(p, t)
			if i > 0 && t <= src.Screenshots[i-1] {
				c.Errorf(p, "tick %d must be greater than the previous screenshot tick %d (strictly increasing)", t, src.Screenshots[i-1])
			}
		}
	}
	if err := c.Err(); err != nil {
		return nil, err
	}
	return sc, nil
}

func compileInputs(c *Checker, src []InputSource, tick func(string, int)) []Input {
	if len(src) == 0 {
		return nil
	}
	out := make([]Input, len(src))
	held := map[string]int{} // key → tick it was pressed; lookup only, never iterated
	for i := range src {
		in := &src[i]
		p := Path("inputs", i)
		tick(Path(p, "tick"), in.Tick)
		if i > 0 && in.Tick < src[i-1].Tick {
			c.Errorf(Path(p, "tick"), "tick %d is before the previous input's tick %d (inputs must be in tick order)", in.Tick, src[i-1].Tick)
		}
		ev := Input{Tick: in.Tick, Text: in.Text}
		if in.Press == nil && in.Release == nil && in.Buttons == nil && in.Mouse == nil && in.Text == "" {
			c.Errorf(p, "input event has no press, release, buttons, mouse or text")
		}
		ev.Press = keyList(c, Path(p, "press"), in.Press)
		ev.Release = keyList(c, Path(p, "release"), in.Release)
		for _, key := range ev.Press {
			if indexOf(ev.Release, key) >= 0 {
				c.Errorf(Path(Path(p, "press"), indexOf(in.Press, key)), "key %q is both pressed and released in the same event; release it at a later tick", key)
				continue
			}
			if t, ok := held[key]; ok {
				c.Errorf(Path(Path(p, "press"), indexOf(in.Press, key)), "key %q is already held (pressed at tick %d)", key, t)
				continue
			}
			held[key] = in.Tick
		}
		for _, key := range ev.Release {
			if indexOf(ev.Press, key) >= 0 {
				continue // reported above
			}
			if _, ok := held[key]; !ok {
				c.Errorf(Path(Path(p, "release"), indexOf(in.Release, key)), "key %q is released but not held (press it at an earlier tick)", key)
				continue
			}
			delete(held, key)
		}
		if in.Buttons != nil {
			ev.Buttons = make([]string, 0, len(in.Buttons))
			for k, b := range in.Buttons {
				bp := Path(Path(p, "buttons"), k)
				switch {
				case !isMouseButton(b):
					c.Errorf(bp, "unknown mouse button %q (want one of %v)", b, MouseButtons)
				case indexOf(ev.Buttons, b) >= 0:
					c.Errorf(bp, "duplicate mouse button %q", b)
				default:
					ev.Buttons = append(ev.Buttons, b)
				}
			}
		}
		if in.Mouse != nil {
			m := gmath.V2(in.Mouse.X, in.Mouse.Y)
			if !gmath.IsFinite(m.X) || !gmath.IsFinite(m.Y) {
				c.Errorf(Path(p, "mouse"), "not a finite position")
			}
			ev.Mouse = &m
		}
		out[i] = ev
	}
	return out
}

// keyList validates key names at path and returns the valid, distinct ones (nil for an
// empty list).
func keyList(c *Checker, path string, keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	out := make([]string, 0, len(keys))
	for i, k := range keys {
		kp := Path(path, i)
		switch {
		case !IsKeyCode(k):
			if s := suggestKey(k); s != "" {
				c.Errorf(kp, "unknown key %q (did you mean %q? keys are W3C KeyboardEvent.code names)", k, s)
			} else {
				c.Errorf(kp, "unknown key %q (keys are W3C KeyboardEvent.code names such as KeyW, Space, ArrowLeft)", k)
			}
		case indexOf(out, k) >= 0:
			c.Errorf(kp, "duplicate key %q", k)
		default:
			out = append(out, k)
		}
	}
	return out
}

func compileExpect(c *Checker, src []ExpectSource, tick func(string, int)) []Expectation {
	if len(src) == 0 {
		return nil
	}
	out := make([]Expectation, 0, len(src))
	for i := range src {
		e := &src[i]
		p := Path("expect", i)
		tick(Path(p, "tick"), e.Tick)
		isCmp := e.Entity != "" || e.Path != "" || e.Op != "" || e.Value != nil
		isTrace := e.Trace != "" || e.CountMin != nil || e.CountMax != nil
		ex := Expectation{Tick: e.Tick}
		switch {
		case isCmp && isTrace:
			c.Errorf(p, "mixes an entity comparison (entity, path, op, value) with a trace count (trace, count_min, count_max); use two expectations")
		case isTrace:
			ex.Trace, ex.CountMin, ex.CountMax = e.Trace, e.CountMin, e.CountMax
			if e.Trace == "" {
				c.Errorf(Path(p, "trace"), "is required with count_min/count_max (name of a trace event)")
			} else {
				c.Name(Path(p, "trace"), e.Trace)
			}
			if e.CountMin == nil && e.CountMax == nil {
				c.Errorf(p, "trace expectation needs count_min, count_max or both")
			}
			if e.CountMin != nil && *e.CountMin < 0 {
				c.Errorf(Path(p, "count_min"), "%d must not be negative", *e.CountMin)
			}
			if e.CountMax != nil && *e.CountMax < 0 {
				c.Errorf(Path(p, "count_max"), "%d must not be negative", *e.CountMax)
			}
			if e.CountMin != nil && e.CountMax != nil && *e.CountMin > *e.CountMax {
				c.Errorf(Path(p, "count_max"), "%d is less than count_min %d", *e.CountMax, *e.CountMin)
			}
			if e.CountMin != nil {
				ex.CountMin = new(int)
				*ex.CountMin = *e.CountMin
			}
			if e.CountMax != nil {
				ex.CountMax = new(int)
				*ex.CountMax = *e.CountMax
			}
		case isCmp:
			ex.Entity, ex.Path, ex.Op = e.Entity, e.Path, e.Op
			compileComparison(c, p, e, &ex)
		default:
			c.Errorf(p, "expectation needs either entity, path, op and value, or trace with count_min/count_max")
		}
		out = append(out, ex)
	}
	return out
}

// Kinds of values an entity summary path yields.
type pathType int

const (
	ptNumber  pathType = iota // position.x
	ptVec3                    // position
	ptBool                    // visible
	ptString                  // kind
	ptStrings                 // tags
	ptAny                     // state.<field>
)

var pathTypeNames = [...]string{"a number", "a vector [x, y, z]", "a boolean", "a string", "a list of strings", "a game state value"}

// ExpectPaths documents the entity summary paths an expectation may use.
var ExpectPaths = []string{
	"position", "position.x", "position.y", "position.z",
	"rotation_deg", "rotation_deg.x", "rotation_deg.y", "rotation_deg.z",
	"scale", "scale.x", "scale.y", "scale.z",
	"aabb.min", "aabb.min.x", "aabb.min.y", "aabb.min.z",
	"aabb.max", "aabb.max.x", "aabb.max.y", "aabb.max.z",
	"visible", "tags", "kind", "model", "material", "parent",
	"state.<field>[.<field>…]",
}

// expectPathType checks the path grammar (dotted identifiers) and returns the type of
// the value the path selects.
func expectPathType(path string) (pathType, error) {
	segs := strings.Split(path, ".")
	for _, s := range segs {
		if !isIdent(s) {
			return 0, fmt.Errorf("path %q: segments must be identifiers ([A-Za-z_][A-Za-z0-9_]*) separated by '.'", path)
		}
	}
	axis := func(s string) bool { return s == "x" || s == "y" || s == "z" }
	bad := fmt.Errorf("path %q: unknown path (want one of %s)", path, strings.Join(ExpectPaths, ", "))
	switch segs[0] {
	case "position", "rotation_deg", "scale":
		switch {
		case len(segs) == 1:
			return ptVec3, nil
		case len(segs) == 2 && axis(segs[1]):
			return ptNumber, nil
		}
	case "aabb":
		if len(segs) >= 2 && (segs[1] == "min" || segs[1] == "max") {
			switch {
			case len(segs) == 2:
				return ptVec3, nil
			case len(segs) == 3 && axis(segs[2]):
				return ptNumber, nil
			}
		}
	case "visible":
		if len(segs) == 1 {
			return ptBool, nil
		}
	case "tags":
		if len(segs) == 1 {
			return ptStrings, nil
		}
	case "kind", "model", "material", "parent":
		if len(segs) == 1 {
			return ptString, nil
		}
	case "state":
		if len(segs) >= 2 {
			return ptAny, nil
		}
		return 0, fmt.Errorf("path %q: name a field of the game state, for example state.score", path)
	}
	return 0, bad
}

func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// compileComparison validates the entity comparison fields of e into ex.
func compileComparison(c *Checker, p string, e *ExpectSource, ex *Expectation) {
	if e.Entity == "" {
		c.Errorf(Path(p, "entity"), "is required (name of an entity of the scene)")
	} else {
		c.Name(Path(p, "entity"), e.Entity)
	}
	pt, ptOK := ptAny, false
	if e.Path == "" {
		c.Errorf(Path(p, "path"), "is required (for example position.x, tags, visible, state.score)")
	} else if t, err := expectPathType(e.Path); err != nil {
		c.Errorf(Path(p, "path"), "%v", err)
	} else {
		pt, ptOK = t, true
	}
	opOK := false
	if e.Op == "" {
		c.Errorf(Path(p, "op"), "is required (one of %s)", strings.Join(ExpectOps, " "))
	} else if indexOf(ExpectOps, e.Op) < 0 {
		c.Errorf(Path(p, "op"), "unknown operator %q (want one of %s)", e.Op, strings.Join(ExpectOps, " "))
	} else {
		opOK = true
	}
	if e.Value == nil {
		c.Errorf(Path(p, "value"), "is required")
		return
	}
	v, err := normValue(e.Value)
	if err != nil {
		c.Errorf(Path(p, "value"), "%v", err)
		return
	}
	ex.Value = v
	if !ptOK || !opOK {
		return
	}
	if msg := checkOpValue(pt, e.Op, v); msg != "" {
		at := Path(p, "value")
		if strings.HasPrefix(msg, "op ") {
			at = Path(p, "op")
		}
		c.Errorf(at, "%s (path %s is %s)", msg, e.Path, pathTypeNames[pt])
	}
}

// checkOpValue returns why op and v do not fit a path of type pt, or "".
func checkOpValue(pt pathType, op string, v any) string {
	ordering := op == "<" || op == "<=" || op == ">=" || op == ">"
	isNum := func(x any) bool { _, ok := x.(float64); return ok }
	isStr := func(x any) bool { _, ok := x.(string); return ok }
	isBool := func(x any) bool { _, ok := x.(bool); return ok }
	list, isList := v.([]any)
	switch pt {
	case ptNumber:
		if op == "contains" {
			return "op contains needs a string, list or state path"
		}
		if !isNum(v) {
			return "value must be a number"
		}
	case ptVec3:
		if op != "==" && op != "!=" {
			return "op " + op + " is not defined on vectors; compare one axis (for example position.x) or use == / !="
		}
		if !isList || len(list) != 3 || !isNum(list[0]) || !isNum(list[1]) || !isNum(list[2]) {
			return "value must be [x, y, z] (3 numbers)"
		}
	case ptBool:
		if op != "==" && op != "!=" {
			return "op " + op + " is not defined on booleans; use == or !="
		}
		if !isBool(v) {
			return "value must be true or false"
		}
	case ptString:
		if ordering {
			return "op " + op + " is not defined on strings; use ==, != or contains"
		}
		if !isStr(v) {
			return "value must be a string"
		}
	case ptStrings:
		switch op {
		case "contains":
			if !isStr(v) {
				return "value must be a tag name (string)"
			}
		case "==", "!=":
			if !isList {
				return "value must be a list of tag names"
			}
			for _, x := range list {
				if !isStr(x) {
					return "value must be a list of tag names"
				}
			}
		default:
			return "op " + op + " is not defined on tags; use contains, == or !="
		}
	case ptAny:
		switch {
		case ordering && !isNum(v):
			return "value must be a number for " + op
		case op == "contains" && isList:
			return "value of contains must be a string, number or boolean"
		}
	}
	return ""
}

// normValue converts an expectation value to float64, string, bool or []any of those.
func normValue(v any) (any, error) {
	switch x := v.(type) {
	case float64:
		if !gmath.IsFinite(float32(x)) {
			return nil, fmt.Errorf("value %v is not a finite float32 number", x)
		}
		return x, nil
	case float32:
		return normValue(float64(x))
	case int:
		return float64(x), nil
	case int64:
		return float64(x), nil
	case string, bool:
		return x, nil
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			if _, isList := e.([]any); isList {
				return nil, fmt.Errorf("value lists cannot be nested")
			}
			n, err := normValue(e)
			if err != nil {
				return nil, err
			}
			out[i] = n
		}
		return out, nil
	case []string:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = e
		}
		return out, nil
	case []float64:
		out := make([]any, len(x))
		for i, e := range x {
			n, err := normValue(e)
			if err != nil {
				return nil, err
			}
			out[i] = n
		}
		return out, nil
	case map[string]any:
		return nil, fmt.Errorf("value cannot be an object; compare one field with a longer path")
	}
	return nil, fmt.Errorf("value of type %T is not a number, string, boolean or list", v)
}
