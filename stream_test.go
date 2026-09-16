package veduta

import (
	"bytes"
	"strings"
	"testing"

	"github.com/riftbane/veduta/v2/gmath"
	"github.com/riftbane/veduta/v2/sim"
)

// runWorld starts the test world at cell at and walks the script for ticks.
func runWorld(t *testing.T, seed uint64, at [2]int32, ticks int, g *testGame) (string, *engine) {
	t.Helper()
	p, a := testAssets()
	var buf bytes.Buffer
	e := newEngine(g, p, a)
	if err := e.start(runOptions{World: "land", At: at, Seed: seed, Trace: &buf, Headless: true}); err != nil {
		t.Fatal(err)
	}
	sc := walkScript(t)
	for tick := 1; tick <= ticks; tick++ {
		if err := e.step(sc.Input(uint64(tick))); err != nil {
			t.Fatal(err)
		}
	}
	return buf.String(), e
}

func TestWorldLoadsWindowAndStreams(t *testing.T) {
	_, e := runWorld(t, 1, [2]int32{0, 0}, 0, &testGame{})
	w := e.ctx.World()
	if w == nil || w.Name != "land" || len(w.Loaded()) != 9 || w.FocusedCell() != [2]int32{0, 0} {
		t.Fatalf("world %+v", w)
	}
	if e.rec.Count("chunk_load") != 9 || e.rec.Count("world_load") != 1 || e.rec.Count("spawn") != 0 || e.rec.Count(sim.EventCollision) != 0 {
		t.Fatalf("events %v", e.rec.Counts())
	}
	s := e.ctx.Scene
	if s.Find("player") == nil || s.Find("chunk_0_0_ground") == nil || s.Find("chunk_n1_n1_ground") == nil || s.Find("place_start_walls") == nil {
		t.Fatal("entities missing")
	}
	if roof := s.Find("place_start_roof"); roof == nil || roof.Parent != s.Find("place_start_walls").ID {
		t.Fatal("prefab parent not resolved")
	}
	if g := s.Find("chunk_0_0_ground"); !g.AABB.IsEmpty() || g.Transform.Position != gmath.Zero3 {
		t.Fatalf("ground %+v", g)
	}
	// The player stands on the hut: an overlap at load is not a collision.
	if p := s.Find("player"); len(e.ctx.Overlapping(p)) == 0 {
		t.Fatal("the player does not overlap the hut it stands on")
	}
	// Move the focus one chunk east: three chunks load, none unload (hysteresis).
	w.FocusCell([2]int32{4, 0})
	before := e.rec.Count("chunk_load")
	e.step(Input{})
	if len(w.Loaded()) != 12 || e.rec.Count("chunk_load")-before != 3 || e.rec.Count("chunk_unload") != 0 {
		t.Fatalf("after one chunk east: %d loaded, events %v", len(w.Loaded()), e.rec.Counts())
	}
	// Back and forth over the border loads nothing.
	w.FocusCell([2]int32{3, 0})
	e.step(Input{})
	w.FocusCell([2]int32{4, 0})
	e.step(Input{})
	if len(w.Loaded()) != 12 || e.rec.Count("chunk_load")-before != 3 {
		t.Fatalf("oscillation reloaded: %v", e.rec.Counts())
	}
	// Two chunks east: the west column goes.
	w.FocusCell([2]int32{8, 0})
	e.step(Input{})
	if len(w.Loaded()) != 12 || e.rec.Count("chunk_unload") != 3 || e.ctx.Scene.Find("chunk_n1_n1_ground") != nil {
		t.Fatalf("after two chunks east: %d loaded, events %v", len(w.Loaded()), e.rec.Counts())
	}
	if w.Loaded()[0] != [2]int32{0, -1} || w.CellOf(gmath.V3(-0.1, 0, 2.6)) != [2]int32{-1, 5} {
		t.Fatalf("loaded %v", w.Loaded())
	}
	// No entity outlives its chunk: every live streamed entity belongs to a loaded chunk.
	for _, ent := range e.ctx.Scene.Entities() {
		if ent.Alive() && strings.HasPrefix(ent.Name, "chunk_n1_") {
			t.Fatalf("%s still alive", ent.Name)
		}
	}
}

func TestWorldDeterministicAndCollects(t *testing.T) {
	t1, e1 := runWorld(t, 42, [2]int32{0, 0}, 120, &testGame{})
	t2, e2 := runWorld(t, 42, [2]int32{0, 0}, 120, &testGame{})
	if t1 != t2 || e1.rec.Hash() != e2.rec.Hash() {
		t.Fatal("same seed and input produced different traces")
	}
	if e1.rec.Count("chunk_load") <= 9 || e1.rec.Count("chunk_unload") == 0 {
		t.Fatalf("the walk did not stream: %v", e1.rec.Counts())
	}
	if len(e1.monitor.First()) != 0 {
		t.Fatalf("violations %v", e1.monitor.First())
	}
	_, e3 := runWorld(t, 42, [2]int32{40, -20}, 120, &testGame{})
	if e3.rec.Hash() == e1.rec.Hash() {
		t.Fatal("the start cell does not change the run")
	}
	// The player started at the centre of cell 40 (20.25 m) and walked 1 m east.
	if p := e3.ctx.Scene.Find("player"); p == nil || p.Transform.Position.X < 21 || p.Transform.Position.X > 21.5 {
		t.Fatalf("player at %v, want near x = 21.25", p.Transform.Position)
	}
}

func TestWorldSnapshotRestoreIdentical(t *testing.T) {
	full, _ := runWorld(t, 7, [2]int32{0, 0}, 120, &testGame{})
	want := strings.Split(strings.TrimSpace(full), "\n")[61:]

	p, a := testAssets()
	e := newEngine(&testGame{}, p, a)
	if err := e.start(runOptions{World: "land", Seed: 7, Headless: true}); err != nil {
		t.Fatal(err)
	}
	sc := walkScript(t)
	for tick := 1; tick <= 60; tick++ {
		e.step(sc.Input(uint64(tick)))
	}
	snap, err := e.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	scState := sc.State()

	p2, a2 := testAssets()
	e2 := newEngine(&testGame{}, p2, a2)
	if err := e2.prepare(runOptions{Scene: "main", Seed: 999, Headless: true}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := e2.restore(snap, &buf); err != nil {
		t.Fatal(err)
	}
	if w := e2.ctx.World(); w == nil || len(w.Loaded()) != len(e.ctx.World().Loaded()) || w.FocusedCell() != e.ctx.World().FocusedCell() {
		t.Fatal("restored world differs")
	}
	sc2 := walkScript(t)
	sc2.SetState(scState)
	for tick := 61; tick <= 120; tick++ {
		e2.step(sc2.Input(uint64(tick)))
	}
	got := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(got) != len(want) {
		t.Fatalf("restored run has %d lines, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d differs after restore:\n got %.300s\nwant %.300s", i+61, got[i], want[i])
		}
	}
	// The restored run renders: the ground meshes were regenerated.
	cam, _ := e2.ctx.Scene.CameraPreset("scene", 4.0/3)
	if _, err := e2.render(cam, 64, 48, 0, false); err != nil {
		t.Fatal(err)
	}
}

func TestWorldBoundsAndLoadScene(t *testing.T) {
	p, a := testAssets()
	e := newEngine(&testGame{}, p, a)
	// The project's bounds end at 10 m; the world reaches 32 m and the player starts at 20.
	if err := e.start(runOptions{World: "land", At: [2]int32{40, 0}, Seed: 1, Headless: true, Invariants: []string{"within_bounds"}}); err != nil {
		t.Fatal(err)
	}
	if v := e.monitor.First(); len(v) != 0 {
		t.Fatalf("violations in the world: %v", v)
	}
	if b := e.world.worldBounds(); b.Min.X != -32 || b.Max.Z != 32 || b.Min.Y != -5 || b.Max.Y != 10 {
		t.Fatalf("world bounds %v", b)
	}
	if err := e.ctx.LoadScene("main"); err != nil {
		t.Fatal(err)
	}
	if e.ctx.World() != nil || e.ctx.Scene.Find("chunk_0_0_ground") != nil {
		t.Fatal("the world outlived LoadScene")
	}
	e.ctx.Scene.Find("player").Transform.Position.X = 20
	e.step(Input{})
	if v := e.monitor.First(); len(v) != 1 || v[0].Name != "within_bounds" {
		t.Fatalf("the project's bounds did not come back: %v", v)
	}
	// And back into the world from Update.
	if err := e.ctx.LoadWorld("land", [2]int32{2, 2}); err != nil {
		t.Fatal(err)
	}
	e.ctx.Scene.Find("player").Transform.Position.X = 20
	e.step(Input{})
	if v := e.monitor.First(); len(v) != 0 {
		t.Fatalf("20 m is inside the world, violations %v", v)
	}
	e.ctx.Scene.Find("player").Transform.Position.X = 40
	e.step(Input{})
	if v := e.monitor.First(); len(v) != 1 || !strings.Contains(v[0].Detail, "32") {
		t.Fatalf("40 m is outside the world, violations %v", v)
	}
	if err := e.ctx.LoadWorld("nowhere", [2]int32{}); err == nil {
		t.Fatal("unknown world accepted")
	}
}

func TestWorldRendersGround(t *testing.T) {
	_, e := runWorld(t, 1, [2]int32{0, 0}, 0, &testGame{})
	cam, _ := e.ctx.Scene.CameraPreset("top", 4.0/3)
	f, err := e.render(cam, 96, 72, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(e.res.Models); n != 1+9 {
		t.Fatalf("%d models uploaded, want the box and 9 grounds", n)
	}
	seen := map[uint32]bool{}
	for _, id := range f.FB.ID {
		seen[id] = true
	}
	if !seen[e.ctx.Scene.Find("chunk_0_0_ground").ID] {
		t.Fatal("the ground is not drawn")
	}
	// Streaming away reuses handles: no more meshes than chunks ever loaded at once.
	e.ctx.World().FocusCell([2]int32{40, 40})
	e.step(Input{})
	e.step(Input{})
	if _, err := e.render(cam, 96, 72, 0, false); err != nil {
		t.Fatal(err)
	}
	if n := len(e.res.Models); n != 1+9 {
		t.Fatalf("%d models after moving", n)
	}
}
