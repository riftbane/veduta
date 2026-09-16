package inspect

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/asset/cook"
	"github.com/riftbane/veduta/gmath"
	"github.com/riftbane/veduta/world"
)

var (
	wldTemplateOnce sync.Once
	wldTemplateLib  *asset.Library
	wldTemplateErr  error
)

// wldLibrary returns a copy of the test game library (prefabs and worlds included).
func wldLibrary(t *testing.T) *asset.Library {
	t.Helper()
	wldTemplateOnce.Do(func() { wldTemplateLib, wldTemplateErr = cook.Load(filepath.Join("..", "internal", "testgame")) })
	if wldTemplateErr != nil {
		t.Fatal(wldTemplateErr)
	}
	base := wldTemplateLib
	lib := asset.NewLibrary(base.Project)
	for _, n := range asset.Names(base.Models) {
		lib.Models[n] = base.Models[n]
	}
	for _, n := range asset.Names(base.Textures) {
		lib.Textures[n] = base.Textures[n]
	}
	for _, n := range asset.Names(base.Materials) {
		lib.Materials[n] = base.Materials[n]
	}
	for _, n := range asset.Names(base.Prefabs) {
		lib.Prefabs[n] = base.Prefabs[n]
	}
	for _, n := range asset.Names(base.Worlds) {
		w := *base.Worlds[n]
		lib.Worlds[n] = &w
	}
	return lib
}

func codeCounts(rep *Report) map[string]int {
	out := map[string]int{}
	for _, is := range rep.Issues {
		out[is.Code] += is.Count
	}
	return out
}

func TestWorldTemplateClean(t *testing.T) {
	lib := wldLibrary(t)
	ir, err := NewRenderer(lib)
	if err != nil {
		t.Fatal(err)
	}
	defer ir.Close()
	rep, err := World(ir, "overworld", Options{OutDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Summary.Errors != 0 || rep.Summary.Warnings != 0 {
		t.Fatalf("test game world: %+v", rep.Issues)
	}
	if len(rep.Sheets) != 1 || filepath.Base(rep.Sheets[0]) != "overworld.map.png" {
		t.Fatalf("sheets %v", rep.Sheets)
	}
	if _, err := os.Stat(rep.Sheets[0]); err != nil {
		t.Fatal(err)
	}
	if rep.Metrics["visible_triangles"].(int) <= 0 || rep.Metrics["extent_m"].(float64) != 8192 {
		t.Fatalf("metrics %v", rep.Metrics)
	}
	if _, err := World(ir, "nowhere", Options{OutDir: t.TempDir()}); err == nil {
		t.Fatal("unknown world accepted")
	}
	if _, err := World(ir, "overworld", Options{OutDir: t.TempDir(), Sheets: []string{"ids"}}); err == nil {
		t.Fatal("unknown sheet accepted")
	}
}

func TestWorldIssues(t *testing.T) {
	lib := wldLibrary(t)
	w := lib.Worlds["overworld"]
	w.Biomes = append(w.Biomes, asset.Biome{Name: "rock", Ground: "bark", Weight: 1}) // albedo only: no texture
	w.Places = []asset.Place{
		{Name: "a", Prefab: "village", Cell: [2]int32{0, 0}},
		{Name: "b", Prefab: "village", Cell: [2]int32{6, 4}},  // overlaps a
		{Name: "c", Prefab: "house", Cell: [2]int32{14, 0}},   // 2 cells from a, rules want 3
		{Name: "d", Prefab: "castle", Cell: [2]int32{40, 40}}, // no such prefab
		{Name: "e", Prefab: "house", Cell: [2]int32{8190, 8190}},
	}
	w.Camera.Size = 60
	w.Scatter[0].Density = 1
	ir, err := NewRenderer(lib)
	if err != nil {
		t.Fatal(err)
	}
	defer ir.Close()
	rep, err := World(ir, "overworld", Options{OutDir: t.TempDir(), Sheets: []string{"none"}})
	if err != nil {
		t.Fatal(err)
	}
	got := codeCounts(rep)
	for _, code := range []string{world.CodeMissingPrefab, world.CodeNotTiling, world.CodePlaceOverlap, world.CodePlaceTooClose, world.CodePlaceOutside, world.CodeBudget, world.CodeViewShort} {
		if got[code] == 0 {
			t.Errorf("no %s: %v", code, got)
		}
	}
	if got[world.CodePlaceOverlap] != 1 {
		t.Errorf("overlap reported %d times, want once per pair", got[world.CodePlaceOverlap])
	}
	if rep.Issues[0].Severity != Error || len(rep.Sheets) != 0 {
		t.Fatalf("order %v sheets %v", rep.Issues[0], rep.Sheets)
	}
	// Focus keeps one code.
	f, err := World(ir, "overworld", Options{OutDir: t.TempDir(), Sheets: []string{"none"}, Focus: world.CodePlaceOverlap})
	if err != nil || len(f.Issues) != 1 || f.Issues[0].Code != world.CodePlaceOverlap {
		t.Fatalf("focus: %v %+v", err, f.Issues)
	}
}

func TestMapImage(t *testing.T) {
	lib := wldLibrary(t)
	g := world.New(lib.Worlds["overworld"], lib.Prefab)
	img := MapImage(g, [2]int32{0, 0}, 20)
	if img.W != 41*11 || img.H <= 41*11 {
		t.Fatalf("map %d×%d", img.W, img.H)
	}
	// Every cell pixel carries a palette colour (opaque).
	for i := 0; i < 41*11*41*11; i += 997 {
		if img.Pix[i]>>24 != 0xff {
			t.Fatalf("pixel %d is %08x", i, img.Pix[i])
		}
	}
	edge := MapImage(g, [2]int32{-8192, -8192}, 5)
	if edge.W != 6*80 {
		t.Fatalf("clipped map width %d", edge.W)
	}
}

func TestPrefab(t *testing.T) {
	lib := wldLibrary(t)
	ir, err := NewRenderer(lib)
	if err != nil {
		t.Fatal(err)
	}
	defer ir.Close()
	rep, err := Prefab(ir, "village", Options{OutDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Summary.Errors != 0 || rep.Summary.Warnings != 0 || len(rep.Sheets) != 1 || rep.Metrics["entities"] != 5 {
		t.Fatalf("village: %+v %v", rep.Issues, rep.Metrics)
	}
	if _, err := os.Stat(rep.Sheets[0]); err != nil {
		t.Fatal(err)
	}
	// The test game's gem prefab uses a game kind: information, not an error.
	gem, err := Prefab(ir, "gem", Options{OutDir: t.TempDir(), Sheets: []string{"none"}})
	if err != nil || gem.Summary.Errors != 0 || gem.Summary.Info != 1 || gem.Issues[0].Code != pfKind {
		t.Fatalf("gem: %v %+v", err, gem.Issues)
	}
	lib.Prefabs["bad"] = &asset.Prefab{Name: "bad", Footprint: gmath.V2(1, 1), Entities: []asset.Entity{
		{Name: "big", Kind: "static", Model: "house", Position: gmath.V3(0.5, 0, 0.5), Scale: gmath.One3, Visible: true},
		{Name: "twin", Kind: "static", Model: "house", Material: "gold", Position: gmath.V3(0.5, 0, 0.5), Scale: gmath.One3, Visible: true},
		{Name: "ghost", Kind: "static", Model: "castle", Scale: gmath.One3, Visible: true},
	}}
	bad, err := Prefab(ir, "bad", Options{OutDir: t.TempDir(), Sheets: []string{"none"}})
	if err != nil {
		t.Fatal(err)
	}
	got := codeCounts(bad)
	if got[pfMissing] != 2 || got[pfFootprint] != 2 || got[pfOverlap] != 1 {
		t.Fatalf("bad prefab: %v", got)
	}
	if _, err := Prefab(ir, "nowhere", Options{}); err == nil {
		t.Fatal("unknown prefab accepted")
	}
}
