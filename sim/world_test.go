package sim

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"github.com/riftbane/veduta/gmath"
	"github.com/riftbane/veduta/scene"
)

func boxBounds(string) (gmath.AABB, bool) {
	return gmath.AABB{Min: gmath.V3(-0.5, 0, -0.5), Max: gmath.V3(0.5, 1, 0.5)}, true
}

func world() *scene.Scene {
	s := scene.New("t", boxBounds)
	s.Spawn(scene.Entity{Name: "ground", Kind: "static", Model: "box", Transform: scene.Transform{Position: gmath.V3(0, -1, 0), Scale: gmath.V3(20, 1, 20)}, Visible: true})
	s.Spawn(scene.Entity{Name: "wall", Kind: "static", Model: "box", Transform: scene.Transform{Position: gmath.V3(0, -1, 0)}, Visible: true})
	s.Spawn(scene.Entity{Name: "player", Kind: "player", Model: "box", Tags: []string{"player"}, Visible: true})
	s.Spawn(scene.Entity{Name: "gem", Kind: "gem", Model: "box", Tags: []string{"gem"}, Transform: scene.Transform{Position: gmath.V3(3, 0, 0)}, Visible: true})
	s.Update()
	return s
}

func TestContactsBeginOnce(t *testing.T) {
	s := world()
	c := NewContacts()
	if got := c.Step(s); len(got) != 0 {
		t.Fatalf("initial contacts %v (static/static and touching must not count)", got)
	}
	gem := s.Find("gem")
	gem.Transform.Position = gmath.V3(0.5, 0, 0)
	s.Update()
	if got := c.Step(s); len(got) != 1 || got[0] != (Pair{3, 4}) {
		t.Fatalf("contacts %v", got)
	}
	if got := c.Step(s); len(got) != 0 {
		t.Fatalf("contact reported twice: %v", got)
	}
	if !c.Touching(4, 3) {
		t.Fatal("Touching should be symmetric")
	}
	gem.Transform.Position = gmath.V3(3, 0, 0)
	s.Update()
	c.Step(s)
	gem.Transform.Position = gmath.V3(0.2, 0, 0)
	s.Update()
	if got := c.Step(s); len(got) != 1 {
		t.Fatalf("re-entering contact not reported: %v", got)
	}
}

func TestInvariants(t *testing.T) {
	s := world()
	bounds := gmath.AABB{Min: gmath.V3(-10, -5, -10), Max: gmath.V3(10, 10, 10)}
	custom := map[string]func() bool{"score_non_negative": func() bool { return true }}
	var list []Invariant
	for _, spec := range []string{"finite_positions", "within_bounds", "entity_count_max:4", "no_overlap:player,gem", "score_non_negative"} {
		inv, err := ParseInvariant(spec, bounds, custom)
		if err != nil {
			t.Fatal(err)
		}
		list = append(list, inv)
	}
	for _, bad := range []string{"entity_count_max", "entity_count_max:0", "no_overlap:a", "within_bounds:3", "nope"} {
		if _, err := ParseInvariant(bad, bounds, custom); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	m := NewMonitor(list)
	if v := m.Check(0, s); len(v) != 0 {
		t.Fatalf("clean world violates %v", v)
	}
	s.Find("player").Transform.Position = gmath.V3(50, 0, 0)
	s.Find("gem").Transform.Position = gmath.V3(float32(math.NaN()), 0, 0)
	s.Spawn(scene.Entity{Kind: "extra"})
	s.Update()
	v := m.Check(7, s)
	names := []string{}
	for _, x := range v {
		names = append(names, x.Name)
	}
	if strings.Join(names, " ") != "finite_positions within_bounds entity_count_max:4" {
		t.Fatalf("violations %v", v)
	}
	if v2 := m.Check(8, s); len(v2) != 0 {
		t.Fatalf("still-failing invariants must not be reported again: %v", v2)
	}
	if f := m.First(); len(f) != 3 || f[0].Tick != 7 || !strings.Contains(f[1].Detail, "player at [50,0,0]") {
		t.Fatalf("first violations %+v", f)
	}
}

func TestRecorderHashAndCounts(t *testing.T) {
	run := func() (string, string) {
		var buf bytes.Buffer
		r := NewRecorder(&buf)
		s := world()
		r.Emit(EventSceneLoad, map[string]any{"scene": "t"})
		if err := r.EndTick(0, Summaries(s)); err != nil {
			t.Fatal(err)
		}
		r.Emit("gem_collected", map[string]any{"gem": "gem", "score": 1})
		r.EndTick(1, Summaries(s))
		return r.Hash(), buf.String()
	}
	h1, t1 := run()
	h2, t2 := run()
	if h1 != h2 || t1 != t2 {
		t.Fatal("trace is not deterministic")
	}
	lines := strings.Split(strings.TrimSpace(t1), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[1], `{"entities":[{"aabb":`) || !strings.Contains(lines[1], `"events":[{"event":"gem_collected","gem":"gem","score":1}],"tick":1}`) {
		t.Fatalf("trace:\n%s", t1)
	}
	r := NewRecorder(nil)
	r.Emit("a", nil)
	r.Emit("a", nil)
	r.EndTick(0, nil)
	if r.Count("a") != 2 || r.Ticks() != 1 {
		t.Fatal("counts wrong")
	}
}
