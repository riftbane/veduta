package asset

import (
	"reflect"
	"strings"
	"testing"

	"github.com/riftbane/veduta/gmath"
)

func TestParseScenarioSample(t *testing.T) {
	sc, err := ParseScenario("tests/scenarios/move.scenario.json", readTestdata(t, "scenarios/move.scenario.json"))
	if err != nil {
		t.Fatal(err)
	}
	if sc.Name != "move" || sc.Scene != "main" || sc.Seed != 42 || sc.Ticks != 300 {
		t.Fatalf("header %+v", sc)
	}
	mouse := gmath.V2(320, 180)
	wantInputs := []Input{
		{Tick: 10, Press: []string{"KeyW"}},
		{Tick: 70, Release: []string{"KeyW"}},
		{Tick: 80, Press: []string{"Space"}},
		{Tick: 81, Release: []string{"Space"}, Mouse: &mouse, Buttons: []string{"left"}},
		{Tick: 82, Buttons: []string{}, Text: "hi"},
	}
	if !reflect.DeepEqual(sc.Inputs, wantInputs) {
		t.Fatalf("inputs %+v", sc.Inputs)
	}
	if sc.Inputs[4].Buttons == nil {
		t.Fatal(`"buttons": [] must stay distinct from an absent field`)
	}
	one, zero := 1, 0
	wantExpect := []Expectation{
		{Tick: 120, Entity: "player", Path: "position.z", Op: "<", Value: float64(-1)},
		{Tick: 120, Entity: "player", Path: "tags", Op: "contains", Value: "player"},
		{Tick: 300, Trace: "gem_collected", CountMin: &one},
		{Tick: 300, Trace: "invariant_violation", CountMax: &zero},
	}
	if !reflect.DeepEqual(sc.Expect, wantExpect) {
		t.Fatalf("expect %+v", sc.Expect)
	}
	if !sc.Expect[2].IsTrace() || sc.Expect[0].IsTrace() {
		t.Fatal("IsTrace")
	}
	if !reflect.DeepEqual(sc.Invariants, []string{"finite_positions", "within_bounds", "entity_count_max:500", "no_overlap:player,wall"}) {
		t.Fatalf("invariants %v", sc.Invariants)
	}
	if !reflect.DeepEqual(sc.Screenshots, []int{0, 60, 120, 300}) {
		t.Fatalf("screenshots %v", sc.Screenshots)
	}
}

// TestParseScenarioStick: the stick holds a position from its tick on, so it is an input
// event on its own; either axis left out is at rest.
func TestParseScenarioStick(t *testing.T) {
	sc, err := ParseScenario("stick.scenario.json", []byte(`{"veduta": "scenario/1", "scene": "main", "ticks": 10,
  "inputs": [{"tick": 1, "stick": {"x": -1, "y": 0.5}}, {"tick": 5, "stick": {"y": 1}}, {"tick": 9, "stick": {}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	a, b, rest := gmath.V2(-1, 0.5), gmath.V2(0, 1), gmath.Vec2{}
	want := []Input{{Tick: 1, Stick: &a}, {Tick: 5, Stick: &b}, {Tick: 9, Stick: &rest}}
	if !reflect.DeepEqual(sc.Inputs, want) {
		t.Fatalf("inputs %+v", sc.Inputs)
	}
}

func TestParseScenarioMinimal(t *testing.T) {
	sc, err := ParseScenario("idle.scenario.json", []byte(`{"veduta": "scenario/1", "scene": "main", "ticks": 1}`))
	if err != nil {
		t.Fatal(err)
	}
	want := &Scenario{Name: "idle", Scene: "main", Ticks: 1}
	if !reflect.DeepEqual(sc, want) {
		t.Fatalf("got %+v", sc)
	}
}

func TestParseScenarioErrors(t *testing.T) {
	head := `{"veduta": "scenario/1", "scene": "main", "ticks": 100, `
	with := func(body string) string { return head + body + `}` }
	cases := []struct {
		name, src string
		wants     []wantErr
	}{
		{"scene missing", `{"veduta": "scenario/1", "ticks": 10}`,
			[]wantErr{{`scene: is required`, ""}}},
		{"scene invalid", `{"veduta": "scenario/1", "scene": "Main", "ticks": 10}`,
			[]wantErr{{`scene: name "Main"`, `"Main"`}}},
		{"ticks missing", `{"veduta": "scenario/1", "scene": "main"}`,
			[]wantErr{{`ticks: is required`, ""}}},
		{"ticks too many", `{"veduta": "scenario/1", "scene": "main", "ticks": 1000001}`,
			[]wantErr{{`ticks: 1000001 out of range [1, 1000000]`, `1000001`}}},
		{"ticks negative", `{"veduta": "scenario/1", "scene": "main", "ticks": -3}`,
			[]wantErr{{`ticks: -3 out of range`, `-3`}}},
		{"seed negative", `{"veduta": "scenario/1", "scene": "main", "ticks": 3, "seed": -1}`,
			[]wantErr{{`seed: cannot use JSON number`, `-1`}}},
		{"input tick after end", with(`"inputs": [{"tick": 101, "press": ["KeyW"]}]`),
			[]wantErr{{`inputs[0].tick: 101 is after the last tick (ticks = 100)`, `101`}}},
		{"input tick negative", with(`"inputs": [{"tick": -1, "press": ["KeyW"]}]`),
			[]wantErr{{`inputs[0].tick: -1 must not be negative`, `-1`}}},
		{"inputs out of order", with(`"inputs": [{"tick": 50, "press": ["KeyW"]}, {"tick": 5, "release": ["KeyW"]}]`),
			[]wantErr{{`inputs[1].tick: tick 5 is before the previous input's tick 50`, `5,`}}},
		{"unknown key with suggestion", with(`"inputs": [{"tick": 1, "press": ["w", "space", "up", "1"]}]`),
			[]wantErr{
				{`inputs[0].press[0]: unknown key "w" (did you mean "KeyW"?`, `"w"`},
				{`inputs[0].press[1]: unknown key "space" (did you mean "Space"?`, `"space"`},
				{`did you mean "ArrowUp"?`, `"up"`},
				{`did you mean "Digit1"?`, `"1"`},
			}},
		{"unknown key", with(`"inputs": [{"tick": 1, "press": ["Jump"]}]`),
			[]wantErr{{`unknown key "Jump" (keys are W3C KeyboardEvent.code names`, `"Jump"`}}},
		{"duplicate key", with(`"inputs": [{"tick": 1, "press": ["KeyA", "KeyA"]}]`),
			[]wantErr{{`inputs[0].press[1]: duplicate key "KeyA"`, `"KeyA"]`}}},
		{"press held key", with(`"inputs": [{"tick": 1, "press": ["KeyA"]}, {"tick": 2, "press": ["KeyA"]}]`),
			[]wantErr{{`inputs[1].press[0]: key "KeyA" is already held (pressed at tick 1)`, `"KeyA"]}]`}}},
		{"release not held", with(`"inputs": [{"tick": 1, "release": ["KeyA"]}]`),
			[]wantErr{{`inputs[0].release[0]: key "KeyA" is released but not held`, `"KeyA"`}}},
		{"press and release together", with(`"inputs": [{"tick": 1, "press": ["KeyA"], "release": ["KeyA"]}]`),
			[]wantErr{{`inputs[0].press[0]: key "KeyA" is both pressed and released`, `"KeyA"`}}},
		{"bad buttons", with(`"inputs": [{"tick": 1, "buttons": ["left", "back", "left"]}]`),
			[]wantErr{{`inputs[0].buttons[1]: unknown mouse button "back" (want one of [left middle right])`, `"back"`},
				{`inputs[0].buttons[2]: duplicate mouse button "left"`, `"left"]`}}},
		{"empty event", with(`"inputs": [{"tick": 1}]`),
			[]wantErr{{`inputs[0]: input event has no press, release, buttons, mouse, stick or text`, `{"tick": 1}`}}},
		{"stick out of range", with(`"inputs": [{"tick": 1, "stick": {"x": 1.5, "y": -2}}]`),
			[]wantErr{{`inputs[0].stick: x 1.5 and y -2 must be in [-1, 1]`, `{"x": 1.5`}}},
		{"stick unknown field", with(`"inputs": [{"tick": 1, "stick": {"x": 1, "z": 0}}]`),
			[]wantErr{{`inputs[0].stick.z: unknown field`, `"z"`}}},
		{"mixed expectation", with(`"expect": [{"tick": 1, "entity": "p", "path": "visible", "op": "==", "value": true, "trace": "x", "count_min": 1}]`),
			[]wantErr{{`expect[0]: mixes an entity comparison`, `{"tick": 1, "entity"`}}},
		{"empty expectation", with(`"expect": [{"tick": 1}]`),
			[]wantErr{{`expect[0]: expectation needs either`, `{"tick": 1}`}}},
		{"comparison missing parts", with(`"expect": [{"tick": 1, "entity": "p"}]`),
			[]wantErr{{`expect[0].path: is required`, ""}, {`expect[0].op: is required`, ""}, {`expect[0].value: is required`, ""}}},
		{"value null", with(`"expect": [{"tick": 1, "entity": "p", "path": "visible", "op": "==", "value": null}]`),
			[]wantErr{{`expect[0].value: is required`, `null`}}},
		{"value object", with(`"expect": [{"tick": 1, "entity": "p", "path": "state.a", "op": "==", "value": {"x": 1}}]`),
			[]wantErr{{`expect[0].value: value cannot be an object`, `{"x": 1}`}}},
		{"nested list", with(`"expect": [{"tick": 1, "entity": "p", "path": "state.a", "op": "==", "value": [[1]]}]`),
			[]wantErr{{`value lists cannot be nested`, `[[1]]`}}},
		{"bad entity", with(`"expect": [{"tick": 1, "entity": "P 1", "path": "visible", "op": "==", "value": true}]`),
			[]wantErr{{`expect[0].entity: name "P 1"`, `"P 1"`}}},
		{"path grammar", with(`"expect": [{"tick": 1, "entity": "p", "path": "position..x", "op": "<", "value": 1}]`),
			[]wantErr{{`expect[0].path: path "position..x": segments must be identifiers`, `"position..x"`}}},
		{"path unknown root", with(`"expect": [{"tick": 1, "entity": "p", "path": "postion.x", "op": "<", "value": 1}]`),
			[]wantErr{{`expect[0].path: path "postion.x": unknown path (want one of position,`, `"postion.x"`}}},
		{"path bad axis", with(`"expect": [{"tick": 1, "entity": "p", "path": "position.w", "op": "<", "value": 1}]`),
			[]wantErr{{`unknown path`, `"position.w"`}}},
		{"bare state", with(`"expect": [{"tick": 1, "entity": "p", "path": "state", "op": "==", "value": 1}]`),
			[]wantErr{{`name a field of the game state`, `"state"`}}},
		{"unknown op", with(`"expect": [{"tick": 1, "entity": "p", "path": "position.x", "op": "=", "value": 1}]`),
			[]wantErr{{`expect[0].op: unknown operator "="`, `"="`}}},
		{"contains on number", with(`"expect": [{"tick": 1, "entity": "p", "path": "position.x", "op": "contains", "value": 1}]`),
			[]wantErr{{`expect[0].op: op contains needs a string, list or state path (path position.x is a number)`, `"contains"`}}},
		{"number needs number", with(`"expect": [{"tick": 1, "entity": "p", "path": "aabb.min.y", "op": ">", "value": "1"}]`),
			[]wantErr{{`expect[0].value: value must be a number (path aabb.min.y is a number)`, `"1"}`}}},
		{"ordering on vector", with(`"expect": [{"tick": 1, "entity": "p", "path": "position", "op": "<", "value": [1, 2, 3]}]`),
			[]wantErr{{`expect[0].op: op < is not defined on vectors`, `"<"`}}},
		{"vector value", with(`"expect": [{"tick": 1, "entity": "p", "path": "scale", "op": "==", "value": [1, 2]}]`),
			[]wantErr{{`expect[0].value: value must be [x, y, z]`, `[1, 2]`}}},
		{"bool value", with(`"expect": [{"tick": 1, "entity": "p", "path": "visible", "op": "==", "value": 1}]`),
			[]wantErr{{`value must be true or false`, `1}`}}},
		{"bool op", with(`"expect": [{"tick": 1, "entity": "p", "path": "visible", "op": "contains", "value": true}]`),
			[]wantErr{{`op contains is not defined on booleans`, `"contains"`}}},
		{"string ordering", with(`"expect": [{"tick": 1, "entity": "p", "path": "kind", "op": "<", "value": "a"}]`),
			[]wantErr{{`op < is not defined on strings`, `"<"`}}},
		{"tags equality needs list", with(`"expect": [{"tick": 1, "entity": "p", "path": "tags", "op": "==", "value": "gem"}]`),
			[]wantErr{{`value must be a list of tag names`, `"gem"`}}},
		{"tags contains needs string", with(`"expect": [{"tick": 1, "entity": "p", "path": "tags", "op": "contains", "value": 3}]`),
			[]wantErr{{`value must be a tag name`, `3}`}}},
		{"state ordering needs number", with(`"expect": [{"tick": 1, "entity": "p", "path": "state.name", "op": ">=", "value": "a"}]`),
			[]wantErr{{`value must be a number for >=`, `"a"}`}}},
		{"trace without counts", with(`"expect": [{"tick": 1, "trace": "hit"}]`),
			[]wantErr{{`expect[0]: trace expectation needs count_min, count_max or both`, `{"tick": 1, "trace"`}}},
		{"counts without trace", with(`"expect": [{"tick": 1, "count_min": 1}]`),
			[]wantErr{{`expect[0].trace: is required with count_min/count_max`, ""}}},
		{"counts invalid", with(`"expect": [{"tick": 1, "trace": "Hit", "count_min": 5, "count_max": 2}, {"tick": 2, "trace": "hit", "count_min": -1}]`),
			[]wantErr{{`expect[0].trace: name "Hit"`, `"Hit"`}, {`expect[0].count_max: 2 is less than count_min 5`, `2}`},
				{`expect[1].count_min: -1 must not be negative`, `-1`}}},
		{"expect tick", with(`"expect": [{"tick": 500, "trace": "hit", "count_min": 1}]`),
			[]wantErr{{`expect[0].tick: 500 is after the last tick (ticks = 100)`, `500`}}},
		{"invariants", with(`"invariants": ["finite_positions:1", "entity_count_max:0", "entity_count_max", "entity_count_max:+5", "no_overlap:a", "no_overlap:a,B", "no_overlap:a,b,c", "speed:3", "Game", "within_bounds", "within_bounds"]`),
			[]wantErr{
				{`invariants[0]: invariant "finite_positions:1": finite_positions takes no argument`, `"finite_positions:1"`},
				{`invariants[1]: invariant "entity_count_max:0": N must be in [1, 2147483647]`, `"entity_count_max:0"`},
				{`invariants[2]: invariant "entity_count_max": want entity_count_max:N`, `"entity_count_max",`},
				{`invariants[3]: invariant "entity_count_max:+5": N must be a positive integer`, `"entity_count_max:+5"`},
				{`invariants[4]: invariant "no_overlap:a": want no_overlap:tagA,tagB`, `"no_overlap:a"`},
				{`invariants[5]: invariant "no_overlap:a,B": tag name "B"`, `"no_overlap:a,B"`},
				{`invariants[6]: invariant "no_overlap:a,b,c": want no_overlap:tagA,tagB`, `"no_overlap:a,b,c"`},
				{`invariants[7]: invariant "speed:3": unknown built-in "speed"`, `"speed:3"`},
				{`invariants[8]: invariant name "Game"`, `"Game"`},
				{`invariants[10]: duplicate invariant "within_bounds"`, `"within_bounds"]`},
			}},
		{"screenshots", with(`"screenshots": [0, 50, 50, 40, 101, -1]`),
			[]wantErr{
				{`screenshots[2]: tick 50 must be greater than the previous screenshot tick 50`, `50, 40`},
				{`screenshots[3]: tick 40 must be greater than`, `40,`},
				{`screenshots[4]: 101 is after the last tick`, `101`},
				{`screenshots[5]: -1 must not be negative`, `-1]`},
				{`screenshots[5]: tick -1 must be greater than the previous screenshot tick 101`, `-1]`},
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseScenario("s.scenario.json", []byte(c.src))
			checkErrs(t, c.src, err, c.wants...)
		})
	}
}

// Tick ranges are not checked against an invalid ticks value, but every other problem
// is still reported.
func TestParseScenarioReportsAll(t *testing.T) {
	src := "{\n  \"veduta\": \"scenario/1\",\n  \"scene\": \"\",\n  \"ticks\": 0,\n  \"inputs\": [ { \"tick\": 5000, \"press\": [\"KeyQ\", \"q\"] } ],\n  \"expect\": [ { \"tick\": 3, \"trace\": \"x\" } ]\n}"
	_, err := ParseScenario("s.scenario.json", []byte(src))
	es := sourceErrors(t, err)
	want := [][2]int{{3, 12}, {4, 12}, {5, 49}, {6, 15}}
	if len(es) != len(want) {
		t.Fatalf("got %d errors, want %d:\n%v", len(es), len(want), err)
	}
	for i, w := range want {
		if es[i].Line != w[0] || es[i].Col != w[1] {
			t.Errorf("error %d = %v, want at %d:%d", i, es[i], w[0], w[1])
		}
	}
}

func TestExpectationValueTypes(t *testing.T) {
	ok := []struct {
		path, op string
		value    any
	}{
		{"position.x", "<", -1.5}, {"position.y", "==", 0}, {"rotation_deg.y", ">=", 90},
		{"scale", "==", []any{1.0, 1.0, 1.0}}, {"aabb.max", "!=", []float64{1, 2, 3}},
		{"visible", "!=", false}, {"tags", "contains", "gem"}, {"tags", "==", []string{"a", "b"}},
		{"kind", "contains", "coll"}, {"parent", "==", ""}, {"model", "!=", "hero"},
		{"state.score", ">", 3}, {"state.inv.count", "==", "x"}, {"state.flags", "contains", true},
		{"state.Score", "==", []any{"a", 1.0, true}},
	}
	for _, c := range ok {
		sc, err := CompileScenario("t", &ScenarioSource{Scene: "main", Ticks: 10, Expect: []ExpectSource{
			{Tick: 10, Entity: "p", Path: c.path, Op: c.op, Value: c.value}}}, nil)
		if err != nil {
			t.Errorf("%s %s %v: %v", c.path, c.op, c.value, err)
			continue
		}
		if v := sc.Expect[0].Value; reflect.TypeOf(v).Kind() == reflect.Int {
			t.Errorf("value %v not normalized", v)
		}
	}
	sc, err := CompileScenario("t", &ScenarioSource{Scene: "main", Ticks: 10, Expect: []ExpectSource{
		{Tick: 1, Entity: "p", Path: "position.x", Op: "<", Value: 3}}}, nil)
	if err != nil || sc.Expect[0].Value != float64(3) {
		t.Fatalf("int value: %v %v", sc, err)
	}
}

func TestParseInvariant(t *testing.T) {
	cases := []struct {
		in   string
		want Invariant
	}{
		{"finite_positions", Invariant{Name: "finite_positions"}},
		{"within_bounds", Invariant{Name: "within_bounds"}},
		{"entity_count_max:500", Invariant{Name: "entity_count_max", Max: 500}},
		{"entity_count_max:007", Invariant{Name: "entity_count_max", Max: 7}},
		{"no_overlap:gem,wall", Invariant{Name: "no_overlap", Tags: [2]string{"gem", "wall"}}},
		{"no_overlap:wall,wall", Invariant{Name: "no_overlap", Tags: [2]string{"wall", "wall"}}},
		{"score_never_negative", Invariant{Name: "score_never_negative"}},
	}
	for _, c := range cases {
		got, err := ParseInvariant(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParseInvariant(%q) = %+v, %v", c.in, got, err)
		}
		if got.Builtin() != (c.in != "score_never_negative") {
			t.Errorf("%q Builtin = %v", c.in, got.Builtin())
		}
	}
	if s := (Invariant{Name: "entity_count_max", Max: 7}).String(); s != "entity_count_max:7" {
		t.Errorf("String = %q", s)
	}
	for _, bad := range []string{"", ":", "entity_count_max:", "entity_count_max:99999999999", "no_overlap:", "no_overlap:,a", "no_overlap:a, b", "finite_positions:", "A"} {
		if _, err := ParseInvariant(bad); err == nil {
			t.Errorf("ParseInvariant(%q) accepted", bad)
		}
	}
}

func TestScenarioDocExamples(t *testing.T) {
	for i, ex := range docJSON(t, "scenario.md") {
		if _, err := ParseScenario("example.scenario.json", []byte(ex)); err != nil {
			t.Errorf("docs/scenario.md example %d: %v", i, err)
		}
	}
	doc := string(mustRead(t, "../docs/scenario.md"))
	for _, p := range ExpectPaths {
		root := strings.SplitN(p, ".", 2)[0]
		if !strings.Contains(doc, "`"+root) {
			t.Errorf("docs/scenario.md does not mention path %s", p)
		}
	}
}
