package scene

import (
	"reflect"
	"testing"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/gmath"
)

// drawNames draws the scene and returns the entity names of the commands in order and
// the stats.
func drawNames(s *Scene, res *Resources, cam Camera) ([]string, []gfx.MeshID, DrawStats) {
	var dl gfx.DrawList
	var st DrawStats
	s.Draw(&dl, res, DrawOptions{Camera: cam, Width: 320, Height: 240, Mode: gfx.ModeColor, Stats: &st})
	var names []string
	var meshes []gfx.MeshID
	for _, c := range dl.Cmds {
		names = append(names, s.Get(c.ID).Name)
		meshes = append(meshes, c.Mesh)
	}
	return names, meshes, st
}

// Entities whose drawn bounds lie wholly outside the view volume produce no command;
// one that straddles a plane still does.
func TestDrawCullsOutsideView(t *testing.T) {
	res := drawResources()
	s := New("cull", res.Bounds)
	for _, e := range []Entity{
		{Name: "front", Transform: at(0, 0, 0)},
		{Name: "behind", Transform: at(0, 0, 20)},
		{Name: "beyond_far", Transform: at(0, 0, -200)},
		{Name: "left", Transform: at(-40, 0, 0)},
		{Name: "straddles_left", Transform: at(-6.2, 0, 0)},
		{Name: "above", Transform: at(0, 30, -5)},
		{Name: "near_plane", Transform: at(0, 0, 9.6)},
		{Name: "hidden", Transform: at(0, 0, 1)},
		{Name: "hitbox_far", Transform: at(0, 0, -3), Hitbox: &gmath.AABB{Min: gmath.V3(100, 0, 0), Max: gmath.V3(101, 1, 1)}},
	} {
		e.Model, e.Visible = "box", e.Name != "hidden"
		s.Spawn(e)
	}
	s.Update()
	// Perspective from z = 10 looking down -Z, 60° vertical: at the origin the view is
	// ±5.77 m high and ±7.70 m wide; far is 100 m away.
	cam := Camera{FovDeg: 60, Near: 0.5, Far: 100, Position: gmath.V3(0, 0, 10)}
	names, _, st := drawNames(s, res, cam)
	want := []string{"front", "straddles_left", "near_plane", "hitbox_far"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("drawn %v, want %v", names, want)
	}
	if st != (DrawStats{Entities: 8, Culled: 4}) {
		t.Fatalf("stats %+v", st)
	}
	// Orthographic: only the sides and the near and far planes cut.
	ortho := Camera{Ortho: true, Size: 10, Near: 0.1, Far: 50, Position: gmath.V3(0, 0, 10)}
	names, _, st = drawNames(s, res, ortho)
	if want := []string{"front", "straddles_left", "near_plane", "hitbox_far"}; !reflect.DeepEqual(names, want) || st.Culled != 4 {
		t.Fatalf("ortho: drawn %v (%+v), want %v", names, st, want)
	}
}

// lodResources adds "tree": base mesh 1, automatic levels at 10 and 20 m (meshes 2 and
// 3), "tree_far" from 40 m, not drawn beyond 60 m; "tree_far" is mesh 4 with two parts.
func lodResources() *Resources {
	res := drawResources()
	part := []gfx.MeshPart{{First: 0, Count: 3}}
	bounds := gmath.AABB{Min: gmath.V3(-0.5, 0, -0.5), Max: gmath.V3(0.5, 2, 0.5)}
	tree := &asset.Model{Name: "tree", Materials: []string{""}, DrawDistance: 60,
		Mesh: gfx.MeshData{Bounds: bounds, Parts: part},
		LODs: []asset.LOD{{Distance: 10, Mesh: gfx.MeshData{Parts: part}}, {Distance: 20, Mesh: gfx.MeshData{Parts: part}}, {Distance: 40, Model: "tree_far"}},
	}
	far := &asset.Model{Name: "tree_far", Materials: []string{"", "leaf"},
		Mesh: gfx.MeshData{Bounds: bounds, Parts: []gfx.MeshPart{{First: 0, Count: 3}, {First: 3, Count: 3, Material: 1}}}}
	res.Models["tree"] = &ModelRes{Mesh: 1, Model: tree, LODs: []gfx.MeshID{2, 3, 0}}
	res.Models["tree_far"] = &ModelRes{Mesh: 4, Model: far}
	return res
}

// Levels of detail are chosen by the distance from the eye to the nearest point of the
// drawn bounds as seen through a 60° lens; beyond draw_distance nothing is drawn.
func TestDrawLevelsOfDetail(t *testing.T) {
	res := lodResources()
	s := New("lod", res.Bounds)
	// Nearest points at z = -0.5 + position: distances 5.5, 12.5, 25.5, 45.5, 65.5 m.
	for i, z := range []float32{5, -2, -15, -35, -55} {
		s.Spawn(Entity{Name: string(rune('a' + i)), Model: "tree", Visible: true, Transform: at(0, -1, z)})
	}
	s.Update()
	cam := Camera{FovDeg: 60, Near: 0.1, Far: 200, Position: gmath.V3(0, 0, 11)}
	names, meshes, st := drawNames(s, res, cam)
	if want := []string{"a", "b", "c", "d", "d"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("drawn %v, want %v", names, want)
	}
	if want := []gfx.MeshID{1, 2, 3, 4, 4}; !reflect.DeepEqual(meshes, want) {
		t.Fatalf("meshes %v, want %v", meshes, want)
	}
	if st != (DrawStats{Entities: 5, Distant: 1, Reduced: 3}) {
		t.Fatalf("stats %+v", st)
	}

	// A 30° lens sees farther: distances count tan(15°)/tan(30°) = 0.464 of the meters.
	narrow := cam
	narrow.FovDeg = 30
	if _, meshes, _ := drawNames(s, res, narrow); !reflect.DeepEqual(meshes, []gfx.MeshID{1, 1, 2, 3, 3}) {
		t.Fatalf("narrow lens meshes %v", meshes)
	}

	// Orthographic: every entity at 0.866 × size.
	for _, c := range []struct {
		size float32
		want gfx.MeshID
	}{{10, 1}, {12, 2}, {30, 3}, {50, 4}, {80, 0}} {
		ortho := Camera{Ortho: true, Size: c.size, Near: 0.1, Far: 200, Position: gmath.V3(0, 0, 11)}
		_, meshes, _ := drawNames(s, res, ortho)
		for _, m := range meshes {
			if m != c.want {
				t.Fatalf("ortho size %v: meshes %v, want all %d", c.size, meshes, c.want)
			}
		}
		if c.want == 0 && len(meshes) != 0 {
			t.Fatalf("ortho size %v: drawn %v beyond draw_distance", c.size, meshes)
		}
	}

	// A level naming a model that is not uploaded draws the base mesh.
	delete(res.Models, "tree_far")
	if _, meshes, st := drawNames(s, res, cam); !reflect.DeepEqual(meshes, []gfx.MeshID{1, 2, 3, 1}) || st.Reduced != 2 {
		t.Fatalf("missing lod model: meshes %v, stats %+v", meshes, st)
	}
}

// countingBackend records mesh uploads.
type countingBackend struct {
	gfx.Backend
	created int
	updated []gfx.MeshID
}

func (b *countingBackend) CreateMesh(*gfx.MeshData) (gfx.MeshID, error) {
	b.created++
	return gfx.MeshID(b.created), nil
}

func (b *countingBackend) UpdateMesh(id gfx.MeshID, _ *gfx.MeshData) error {
	b.updated = append(b.updated, id)
	return nil
}

// AddModel uploads every level of a runtime model and Remove frees every handle for the
// next model.
func TestAddModelLevels(t *testing.T) {
	b := &countingBackend{}
	r := &Resources{Models: map[string]*ModelRes{}}
	m := &asset.Model{LODs: []asset.LOD{{Distance: 5}, {Distance: 9, Model: "x"}, {Distance: 12}}}
	if err := r.AddModel(b, "a", m); err != nil {
		t.Fatal(err)
	}
	if a := r.Models["a"]; b.created != 3 || a.Mesh != 1 || !reflect.DeepEqual(a.LODs, []gfx.MeshID{2, 0, 3}) {
		t.Fatalf("created %d, %+v", b.created, a)
	}
	r.Remove("a")
	if err := r.AddModel(b, "b", &asset.Model{LODs: []asset.LOD{{Distance: 5}}}); err != nil {
		t.Fatal(err)
	}
	if b.created != 3 || len(b.updated) != 2 || r.Models["b"].Mesh == 0 || r.Models["b"].LODs[0] == 0 {
		t.Fatalf("reuse: created %d updated %v %+v", b.created, b.updated, r.Models["b"])
	}
	// Replacing a model keeps its handles; one more level takes the last free handle.
	if err := r.AddModel(b, "b", m); err != nil {
		t.Fatal(err)
	}
	if b.created != 3 || len(b.updated) != 5 {
		t.Fatalf("replace: created %d updated %v %+v", b.created, b.updated, r.Models["b"])
	}
}
