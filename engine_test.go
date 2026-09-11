package veduta

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"strings"
	"testing"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gmath"
	"github.com/riftbane/veduta/scene"
	"github.com/riftbane/veduta/sim"
)

// testGame is a tiny game: tplayer moves with WASD (plus RNG jitter so seeds matter),
// tgem is collected on contact with the player.
type testGame struct {
	score int
}

var current *testGame

func init() {
	RegisterKind("tplayer", func(e *scene.Entity) Behaviour {
		return BehaviourFunc(func(ctx *Context, e *scene.Entity, in Input) {
			dir := gmath.V3(in.Axis("KeyA", "KeyD"), 0, in.Axis("KeyW", "KeyS"))
			step := dir.Scale(3 * ctx.DT)
			step.X += (ctx.RNG.Float32() - 0.5) * 0.001
			e.Transform.Position = e.Transform.Position.Add(step)
		})
	})
	RegisterKind("tgem", func(e *scene.Entity) Behaviour {
		e.State = &gemState{Value: 1}
		return BehaviourFunc(func(ctx *Context, e *scene.Entity, in Input) {
			for _, o := range ctx.Overlapping(e) {
				if o.HasTag("player") {
					st := e.State.(*gemState)
					current.score += st.Value
					ctx.Trace("gem_collected", map[string]any{"gem": e.Name, "score": current.score})
					ctx.Despawn(e)
					return
				}
			}
		})
	})
}

type gemState struct{ Value int }

func (g *testGame) Init(ctx *Context) error {
	current = g
	ctx.Invariant("score_non_negative", func() bool { return g.score >= 0 })
	ctx.RegisterState(g)
	return nil
}

func (g *testGame) Update(ctx *Context, in Input) {
	if in.JustPressed("KeyR") {
		g.score = 0
		ctx.LoadScene(ctx.Scene.Name)
	}
}

func (g *testGame) Draw(ctx *Context, dl *gfx.DrawList) {
	b := ctx.HUD(dl)
	ctx.Text(b, 4, 4, 1, fmt.Sprintf("SCORE %d", g.score), 0xffffffff)
	b.End()
}

func (g *testGame) SaveState(enc *gob.Encoder) error { return enc.Encode(g.score) }
func (g *testGame) LoadState(dec *gob.Decoder) error { return dec.Decode(&g.score) }

func cube() *asset.Model {
	m := &asset.Model{Name: "box", Materials: []string{""}}
	faces := [][3]gmath.Vec3{
		{{X: 1}, {Z: -1}, {Y: 1}}, {{X: -1}, {Z: 1}, {Y: 1}}, {{Y: 1}, {X: 1}, {Z: -1}},
		{{Y: -1}, {X: 1}, {Z: 1}}, {{Z: 1}, {X: 1}, {Y: 1}}, {{Z: -1}, {X: -1}, {Y: 1}},
	}
	for _, f := range faces {
		n, u, v := f[0], f[1].Scale(0.5), f[2].Scale(0.5)
		c := n.Scale(0.5).Add(gmath.V3(0, 0.5, 0))
		base := uint32(len(m.Mesh.Vertices))
		for _, p := range []gmath.Vec3{c.Sub(u).Sub(v), c.Add(u).Sub(v), c.Add(u).Add(v), c.Sub(u).Add(v)} {
			m.Mesh.Vertices = append(m.Mesh.Vertices, gfx.Vertex{Pos: p, Normal: n})
		}
		m.Mesh.Indices = append(m.Mesh.Indices, base, base+1, base+2, base, base+2, base+3)
	}
	m.Mesh.Parts = []gfx.MeshPart{{First: 0, Count: 36}}
	m.Mesh.Bounds = gmath.AABB{Min: gmath.V3(-0.5, 0, -0.5), Max: gmath.V3(0.5, 1, 0.5)}
	return m
}

func testAssets() (*asset.Project, *Assets) {
	p := &asset.Project{Name: "t", TickRate: 60, DefaultScene: "main", DefaultSeed: 1,
		Resolution: [2]int{320, 180}, InspectResolution: [2]int{160, 90},
		Invariants: []string{"finite_positions", "within_bounds", "score_non_negative"},
		Bounds:     gmath.AABB{Min: gmath.V3(-10, -5, -10), Max: gmath.V3(10, 10, 10)}}
	red := &asset.Material{Name: "red", Albedo: 0xffd04040, Alpha: "opaque", Cutoff: 0.5}
	gem := &asset.Material{Name: "gem", Albedo: 0xff40a0ff, Alpha: "opaque", Cutoff: 0.5}
	main := &asset.Scene{Name: "main", Camera: asset.Camera{FovDeg: 60, Near: 0.1, Far: 100, Position: gmath.V3(0, 6, 8)},
		Light: gfx.DefaultLight, Background: 0xff202830,
		Entities: []asset.Entity{
			{Name: "ground", Kind: "static", Model: "box", Position: gmath.V3(0, -1, 0), Scale: gmath.V3(12, 1, 12), Visible: true},
			{Name: "player", Kind: "tplayer", Model: "box", Material: "red", Scale: gmath.One3, Tags: []string{"player"}, Visible: true},
			{Name: "gem_1", Kind: "tgem", Model: "box", Material: "gem", Position: gmath.V3(0, 0, -3), Scale: gmath.V3(0.4, 0.4, 0.4), Tags: []string{"gem"}, Visible: true},
			{Name: "gem_2", Kind: "tgem", Model: "box", Material: "gem", Position: gmath.V3(4, 0, 0), Scale: gmath.V3(0.4, 0.4, 0.4), Tags: []string{"gem"}, Visible: true},
		}}
	return p, &Assets{
		Models:    map[string]*asset.Model{"box": cube()},
		Textures:  map[string]*asset.Texture{},
		Materials: map[string]*asset.Material{"red": red, "gem": gem},
		Scenes:    map[string]*asset.Scene{"main": main},
	}
}

func walkScript(t *testing.T) *sim.Script {
	sc, err := sim.NewScript([]sim.InputEvent{
		{Tick: 10, Press: []string{"KeyW"}},
		{Tick: 70, Release: []string{"KeyW"}},
		{Tick: 80, Press: []string{"KeyD"}},
		{Tick: 100, Release: []string{"KeyD"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return sc
}

// run simulates ticks and returns the trace text and engine.
func run(t *testing.T, seed uint64, ticks int, g *testGame) (string, *engine) {
	t.Helper()
	p, a := testAssets()
	var buf bytes.Buffer
	e := newEngine(g, p, a)
	if err := e.start(runOptions{Scene: "main", Seed: seed, Trace: &buf, Headless: true}); err != nil {
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

func TestEngineDeterministicAndCollects(t *testing.T) {
	t1, e1 := run(t, 42, 120, &testGame{})
	t2, e2 := run(t, 42, 120, &testGame{})
	if t1 != t2 || e1.rec.Hash() != e2.rec.Hash() {
		t.Fatal("same seed and input produced different traces")
	}
	_, e3 := run(t, 43, 120, &testGame{})
	if e3.rec.Hash() == e1.rec.Hash() {
		t.Fatal("seed does not influence the trace")
	}
	if e1.rec.Count("gem_collected") != 1 || e1.ctx.Scene.Find("gem_1") != nil || e1.ctx.Scene.Find("gem_2") == nil {
		t.Fatalf("gem collection wrong: count %d", e1.rec.Count("gem_collected"))
	}
	lines := strings.Split(strings.TrimSpace(t1), "\n")
	if len(lines) != 121 || !strings.Contains(lines[0], `"event":"scene_load"`) {
		t.Fatalf("trace has %d lines; first: %.200s", len(lines), lines[0])
	}
	if e1.rec.Count(sim.EventCollision) < 1 {
		t.Fatal("no collision event recorded")
	}
	if len(e1.monitor.First()) != 0 {
		t.Fatalf("unexpected violations %v", e1.monitor.First())
	}
}

func TestEngineSnapshotRestoreIdentical(t *testing.T) {
	full, _ := run(t, 7, 120, &testGame{})
	want := strings.Split(strings.TrimSpace(full), "\n")[61:]

	p, a := testAssets()
	g := &testGame{}
	e := newEngine(g, p, a)
	if err := e.start(runOptions{Scene: "main", Seed: 7, Headless: true}); err != nil {
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
	g2 := &testGame{}
	e2 := newEngine(g2, p2, a2)
	if err := e2.prepare(runOptions{Scene: "main", Seed: 999, Headless: true}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := e2.restore(snap, &buf); err != nil {
		t.Fatal(err)
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
}

func TestEngineInvariantViolationAndReset(t *testing.T) {
	p, a := testAssets()
	p.Bounds = gmath.AABB{Min: gmath.V3(-1, -5, -1), Max: gmath.V3(1, 5, 1)}
	e := newEngine(&testGame{}, p, a)
	if err := e.start(runOptions{Scene: "main", Seed: 1, Headless: true, Invariants: []string{"within_bounds"}}); err == nil {
		// the ground and gem_2 start outside these bounds: violation at tick 0
		if v := e.monitor.First(); len(v) != 1 || v[0].Tick != 0 || v[0].Name != "within_bounds" {
			t.Fatalf("violations %+v", v)
		}
	} else {
		t.Fatal(err)
	}
	if e.rec.Count(sim.EventInvariantViolation) != 1 {
		t.Fatal("violation not traced")
	}
	var in sim.InputState
	in.KeyDown("KeyR")
	if err := e.step(in.Next()); err != nil {
		t.Fatal(err)
	}
	if e.rec.Count(sim.EventSceneLoad) != 2 || e.ctx.Scene.Find("gem_1") == nil {
		t.Fatal("KeyR did not reload the scene")
	}
	if _, err := ParseKindErr(); err != nil {
		t.Fatal(err)
	}
}

// ParseKindErr checks that an unregistered kind fails loudly.
func ParseKindErr() (bool, error) {
	p, a := testAssets()
	a.Scenes["main"].Entities[1].Kind = "nokind"
	e := newEngine(&testGame{}, p, a)
	err := e.start(runOptions{Scene: "main", Seed: 1})
	if err == nil || !strings.Contains(err.Error(), "not registered") {
		return false, fmt.Errorf("unregistered kind: %v", err)
	}
	a.Scenes["main"].Entities[1].Kind = "tplayer"
	return true, nil
}

func TestEngineRender(t *testing.T) {
	_, e := run(t, 1, 30, &testGame{})
	defer e.close()
	cam, err := e.ctx.Scene.CameraPreset("scene", 160.0/90)
	if err != nil {
		t.Fatal(err)
	}
	f, err := e.render(cam, 160, 90, gfx.ModeColor, false)
	if err != nil {
		t.Fatal(err)
	}
	player := e.ctx.Scene.Find("player")
	seen, hud := 0, 0
	for i, id := range f.FB.ID {
		if id == player.ID {
			seen++
		}
		if i < 160*12 && f.FB.Color[i] == 0xffffffff {
			hud++
		}
	}
	if seen == 0 || hud == 0 {
		t.Fatalf("player pixels %d, HUD pixels %d", seen, hud)
	}
	if f.Stats.Drawn == 0 {
		t.Fatal("nothing drawn")
	}
}
