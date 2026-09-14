package veduta

import (
	"bytes"
	"path/filepath"
	"strconv"
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

// twodProject is the 2D fixture: an orthographic camera 12 m tall at x = y = 0, sprites
// drawn as quads with hitboxes and layers, and two planes that show which rotation makes a
// plane face the camera.
var twodProject = filepath.Join("testdata", "twod")

// twodXY maps a world point of the fixture's scene to the pixel of its 320×240 frame that
// contains it; twodPixel formats it for query --at.
func twodXY(x, y float32) (int, int) {
	const ppm = 240.0 / 12 // pixels per meter
	// Rounded explicitly so arm64 cannot fuse the products into a multiply-add.
	return int(160 + float32(x*ppm)), int(120 - float32(y*ppm))
}

func twodPixel(x, y float32) string {
	px, py := twodXY(x, y)
	return strconv.Itoa(px) + "," + strconv.Itoa(py)
}

// The three settings of a sprite material, each checked against the alternative on the
// fixture's coin (a yellow disc on transparent texels):
//   - unlit: lit, a quad facing the camera takes the light at a grazing angle and comes
//     out dark;
//   - cutout: opaque draws the transparent texels as a black square; blend draws the disc
//     but owns no id pixel, so query --at and the ids buffer never see the sprite;
//   - nearest: magnified with bilinear filtering, the disc's edge mixes with the black of
//     the transparent texels into a dark fringe.
func TestSpriteMaterialTraps(t *testing.T) {
	const yellow, sky, black = 0xfff2c230, 0xff3060a0, 0xff000000
	// render draws the fixture's first frame with the coin's material changed and the
	// coin scaled by zoom (about its center) over the sky alone.
	render := func(t *testing.T, zoom float32, change func(m *asset.Material)) (*gfx.Framebuffer, uint32) {
		t.Helper()
		p, a, err := loadProject(twodProject)
		if err != nil {
			t.Fatal(err)
		}
		coin := *a.Materials["coin"]
		change(&coin)
		a.Materials["coin"] = &coin
		e := newEngine(&testGame{}, p, a)
		t.Cleanup(e.close)
		if err := e.start(runOptions{Scene: "main", Seed: 1, Headless: true}); err != nil {
			t.Fatal(err)
		}
		for _, o := range e.ctx.Scene.Entities() {
			o.Visible = o.Name == "coin" || o.Name == "sky"
		}
		ent := e.ctx.Scene.Find("coin")
		ent.Transform.Scale = gmath.V3(zoom, zoom, 1)
		e.ctx.Scene.Update()
		f, err := e.render(e.ctx.Scene.Camera, 320, 240, gfx.ModeColor, false)
		if err != nil {
			t.Fatal(err)
		}
		return f.FB, ent.ID
	}
	at := func(fb *gfx.Framebuffer, x, y float32) (color, id uint32) {
		px, py := twodXY(x, y)
		return fb.Color[py*fb.W+px], fb.ID[py*fb.W+px]
	}
	// fringe counts the coin's pixels whose color is not the disc's.
	fringe := func(fb *gfx.Framebuffer, id uint32) (n int) {
		for i, v := range fb.ID {
			if v == id && fb.Color[i] != yellow {
				n++
			}
		}
		return n
	}
	keep := func(*asset.Material) {}

	fb, id := render(t, 1, keep)
	if c, v := at(fb, 0, -3); c != yellow || v != id {
		t.Fatalf("sprite material: center %08x id %d", c, v)
	}
	if c, v := at(fb, 0.45, -2.55); c != sky || v == id {
		t.Fatalf("sprite material: corner %08x id %d", c, v)
	}
	if fb, id := render(t, 8, keep); fringe(fb, id) != 0 {
		t.Fatalf("sprite material magnified: %d coin pixels are not the disc's color", fringe(fb, id))
	}

	t.Run("lit", func(t *testing.T) {
		fb, _ := render(t, 1, func(m *asset.Material) { m.Unlit = false })
		c, _ := at(fb, 0, -3)
		if r, g, _, _ := gfx.UnpackRGBA(c); r > 0xf2*3/4 || g > 0xc2*3/4 {
			t.Errorf("lit center %08x: want it clearly darker than the texel %08x", c, uint32(yellow))
		}
	})
	t.Run("opaque", func(t *testing.T) {
		fb, id := render(t, 1, func(m *asset.Material) { m.Alpha = "opaque" })
		if c, v := at(fb, 0.45, -2.55); c != black || v != id {
			t.Errorf("opaque corner %08x id %d: want the black of a transparent texel, owned by the coin", c, v)
		}
	})
	t.Run("blend", func(t *testing.T) {
		fb, id := render(t, 1, func(m *asset.Material) { m.Alpha = "blend" })
		if c, v := at(fb, 0, -3); c != yellow || v == id {
			t.Errorf("blend center %08x id %d: want the disc drawn and the pixel owned by the sky", c, v)
		}
	})
	t.Run("bilinear", func(t *testing.T) {
		fb, id := render(t, 8, func(m *asset.Material) { m.Filter = gfx.FilterBilinear })
		if n := fringe(fb, id); n < 20 {
			t.Errorf("bilinear filtering left %d fringe pixels on the magnified disc's edge", n)
		}
	})
}

// Neither layer nor draw order overrides the depth test: the fixture's translucent shade
// (layer 1, drawn after every opaque sprite) darkens the hero only when it is nearer the
// camera. At the hero's own z it is hidden where the hero is.
func TestBlendedSpriteNeedsNearerZ(t *testing.T) {
	const hero = 0xffe04848
	for _, tc := range []struct {
		shadeZ float32
		dark   bool
	}{{2, true}, {1, false}} {
		p, a, err := loadProject(twodProject)
		if err != nil {
			t.Fatal(err)
		}
		e := newEngine(&testGame{}, p, a)
		defer e.close()
		if err := e.start(runOptions{Scene: "main", Seed: 1, Headless: true}); err != nil {
			t.Fatal(err)
		}
		e.ctx.Scene.Find("hero").Transform.Position = gmath.V3(4, 0, 1)
		e.ctx.Scene.Find("shade").Transform.Position.Z = tc.shadeZ
		e.ctx.Scene.Update()
		f, err := e.render(e.ctx.Scene.Camera, 320, 240, gfx.ModeColor, false)
		if err != nil {
			t.Fatal(err)
		}
		x, y := twodXY(4, 0)
		if c := f.FB.Color[y*f.FB.W+x]; (c != hero) != tc.dark {
			t.Errorf("shade at z = %g: hero pixel %08x (unshaded %08x), want darkened %v", tc.shadeZ, c, uint32(hero), tc.dark)
		}
	}
}

// A plane model turned to face the camera has no thickness along Z: its AABB is flat at
// z = 1, and at z = 0 only the rounding of the 90° rotation leaves a few 1e-8 m, so
// whether two such quads overlap depends on where they stand. A thin box does not.
func TestRotatedPlaneAABBIsFlat(t *testing.T) {
	p, a, err := loadProject(twodProject)
	if err != nil {
		t.Fatal(err)
	}
	e := newEngine(&testGame{}, p, a)
	defer e.close()
	if err := e.start(runOptions{Scene: "main", Seed: 1, Headless: true}); err != nil {
		t.Fatal(err)
	}
	plane := e.ctx.Scene.Find("plane_facing_camera") // at z = 1
	if d := plane.AABB.Size().Z; d != 0 {
		t.Errorf("plane at z = 1: AABB depth %g, want 0", d)
	}
	plane.Transform.Position.Z = 0
	e.ctx.Scene.Update()
	if d := plane.AABB.Size().Z; d == 0 || d > 1e-7 {
		t.Errorf("plane at z = 0: AABB depth %g, want a rounding residue in (0, 1e-7]", d)
	}
	quad := e.ctx.Scene.Find("sky") // the template's thin box, no hitbox
	if d := quad.AABB.Size().Z; d < 0.019 || d > 0.021 {
		t.Errorf("quad AABB depth %g, want 0.02", d)
	}
}

// The 2D fixture renders through the real headless path (compiled from the sources on
// disk), answers queries at pixels, and simulates a collision between two sprites.
func TestTwoDFixture(t *testing.T) {
	dir := t.TempDir()
	code, rep := runCmd(t, "-project", twodProject, "-headless", "render", "--bundle", "--out", filepath.Join(dir, "twod.png"))
	if code != exitOK {
		t.Fatalf("render: %d %v", code, rep)
	}
	seen := map[string]float64{}
	for _, e := range rep["entities"].([]any) {
		m := e.(map[string]any)
		seen[m["name"].(string)] = m["pixels"].(float64)
	}
	for _, name := range []string{"coin", "hero", "sky", "plane_facing_camera"} {
		if seen[name] == 0 {
			t.Errorf("%s is not visible: %v", name, seen)
		}
	}
	// A plane faces +Y: rotated -90° about X it faces away from a camera looking down -Z
	// and is culled (+90° faces the camera). A blended sprite never owns id pixels.
	for _, name := range []string{"plane_facing_away", "shade"} {
		if seen[name] != 0 {
			t.Errorf("%s owns %v pixels", name, seen[name])
		}
	}

	bundle := rep["bundle"].(string)
	query := func(at string) (entity, color string) {
		t.Helper()
		code, rep := runCmd(t, "-project", twodProject, "-headless", "query", "--frame", bundle, "--at", at)
		if code != exitOK {
			t.Fatalf("query %s: %d %v", at, code, rep)
		}
		px := rep["pixel"].(map[string]any)
		if e, ok := px["entity"].(map[string]any); ok {
			entity = e["name"].(string)
		}
		return entity, px["color"].(string)
	}
	// The coin's center is its texel color exactly: unlit, nearest filtering.
	if e, c := query(twodPixel(0, -3)); e != "coin" || c != "#f2c230" {
		t.Errorf("coin center: %s %s", e, c)
	}
	// Its corner is a transparent texel: cutout discards it and the sky shows through.
	if e, c := query(twodPixel(0.45, -2.55)); e != "sky" || c != "#3060a0" {
		t.Errorf("coin corner: %s %s", e, c)
	}
	// The blended shade (layer 1) darkens the sky without owning the pixel.
	if e, c := query(twodPixel(4, 0)); e != "sky" || c == "#3060a0" {
		t.Errorf("under the shade: %s %s", e, c)
	}

	code, sim := runCmd(t, "-project", twodProject, "-headless", "simulate", "--scene", "main", "--ticks", "12", "--out", filepath.Join(dir, "run"))
	if code != exitOK || sim["verdict"] != "pass" {
		t.Fatalf("simulate: %d %v", code, sim)
	}
	if ev := sim["events"].(map[string]any); ev["collision"] != 1.0 {
		t.Fatalf("hero and coin collided %v times, want 1: %v", ev["collision"], ev)
	}
}
