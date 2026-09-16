package veduta

import (
	"path/filepath"
	"testing"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/asset/cook"
	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gmath"
	"github.com/riftbane/veduta/scene"
)

// BenchmarkWorldFrame renders one 320×240 frame of the test game's world, 7×7 chunks loaded,
// from a third-person camera that sees 100 m: with the levels of detail and the draw
// distances (lod), and with every level and draw distance stripped (full). It reports the
// triangles submitted and drawn per frame and the entities outside the view.
func BenchmarkWorldFrame(b *testing.B) {
	for _, kind := range []string{"player", "collectible"} { // the test game's, linked by the external tests
		if lookupKind(kind) == nil {
			RegisterKind(kind, func(*scene.Entity) Behaviour { return nil })
		}
	}
	for _, c := range []struct {
		name string
		full bool
	}{{"lod", false}, {"full", true}} {
		b.Run(c.name, func(b *testing.B) {
			lib, err := cook.Load(filepath.Join("internal", "testgame"))
			if err != nil {
				b.Fatal(err)
			}
			if c.full {
				for _, name := range asset.Names(lib.Models) {
					lib.Models[name].LODs, lib.Models[name].DrawDistance = nil, 0
				}
				lib.Worlds["overworld"].Terrain.LODDistance = 1e5
			}
			lib.Worlds["overworld"].View = 3
			e := newEngine(&testGame{}, lib.Project, lib)
			if err := e.start(runOptions{World: "overworld", Seed: 1, Headless: true}); err != nil {
				b.Fatal(err)
			}
			defer e.close()
			cam := scene.Camera{FovDeg: 55, Near: 0.1, Far: 100, Position: gmath.V3(0.5, 8, 12), Target: gmath.V3(0.5, 0, -12)}
			f, err := e.render(cam, 320, 240, gfx.ModeColor, false)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if f, err = e.render(cam, 320, 240, gfx.ModeColor, false); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(f.Stats.Triangles), "tris/frame")
			b.ReportMetric(float64(f.Stats.Drawn), "drawn/frame")
			b.ReportMetric(float64(f.Draw.Culled), "culled/frame")
		})
	}
}
