package tilemap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/asset/texture"
	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/gfx/soft"
	"github.com/riftbane/veduta/v2/gmath"
	"github.com/riftbane/veduta/v2/internal/golden"
	"github.com/riftbane/veduta/v2/scene"
)

// testLibrary compiles testdata/tilemap: the textures of a farm (grass, sand and water with
// borders, water animated, a mark in the grass's top-left texel to show which way up it is
// drawn) and the farm's map. The VS Code extension's tests read the same files.
func testLibrary(t testing.TB) *asset.Library {
	t.Helper()
	dir := filepath.Join("..", "testdata", "tilemap")
	lib := asset.NewLibrary(nil)
	for _, name := range []string{"grass", "water", "sand", "dirt"} {
		src, err := os.ReadFile(filepath.Join(dir, name+".vtex"))
		if err != nil {
			t.Fatal(err)
		}
		tx, err := texture.Parse(name+".vtex", src, texture.Options{})
		if err != nil {
			t.Fatal(err)
		}
		lib.Textures[name] = tx
	}
	src, err := os.ReadFile(filepath.Join(dir, "farm.vmap"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := asset.ParseMap("farm.vmap", src)
	if err != nil {
		t.Fatal(err)
	}
	lib.Maps["farm"] = m
	return lib
}

// render draws the map with an orthographic camera looking at (x, y) showing height
// meters, at tick.
func render(t *testing.T, m *Map, lib *asset.Library, x, y, height float32, w, h int, tick uint64) *gfx.Image {
	t.Helper()
	r := soft.New(soft.Options{})
	defer r.Close()
	textures := map[string]*asset.Texture{}
	for k, v := range lib.Textures {
		textures[k] = v
	}
	for k, v := range m.Textures() {
		textures[k] = v
	}
	models := m.Models()
	res, err := scene.Upload(r, models, textures, m.Materials())
	if err != nil {
		t.Fatal(err)
	}
	s := scene.New("test", res.Bounds)
	s.Background = 0xff101018
	var dl gfx.DrawList
	s.Draw(&dl, res, scene.DrawOptions{Camera: scene.Camera2D(gmath.V2(x, y), height), Width: w, Height: h,
		Tick: tick, Rate: 20, Statics: m.Statics()})
	fb := gfx.NewFramebuffer(w, h, false)
	if err := r.Begin(fb); err != nil {
		t.Fatal(err)
	}
	if err := r.Draw(&dl); err != nil {
		t.Fatal(err)
	}
	if err := r.End(); err != nil {
		t.Fatal(err)
	}
	return fb.Image()
}

// TestRender: cells, borders by priority (water over sand over grass, a path layer's
// border over the ground), the corners of lone cells and the ripple of animated water.
func TestRender(t *testing.T) {
	lib := testLibrary(t)
	m := New(lib.Maps["farm"], lib)
	golden.Image(t, "tilemap_farm", render(t, m, lib, 0, 0, 18, 400, 360, 0))
	golden.Image(t, "tilemap_farm_tick10", render(t, m, lib, -5, 4, 6, 320, 240, 10))
}

// TestPicture: the map composed texel by texel, as the inspector and the VS Code map editor
// show it, at the start and when the water has moved to its second frame.
func TestPicture(t *testing.T) {
	lib := testLibrary(t)
	m := New(lib.Maps["farm"], lib)
	if w, h := m.CellSize(); w != 16 || h != 16 {
		t.Fatalf("cell %d × %d", w, h)
	}
	golden.Image(t, "tilemap_farm_picture", m.Picture(0, 20))
	golden.Image(t, "tilemap_farm_picture_tick10", m.Picture(10, 20))
}

func TestEdgeAtlas(t *testing.T) {
	lib := testLibrary(t)
	a := EdgeAtlas("map:edge:water", lib.Textures["water"])
	if a.Grid != [2]int{1, 2} || a.Play != "flow" || a.Clip("flow") == nil {
		t.Fatalf("atlas grid %v, play %q: want a column of the water's 2 frames and its clips", a.Grid, a.Play)
	}
	if img := a.Data.Levels[0]; img.W != 4*10 || img.H != 2*4*10 {
		t.Fatalf("atlas %d × %d, want 40 × 80: 4 × 4 quarters of 8 × 8 pixels padded to 10, per frame", img.W, img.H)
	}
	golden.Image(t, "tilemap_edge_atlas", a.Data.Levels[0])
}

func TestCells(t *testing.T) {
	lib := testLibrary(t)
	m := New(lib.Maps["farm"], lib)
	if w, h := m.Size(); w != 20 || h != 18 {
		t.Fatalf("size %d × %d", w, h)
	}
	// The origin is the top-left corner: cell (0, 0) spans x -10..-9, y 9..8.
	for _, c := range []struct {
		x, y   float32
		cx, cy int
	}{{-10, 9, 0, 0}, {-9.5, 8.5, 0, 0}, {-9, 8.99, 1, 0}, {-10.01, 9, -1, 0}, {0, 0, 10, 9}, {9.99, -8.99, 19, 17}} {
		if cx, cy := m.CellAt(c.x, c.y); cx != c.cx || cy != c.cy {
			t.Errorf("cell at (%v, %v) = (%d, %d), want (%d, %d)", c.x, c.y, cx, cy, c.cx, c.cy)
		}
	}
	if x, y := m.Center(0, 0); x != -9.5 || y != 8.5 {
		t.Errorf("center of (0, 0) = (%v, %v)", x, y)
	}
	if g := m.Get(0, 3, 4); g == nil || g.Name != "water" {
		t.Errorf("get (3, 4) = %v, want water", g)
	}
	if m.Get(1, 0, 0) != nil || m.Get(0, -1, 0) != nil || m.Get(2, 0, 0) != nil {
		t.Errorf("empty, off-map and missing-layer cells are not nil")
	}
	if !m.Has(-1, 10, 1, "path") || m.Has(0, 10, 1, "path") || !m.Has(0, 10, 1, "tillable") || m.Has(-1, 0, 0, "water") {
		t.Errorf("has: tags by layer wrong")
	}
	var events []string
	m.OnEvent = func(name string, f map[string]any) {
		events = append(events, name+" "+f["layer"].(string)+" "+f["terrain"].(string))
	}
	if err := m.Set(0, 0, 0, "water"); err != nil || m.Get(0, 0, 0).Name != "water" {
		t.Fatalf("set: %v", err)
	}
	if err := m.Set(1, 10, 1, ""); err != nil || m.Get(1, 10, 1) != nil {
		t.Fatalf("clear: %v", err)
	}
	m.Set(1, 10, 2, "dirt") // unchanged: no event
	if strings.Join(events, "; ") != "map_set ground water; map_set paths " {
		t.Errorf("events %q", events)
	}
	for _, c := range []struct {
		l, x, y int
		terrain string
		want    string
	}{{0, 20, 0, "water", "off the 20 × 18 map"}, {3, 0, 0, "water", "no layer 3"}, {0, 0, 0, "lava", `no terrain "lava" (it has grass, water, sand, dirt)`}} {
		if err := m.Set(c.l, c.x, c.y, c.terrain); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("set %+v: %v, want %q", c, err, c.want)
		}
	}
	if err := m.SetCells([][]uint8{{1}}); err == nil {
		t.Errorf("SetCells with one layer accepted")
	}
}

// TestRebuild: painting a cell rebuilds the chunks whose look it changes, the chunk it is in
// and those its border reaches, and no other.
func TestRebuild(t *testing.T) {
	lib := testLibrary(t)
	m := New(lib.Maps["farm"], lib)
	before := map[string]*asset.Model{}
	for k, v := range m.Models() {
		before[k] = v
	}
	if len(before) != 7 { // 2 × 2 chunks, 2 layers, but no path near the bottom right
		t.Fatalf("%d chunk models, want 7", len(before))
	}
	for _, c := range []struct {
		x, y int
		want []string
	}{
		{5, 5, []string{"map:farm:0:0:0"}},                    // well inside chunk (0, 0)
		{15, 5, []string{"map:farm:0:0:0", "map:farm:0:1:0"}}, // its border reaches chunk (1, 0)
	} {
		m.Set(0, c.x, c.y, "dirt")
		after := m.Models()
		for name, md := range after {
			changed := md != before[name]
			if want := strings.Contains(strings.Join(c.want, " "), name); changed != want {
				t.Errorf("painting (%d, %d): %s rebuilt: %v, want %v", c.x, c.y, name, changed, want)
			}
			before[name] = md
		}
	}
	// Emptying every cell of a chunk, and those whose border reaches it, drops its model.
	for y := 15; y < 18; y++ {
		for x := 0; x < 16; x++ {
			m.Set(1, x, y, "")
		}
	}
	if _, ok := m.Models()["map:farm:1:0:1"]; ok {
		t.Errorf("an empty chunk still has a model")
	}
	if n := len(m.Statics()); n != 6 {
		t.Errorf("%d statics, want 6", n)
	}
}

// cliffsLibrary adds testdata/tilemap's cliffs to testLibrary: an autotile (from a PNG, its
// second frame lighter) over grass, in islands, lakes, lines, lone cells and cells that
// touch at a corner or the map's side.
func cliffsLibrary(t testing.TB) *asset.Library {
	t.Helper()
	lib := testLibrary(t)
	dir := filepath.Join("..", "testdata", "tilemap")
	src, err := os.ReadFile(filepath.Join(dir, "cliff.vtex"))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := texture.Parse("cliff.vtex", src, texture.Options{FS: os.DirFS(dir)})
	if err != nil {
		t.Fatal(err)
	}
	lib.Textures["cliff"] = tx
	if src, err = os.ReadFile(filepath.Join(dir, "cliffs.vmap")); err != nil {
		t.Fatal(err)
	}
	m, err := asset.ParseMap("cliffs.vmap", src)
	if err != nil {
		t.Fatal(err)
	}
	lib.Maps["cliffs"] = m
	return lib
}

// TestAutotileClasses: the tile each class of quarter is taken from has that class there,
// and every tile but the lake's middle is drawn whole by the cell that looks like it.
func TestAutotileClasses(t *testing.T) {
	for c, tiles := range classTile {
		for q, tile := range tiles {
			if got := tileClasses[tile][q]; got != c {
				t.Errorf("class %d quarter %d: tile %d has class %d there", c, q, tile, got)
			}
		}
	}
	seen := map[[quarters]int]int{}
	for tile, cls := range tileClasses {
		if tile == lakeEmpty {
			continue
		}
		if other, ok := seen[cls]; ok {
			t.Errorf("tiles %d and %d have the same classes %v", other, tile, cls)
		}
		seen[cls] = tile
	}
	if tiles, whole := autoPick(0xff); !whole || tiles[0] != 7 {
		t.Errorf("a cell among its own: %v %v, want the island's middle whole", tiles, whole)
	}
	if tiles, whole := autoPick(0); whole || tiles != [quarters]int{0, 2, 12, 14} {
		t.Errorf("a lone cell: %v %v, want the island's four corners", tiles, whole)
	}
}

// TestAutotile: cliffs drawn by the renderer and composed by Picture, and the atlas.
func TestAutotile(t *testing.T) {
	lib := cliffsLibrary(t)
	m := New(lib.Maps["cliffs"], lib)
	if w, h := m.CellSize(); w != 16 || h != 16 {
		t.Fatalf("cell %d × %d, want a tile", w, h)
	}
	a := m.Textures()["map:auto:cliff"]
	if a == nil || a.Grid != [2]int{1, 2} || a.Play != "shine" {
		t.Fatalf("atlas %+v: want a column of the 2 frames and the clips", a)
	}
	if img := a.Data.Levels[0]; img.W != 6*18 || img.H != 2*3*18 {
		t.Fatalf("atlas %d × %d, want 108 × 108: 6 × 3 tiles padded to 18, per frame", img.W, img.H)
	}
	golden.Image(t, "tilemap_auto_atlas", a.Data.Levels[0])
	golden.Image(t, "tilemap_cliffs", render(t, m, lib, 0, 0, 12, 320, 240, 0))
	golden.Image(t, "tilemap_cliffs_picture", m.Picture(0, 20))
	golden.Image(t, "tilemap_cliffs_picture_tick10", m.Picture(10, 20))
}
