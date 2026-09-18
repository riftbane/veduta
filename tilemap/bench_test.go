package tilemap

import (
	"strings"
	"testing"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/gfx/soft"
	"github.com/riftbane/veduta/v2/gmath"
	"github.com/riftbane/veduta/v2/scene"
)

// benchMap is an 80 × 60 farm: water, sand and grass in noise-like patches on the ground,
// paths on a second layer.
func benchMap(t testing.TB) (*Map, *asset.Library) {
	lib := testLibrary(t)
	for _, name := range []string{"grass", "sand", "dirt"} { // ground textures tile, as a game's do
		tx := *lib.Textures[name]
		tx.Tiling, tx.Data.Wrap = true, gfx.WrapRepeat
		lib.Textures[name] = &tx
	}
	keys := ".~sd"
	var ground, paths []string
	for y := 0; y < 60; y++ {
		var g, p strings.Builder
		for x := 0; x < 80; x++ {
			v := (x*7 + y*13 + x*y/5) % 17
			switch {
			case v < 3:
				g.WriteByte(keys[1])
			case v < 6:
				g.WriteByte(keys[2])
			default:
				g.WriteByte(keys[0])
			}
			if x%9 == 0 || y%7 == 0 {
				p.WriteByte('d')
			} else {
				p.WriteByte(' ')
			}
		}
		ground, paths = append(ground, g.String()), append(paths, p.String())
	}
	src := &asset.MapSource{Veduta: asset.TypeMap, Size: []int{80, 60}, Terrains: []asset.MapTerrainSource{
		{Key: ".", Name: "grass", Texture: "grass"}, {Key: "~", Name: "water", Texture: "water"},
		{Key: "s", Name: "sand", Texture: "sand"}, {Key: "d", Name: "dirt", Texture: "dirt"}},
		Layers: []asset.MapLayerSource{{Name: "ground", Rows: ground}, {Name: "paths", Rows: paths}}}
	m, err := asset.CompileMap("big", src, nil)
	if err != nil {
		t.Fatal(err)
	}
	return New(m, lib), lib
}

// BenchmarkDraw: a frame of the console (320 × 240, 12 cells high) over the big farm.
func BenchmarkDraw(b *testing.B) {
	m, lib := benchMap(b)
	r := soft.New(soft.Options{})
	defer r.Close()
	textures := map[string]*asset.Texture{}
	for k, v := range lib.Textures {
		textures[k] = v
	}
	for k, v := range m.Textures() {
		textures[k] = v
	}
	res, err := scene.Upload(r, m.Models(), textures, m.Materials())
	if err != nil {
		b.Fatal(err)
	}
	s := scene.New("bench", res.Bounds)
	fb := gfx.NewFramebuffer(320, 240, false)
	var dl gfx.DrawList
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dl.Reset()
		s.Draw(&dl, res, scene.DrawOptions{Camera: scene.Camera2D(gmath.V2(40, -30), 12), Width: 320, Height: 240,
			Tick: uint64(i), Rate: 20, Statics: m.Statics()})
		r.Begin(fb)
		r.Draw(&dl)
		r.End()
	}
	b.ReportMetric(float64(r.Stats().Triangles), "triangles")
}

// BenchmarkPaint: a cell painted and its chunks rebuilt, as a game does when the player
// tills the soil.
func BenchmarkPaint(b *testing.B) {
	m, _ := benchMap(b)
	m.Models()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Set(0, 40, 30, []string{"water", "grass"}[i%2])
		m.Models()
	}
}
