package inspect

import (
	"os"
	"testing"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/asset/texture"
	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/internal/golden"
)

func mapLibrary(t *testing.T, mapSrc string) *asset.Library {
	t.Helper()
	lib := asset.NewLibrary(nil)
	for name, src := range map[string]string{
		"grass": `{"veduta": "texture/1", "size": [8, 8], "layers": [{"type": "solid", "color": "#3f8f3a"}]}`,
		"water": `{"veduta": "texture/1", "size": [8, 8], "layers": [{"type": "solid", "color": "#2a6fdb"}], "edge": {"priority": 5, "seed": 2}}`,
	} {
		tx, err := texture.Parse(name+".vtex", []byte(src), texture.Options{})
		if err != nil {
			t.Fatal(err)
		}
		lib.Textures[name] = tx
	}
	m, err := asset.ParseMap("pond.vmap", []byte(mapSrc))
	if err != nil {
		t.Fatal(err)
	}
	lib.Maps["pond"] = m
	s, err := asset.ParseScene("main.vscene", []byte(`{"veduta": "scene/1", "map": "pond", "entities": [],
		"camera": {"type": "orthographic", "size": 6, "position": [4, -3, 100], "look_at": [4, -3, 0]}}`))
	if err != nil {
		t.Fatal(err)
	}
	lib.Scenes["main"] = s
	return lib
}

func TestMap(t *testing.T) {
	lib := mapLibrary(t, `{"veduta": "map/1", "size": [8, 6],
		"terrains": [{"key": ".", "name": "grass", "texture": "grass"}, {"key": "~", "name": "water", "texture": "water"},
		             {"key": "s", "name": "sand", "texture": "sand"}, {"key": "=", "name": "soil", "texture": "grass"}],
		"layers": [{"name": "ground", "rows": ["........", "..~~~...", ".~~~~~..", "..~~~...", "........", ".......s"]}],
		"objects": [{"name": "a", "at": [0, 0], "size": [2, 2]}, {"name": "b", "at": [1, 1]}, {"name": "c", "at": [5, 4], "size": [3, 2]}]}`)
	ir, err := NewRenderer(lib)
	if err != nil {
		t.Fatal(err)
	}
	defer ir.Close()
	rep, err := Map(ir, "pond", Options{OutDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Has(MapMissingAsset) || !rep.Has(MapUnusedTerrain) || !rep.Has(MapObjectsOverlap) || rep.Summary.Errors != 1 {
		t.Errorf("issues %+v", rep.Issues)
	}
	if rep.Metrics["size"] != [2]int{8, 6} || rep.Metrics["objects"] != 3 || rep.Metrics["chunks"] != 1 {
		t.Errorf("metrics %v", rep.Metrics)
	}
	f, err := os.Open(rep.Sheets[0])
	if err != nil {
		t.Fatal(err)
	}
	img, err := gfx.DecodePNG(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	golden.Image(t, "inspect_map_pond_summary", img)
	// The scene standing on the map draws it and sees something.
	srep, err := Scene(ir, "main", Options{OutDir: t.TempDir(), Sheets: []string{"none"}})
	if err != nil {
		t.Fatal(err)
	}
	if srep.Has("SCENE_CAMERA_SEES_NOTHING") {
		t.Errorf("a scene on a map sees nothing: %+v", srep.Issues)
	}
}
