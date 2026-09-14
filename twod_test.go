package veduta

import (
	"bytes"
	"strings"
	"testing"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gmath"
	"github.com/riftbane/veduta/scene"
	"github.com/riftbane/veduta/sim"
)

func init() {
	// tslide moves right by half a meter per tick, in the XY plane of a 2D game.
	RegisterKind("tslide", func(e *scene.Entity) Behaviour {
		return BehaviourFunc(func(ctx *Context, e *scene.Entity, in Input) {
			e.Transform.Position.X += 0.5
		})
	})
	// tlift climbs a quarter meter per tick: up the screen of a 2D game.
	RegisterKind("tlift", func(e *scene.Entity) Behaviour {
		return BehaviourFunc(func(ctx *Context, e *scene.Entity, in Input) {
			e.Transform.Position.Y += 0.25
		})
	})
}

// The trajectory tile of a 2D game (orthographic camera looking down -Z) is drawn in the
// XY plane, where the game moves; seen from the top, as for a 3D game, a climb collapses
// onto a single point.
func TestTrajectoryTileOf2DGame(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cam   asset.Camera
		label string
		rows  func(n int) bool
	}{
		{"2d", asset.Camera{Ortho: true, Size: 10, Near: 0.1, Far: 200, Position: gmath.V3(0, 0, 100)}, "trajectories (xy)",
			func(n int) bool { return n > 40 }},
		{"3d", asset.Camera{FovDeg: 60, Near: 0.1, Far: 200, Position: gmath.V3(0, 6, 10)}, "trajectories (top)",
			func(n int) bool { return n <= 2 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, a := flatAssets(true)
			src := a.Scenes["flat"]
			src.Camera = tc.cam
			src.Entities = append(src.Entities, asset.Entity{Name: "lift", Kind: "tlift", Model: "flat", Position: gmath.V3(3, -2, 0), Scale: gmath.One3, Visible: true})
			e := newEngine(&testGame{}, p, a)
			defer e.close()
			if err := e.start(runOptions{Scene: "flat", Seed: 1, Headless: true}); err != nil {
				t.Fatal(err)
			}
			trail := newTrails()
			trail.record(e.ctx.Scene)
			for tick := 1; tick <= 20; tick++ {
				if err := e.step(Input{}); err != nil {
					t.Fatal(err)
				}
				trail.record(e.ctx.Scene)
			}
			img, label, err := trail.render(e, 160, 120)
			if err != nil {
				t.Fatal(err)
			}
			if label != tc.label {
				t.Errorf("label %q, want %q", label, tc.label)
			}
			lift := e.ctx.Scene.Find("lift")
			color := gfx.IDColor(lift.ID)
			rows := map[int]bool{}
			for y := 0; y < img.H; y++ {
				for x := 0; x < img.W; x++ {
					if img.At(x, y) == color {
						rows[y] = true
					}
				}
			}
			if !tc.rows(len(rows)) {
				t.Errorf("the lift's 5 m climb covers %d rows of the tile", len(rows))
			}
		})
	}
}

// flatAssets is a 2D world: a hero sliding right into a coin, both drawn with a model that
// has no thickness along Z (an XY quad), optionally with a hitbox on each (and the hero
// on layer 3).
func flatAssets(hitbox bool) (*asset.Project, *Assets) {
	p, a := testAssets()
	quad := cube()
	quad.Name = "flat"
	quad.Mesh.Bounds = gmath.AABB{Min: gmath.V3(-0.5, -0.5, 0), Max: gmath.V3(0.5, 0.5, 0)}
	a.Models["flat"] = quad
	var box *gmath.AABB
	layer := 0
	if hitbox {
		box = &gmath.AABB{Min: gmath.V3(-0.5, -0.5, -0.5), Max: gmath.V3(0.5, 0.5, 0.5)}
		layer = 3
	}
	a.Scenes["flat"] = &asset.Scene{Name: "flat",
		Camera: asset.Camera{Ortho: true, Size: 10, Near: 0.1, Far: 200, Position: gmath.V3(0, 0, 100)},
		Light:  gfx.DefaultLight, Background: 0xff202830,
		Entities: []asset.Entity{
			{Name: "hero", Kind: "tslide", Model: "flat", Position: gmath.V3(-3, 0, 0), Scale: gmath.One3, Tags: []string{"hero"}, Visible: true, Hitbox: box, Layer: layer},
			{Name: "coin", Kind: "static", Model: "flat", Scale: gmath.One3, Tags: []string{"coin"}, Visible: true, Hitbox: box},
		}}
	return p, a
}

func runFlat(t *testing.T, hitbox bool, ticks int) (string, *engine) {
	t.Helper()
	p, a := flatAssets(hitbox)
	var buf bytes.Buffer
	e := newEngine(&testGame{}, p, a)
	if err := e.start(runOptions{Scene: "flat", Seed: 1, Trace: &buf, Headless: true, Invariants: []string{"no_overlap:hero,coin"}}); err != nil {
		t.Fatal(err)
	}
	for tick := 1; tick <= ticks; tick++ {
		if err := e.step(Input{}); err != nil {
			t.Fatal(err)
		}
	}
	return buf.String(), e
}

// Two quads in the same plane: without thickness their AABBs never overlap (Overlaps is
// strict on every axis), so the hero slides through the coin silently; with hitboxes the
// collision event, the no_overlap invariant and ctx.Overlapping all see the contact.
func TestHitboxMakesCoplanarQuadsCollide(t *testing.T) {
	_, flat := runFlat(t, false, 6) // hero x: -3 → 0, straight through the coin
	defer flat.close()
	if n := flat.rec.Count(sim.EventCollision); n != 0 {
		t.Fatalf("zero-thickness quads collided %d times; the documented trap no longer holds", n)
	}
	if v := flat.monitor.First(); len(v) != 0 {
		t.Fatalf("zero-thickness quads violated %v", v)
	}
	hero := flat.ctx.Scene.Find("hero")
	if hero.WorldPosition().X != 0 || hero.AABB.Size().Z != 0 || len(flat.ctx.Overlapping(hero)) != 0 {
		t.Fatalf("hero at %v with AABB %v overlaps %v", hero.WorldPosition(), hero.AABB, flat.ctx.Overlapping(hero))
	}

	trace, boxed := runFlat(t, true, 6)
	defer boxed.close()
	if n := boxed.rec.Count(sim.EventCollision); n != 1 {
		t.Fatalf("quads with hitboxes collided %d times, want 1", n)
	}
	if v := boxed.monitor.First(); len(v) != 1 || v[0].Name != "no_overlap:hero,coin" {
		t.Fatalf("violations %+v", v)
	}
	hero, coin := boxed.ctx.Scene.Find("hero"), boxed.ctx.Scene.Find("coin")
	if o := boxed.ctx.Overlapping(hero); len(o) != 1 || o[0] != coin {
		t.Fatalf("hero overlaps %v", o)
	}
	// The trace's aabb is the hitbox in world space.
	last := strings.Split(strings.TrimSpace(trace), "\n")[6]
	if !strings.Contains(last, `"aabb":{"max":[0.5,0.5,0.5],"min":[-0.5,-0.5,-0.5]},"id":1`) {
		t.Fatalf("tick 6 does not report the hero's hitbox as its aabb: %s", last)
	}
}

// Snapshots carry hitboxes and layers: a run restored mid-way reports the same collision.
func TestSnapshotKeepsHitboxAndLayer(t *testing.T) {
	full, e := runFlat(t, true, 8)
	e.close()
	want := strings.Split(strings.TrimSpace(full), "\n")[4:]

	_, half := runFlat(t, true, 3)
	defer half.close()
	snap, err := half.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	p, a := flatAssets(false) // the scene file no longer has hitboxes: only the snapshot does
	e2 := newEngine(&testGame{}, p, a)
	defer e2.close()
	if err := e2.prepare(runOptions{Scene: "flat", Seed: 1, Headless: true, Invariants: []string{"no_overlap:hero,coin"}}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := e2.restore(snap, &buf); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := e2.step(Input{}); err != nil {
			t.Fatal(err)
		}
	}
	got := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("restored run differs:\n got %s\nwant %s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if e2.rec.Count(sim.EventCollision) != 1 {
		t.Fatal("the restored hitboxes did not collide")
	}
	if l := e2.ctx.Scene.Find("hero").Layer; l != 3 {
		t.Fatalf("restored hero layer %d, want 3", l)
	}
}
