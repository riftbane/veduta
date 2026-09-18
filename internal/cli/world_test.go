package cli

import (
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/riftbane/veduta/v2/asset"
)

// gameSession copies the engine's test game into a temp dir (no build) and opens it.
func gameSession(t *testing.T) (*Session, string) {
	t.Helper()
	src := filepath.Join("..", "testgame")
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if strings.HasPrefix(rel, filepath.Join("assets", ".cooked")) || rel == "out" || rel == "bin" {
			return filepath.SkipDir
		}
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(out, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := OpenSession(dst, &Env{Version: "dev", Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	return s, dst
}

func TestWorldMapQuery(t *testing.T) {
	s, dir := gameSession(t)
	rep, err := s.WorldMap("overworld", [2]int32{0, 0}, 48)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Cells != 97*97 || len(rep.Biomes) != 2 || rep.Biomes[0].Share+rep.Biomes[1].Share < 0.999 || len(rep.Sites) == 0 || rep.Scatter == 0 {
		t.Fatalf("%+v", rep)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rep.Sheet))); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rep.Human(), "biome plain") {
		t.Fatal(rep.Human())
	}
	if _, err := s.WorldMap("overworld", [2]int32{0, 0}, MaxMapRadius+1); err == nil {
		t.Fatal("radius over the limit accepted")
	}
	if _, err := s.WorldMap("nowhere", [2]int32{0, 0}, 8); err == nil {
		t.Fatal("unknown world accepted")
	}
	site := rep.Sites[0]
	q, err := s.WorldQuery("overworld", site.Cell)
	if err != nil {
		t.Fatal(err)
	}
	if q.Occupant == nil || q.Occupant.Key != site.Key || q.Biome != "plain" {
		t.Fatalf("query %+v", q)
	}
	if _, err := s.WorldQuery("overworld", [2]int32{9000, 0}); err == nil {
		t.Fatal("cell outside accepted")
	}
}

func TestWorldPlaceAndRemove(t *testing.T) {
	s, dir := gameSession(t)
	file := filepath.Join(dir, "assets", "worlds", "overworld.vworld")
	before, _ := os.ReadFile(file)

	// Outside the world: refused, nothing written.
	cell := [2]int32{8190, 0}
	rep, err := s.WorldPlace(WorldPlaceOptions{World: "overworld", Prefab: "village", Name: "far", Cell: &cell})
	if err != nil || rep.Valid || len(rep.Issues) != 1 || rep.Issues[0].Code != "WORLD_PLACE_OUTSIDE" || rep.Written || rep.ExitCode() != 1 {
		t.Fatalf("outside: %v %+v", err, rep)
	}
	if after, _ := os.ReadFile(file); string(after) != string(before) {
		t.Fatal("an invalid place changed the file")
	}
	// Dry run: valid, not written.
	rep, err = s.WorldPlace(WorldPlaceOptions{World: "overworld", Prefab: "village", Name: "capital", Near: [2]int32{0, 0}, DryRun: true})
	if err != nil || !rep.Valid || rep.Written || rep.Searched == 0 && rep.Cell != [2]int32{0, 0} {
		t.Fatalf("dry run: %v %+v", err, rep)
	}
	if after, _ := os.ReadFile(file); string(after) != string(before) {
		t.Fatal("a dry run changed the file")
	}
	// Written: the file keeps every other byte and now compiles with the place.
	rep, err = s.WorldPlace(WorldPlaceOptions{World: "overworld", Prefab: "village", Name: "capital", Near: [2]int32{0, 0}, Rotation: 90})
	if err != nil || !rep.Valid || !rep.Written || rep.Footprint != [2]int32{8, 12} {
		t.Fatalf("place: %v %+v", err, rep)
	}
	after, _ := os.ReadFile(file)
	want := strings.Replace(string(before), `"places": [],`, "\"places\": [\n    { \"name\": \"capital\", \"prefab\": \"village\", \"cell\": ["+itoa(rep.Cell[0])+", "+itoa(rep.Cell[1])+"], \"rotation\": 90 }\n  ],", 1)
	if string(after) != want {
		t.Fatalf("file after place:\n%s\nwant:\n%s", after, want)
	}
	w, err := asset.ParseWorld("overworld.vworld", after, nil)
	if err != nil || len(w.Places) != 1 || w.Places[0].Name != "capital" {
		t.Fatalf("parsed %v %+v", err, w)
	}
	// A second one lands after the first; the same name is refused.
	if _, err := s.WorldPlace(WorldPlaceOptions{World: "overworld", Prefab: "house", Name: "capital", Near: [2]int32{30, 30}}); err == nil {
		t.Fatal("duplicate name accepted")
	}
	rep2, err := s.WorldPlace(WorldPlaceOptions{World: "overworld", Prefab: "house", Name: "hut", Near: [2]int32{30, 30}, Within: 20})
	if err != nil || !rep2.Valid || !rep2.Written {
		t.Fatalf("second place: %v %+v", err, rep2)
	}
	after, _ = os.ReadFile(file)
	if !strings.Contains(string(after), "\"rotation\": 90 },\n    { \"name\": \"hut\", \"prefab\": \"house\", \"cell\": [") {
		t.Fatalf("second place not appended:\n%s", after)
	}
	// Validating the first place's cell now overlaps it.
	c := rep.Cell
	if rep3, err := s.WorldPlace(WorldPlaceOptions{World: "overworld", Prefab: "village", Name: "twin", Cell: &c, Rotation: 90}); err != nil || rep3.Valid || rep3.Issues[0].Code != "WORLD_PLACE_OVERLAP" {
		t.Fatalf("overlap: %v %+v", err, rep3)
	}
	// Remove both: the file is back to its original bytes.
	if r, err := s.WorldRemove("overworld", "capital"); err != nil || !r.Removed {
		t.Fatalf("remove: %v %+v", err, r)
	}
	if r, err := s.WorldRemove("overworld", "hut"); err != nil || !r.Removed {
		t.Fatalf("remove: %v %+v", err, r)
	}
	if after, _ := os.ReadFile(file); string(after) != string(before) {
		t.Fatalf("file after removals:\n%s", after)
	}
	if _, err := s.WorldRemove("overworld", "capital"); err == nil {
		t.Fatal("removing a missing place succeeded")
	}
	for _, o := range []WorldPlaceOptions{
		{World: "overworld", Prefab: "village", Name: "site_1"},
		{World: "overworld", Prefab: "castle", Name: "x"},
		{World: "overworld", Prefab: "house", Name: "x", Rotation: 45},
		{World: "nowhere", Prefab: "house", Name: "x"},
	} {
		if _, err := s.WorldPlace(o); err == nil {
			t.Fatalf("accepted %+v", o)
		}
	}
}

func itoa(v int32) string { return strings.TrimSpace(strings.Repeat(" ", 0) + jsonInt(v)) }

func jsonInt(v int32) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestEditPlaces(t *testing.T) {
	add := &asset.PlaceSource{Name: "a", Prefab: "p", Cell: []int{1, -2}}
	cases := []struct {
		name, in, want string
		remove         string
	}{
		{"missing field, before entities", "{\n  \"veduta\": \"world/1\",\n  \"sites\": [],\n  \"entities\": []\n}",
			"{\n  \"veduta\": \"world/1\",\n  \"sites\": [],\n  \"places\": [\n    { \"name\": \"a\", \"prefab\": \"p\", \"cell\": [1, -2] }\n  ],\n  \"entities\": []\n}", ""},
		{"missing field, at the end", "{\n  \"veduta\": \"world/1\",\n  \"biomes\": []\n}",
			"{\n  \"veduta\": \"world/1\",\n  \"biomes\": [],\n  \"places\": [\n    { \"name\": \"a\", \"prefab\": \"p\", \"cell\": [1, -2] }\n  ]\n}", ""},
		{"empty array", "{\n\t\"places\": [],\n\t\"entities\": []\n}",
			"{\n\t\"places\": [\n\t\t{ \"name\": \"a\", \"prefab\": \"p\", \"cell\": [1, -2] }\n\t],\n\t\"entities\": []\n}", ""},
		{"empty array inline", "{ \"places\": [] }",
			"{ \"places\": [ { \"name\": \"a\", \"prefab\": \"p\", \"cell\": [1, -2] } ] }", ""},
		{"append", "{\n  \"places\": [\n    { \"name\": \"z\", \"prefab\": \"p\", \"cell\": [0, 0] }\n  ]\n}",
			"{\n  \"places\": [\n    { \"name\": \"z\", \"prefab\": \"p\", \"cell\": [0, 0] },\n    { \"name\": \"a\", \"prefab\": \"p\", \"cell\": [1, -2] }\n  ]\n}", ""},
		{"append inline", "{ \"places\": [ {\"name\": \"z\", \"prefab\": \"p\", \"cell\": [0, 0]} ] }",
			"{ \"places\": [ {\"name\": \"z\", \"prefab\": \"p\", \"cell\": [0, 0]}, { \"name\": \"a\", \"prefab\": \"p\", \"cell\": [1, -2] } ] }", ""},
		{"remove only", "{\n  \"places\": [\n    { \"name\": \"z\", \"prefab\": \"p\", \"cell\": [0, 0] }\n  ],\n  \"x\": 1\n}",
			"{\n  \"places\": [],\n  \"x\": 1\n}", "z"},
		{"remove first", "{\"places\": [\n  {\"name\": \"a\"},\n  {\"name\": \"b\"},\n  {\"name\": \"c\"}\n]}",
			"{\"places\": [\n  {\"name\": \"b\"},\n  {\"name\": \"c\"}\n]}", "a"},
		{"remove middle", "{\"places\": [\n  {\"name\": \"a\"},\n  {\"name\": \"b\"},\n  {\"name\": \"c\"}\n]}",
			"{\"places\": [\n  {\"name\": \"a\"},\n  {\"name\": \"c\"}\n]}", "b"},
		{"remove last", "{\"places\": [\n  {\"name\": \"a\"},\n  {\"name\": \"b\"},\n  {\"name\": \"c\"}\n]}",
			"{\"places\": [\n  {\"name\": \"a\"},\n  {\"name\": \"b\"}\n]}", "c"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := add
			if c.remove != "" {
				a = nil
			}
			got, err := editPlaces([]byte(c.in), a, c.remove)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != c.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, c.want)
			}
		})
	}
	if _, err := editPlaces([]byte(`{"places": [{"name": "a"}]}`), nil, "b"); err == nil {
		t.Fatal("removing a missing place succeeded")
	}
	if _, err := editPlaces([]byte(`{"places": 3}`), add, ""); err == nil {
		t.Fatal("non-array places accepted")
	}
	if _, err := editPlaces([]byte(`{"veduta": "world/1"}`), nil, "a"); err == nil {
		t.Fatal("removing from a file without places succeeded")
	}
}

func TestWorldToolsRegistered(t *testing.T) {
	names := map[string]bool{}
	m := &mcpServer{}
	m.extraTools = mcpExtraTools(m)
	for _, tl := range m.tools() {
		names[tl.Name] = true
	}
	for _, want := range []string{"world_map", "world_query", "world_place", "world_remove", "world_terrain", "world_vegetation", "inspect"} {
		if !names[want] {
			t.Errorf("missing tool %s", want)
		}
	}
	if !reflect.DeepEqual(InspectKinds, []string{"model", "texture", "scene", "prefab", "world"}) {
		t.Fatal(InspectKinds)
	}
}

func TestWorldTerrainAndVegetation(t *testing.T) {
	s, dir := gameSession(t)
	file := filepath.Join(dir, "assets", "worlds", "overworld.vworld")
	before, _ := os.ReadFile(file)
	h := float32(6)
	// A hill: the ground rises at its centre; a dry run writes nothing.
	rep, err := s.WorldTerrain(WorldTerrainOptions{World: "overworld", Name: "peak", Kind: "hill", Cell: [2]int32{20, -20}, Radius: 10, Height: &h, DryRun: true})
	if err != nil || rep.Written || rep.After.Max < rep.Before.Max+4 || rep.After.Centre < 5 || rep.Feature.Level != 6 || rep.Feature.Falloff != 10 {
		t.Fatalf("hill: %v %+v", err, rep)
	}
	if after, _ := os.ReadFile(file); string(after) != string(before) {
		t.Fatal("a dry run changed the file")
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rep.Sheet))); err != nil || !strings.Contains(rep.Human(), "hill peak") {
		t.Fatalf("sheet %v %s", err, rep.Human())
	}
	// A lake: written, water where there was none, its level resolved.
	lake, err := s.WorldTerrain(WorldTerrainOptions{World: "overworld", Name: "tarn", Kind: "lake", Cell: [2]int32{40, 30}, Radius: 6})
	if err != nil || !lake.Written || lake.Before.WaterCells != 0 || lake.After.WaterCells < 60 || lake.Feature.Depth != 2 || lake.After.Centre > -1.5 {
		t.Fatalf("lake: %v %+v", err, lake)
	}
	after, _ := os.ReadFile(file)
	if !strings.Contains(string(after), "\"height\": -0.5 },\n    { \"name\": \"tarn\", \"kind\": \"lake\", \"cell\": [40, 30], \"radius\": 6 }\n  ],") {
		t.Fatalf("feature not written:\n%s", after)
	}
	// The same name, a bad kind or a bad radius is refused with the compiler's reason.
	for _, o := range []WorldTerrainOptions{
		{World: "overworld", Name: "tarn", Kind: "hill", Cell: [2]int32{5, 5}, Radius: 4, Height: &h},
		{World: "overworld", Name: "x", Kind: "volcano", Cell: [2]int32{5, 5}, Radius: 4},
		{World: "overworld", Name: "x", Kind: "plain", Cell: [2]int32{5, 5}, Radius: 0},
		{World: "overworld", Name: "x", Kind: "hill", Cell: [2]int32{5, 5}, Radius: 4},
	} {
		if _, err := s.WorldTerrain(o); err == nil {
			t.Fatalf("accepted %+v", o)
		}
	}
	q, err := s.WorldQuery("overworld", [2]int32{40, 30})
	if err != nil || q.Water == nil || q.Height > *q.Water || !slices.Contains(q.Features, "tarn") {
		t.Fatalf("query in the pond: %v %+v", err, q)
	}

	// Vegetation: a flora model the project has, in an area.
	os.WriteFile(filepath.Join(dir, "assets", "models", "tuft.vmodel"), []byte(`{"veduta": "model/1", "pivot": "bottom-center", "draw_distance": 15,
		"parts": [{"shape": "lathe", "profile": [[0.1, 0], [0, 0.3]], "segments": 4, "material": "leaf"}]}`), 0o644)
	cell := [2]int32{-12, 10}
	veg, err := s.WorldVegetation(WorldVegetationOptions{World: "overworld", Name: "tuft_meadow", Model: "tuft", Density: 0.5, Cell: &cell, Radius: 6})
	if err != nil || !veg.Written || veg.Plants < 20 || veg.Triangles != 4*veg.Plants || veg.DrawDistance != 15 || len(veg.Warnings) != 0 {
		t.Fatalf("vegetation: %v %+v", err, veg)
	}
	// Trees: the prefab allows forest only, so a plain-only rule plants nothing and says so.
	north := [2]int32{0, -40}
	grove, err := s.WorldVegetation(WorldVegetationOptions{World: "overworld", Name: "north_grove", Prefab: "tree", Density: 0.3, Cell: &north, Radius: 10, DryRun: true})
	if err != nil || grove.Written || grove.Plants == 0 || grove.MaxChunkTriangles == 0 || grove.Triangles%grove.Plants != 0 {
		t.Fatalf("grove: %v %+v", err, grove)
	}
	none, err := s.WorldVegetation(WorldVegetationOptions{World: "overworld", Name: "north_grove", Prefab: "tree", Density: 0.3, Biomes: []string{"plain"}, Cell: &north, Radius: 10, DryRun: true})
	if err != nil || none.Plants != 0 || len(none.Warnings) != 1 {
		t.Fatalf("plain grove: %v %+v", err, none)
	}
	for _, o := range []WorldVegetationOptions{
		{World: "overworld", Name: "a", Model: "nowhere", Density: 0.1},
		{World: "overworld", Name: "a", Prefab: "village", Density: 0.1},
		{World: "overworld", Name: "a", Model: "tuft", Density: 0},
		{World: "overworld", Name: "tuft_meadow", Model: "tuft", Density: 0.1},
	} {
		if _, err := s.WorldVegetation(o); err == nil {
			t.Fatalf("accepted %+v", o)
		}
	}
	m, err := s.WorldMap("overworld", [2]int32{-12, 10}, 12)
	if err != nil || len(m.Features) == 0 || m.Features[0].Name != "pond" || m.Ground.WaterCells == 0 || m.Vegetation[len(m.Vegetation)-1].Name != "tuft_meadow" || m.Vegetation[len(m.Vegetation)-1].Plants < 20 {
		t.Fatalf("map: %v %+v", err, m)
	}
	// Remove both: the file is back to its bytes.
	for _, name := range []string{"tuft_meadow", "tarn"} {
		if r, err := s.WorldRemove("overworld", name); err != nil || !r.Removed || r.Kind == "place" {
			t.Fatalf("remove %s: %v %+v", name, err, r)
		}
	}
	if after, _ := os.ReadFile(file); string(after) != string(before) {
		t.Fatalf("file after removals:\n%s", after)
	}
	if _, err := s.WorldRemove("overworld", "tarn"); err == nil {
		t.Fatal("removed a feature twice")
	}
}
