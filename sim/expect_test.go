package sim

import (
	"testing"

	"github.com/riftbane/veduta/gmath"
	"github.com/riftbane/veduta/scene"
)

type playerState struct {
	Score int     `json:"score"`
	Speed float32 `json:"speed"`
}

func TestEvaluate(t *testing.T) {
	s := world()
	p := s.Find("player")
	p.Transform.Position = gmath.V3(1, 0, -2.5)
	p.State = &playerState{Score: 3, Speed: 2.5}
	s.Update()
	rec := NewRecorder(nil)
	rec.Emit("gem_collected", nil)
	rec.EndTick(0, nil)
	one, three := 1, 3
	cases := []struct {
		x    Expectation
		pass bool
	}{
		{Expectation{Entity: "player", Path: "position.z", Op: "<", Value: -1.0}, true},
		{Expectation{Entity: "player", Path: "position.x", Op: "==", Value: 1.0}, true},
		{Expectation{Entity: "player", Path: "state.score", Op: ">=", Value: 3.0}, true},
		{Expectation{Entity: "player", Path: "state.speed", Op: "!=", Value: 2.5}, false},
		{Expectation{Entity: "player", Path: "tags", Op: "contains", Value: "player"}, true},
		{Expectation{Entity: "player", Path: "visible", Op: "==", Value: true}, true},
		{Expectation{Entity: "player", Path: "name", Op: "contains", Value: "lay"}, true},
		{Expectation{Entity: "player", Path: "aabb.max.y", Op: "==", Value: 1.0}, true},
		{Expectation{Trace: "gem_collected", CountMin: &one}, true},
		{Expectation{Trace: "gem_collected", CountMin: &three}, false},
		{Expectation{Trace: "nothing", CountMax: &one}, true},
	}
	for i, c := range cases {
		r := Evaluate(c.x, s, rec)
		if r.Pass != c.pass || r.Error != "" {
			t.Errorf("case %d %+v: pass=%v actual=%v err=%q", i, c.x, r.Pass, r.Actual, r.Error)
		}
	}
	for _, bad := range []Expectation{
		{Entity: "ghost", Path: "position.x", Op: "<", Value: 1.0},
		{Entity: "player", Path: "position.q", Op: "<", Value: 1.0},
		{Entity: "player", Path: "name", Op: "<", Value: 1.0},
	} {
		if r := Evaluate(bad, s, rec); r.Pass || r.Error == "" {
			t.Errorf("%+v should fail with an error, got %+v", bad, r)
		}
	}
	_ = scene.KindStatic
}
