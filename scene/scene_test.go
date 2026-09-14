package scene

import (
	"testing"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gmath"
)

func unitBounds(string) (gmath.AABB, bool) {
	return gmath.AABB{Min: gmath.V3(-0.5, 0, -0.5), Max: gmath.V3(0.5, 1, 0.5)}, true
}

func testScene(t *testing.T) *Scene {
	t.Helper()
	src := &asset.Scene{
		Name:       "main",
		Camera:     asset.Camera{FovDeg: 60, Near: 0.1, Far: 200, Position: gmath.V3(0, 5, 10)},
		Light:      gfx.DefaultLight,
		Background: 0xff202830,
		Entities: []asset.Entity{
			{Name: "ground", Kind: "static", Model: "box", Scale: gmath.V3(10, 0.1, 10), Visible: true},
			{Name: "player", Kind: "player", Model: "box", Position: gmath.V3(0, 0, 0), RotationDeg: gmath.V3(0, 90, 0), Scale: gmath.One3, Tags: []string{"player"}, Visible: true},
			{Name: "hat", Kind: "static", Model: "box", Position: gmath.V3(0, 1, 0), Scale: gmath.V3(0.5, 0.5, 0.5), Parent: "player", Visible: true},
			{Name: "cam2", Kind: "camera", Position: gmath.V3(0, 2, 5), Scale: gmath.One3, Visible: true},
		},
	}
	s, err := Load(src, unitBounds)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestLoadAssignsIDsInFileOrder(t *testing.T) {
	s := testScene(t)
	for i, name := range []string{"ground", "player", "hat", "cam2"} {
		e := s.Find(name)
		if e == nil || e.ID != uint32(i+1) {
			t.Fatalf("%s: got %+v", name, e)
		}
	}
	if s.Find("hat").Parent != 2 {
		t.Fatal("parent not resolved")
	}
}

func TestHierarchyAndAABB(t *testing.T) {
	s := testScene(t)
	p, hat := s.Find("player"), s.Find("hat")
	p.Transform.Position = gmath.V3(3, 0, -2)
	s.Update()
	if got := hat.WorldPosition(); got.Sub(gmath.V3(3, 1, -2)).Len() > 1e-6 {
		t.Fatalf("hat world position %v", got)
	}
	want := gmath.AABB{Min: gmath.V3(2.75, 1, -2.25), Max: gmath.V3(3.25, 1.5, -1.75)}
	if hat.AABB.Min.Sub(want.Min).Len() > 1e-5 || hat.AABB.Max.Sub(want.Max).Len() > 1e-5 {
		t.Fatalf("hat AABB %v, want %v", hat.AABB, want)
	}
	if !s.Find("cam2").AABB.IsEmpty() {
		t.Fatal("entity without model should have an empty AABB")
	}
}

// A hitbox replaces the model bounds, follows the world transform (parents included),
// gives an AABB to an entity without a model, and is copied by Load, Spawn and Restore.
func TestHitboxAABB(t *testing.T) {
	s := testScene(t)
	box := gmath.AABB{Min: gmath.V3(-1, -0.25, -0.5), Max: gmath.V3(1, 0.25, 0.5)}
	hat := s.Find("hat") // model "box", child of the player, scale 0.5
	hat.Hitbox = &box
	s.Update()
	want := box.Transform(hat.World())
	if hat.AABB != want {
		t.Fatalf("hat AABB %v, want the hitbox in world space %v", hat.AABB, want)
	}
	if got := hat.AABB.Size(); got.Sub(gmath.V3(0.5, 0.25, 1)).Len() > 1e-5 { // scaled by 0.5, turned 90° with the player
		t.Fatalf("hat AABB size %v", got)
	}

	trigger := s.Spawn(Entity{Name: "trigger", Kind: "static", Hitbox: &box,
		Transform: Transform{Position: gmath.V3(4, 0, 0)}})
	if trigger.AABB != box.Translate(gmath.V3(4, 0, 0)) {
		t.Fatalf("model-less trigger AABB %v", trigger.AABB)
	}
	box.Max.X = 9 // the template's box; the spawned entity keeps its own copy
	s.Update()
	if trigger.AABB.Max.X != 5 {
		t.Fatalf("hitbox shared with the template: trigger %v", trigger.AABB)
	}

	inverted := s.Spawn(Entity{Name: "none", Kind: "static", Model: "box", Hitbox: &gmath.AABB{Min: gmath.One3, Max: gmath.Zero3}})
	if !inverted.AABB.IsEmpty() {
		t.Fatalf("an empty hitbox must leave the AABB empty, not fall back to the model: %v", inverted.AABB)
	}

	var ents []Entity
	for _, e := range s.Entities() {
		ents = append(ents, *e)
	}
	r := New("copy", unitBounds)
	if err := r.Restore(ents, s.NextID()); err != nil {
		t.Fatal(err)
	}
	if rt := r.Find("trigger"); rt.Hitbox == nil || rt.Hitbox == trigger.Hitbox || rt.AABB != trigger.AABB {
		t.Fatalf("restored trigger %+v", rt)
	}

	src := &asset.Scene{Name: "load", Camera: asset.Camera{FovDeg: 60, Near: 0.1, Far: 10, Position: gmath.V3(0, 0, 5)},
		Entities: []asset.Entity{{Name: "zone", Kind: "static", Position: gmath.V3(0, 2, 0), Scale: gmath.V3(2, 2, 2),
			Hitbox: &gmath.AABB{Min: gmath.V3(0, 0, 0), Max: gmath.V3(1, 1, 1)}}}}
	l, err := Load(src, unitBounds)
	if err != nil {
		t.Fatal(err)
	}
	if z := l.Find("zone"); z.AABB != (gmath.AABB{Min: gmath.V3(0, 2, 0), Max: gmath.V3(2, 4, 2)}) || z.Hitbox == src.Entities[0].Hitbox {
		t.Fatalf("loaded zone AABB %v (hitbox %p, source %p)", z.AABB, z.Hitbox, src.Entities[0].Hitbox)
	}
}

func TestSpawnDespawnFlush(t *testing.T) {
	s := testScene(t)
	var events []string
	s.OnEvent = func(name string, f map[string]any) { events = append(events, name+":"+f["name"].(string)) }
	a := s.Spawn(Entity{Kind: "gem", Model: "box", Visible: true})
	b := s.Spawn(Entity{Name: "player", Kind: "player"})
	if a.ID != 5 || a.Name != "gem_5" || b.Name != "player#6" {
		t.Fatalf("spawned %q(%d) %q(%d)", a.Name, a.ID, b.Name, b.ID)
	}
	if a.Transform.Scale != gmath.One3 || a.AABB.IsEmpty() {
		t.Fatal("spawned entity not initialized")
	}
	s.Despawn(s.Find("player")) // also despawns the hat
	if s.Find("hat") != nil || s.Len() != 4 {
		t.Fatalf("after despawn: len %d", s.Len())
	}
	if len(s.Entities()) != 6 {
		t.Fatal("despawned entities must stay in the slice until Flush")
	}
	s.Flush()
	if len(s.Entities()) != 4 || s.Get(2) != nil {
		t.Fatal("flush did not remove despawned entities")
	}
	want := []string{"spawn:gem_5", "spawn:player#6", "despawn:player", "despawn:hat"}
	if len(events) != len(want) {
		t.Fatalf("events %v", events)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("events %v, want %v", events, want)
		}
	}
	if s.Spawn(Entity{Kind: "x"}).ID != 7 {
		t.Fatal("ids must never be reused")
	}
}

func TestCameraPresets(t *testing.T) {
	s := testScene(t)
	for _, p := range append(append([]string{}, Presets...), "orbit:45", "cam2") {
		c, err := s.CameraPreset(p, 16.0/9)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		// The scene center must project inside the frame.
		v := c.GfxView(640, 360)
		q := v.Proj.Mul(v.View).Project(s.Bounds().Center())
		if p != "cam2" && (gmath.Abs(q.X) > 1 || gmath.Abs(q.Y) > 1 || q.Z < -1 || q.Z > 1) {
			t.Errorf("%s: scene center projects to %v", p, q)
		}
	}
	c, _ := s.CameraPreset("cam2", 1)
	if c.Target.Sub(gmath.V3(0, 2, 4)).Len() > 1e-5 {
		t.Errorf("camera entity looks at %v", c.Target)
	}
	if _, err := s.CameraPreset("nope", 1); err == nil {
		t.Error("unknown preset accepted")
	}
}

func TestFrameOrthoFitsBox(t *testing.T) {
	b := gmath.AABB{Min: gmath.V3(-4, 0, -1), Max: gmath.V3(4, 2, 1)}
	c := FrameOrtho(b, gmath.V3(0, 0, -1), 2)
	v := c.GfxView(200, 100)
	for _, p := range b.Corners() {
		q := v.Proj.Mul(v.View).Project(p)
		if gmath.Abs(q.X) > 1 || gmath.Abs(q.Y) > 1 {
			t.Fatalf("corner %v projects outside: %v", p, q)
		}
	}
}
