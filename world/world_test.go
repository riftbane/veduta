package world

import (
	"bytes"
	"math"
	"reflect"
	"testing"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/gmath"
)

func prefab(t *testing.T, name, src string) *asset.Prefab {
	t.Helper()
	p, err := asset.ParsePrefab(name+".prefab.json", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// testPrefabs: a one-cell tree, a 3×2 house that keeps 1 m from houses and 6 m from
// villages, a 12×8 village that keeps 4 m from villages.
func testPrefabs(t *testing.T) func(string) *asset.Prefab {
	t.Helper()
	m := map[string]*asset.Prefab{
		"tree": prefab(t, "tree", `{"veduta": "prefab/1", "footprint": [1, 1], "tags": ["tree"], "rules": {"biomes": ["forest"], "min_distance": {"village": 2}},
		  "entities": [{"name": "trunk", "kind": "static", "model": "tree", "position": [0.5, 0, 0.5]}]}`),
		"house": prefab(t, "house", `{"veduta": "prefab/1", "footprint": [3, 2], "tags": ["house"], "rules": {"biomes": ["plain"], "min_distance": {"house": 1, "village": 6}},
		  "entities": [{"name": "walls", "kind": "static", "model": "house", "position": [0.5, 0, 0.5], "rotation_deg": [0, 30, 0]},
		               {"name": "roof", "kind": "static", "model": "roof", "parent": "walls", "position": [0, 2, 0]}]}`),
		"bush": prefab(t, "bush", `{"veduta": "prefab/1", "footprint": [1, 1], "tags": ["bush"], "rules": {"min_distance": {"village": 2}},
		  "entities": [{"name": "leaves", "kind": "static", "model": "bush", "position": [0.5, 0, 0.5]}]}`),
		"village": prefab(t, "village", `{"veduta": "prefab/1", "footprint": [12, 8], "tags": ["village"], "rules": {"biomes": ["plain"], "min_distance": {"village": 4}},
		  "entities": [{"name": "hall", "kind": "static", "model": "house", "position": [6, 0, 4]}]}`),
	}
	return func(n string) *asset.Prefab { return m[n] }
}

const testWorldSrc = `{
  "veduta": "world/1", "seed": 7, "cell": 1, "chunk": 16, "extent": 8, "view": 1, "biome_scale": 32,
  "camera": { "type": "orthographic", "size": 12, "position": [0, 30, 0], "look_at": [0, 0, 0] },
  "biomes":  [ { "name": "plain", "ground": "grass", "weight": 3 }, { "name": "forest", "ground": "moss", "weight": 1 } ],
  "scatter": [ { "prefab": "tree", "density": 0.2 } ],
  "sites":   [ { "tag": "village", "prefabs": ["village"], "spacing": 24, "chance": 0.8 },
               { "tag": "house", "prefabs": ["house"], "spacing": 12, "chance": 0.5 } ],
  "places":  [ { "name": "home", "prefab": "house", "cell": [14, 3], "rotation": 90 } ],
  "entities": [ { "name": "player", "kind": "player", "model": "hero" } ]
}`

func testWorld(t *testing.T, src string) (*asset.World, func(string) *asset.Prefab) {
	t.Helper()
	pf := testPrefabs(t)
	w, err := asset.ParseWorld("test.world.json", []byte(src), pf)
	if err != nil {
		t.Fatal(err)
	}
	return w, pf
}

func newGen(t *testing.T) *Gen {
	t.Helper()
	return New(testWorld(t, testWorldSrc))
}

func TestNoiseIntegerAndInRange(t *testing.T) {
	if smooth(0) != 0 || smooth(65536) != 65536 || smooth(32768) != 32768 {
		t.Fatalf("smooth: %d %d %d", smooth(0), smooth(65536), smooth(32768))
	}
	f := field{seed: 3, scale: 16}
	var lo, hi uint32 = math.MaxUint32, 0
	for z := int32(-200); z < 200; z++ {
		for x := int32(-200); x < 200; x++ {
			v := f.value(x, z)
			lo, hi = min(lo, v), max(hi, v)
			// Neighbours differ little: a lattice step is 16 cells.
			if d := int64(v) - int64(f.value(x+1, z)); d > 1<<30 || d < -(1<<30) {
				t.Fatalf("value jumps by %d at %d,%d", d, x, z)
			}
		}
	}
	if hi-lo < 1<<30 {
		t.Fatalf("noise range %d..%d is flat", lo, hi)
	}
	if f4 := (field{seed: 4, scale: 16}); f4.value(5, 5) == f.value(5, 5) && f4.value(6, 9) == f.value(6, 9) {
		t.Fatal("seed has no effect")
	}
	if floorDiv(-1, 16) != -1 || floorDiv(-16, 16) != -1 || floorDiv(-17, 16) != -2 || floorDiv(15, 16) != 0 {
		t.Fatal("floorDiv")
	}
}

func TestBiomeSharesFollowWeights(t *testing.T) {
	g := newGen(t)
	r := g.Region([2]int32{0, 0}, 120)
	if r.Shares[0] < 0.6 || r.Shares[0] > 0.9 || r.Shares[1] < 0.1 || r.Shares[1] > 0.4 {
		t.Fatalf("shares %v, want about 0.75 / 0.25", r.Shares)
	}
	if b := g.Bounds(); b != (Rect{-128, -128, 256, 256}) || r.Rect != (Rect{-120, -120, 241, 241}) {
		t.Fatalf("bounds %v region %v", b, r.Rect)
	}
}

func chunkSummary(c *Chunk) []Struct {
	out := append([]Struct(nil), c.Scatter...)
	for i := range out {
		out[i].Prefab = nil
	}
	return out
}

func TestChunkIndependentOfOrderAndRun(t *testing.T) {
	a, b := newGen(t), newGen(t)
	ca0, ca1 := a.Chunk(0, 0), a.Chunk(1, 0)
	cb1, cb0 := b.Chunk(1, 0), b.Chunk(0, 0)
	if !reflect.DeepEqual(chunkSummary(ca0), chunkSummary(cb0)) || !reflect.DeepEqual(chunkSummary(ca1), chunkSummary(cb1)) {
		t.Fatal("scatter depends on generation order")
	}
	if !bytes.Equal(asset.EncodeModel(a.Ground(ca0)).Data, asset.EncodeModel(b.Ground(cb0)).Data) || !reflect.DeepEqual(ca1.Grid, cb1.Grid) {
		t.Fatal("ground mesh differs between runs")
	}
	sa, sb := a.Structures(-1, -1), b.Structures(-1, -1)
	for i := range sa {
		sa[i].Prefab, sb[i].Prefab = nil, nil
	}
	if !reflect.DeepEqual(sa, sb) {
		t.Fatal("structures differ between runs")
	}
	// Somewhere near the origin a chunk has forest, hence trees.
	for cz := int32(-4); cz < 4 && len(ca0.Scatter) == 0; cz++ {
		for cx := int32(-4); cx < 4 && len(ca0.Scatter) == 0; cx++ {
			ca0 = a.Chunk(cx, cz)
		}
	}
	if len(ca0.Scatter) == 0 {
		t.Fatal("no scatter at all")
	}
	prefix := "chunk_" + coord(ca0.X) + "_" + coord(ca0.Z) + "_c"
	for _, s := range ca0.Scatter {
		if !ca0.Rect.Contains(s.Cell[0], s.Cell[1]) || s.Rect != (Rect{s.Cell[0], s.Cell[1], 1, 1}) {
			t.Fatalf("scatter %+v outside its chunk", s)
		}
		if g := a.Biome(s.Cell[0], s.Cell[1]); g != 1 {
			t.Fatalf("tree %s on biome %d", s.Key, g)
		}
	}
	if ca0.Scatter[0].Key[:len(prefix)] != prefix {
		t.Fatalf("key %q", ca0.Scatter[0].Key)
	}
}

func TestSitesKeepTheirRulesAndRegions(t *testing.T) {
	g := newGen(t)
	r := g.Region([2]int32{0, 0}, 127)
	sites := 0
	for i := range r.Structs {
		s := &r.Structs[i]
		if s.Place != "" {
			continue
		}
		sites++
		rule := g.W.Sites[s.Site]
		sp := int32(rule.Spacing)
		region := Rect{s.Cell[0] * sp, s.Cell[1] * sp, sp, sp}
		if !s.Rect.Inside(region) || !s.Rect.Inside(g.Bounds()) {
			t.Fatalf("site %s at %v leaves its region %v", s.Key, s.Rect, region)
		}
		if cx, cz := s.Rect.Center(); g.W.Biomes[g.Biome(cx, cz)].Name != "plain" {
			t.Fatalf("site %s on the wrong biome", s.Key)
		}
		if !contains(s.Tags, rule.Tag) {
			t.Fatalf("site %s tags %v lack %q", s.Key, s.Tags, rule.Tag)
		}
		for j := range r.Structs {
			o := &r.Structs[j]
			if i != j && g.conflicts(s, o) {
				t.Fatalf("%s %v and %s %v are %d apart, need %d", s.Key, s.Rect, o.Key, o.Rect, s.Rect.Gap(o.Rect), g.need(s, o))
			}
		}
	}
	if sites < 20 {
		t.Fatalf("only %d sites in the world", sites)
	}
	// Scatter keeps away from sites and the place.
	for _, sc := range r.Scatter {
		for j := range r.Structs {
			if g.conflicts(&sc, &r.Structs[j]) {
				t.Fatalf("tree %s at %v too close to %s %v", sc.Key, sc.Rect, r.Structs[j].Key, r.Structs[j].Rect)
			}
		}
	}
}

func TestPlaceWinsAndStraddles(t *testing.T) {
	g := newGen(t)
	home := g.Places()[0]
	if home.Key != "place_home" || home.Rect != (Rect{14, 3, 2, 3}) || home.Rotation != 90 {
		t.Fatalf("place %+v", home)
	}
	// A place spanning chunks 0 and 1 is returned by both, whole.
	w2, pf := testWorld(t, `{"veduta": "world/1", "seed": 7, "chunk": 16, "extent": 8, "biome_scale": 32,
	  "camera": { "type": "orthographic", "size": 12, "position": [0, 30, 0], "look_at": [0, 0, 0] },
	  "biomes": [{"name": "plain", "ground": "grass"}], "scatter": [{"prefab": "bush", "density": 1}],
	  "sites": [{"tag": "village", "prefabs": ["village"], "spacing": 24}],
	  "places": [{"name": "v", "prefab": "village", "cell": [10, 10]}]}`)
	g2 := New(w2, pf)
	for _, c := range [][2]int32{{0, 0}, {1, 0}, {0, 1}, {1, 1}, {3, 3}} {
		var places []Struct
		for _, s := range g2.Structures(c[0], c[1]) {
			if s.Place != "" {
				places = append(places, s)
			}
		}
		if c == [2]int32{3, 3} {
			if len(places) != 0 {
				t.Fatalf("place in far chunk: %+v", places)
			}
			continue
		}
		if len(places) != 1 || places[0].Key != "place_v" || places[0].Rect != (Rect{10, 10, 12, 8}) {
			t.Fatalf("chunk %v places %+v", c, places)
		}
	}
	// Every cell is a bush unless a structure grown by the bush's 2 m rule covers it.
	for _, c := range [][2]int32{{0, 0}, {1, 0}} {
		ch := g2.Chunk(c[0], c[1])
		got := map[[2]int32]bool{}
		for _, s := range ch.Scatter {
			got[s.Cell] = true
		}
		cx, cz := ch.Rect.Center()
		near := g2.Region([2]int32{cx, cz}, 16).Structs
		for z := ch.Rect.Z; z < ch.Rect.Z+ch.Rect.D; z++ {
			for x := ch.Rect.X; x < ch.Rect.X+ch.Rect.W; x++ {
				want := true
				for _, s := range near {
					if s.Rect.Grow(2).Contains(x, z) {
						want = false
					}
				}
				if got[[2]int32{x, z}] != want {
					t.Fatalf("cell %d,%d: tree %v, want %v", x, z, got[[2]int32{x, z}], want)
				}
			}
		}
		if !got[[2]int32{ch.Rect.X, ch.Rect.Z + 15}] && !got[[2]int32{ch.Rect.X + 15, ch.Rect.Z + 15}] {
			t.Logf("chunk %v: both far corners taken by structures", c)
		}
	}
	// Sites yield to the place: no village site conflicts with it.
	for _, s := range g2.Region([2]int32{0, 0}, 60).Structs {
		if s.Place == "" && g2.conflicts(&s, &g2.Places()[0]) {
			t.Fatalf("site %s conflicts with the place", s.Key)
		}
	}
	if d := g2.Displaced(pf("village"), [2]int32{-40, -40}, 0); len(d) == 0 {
		t.Log("no site displaced at -40,-40 (depends on the seed)")
	}
}

func TestInstantiateRotation(t *testing.T) {
	g := newGen(t)
	house := testPrefabs(t)("house")
	for _, c := range []struct {
		rot        int
		w, d       int32
		wallsX, wZ float32
	}{
		{0, 3, 2, 0.5, 0.5},   // as authored
		{90, 2, 3, 0.5, 2.5},  // (x-1.5, z-1) = (-1, -0.5) → (z, -x) = (-0.5, 1) + centre (1, 1.5)
		{180, 3, 2, 2.5, 1.5}, // (1, 0.5) + (1.5, 1)
		{270, 2, 3, 1.5, 0.5}, // (-z, x) = (0.5, -1) + (1, 1.5)
	} {
		fw, fd := g.W.Footprint(house, c.rot)
		if int32(fw) != c.w || int32(fd) != c.d {
			t.Fatalf("rot %d footprint %d×%d", c.rot, fw, fd)
		}
		s := Struct{Key: "place_h", Prefab: house, Rect: Rect{10, 20, int32(fw), int32(fd)}, Rotation: c.rot}
		ents := g.Instantiate(&s)
		if len(ents) != 2 || ents[0].Name != "place_h_walls" || ents[1].Name != "place_h_roof" || ents[1].Parent != "place_h_walls" {
			t.Fatalf("rot %d entities %+v", c.rot, ents)
		}
		want := gmath.V3(10+c.wallsX, 0, 20+c.wZ)
		if ents[0].Position != want || ents[0].RotationDeg.Y != float32(30+c.rot) {
			t.Fatalf("rot %d walls at %v (yaw %v), want %v (yaw %v)", c.rot, ents[0].Position, ents[0].RotationDeg.Y, want, 30+c.rot)
		}
		if ents[1].Position != gmath.V3(0, 2, 0) || ents[1].RotationDeg.Y != 0 {
			t.Fatalf("child moved: %+v", ents[1])
		}
	}
}

func TestGroundMesh(t *testing.T) {
	g := newGen(t)
	c := g.Chunk(-1, 2)
	m := g.Ground(c)
	if m.Name != "world:ground:-1:2" || len(m.Mesh.Indices)%3 != 0 || len(m.Parts) != len(m.Mesh.Parts) || len(m.Parts) == 0 {
		t.Fatalf("model %+v", m)
	}
	area := 0.0
	for i := 0; i+2 < len(m.Mesh.Indices); i += 3 {
		a, b, cc := m.Mesh.Vertices[m.Mesh.Indices[i]], m.Mesh.Vertices[m.Mesh.Indices[i+1]], m.Mesh.Vertices[m.Mesh.Indices[i+2]]
		n := b.Pos.Sub(a.Pos).Cross(cc.Pos.Sub(a.Pos))
		if n.Y <= 0 || n.X != 0 || n.Z != 0 {
			t.Fatalf("triangle %d faces %v", i/3, n)
		}
		area += float64(n.Y) / 2
	}
	if area != 256 {
		t.Fatalf("ground covers %v m², want 256", area)
	}
	if len(m.Mesh.Indices)/3 >= 2*256 && len(m.Materials) == 1 {
		t.Fatalf("%d triangles for one material: runs are not merged", len(m.Mesh.Indices)/3)
	}
	for _, v := range m.Mesh.Vertices {
		if v.Normal != gmath.V3(0, 1, 0) || v.UV.X != v.Pos.X || v.UV.Y != v.Pos.Z {
			t.Fatalf("vertex %+v", v)
		}
	}
	if m.Mesh.Bounds != (gmath.AABB{Min: gmath.Zero3, Max: gmath.V3(16, 0, 16)}) {
		t.Fatalf("bounds %v", m.Mesh.Bounds)
	}
	e := g.GroundEntity(c)
	if e.Name != "chunk_n1_2_ground" || e.Model != m.Name || e.Position != gmath.V3(-16, 0, 32) || e.Hitbox == nil || !e.Hitbox.IsEmpty() {
		t.Fatalf("ground entity %+v", e)
	}
	// Materials index parts in biome order, one part per material.
	for k, p := range m.Mesh.Parts {
		if p.Material != k || len(m.Mesh.Parts) != len(m.Materials) {
			t.Fatalf("part %+v", p)
		}
	}
	// Flat ground has no skirts and the same heights everywhere.
	for _, h := range c.Grid.Height {
		if h != 0 {
			t.Fatalf("flat world has height %d", h)
		}
	}
}

func TestValidateAndSolve(t *testing.T) {
	g := newGen(t)
	pf := testPrefabs(t)
	house := pf("house")
	codes := func(is []Issue) []string {
		var out []string
		for _, i := range is {
			out = append(out, i.Code)
		}
		return out
	}
	if got := codes(g.Validate(house, [2]int32{126, 0}, 0, "")); !reflect.DeepEqual(got, []string{CodePlaceOutside}) {
		t.Fatalf("outside: %v", got)
	}
	if got := codes(g.Validate(house, [2]int32{14, 3}, 90, "")); !reflect.DeepEqual(got, []string{CodePlaceOverlap}) {
		t.Fatalf("overlap: %v", got)
	}
	if got := codes(g.Validate(house, [2]int32{14, 3}, 90, "home")); got != nil {
		t.Fatalf("self: %v", got)
	}
	if got := codes(g.Validate(house, [2]int32{16, 3}, 0, "")); !reflect.DeepEqual(got, []string{CodePlaceTooClose}) {
		t.Fatalf("too close: %v", got)
	}
	// A forest cell refuses a house (plain only).
	var forest [2]int32
	found := false
	for z := int32(-100); z < 100 && !found; z++ {
		for x := int32(-100); x < 100; x++ {
			if g.Biome(x, z) == 1 && g.Biome(x+1, z) == 1 && g.Biome(x, z+1) == 1 && g.Biome(x+1, z+1) == 1 {
				forest, found = [2]int32{x, z}, true
				break
			}
		}
	}
	if !found {
		t.Fatal("no forest")
	}
	if got := codes(g.Validate(house, forest, 0, "")); !reflect.DeepEqual(got, []string{CodePlaceBiome}) {
		t.Fatalf("biome: %v at %v", got, forest)
	}
	// Solve: the centre is taken by "home"; the first free cell east of it wins.
	cell, ok, rejected := g.Solve(house, [2]int32{14, 3}, 10, 0)
	if !ok || rejected[CodePlaceOverlap] == 0 {
		t.Fatalf("solve: %v %v %v", cell, ok, rejected)
	}
	if len(g.Validate(house, cell, 0, "")) != 0 {
		t.Fatalf("solved cell %v is not valid", cell)
	}
	// Ring order: from [0,0] with nothing in the way, the answer is the centre itself;
	// with the centre blocked by a place, ring 1 starts east.
	w2, _ := testWorld(t, `{"veduta": "world/1", "seed": 1, "chunk": 16, "extent": 8,
	  "camera": {"type": "orthographic", "size": 12, "position": [0, 30, 0], "look_at": [0, 0, 0]},
	  "biomes": [{"name": "plain", "ground": "grass"}], "places": [{"name": "a", "prefab": "tree", "cell": [0, 0]}]}`)
	g2 := New(w2, pf)
	tree := pf("tree")
	tree = &asset.Prefab{Name: "t", Footprint: tree.Footprint}
	if c, ok, _ := g2.Solve(tree, [2]int32{0, 0}, 3, 0); !ok || c != [2]int32{1, 0} {
		t.Fatalf("first ring cell %v, want [1 0]", c)
	}
	if c, ok, _ := g2.Solve(tree, [2]int32{5, 5}, 3, 0); !ok || c != [2]int32{5, 5} {
		t.Fatalf("free centre %v", c)
	}
	if _, ok, rej := g2.Solve(&asset.Prefab{Name: "huge", Footprint: gmath.V2(300, 300)}, [2]int32{0, 0}, 2, 0); ok || rej[CodePlaceOutside] != 25 {
		t.Fatalf("huge prefab placed: %v", rej)
	}
}

func TestSolveVisitsRingsClockwise(t *testing.T) {
	w, pf := testWorld(t, `{"veduta": "world/1", "seed": 1, "chunk": 16, "extent": 8,
	  "camera": {"type": "orthographic", "size": 12, "position": [0, 30, 0], "look_at": [0, 0, 0]},
	  "biomes": [{"name": "plain", "ground": "grass"}]}`)
	g := New(w, pf)
	_ = g
	var order [][2]int32
	// Block the cells before each target with places: the first free cell must be the
	// next one in ring order (centre, then east and clockwise).
	want := [][2]int32{{0, 0}, {1, 0}, {1, 1}, {0, 1}, {-1, 1}, {-1, 0}, {-1, -1}, {0, -1}, {1, -1}}
	for i, target := range want {
		wt, _ := testWorld(t, `{"veduta": "world/1", "seed": 1, "chunk": 16, "extent": 8,
		  "camera": {"type": "orthographic", "size": 12, "position": [0, 30, 0], "look_at": [0, 0, 0]},
		  "biomes": [{"name": "plain", "ground": "grass"}]}`)
		gt := New(wt, pf)
		// Block every cell of ring ≤ 1 before target with 1×1 places.
		for _, b := range want[:i] {
			gt.places = append(gt.places, Struct{Key: "place_b", Prefab: pf("tree"), Rect: Rect{b[0], b[1], 1, 1}, Tags: nil, Place: "b", Site: -1})
		}
		probe := &asset.Prefab{Name: "probe", Footprint: gmath.V2(1, 1)}
		if c, ok, _ := gt.Solve(probe, [2]int32{0, 0}, 1, 0); !ok || c != target {
			t.Fatalf("step %d: got %v, want %v", i, c, target)
		}
		order = append(order, target)
	}
	if len(order) != 9 {
		t.Fatal(order)
	}
}

func TestQueryAndRegion(t *testing.T) {
	g := newGen(t)
	q := g.Query(14, 3, 3)
	if q.Occupant == nil || q.Occupant.Key != "place_home" || q.Chunk != [2]int32{0, 0} || q.Biome != "plain" || q.Ground != "grass" {
		t.Fatalf("%+v", q)
	}
	if len(q.Nearest) < 2 || q.Nearest[0].Tag != "home" || q.Nearest[0].Distance != 0 {
		t.Fatalf("nearest %+v", q.Nearest)
	}
	q2 := g.Query(-100, -100, 2)
	if q2.Chunk != [2]int32{-7, -7} {
		t.Fatalf("%+v", q2.Chunk)
	}
	r := g.Region([2]int32{14, 3}, 2)
	if r.Rect != (Rect{12, 1, 5, 5}) || len(r.Biomes) != 25 || len(r.Structs) == 0 || r.Structs[0].Key != "place_home" {
		t.Fatalf("region %+v", r)
	}
	if e := g.Region([2]int32{-128, -128}, 1); e.Rect != (Rect{-128, -128, 2, 2}) {
		t.Fatalf("clipped region %v", e.Rect)
	}
	if x, z := g.CellOf(gmath.V3(-0.5, 0, 15.99)); x != -1 || z != 15 {
		t.Fatalf("CellOf %d %d", x, z)
	}
	if g.Origin(-3, 2) != gmath.V3(-3, 0, 2) || g.Center(-3, 2) != gmath.V3(-2.5, 0, 2.5) {
		t.Fatal("origin/center")
	}
	for _, v := range []int32{0, 5, -5, 123456} {
		back, err := ParseCoord(coord(v))
		if err != nil || back != v {
			t.Fatalf("coord %d: %q %v", v, coord(v), err)
		}
	}
}

func TestCheck(t *testing.T) {
	w, pf := testWorld(t, testWorldSrc)
	lib := asset.NewLibrary(nil)
	lib.Textures["grass"] = &asset.Texture{Name: "grass", Tiling: true}
	lib.Textures["flat"] = &asset.Texture{Name: "flat"}
	lib.Materials["grass"] = &asset.Material{Name: "grass", Texture: "grass"}
	lib.Materials["moss"] = &asset.Material{Name: "moss", Texture: "flat"}
	tris := func(n int) *asset.Model {
		m := &asset.Model{Mesh: gfx.MeshData{Indices: make([]uint32, 3*n)}}
		return m
	}
	lib.Models["tree"], lib.Models["house"], lib.Models["roof"] = tris(20), tris(12), tris(8)
	// A missing hero and a village that does not exist.
	w.Places = append(w.Places, asset.Place{Name: "castle", Prefab: "castle", Cell: [2]int32{0, 0}})
	w.Places = append(w.Places, asset.Place{Name: "twin", Prefab: "house", Cell: [2]int32{14, 3}, Rotation: 90})
	g := New(w, pf)
	rep := g.Check(lib)
	got := map[string]int{}
	for _, is := range rep.Issues {
		got[is.Code]++
	}
	want := map[string]int{CodeMissingPrefab: 1, CodeMissingAsset: 1, CodeNotTiling: 1, CodePlaceOverlap: 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("issues %v, want %v\n%+v", got, want, rep.Issues)
	}
	if rep.Issues[len(rep.Issues)-1].Severity != "warning" && got[CodeBudget]+got[CodeViewShort] > 0 {
		t.Fatal("warnings must come last")
	}
	if rep.Metrics["max_chunk_triangles"].(int) <= 0 || rep.Metrics["sampled_chunks"].(int) < 9 {
		t.Fatalf("metrics %v", rep.Metrics)
	}
	// Dense scatter of a heavy model blows the budget; a wide camera sees past the window.
	w.Scatter[0].Density = 1
	w.Camera.Size = 60
	lib.Models["tree"] = tris(400)
	rep = New(w, pf).Check(lib)
	got = map[string]int{}
	for _, is := range rep.Issues {
		got[is.Code]++
	}
	if got[CodeBudget] != 1 || got[CodeViewShort] != 1 {
		t.Fatalf("issues %v", got)
	}
}

func BenchmarkChunk(b *testing.B) {
	w, err := asset.ParseWorld("bench.world.json", []byte(testWorldSrc), nil)
	if err != nil {
		b.Fatal(err)
	}
	var tp = testPrefabsB(b)
	g := New(w, tp)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c := g.Chunk(int32(i%16)-8, int32(i/16%16)-8)
		g.Structures(c.X, c.Z)
	}
}

func testPrefabsB(b *testing.B) func(string) *asset.Prefab {
	m := map[string]*asset.Prefab{}
	for name, src := range map[string]string{
		"tree":    `{"veduta": "prefab/1", "footprint": [1, 1], "tags": ["tree"], "entities": [{"name": "trunk", "kind": "static", "model": "tree"}]}`,
		"house":   `{"veduta": "prefab/1", "footprint": [3, 2], "tags": ["house"], "rules": {"min_distance": {"house": 1}}, "entities": [{"name": "walls", "kind": "static", "model": "house"}]}`,
		"village": `{"veduta": "prefab/1", "footprint": [12, 8], "tags": ["village"], "rules": {"min_distance": {"village": 4}}, "entities": [{"name": "hall", "kind": "static", "model": "house"}]}`,
	} {
		p, err := asset.ParsePrefab(name+".prefab.json", []byte(src))
		if err != nil {
			b.Fatal(err)
		}
		m[name] = p
	}
	return func(n string) *asset.Prefab { return m[n] }
}
