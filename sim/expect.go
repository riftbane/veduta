package sim

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/riftbane/veduta/v2/scene"
)

// Expectation is one scenario check at a tick: either an entity path comparison
// (Entity, Path, Op, Value) or a trace event count (Trace with CountMin/CountMax).
type Expectation struct {
	Tick     uint64
	Entity   string
	Path     string
	Op       string
	Value    any
	Trace    string
	CountMin *int
	CountMax *int
}

// ExpectResult is an evaluated expectation, as reported by simulate.
type ExpectResult struct {
	Tick     uint64 `json:"tick"`
	Entity   string `json:"entity,omitempty"`
	Path     string `json:"path,omitempty"`
	Op       string `json:"op,omitempty"`
	Value    any    `json:"value,omitempty"`
	Trace    string `json:"trace,omitempty"`
	CountMin *int   `json:"count_min,omitempty"`
	CountMax *int   `json:"count_max,omitempty"`
	Actual   any    `json:"actual"`
	Pass     bool   `json:"pass"`
	Error    string `json:"error,omitempty"`
}

func newResult(x Expectation) ExpectResult {
	return ExpectResult{Tick: x.Tick, Entity: x.Entity, Path: x.Path, Op: x.Op, Value: x.Value, Trace: x.Trace, CountMin: x.CountMin, CountMax: x.CountMax}
}

// Evaluate checks x against the scene and the trace recorded so far (event counts are
// cumulative from tick 0 through the current tick).
func Evaluate(x Expectation, s *scene.Scene, rec *Recorder) ExpectResult {
	r := newResult(x)
	if x.Trace != "" {
		n := rec.Count(x.Trace)
		r.Actual = n
		r.Pass = (x.CountMin == nil || n >= *x.CountMin) && (x.CountMax == nil || n <= *x.CountMax)
		return r
	}
	e := s.Find(x.Entity)
	if e == nil {
		r.Error = fmt.Sprintf("entity %q does not exist at tick %d", x.Entity, x.Tick)
		return r
	}
	doc, err := summaryDoc(s, e)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	v, ok := Lookup(doc, x.Path)
	if !ok {
		r.Error = fmt.Sprintf("path %q not found on entity %q", x.Path, x.Entity)
		return r
	}
	r.Actual = v
	r.Pass, err = Compare(v, x.Op, x.Value)
	if err != nil {
		r.Error = err.Error()
	}
	return r
}

// summaryDoc returns the entity summary as generic JSON values (numbers as float64), the
// same shape the trace records.
func summaryDoc(s *scene.Scene, e *scene.Entity) (any, error) {
	raw, err := AppendCanonical(nil, Summary(s, e))
	if err != nil {
		return nil, err
	}
	var doc any
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// Lookup resolves a dotted path in a generic JSON document. Array elements are addressed
// by index or, for vectors, by x, y, z, w (and min, max for bounds): position.x,
// aabb.max.y, tags.0, state.score.
func Lookup(doc any, path string) (any, bool) {
	cur := doc
	if path == "" {
		return cur, true
	}
	for _, seg := range strings.Split(path, ".") {
		switch c := cur.(type) {
		case map[string]any:
			v, ok := c[seg]
			if !ok {
				return nil, false
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil {
				switch seg {
				case "x", "min":
					i = 0
				case "y", "max":
					i = 1
				case "z":
					i = 2
				case "w":
					i = 3
				default:
					return nil, false
				}
			}
			if i < 0 || i >= len(c) {
				return nil, false
			}
			cur = c[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

// Compare applies op (< <= == != >= > contains) to actual and want. Numbers compare
// numerically, strings and booleans support == and !=, contains tests array membership
// or substrings.
func Compare(actual any, op string, want any) (bool, error) {
	if op == "contains" {
		switch a := actual.(type) {
		case []any:
			for _, e := range a {
				if equal(e, want) {
					return true, nil
				}
			}
			return false, nil
		case string:
			w, ok := want.(string)
			if !ok {
				return false, fmt.Errorf("contains on a string needs a string value")
			}
			return strings.Contains(a, w), nil
		}
		return false, fmt.Errorf("contains needs an array or string, got %T", actual)
	}
	if an, ok := number(actual); ok {
		wn, ok := number(want)
		if !ok {
			return false, fmt.Errorf("cannot compare number with %T", want)
		}
		switch op {
		case "<":
			return an < wn, nil
		case "<=":
			return an <= wn, nil
		case "==":
			return an == wn, nil
		case "!=":
			return an != wn, nil
		case ">=":
			return an >= wn, nil
		case ">":
			return an > wn, nil
		}
		return false, fmt.Errorf("unknown op %q", op)
	}
	switch op {
	case "==":
		return equal(actual, want), nil
	case "!=":
		return !equal(actual, want), nil
	}
	return false, fmt.Errorf("op %q needs numbers, got %T", op, actual)
}

func number(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint64:
		return float64(x), true
	case uint32:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	}
	return 0, false
}

func equal(a, b any) bool {
	if an, ok := number(a); ok {
		bn, ok := number(b)
		return ok && an == bn
	}
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return bytes.Equal(ab, bb)
}
