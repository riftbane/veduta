package world

import (
	"math"
	"reflect"
	"testing"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/asset/model"
	"github.com/riftbane/veduta/gmath"
)

const vegetationWorldSrc = `{
  "veduta": "world/1", "seed": 5, "cell": 1, "chunk": 16, "extent": 8, "view": 1, "biome_scale": 32,
  "camera": { "type": "orthographic", "size": 12, "position": [0, 30, 0], "look_at": [0, 0, 0] },
  "biomes": [ { "name": "plain", "ground": "grass", "weight": 3 }, { "name": "forest", "ground": "moss", "weight": 1 } ],
  "terrain": { "relief": 1 },
  "features": [ { "name": "pond", "kind": "lake", "cell": [8, 8], "radius": 4, "roughness": 0 } ],
  "vegetation": [
    { "name": "grove", "prefab": "bush", "density": 0.5, "cell": [-20, -20], "radius": 10 },
    { "name": "grass", "model": "tuft", "density": 0.6 },
    { "name": "tulips", "model": "tulip", "density": 1, "cell": [8, -8], "radius": 6, "scale": [0.5, 1.5] },
    { "name": "meadow", "model": "tuft", "density": 0.3, "biomes": ["plain"], "cell": [8, -8], "radius": 6 }
  ],
  "scatter": [ { "prefab": "tree", "density": 0.3 } ],
  "places": [ { "name": "home", "prefab": "house", "cell": [3, -9] } ]
}`

func vegetationGen(t *testing.T) (*Gen, func(string) *asset.Model) {
	t.Helper()
	pf := testPrefabs(t)
	w, err := asset.ParseWorld("veg.world.json", []byte(vegetationWorldSrc), pf)
	if err != nil {
		t.Fatal(err)
	}
	models := map[string]*asset.Model{}
	for name, src := range map[string]string{
		"tuft":      `{"veduta": "model/1", "pivot": "bottom-center", "lod": [{"distance": 5}, {"distance": 9, "model": "tuft_far"}], "draw_distance": 14, "parts": [{"shape": "cylinder", "radius": 0.1, "height": 0.3, "segments": 8, "material": "blade"}]}`,
		"tuft_far":  `{"veduta": "model/1", "pivot": "bottom-center", "parts": [{"shape": "box", "size": [0.2, 0.2, 0.2], "material": "hay"}]}`,
		"tulip":     `{"veduta": "model/1", "pivot": "bottom-center", "parts": [{"shape": "cylinder", "radius": 0.02, "height": 0.4, "segments": 4, "position": [0.05, 0.2, 0]}, {"shape": "sphere", "radius": 0.08, "segments": 6, "rings": 3, "position": [0.05, 0.45, 0], "material": "petal"}]}`,
		"unused_ok": `{"veduta": "model/1", "parts": [{"shape": "box", "size": [1, 1, 1]}]}`,
	} {
		m, err := model.Parse(name+".model.json", []byte(src))
		if err != nil {
			t.Fatal(err)
		}
		models[name] = m
	}
	return New(w, pf), func(n string) *asset.Model { return models[n] }
}

// A vegetation prefab rule scatters inside its area only, after the scatter rules; flora
// rules put instances on dry cells outside footprints, several rules on one cell.
func TestVegetationRules(t *testing.T) {
	g, _ := vegetationGen(t)
	grove := 0
	for cz := int32(-3); cz < 1; cz++ {
		for cx := int32(-3); cx < 1; cx++ {
			c := g.Chunk(cx, cz)
			for _, s := range c.Scatter {
				if s.Vegetation == "" {
					if s.Prefab.Name != "tree" {
						t.Fatalf("scatter %s is %s", s.Key, s.Prefab.Name)
					}
					continue
				}
				grove++
				dx, dz := float64(s.Cell[0])+0.5+20, float64(s.Cell[1])+0.5+20
				if s.Vegetation != "grove" || s.Prefab.Name != "bush" || math.Hypot(dx, dz) > 10*1.15+0.01 {
					t.Fatalf("grove bush %s at %v (%v from the centre)", s.Key, s.Cell, math.Hypot(dx, dz))
				}
			}
		}
	}
	if grove < 30 {
		t.Fatalf("%d bushes in the grove", grove)
	}

	c := g.Chunk(0, -1) // the tulips and the meadow around [8, -8], the home at [3, -9]
	home := g.Places()[0].Rect
	per := map[int]int{}
	cells := map[[2]int32]int{}
	for _, f := range c.Flora {
		per[f.Rule]++
		cells[f.Cell]++
		if home.Contains(f.Cell[0], f.Cell[1]) || c.Grid.CellWater(f.Cell[0], f.Cell[1]) {
			t.Fatalf("flora %+v on the home or on water", f)
		}
		fx, fz := float64(f.Pos.X)-float64(f.Cell[0]), float64(f.Pos.Z)-float64(f.Cell[1])
		if fx < 0.2 || fx > 0.8 || fz < 0.2 || fz > 0.8 {
			t.Fatalf("flora at %v leaves the middle of cell %v", f.Pos, f.Cell)
		}
		if y := c.HeightAt(f.Pos.X, f.Pos.Z, 1); f.Pos.Y != y {
			t.Fatalf("flora at y %v, ground %v", f.Pos.Y, y)
		}
		if g.W.Vegetation[f.Rule].Name == "tulips" && (f.Scale < 0.5 || f.Scale > 1.5) {
			t.Fatalf("tulip scale %v", f.Scale)
		}
	}
	if per[1] < 100 || per[2] < 60 || per[3] < 10 {
		t.Fatalf("instances per rule %v", per)
	}
	shared := 0
	for _, n := range cells {
		if n > 1 {
			shared++
		}
	}
	if shared == 0 {
		t.Fatal("no cell has two flora rules")
	}
	if got := g.FloraModels(c); !reflect.DeepEqual(got, []string{"tuft", "tulip"}) {
		t.Fatalf("flora models %v", got)
	}
	// Chunks generate alike in any order.
	g2, _ := vegetationGen(t)
	g2.Chunk(1, 1)
	if c2 := g2.Chunk(0, -1); !reflect.DeepEqual(c2.Flora, c.Flora) {
		t.Fatal("flora depends on generation order")
	}
}

// The flora model of a chunk: every instance at level 0, the ones whose rank keeps them
// at each level with that level's geometry, the source's draw distance, faces pointing
// the way their normals do even when mirrored.
func TestFloraModel(t *testing.T) {
	g, models := vegetationGen(t)
	c := g.Chunk(0, -1)
	m := g.Flora(c, "tuft", models)
	if m.Name != "world:flora:tuft:0:-1" || m.DrawDistance != 14 || len(m.LODs) != 2 || m.LODs[0].Distance != 5 || m.LODs[1].Distance != 9 {
		t.Fatalf("model %s draw %v levels %d", m.Name, m.DrawDistance, len(m.LODs))
	}
	if !reflect.DeepEqual(m.Materials, []string{"blade", "hay"}) || len(m.Mesh.Parts) != 2 || len(m.LODs[1].Mesh.Parts) != 2 {
		t.Fatalf("materials %v", m.Materials)
	}
	var insts []Flora
	for _, f := range c.Flora {
		if g.W.Vegetation[f.Rule].Model == "tuft" {
			insts = append(insts, f)
		}
	}
	tuft, far := models("tuft"), models("tuft_far")
	keep := func(k uint) int {
		n := 0
		for _, f := range insts {
			if uint64(f.Rank) < uint64(1)<<32>>k {
				n++
			}
		}
		return n
	}
	if got, want := len(m.Mesh.Indices)/3, len(insts)*tuft.Triangles(0); got != want || m.Mesh.Parts[1].Count != 0 {
		t.Fatalf("level 0: %d triangles, want %d", got, want)
	}
	if got, want := len(m.LODs[0].Mesh.Indices)/3, keep(1)*tuft.Triangles(1); got != want {
		t.Fatalf("level 1: %d triangles, want %d", got, want)
	}
	if got, want := len(m.LODs[1].Mesh.Indices)/3, keep(2)*far.Triangles(0); got != want || m.LODs[1].Mesh.Parts[0].Count != 0 {
		t.Fatalf("level 2: %d triangles, want %d", got, want)
	}
	if keep(1) < len(insts)/3 || keep(1) > 2*len(insts)/3 {
		t.Fatalf("level 1 keeps %d of %d", keep(1), len(insts))
	}
	mirrored := false
	for _, f := range insts {
		mirrored = mirrored || f.Mirror
	}
	if !mirrored {
		t.Fatal("no mirrored instance")
	}
	md := &m.Mesh
	for i := 0; i < len(md.Indices); i += 3 {
		a, b, d := md.Vertices[md.Indices[i]], md.Vertices[md.Indices[i+1]], md.Vertices[md.Indices[i+2]]
		face := cross(a.Pos, b.Pos, d.Pos)
		if face.Dot(a.Normal.Add(b.Normal).Add(d.Normal)) <= 0 && face.Len() > 1e-6 {
			t.Fatalf("triangle %d faces %v against its normals", i/3, face)
		}
	}
	if !m.Mesh.Bounds.ContainsBox(m.LODs[1].Mesh.Bounds) || m.Mesh.Bounds.Min.Y > 1.5 {
		t.Fatalf("bounds %v", m.Mesh.Bounds)
	}
	if g.Flora(c, "nothing", models) != nil {
		t.Fatal("a missing model makes flora")
	}
	e := g.FloraEntity(c, "tuft")
	if e.Name != "chunk_0_n1_flora_tuft" || e.Model != m.Name || e.Position != gmath.V3(0, 0, -16) || !e.Hitbox.IsEmpty() {
		t.Fatalf("entity %+v", e)
	}
}
