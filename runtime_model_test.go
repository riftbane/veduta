package veduta

import (
	"strings"
	"testing"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gmath"
	"github.com/riftbane/veduta/scene"
)

// A game builds a model at runtime, entities draw it with bounds, a new model replaces it
// on the next frame and a removed one draws nothing.
func TestRuntimeModels(t *testing.T) {
	p, a := testAssets()
	e := newEngine(&testGame{}, p, a)
	if err := e.start(runOptions{Scene: "main", Seed: 1, Headless: true}); err != nil {
		t.Fatal(err)
	}
	defer e.close()
	ctx := &e.ctx
	for _, c := range []struct {
		name string
		m    *asset.Model
		want string
	}{
		{"chunk", cube(), "must contain ':'"},
		{"world:x", cube(), "belong to worlds"},
		{"game:nil", nil, "nil model"},
		{"game:bad", &asset.Model{Mesh: gfx.MeshData{Indices: []uint32{0, 1}}}, "whole triangles"},
		{"game:range", &asset.Model{Mesh: gfx.MeshData{Vertices: make([]gfx.Vertex, 2), Indices: []uint32{0, 1, 2}}}, "index 2 is 2"},
		{"game:lod", &asset.Model{Mesh: cube().Mesh, LODs: []asset.LOD{{Distance: 5}}}, "level 1: 0 parts"},
	} {
		if err := ctx.SetModel(c.name, c.m); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("SetModel %q: %v, want %q", c.name, err, c.want)
		}
	}
	big := cube()
	for i := range big.Mesh.Vertices {
		big.Mesh.Vertices[i].Pos = big.Mesh.Vertices[i].Pos.Scale(2)
	}
	big.Mesh.Bounds = gmath.AABB{Min: gmath.V3(-1, -1, -1), Max: gmath.V3(1, 1, 1)}
	if err := ctx.SetModel("game:block", big); err != nil {
		t.Fatal(err)
	}
	ent := ctx.Spawn(scene.Entity{Name: "block", Kind: "static", Model: "game:block", Transform: scene.Identity(), Visible: true})
	ent.Transform.Position = gmath.V3(0, 1, 0)
	if err := e.step(Input{}); err != nil {
		t.Fatal(err)
	}
	if ent.AABB != (gmath.AABB{Min: gmath.V3(-1, 0, -1), Max: gmath.V3(1, 2, 1)}) || ctx.Model("game:block") != big {
		t.Fatalf("bounds %v", ent.AABB)
	}
	cam := ctx.Scene.Camera
	covered := func() int {
		f, err := e.render(cam, 96, 72, gfx.ModeColor, false)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, id := range f.FB.ID {
			if id == ent.ID {
				n++
			}
		}
		return n
	}
	large := covered()
	if large == 0 || e.res.Models["game:block"].Model != big {
		t.Fatalf("runtime model not drawn (%d pixels)", large)
	}
	if err := ctx.SetModel("game:block", cube()); err != nil {
		t.Fatal(err)
	}
	if small := covered(); small == 0 || small >= large {
		t.Fatalf("replaced model covers %d pixels, was %d", small, large)
	}
	ctx.RemoveModel("game:block")
	if n := covered(); n != 0 || e.res.Models["game:block"] != nil {
		t.Fatalf("removed model still drawn (%d pixels)", n)
	}
}
