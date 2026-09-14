package scene

import (
	"reflect"
	"testing"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gmath"
)

// drawResources is one single-part unit box model ("box") and two materials, "solid"
// (opaque) and "glass" (blend), without a backend: Draw only builds commands.
func drawResources() *Resources {
	box := &asset.Model{Name: "box", Materials: []string{""}, Mesh: gfx.MeshData{
		Bounds: gmath.AABB{Min: gmath.V3(-0.5, -0.5, -0.5), Max: gmath.V3(0.5, 0.5, 0.5)},
		Parts:  []gfx.MeshPart{{First: 0, Count: 36}},
	}}
	solid := asset.DefaultMaterial
	glass := asset.DefaultMaterial
	glass.Alpha = "blend"
	return &Resources{
		Models:    map[string]*ModelRes{"box": {Mesh: 1, Model: box}},
		Materials: map[string]*asset.Material{"solid": &solid, "glass": &glass},
		Textures:  map[string]gfx.TextureID{},
	}
}

// drawOrder spawns ents in order and returns the entity names in draw-command order.
func drawOrder(t *testing.T, cam Camera, ents ...Entity) []string {
	t.Helper()
	res := drawResources()
	s := New("order", res.Bounds)
	for _, e := range ents {
		e.Model, e.Visible = "box", true
		s.Spawn(e)
	}
	var dl gfx.DrawList
	s.Draw(&dl, res, DrawOptions{Camera: cam, Width: 320, Height: 240, Mode: gfx.ModeColor})
	var names []string
	for _, c := range dl.Cmds {
		names = append(names, s.Get(c.ID).Name)
	}
	return names
}

func at(x, y, z float32) Transform {
	return Transform{Position: gmath.V3(x, y, z), Rotation: gmath.QuatIdent(), Scale: gmath.One3}
}

// Layer is the first key of the draw order whatever the file order; within a layer the
// order is unchanged (opaque in id order, then blended back to front), and the default
// layer 0 keeps the order of a scene without layers.
func TestDrawOrderLayers(t *testing.T) {
	cam := Camera{Ortho: true, Size: 10, Near: 0.1, Far: 200, Position: gmath.V3(0, 0, 100)}
	got := drawOrder(t, cam,
		Entity{Name: "hud_glass", Material: "glass", Layer: 5, Transform: at(0, 0, 3)},
		Entity{Name: "far_glass", Material: "glass", Transform: at(0, 0, -2)},
		Entity{Name: "near_glass", Material: "glass", Transform: at(0, 0, 2)},
		Entity{Name: "hero", Material: "solid", Transform: at(1, 0, 0)},
		Entity{Name: "sky", Material: "solid", Layer: -3, Transform: at(0, 0, -5)},
		Entity{Name: "shadow", Material: "glass", Layer: -3, Transform: at(0, 0, 1)},
		Entity{Name: "coin", Material: "solid", Transform: at(2, 0, 0)},
	)
	want := []string{"sky", "shadow", "hero", "coin", "far_glass", "near_glass", "hud_glass"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("draw order %v, want %v", got, want)
	}

	plain := drawOrder(t, cam,
		Entity{Name: "b_glass", Material: "glass", Transform: at(0, 0, 1)},
		Entity{Name: "a", Material: "solid"},
		Entity{Name: "c", Material: "solid"},
	)
	if want := []string{"a", "c", "b_glass"}; !reflect.DeepEqual(plain, want) {
		t.Fatalf("draw order without layers %v, want %v", plain, want)
	}
}
