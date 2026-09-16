package world

import (
	"math"
	"testing"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gmath"
)

const terrainWorldSrc = `{
  "veduta": "world/1", "seed": 3, "cell": 1, "chunk": 16, "extent": 8, "view": 1, "biome_scale": 32,
  "camera": { "type": "orthographic", "size": 12, "position": [0, 30, 0], "look_at": [0, 0, 0] },
  "biomes": [ { "name": "plain", "ground": "grass", "weight": 3 }, { "name": "forest", "ground": "moss", "weight": 1 } ],
  "terrain": { "relief": 1.5, "relief_scale": 24 },
  "features": [
    { "name": "peak", "kind": "hill", "cell": [40, 40], "radius": 12, "height": 8, "roughness": 0 },
    { "name": "mesa", "kind": "plain", "cell": [40, 40], "radius": 5, "height": 5, "falloff": 2, "roughness": 0 },
    { "name": "pond", "kind": "lake", "cell": [-30, 10], "radius": 7, "depth": 2, "roughness": 0 },
    { "name": "bay", "kind": "sea", "cell": [-100, -100], "radius": 40, "height": -0.5 }
  ],
  "scatter": [ { "prefab": "bush", "density": 1 } ],
  "sites":   [ { "tag": "village", "prefabs": ["village"], "spacing": 24, "chance": 1 } ],
  "places":  [ { "name": "home", "prefab": "house", "cell": [34, 36] } ],
  "entities": [ { "name": "player", "kind": "player", "model": "hero" } ]
}`

func terrainGen(t testing.TB) *Gen {
	t.Helper()
	pf := testPrefabs(t.(*testing.T))
	w, err := asset.ParseWorld("terrain.world.json", []byte(terrainWorldSrc), pf)
	if err != nil {
		t.Fatal(err)
	}
	return New(w, pf)
}

// Features shape the ground in file order: the hill rises to its height at the centre
// and fades to the relief at its radius, the plain on top cuts a flat top at its level,
// the lake digs a bowl below its water level with a dry rim just above it.
func TestFeaturesShapeTheGround(t *testing.T) {
	g := terrainGen(t)
	relief := func(x, z int32) int64 { h, _ := g.natural(x, z, nil); return h }
	if h := relief(3, 7); h < -1500 || h > 1500 {
		t.Fatalf("relief %d mm outside ±1.5 m", h)
	}
	// The mesa: flat at 5 m within radius - falloff.
	for _, v := range [][2]int32{{40, 40}, {43, 40}, {40, 37}} {
		if h, _ := g.natural(v[0], v[1], g.features); h != 5000 {
			t.Fatalf("mesa at %v: %d mm, want 5000", v, h)
		}
	}
	// The hill beyond the mesa: above the relief, less so farther out; the relief alone
	// from its radius on.
	prev := int64(math.MaxInt64)
	for x := int32(46); x <= 51; x++ {
		h, _ := g.natural(x, 40, g.features)
		if h -= relief(x, 40); h > prev || h <= 0 {
			t.Fatalf("hill at x=%d: %d mm above the relief, %d before", x, h, prev)
		}
		prev = h
	}
	for _, v := range [][2]int32{{53, 40}, {40, 27}, {60, 60}} {
		if h, _ := g.natural(v[0], v[1], g.features); h != relief(v[0], v[1]) {
			t.Fatalf("outside the hill at %v: %d, relief %d", v, h, relief(v[0], v[1]))
		}
	}
	// The pond: its level is the ground at its centre before it; the bottom is 2 m below.
	pond := &g.features[2]
	before, _ := g.natural(-30, 10, g.features[:2])
	if pond.level != before {
		t.Fatalf("pond level %d, ground %d", pond.level, before)
	}
	if h, w := g.natural(-30, 10, g.features); h != pond.level-2000 || w != pond.level {
		t.Fatalf("pond centre %d under %d, want %d under %d", h, w, pond.level-2000, pond.level)
	}
	if h, w := g.natural(-30+8, 10, g.features); h != pond.level+rimMM || w != int64(NoWater) {
		t.Fatalf("pond rim %d (water %d), want %d dry", h, w, pond.level+rimMM)
	}
	// The sea's level is its height; its centre is 8 m (the default depth) below.
	if h, w := g.natural(-100, -100, g.features); h != -8500 || w != -500 {
		t.Fatalf("sea centre %d under %d", h, w)
	}
	// Cells: the pond's middle is water, its rim is not.
	gr := g.Grid(Rect{-40, 0, 20, 20}, 0)
	if !gr.CellWater(-30, 10) || gr.CellWater(-21, 10) || gr.CellWater(-45+10, 0) {
		t.Fatal("pond cells")
	}
}

// Sites never stand on water, places and sites stand on a pad levelled to the ground at
// their centre, and scatter skips water.
func TestTerrainStructuresAndScatter(t *testing.T) {
	g := terrainGen(t)
	reg := g.Region([2]int32{-60, -40}, 60)
	for _, s := range reg.Structs {
		if !g.dryFootprint(s.Rect) {
			t.Fatalf("%s on water", s.Key)
		}
		gr := g.Grid(s.Rect, 0)
		for _, h := range gr.Height {
			if h != s.Ground {
				t.Fatalf("%s: footprint vertex at %d mm, pad at %d", s.Key, h, s.Ground)
			}
		}
	}
	dropped := 0
	for rz := int32(-6); rz < 0; rz++ {
		for rx := int32(-6); rx < 0; rx++ {
			if c := g.siteCandidate(0, rx, rz); c != nil && g.site(0, rx, rz) == nil && !g.dryFootprint(c.Rect) {
				dropped++
			}
		}
	}
	if dropped == 0 {
		t.Log("no village candidate fell in the bay")
	}
	if is := g.Validate(testPrefabs(t)("house"), [2]int32{-31, 9}, 0, ""); len(is) != 1 || is[0].Code != CodePlaceWater {
		t.Fatalf("house in the pond: %+v", is)
	}
	home := g.Places()[0]
	ents := g.Instantiate(&home)
	if want := float32(float64(home.Ground) / 1000); ents[0].Position.Y != want || home.Ground == 0 {
		t.Fatalf("home walls at y %v, ground %d mm", ents[0].Position.Y, home.Ground)
	}
	// Scatter covers every dry free cell of a chunk inside the pond and none under water.
	c := g.Chunk(-2, 0)
	trees := map[[2]int32]Struct{}
	for _, s := range c.Scatter {
		trees[s.Cell] = s
	}
	water := 0
	for z := c.Rect.Z; z < c.Rect.Z+c.Rect.D; z++ {
		for x := c.Rect.X; x < c.Rect.X+c.Rect.W; x++ {
			s, ok := trees[[2]int32{x, z}]
			if c.Grid.CellWater(x, z) {
				water++
				if ok {
					t.Fatalf("bush on water at %d,%d", x, z)
				}
				continue
			}
			if ok {
				if centre := g.Center(x, z); s.Ground != int32(mm(c.HeightAt(centre.X, centre.Z, 1))) {
					t.Fatalf("bush at %d,%d stands at %d, ground %v", x, z, s.Ground, c.HeightAt(centre.X, centre.Z, 1))
				}
			}
		}
	}
	if water < 50 {
		t.Fatalf("only %d water cells in the pond's chunk", water)
	}
}

// HeightAt follows the finest level's triangles exactly, and WaterAt says where they lie
// under water.
func TestHeightAtFollowsTheMesh(t *testing.T) {
	g := terrainGen(t)
	c := g.Chunk(2, 2) // the hill
	m := g.Ground(c)
	origin := g.Origin(c.Rect.X, c.Rect.Z)
	n := len(m.Mesh.Parts)
	checked := 0
	for p := 0; p < n; p++ {
		part := m.Mesh.Parts[p]
		for i := part.First; i < part.First+part.Count; i += 3 {
			a := m.Mesh.Vertices[m.Mesh.Indices[i]].Pos
			b := m.Mesh.Vertices[m.Mesh.Indices[i+1]].Pos
			d := m.Mesh.Vertices[m.Mesh.Indices[i+2]].Pos
			if a.Y != b.Y && a.X == b.X && a.Z == b.Z || cross(a, b, d).Y <= 0 {
				continue // skirts
			}
			// The centroid lies inside the triangle.
			p := gmath.V3(float32((float64(a.X)+float64(b.X)+float64(d.X))/3), 0, float32((float64(a.Z)+float64(b.Z)+float64(d.Z))/3))
			want := float64(a.Y+b.Y+d.Y) / 3
			got := c.HeightAt(p.X+origin.X, p.Z+origin.Z, g.W.Cell)
			if math.Abs(float64(got)-want) > 1e-3 {
				t.Fatalf("height at %v: %v, triangle says %v", p, got, want)
			}
			checked++
		}
	}
	if checked < 100 {
		t.Fatalf("only %d triangles checked", checked)
	}
	if h := g.HeightAt(gmath.V3(40.5, 0, 40.5)); h != 5 {
		t.Fatalf("HeightAt on the mesa: %v", h)
	}
	if level, ok := g.WaterAt(gmath.V3(-29.5, 3, 10.5)); !ok || level != float32(float64(g.features[2].level)/1000) {
		t.Fatalf("WaterAt in the pond: %v %v", level, ok)
	}
	if _, ok := g.WaterAt(gmath.V3(40.5, 0, 40.5)); ok {
		t.Fatal("water on the mesa")
	}
}

func cross(a, b, c gmath.Vec3) gmath.Vec3 { return b.Sub(a).Cross(c.Sub(a)) }

// Every level of a hilly chunk has fewer triangles than the one before, faces up (or out,
// for skirts), keeps one part per material, and hangs its skirts low enough to close the
// crack against any other level of its neighbour.
func TestGroundLevelsAndSkirts(t *testing.T) {
	g := terrainGen(t)
	c := g.Chunk(2, 2)
	m := g.Ground(c)
	if len(m.LODs) < 3 {
		t.Fatalf("%d levels", len(m.LODs))
	}
	meshes := []*gfx.MeshData{&m.Mesh}
	for i := range m.LODs {
		l := &m.LODs[i]
		meshes = append(meshes, &l.Mesh)
		if want := float32(32 * math.Pow(2, float64(i))); i < 2 && l.Distance != want {
			t.Logf("level %d from %v m", i+1, l.Distance)
		}
		if i > 0 && l.Distance <= m.LODs[i-1].Distance {
			t.Fatalf("distances %v then %v", m.LODs[i-1].Distance, l.Distance)
		}
	}
	prev := math.MaxInt
	for k, md := range meshes {
		tris := len(md.Indices) / 3
		if tris >= prev || len(md.Parts) != len(m.Materials) {
			t.Fatalf("level %d: %d triangles after %d, %d parts", k, tris, prev, len(md.Parts))
		}
		prev = tris
		if !m.Mesh.Bounds.ContainsBox(md.Bounds) {
			t.Fatalf("level %d bounds %v outside the model's %v", k, md.Bounds, m.Mesh.Bounds)
		}
		for i := 0; i < len(md.Indices); i += 3 {
			a, b, d := md.Vertices[md.Indices[i]].Pos, md.Vertices[md.Indices[i+1]].Pos, md.Vertices[md.Indices[i+2]].Pos
			n := cross(a, b, d)
			switch {
			case n.Y > 0:
			case n.Y == 0 && (n.X != 0 || n.Z != 0):
				// A skirt faces out of the chunk.
				mid := gmath.V3(float32(float64(a.X+b.X+d.X)/3), 0, float32(float64(a.Z+b.Z+d.Z)/3))
				if n.X < 0 && mid.X > 0 || n.X > 0 && mid.X < 16 || n.Z < 0 && mid.Z > 0 || n.Z > 0 && mid.Z < 16 {
					t.Fatalf("level %d: skirt %v %v %v faces %v", k, a, b, d, n)
				}
			default:
				t.Fatalf("level %d: triangle %v %v %v faces %v", k, a, b, d, n)
			}
		}
	}
	// Along the chunk's west edge (x = 0), each level's edge polyline and skirt: the skirt
	// bottom is below every other level's edge at every sampled point.
	edge := func(md *gfx.MeshData, z float32) (top, bottom float32) {
		top, bottom = float32(math.Inf(-1)), float32(math.Inf(1))
		for i := 0; i < len(md.Indices); i += 3 {
			tri := [3]gmath.Vec3{md.Vertices[md.Indices[i]].Pos, md.Vertices[md.Indices[i+1]].Pos, md.Vertices[md.Indices[i+2]].Pos}
			for e := range 3 {
				p, q := tri[e], tri[(e+1)%3]
				if p.X != 0 || q.X != 0 || p.Z == q.Z || (z-p.Z)*(z-q.Z) > 0 {
					continue
				}
				y := p.Y + (q.Y-p.Y)*(z-p.Z)/(q.Z-p.Z)
				top, bottom = max(top, y), min(bottom, y)
			}
		}
		return top, bottom
	}
	for z := float32(0.25); z < 16; z += 0.5 {
		for i, a := range meshes {
			ta, ba := edge(a, z)
			for j, b := range meshes {
				tb, _ := edge(b, z)
				if ta > tb && ba > tb+1e-4 {
					t.Fatalf("z=%v: level %d edge %v with skirt to %v leaves a crack above level %d's edge %v", z, i, ta, ba, j, tb)
				}
			}
		}
	}
}

// A flat world keeps a flat ground: no skirts, and flat runs still merge.
func TestFlatGroundHasNoSkirts(t *testing.T) {
	g := newGen(t)
	m := g.Ground(g.Chunk(0, 0))
	for _, v := range m.Mesh.Vertices {
		if v.Pos.Y != 0 {
			t.Fatalf("vertex %v off the ground", v.Pos)
		}
	}
	if tris := len(m.Mesh.Indices) / 3; tris >= 2*256 {
		t.Fatalf("%d triangles for a flat chunk", tris)
	}
}

// The water of a chunk is one part with quads at the water level over the cells where
// some corner is under water.
func TestWaterQuads(t *testing.T) {
	g := terrainGen(t)
	c := g.Chunk(-2, 0)
	m := g.Ground(c)
	k := len(m.Materials) - 1
	if m.Materials[k] != WaterMaterial {
		t.Fatalf("materials %v", m.Materials)
	}
	part := m.Mesh.Parts[k]
	level := mmMeters(g.features[2].level)
	area := 0.0
	for i := part.First; i < part.First+part.Count; i += 3 {
		a, b, d := m.Mesh.Vertices[m.Mesh.Indices[i]].Pos, m.Mesh.Vertices[m.Mesh.Indices[i+1]].Pos, m.Mesh.Vertices[m.Mesh.Indices[i+2]].Pos
		if a.Y != level || b.Y != level || d.Y != level {
			t.Fatalf("water at %v %v %v, level %v", a.Y, b.Y, d.Y, level)
		}
		area += float64(cross(a, b, d).Y) / 2
	}
	cells := 0
	for z := int32(0); z < 16; z++ {
		for x := int32(0); x < 16; x++ {
			if g.waterQuad(c, x, z) != NoWater {
				cells++
			}
		}
	}
	if area != float64(cells) || cells == 0 {
		t.Fatalf("water covers %v m², %d cells", area, cells)
	}
	// Every level draws the same water.
	for _, l := range m.LODs {
		if l.Mesh.Parts[k].Count != part.Count {
			t.Fatalf("level water %d indices, base %d", l.Mesh.Parts[k].Count, part.Count)
		}
	}
	// A world naming its water material uses it.
	g.W.Terrain.Water = "lagoon"
	if m := g.Ground(c); m.Materials[len(m.Materials)-1] != "lagoon" {
		t.Fatalf("materials %v", m.Materials)
	}
}

func BenchmarkTerrainChunk(b *testing.B) {
	t := &testing.T{}
	g := terrainGen(t)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c := g.Chunk(int32(i%8)-4, int32(i/8%8)-4)
		g.Ground(c)
	}
}
