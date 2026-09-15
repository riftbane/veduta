package asset

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/riftbane/veduta/gmath"
)

const testWorld = `{
  "veduta": "world/1",
  "seed": 7,
  "cell": 1.0,
  "chunk": 16,
  "extent": 32,
  "view": 1,
  "biome_scale": 48,
  "camera": { "type": "orthographic", "size": 12, "position": [0, 30, 0], "look_at": [0, 0, 0] },
  "light": { "direction": [-0.4, -1, -0.3] },
  "background": "#202830",
  "biomes":  [ { "name": "plain",  "ground": "grass", "weight": 3 },
               { "name": "forest", "ground": "moss" } ],
  "scatter": [ { "prefab": "tree", "biomes": ["forest"], "density": 0.08 } ],
  "sites":   [ { "tag": "city", "prefabs": ["city_small"], "biomes": ["plain"], "spacing": 24, "chance": 0.5 } ],
  "places":  [ { "name": "capital", "prefab": "city_small", "cell": [120, -40], "rotation": 90 } ],
  "entities": [ { "name": "player", "kind": "player", "model": "hero", "tags": ["player"] } ]
}`

func testPrefabs() func(string) *Prefab {
	m := map[string]*Prefab{
		"tree":       {Name: "tree", Footprint: gmath.V2(1, 1)},
		"city_small": {Name: "city_small", Footprint: gmath.V2(12, 8), Tags: []string{"city"}, Distances: []Distance{{"city", 4}}},
		"big":        {Name: "big", Footprint: gmath.V2(40, 40)},
	}
	return func(n string) *Prefab { return m[n] }
}

func TestParseWorldFull(t *testing.T) {
	w, err := ParseWorld("overworld.world.json", []byte(testWorld), testPrefabs())
	if err != nil {
		t.Fatal(err)
	}
	got := *w
	got.Camera, got.Entities = Camera{}, nil
	want := World{
		Name: "overworld", Seed: 7, Cell: 1, Chunk: 16, Extent: 32, View: 1, BiomeScale: 48,
		Light:      w.Light,
		Background: 0xff202830,
		Biomes:     []Biome{{"plain", "grass", 3}, {"forest", "moss", 1}},
		Scatter:    []Scatter{{"tree", []string{"forest"}, 0.08}},
		Sites:      []Site{{"city", []string{"city_small"}, []string{"plain"}, 24, 0.5}},
		Places:     []Place{{"capital", "city_small", [2]int32{120, -40}, 90}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
	if !w.Camera.Ortho || w.Camera.Size != 12 || len(w.Entities) != 1 || w.Entities[0].Kind != "player" {
		t.Fatalf("camera %+v entities %+v", w.Camera, w.Entities)
	}
	if w.Span() != 512 || w.Cells(0) != 0 || w.Cells(0.2) != 1 || w.Cells(3) != 3 || w.Cells(3.01) != 4 || w.Biome("forest") != 1 || w.Biome("sea") != -1 {
		t.Fatal("helpers")
	}
	if x, z := w.Footprint(testPrefabs()("city_small"), 0); x != 12 || z != 8 {
		t.Fatalf("footprint %d×%d", x, z)
	}
	if x, z := w.Footprint(testPrefabs()("city_small"), 270); x != 8 || z != 12 {
		t.Fatalf("rotated footprint %d×%d", x, z)
	}
}

func TestParseWorldDefaults(t *testing.T) {
	src := `{"veduta": "world/1", "camera": {"position": [0, 5, 10], "look_at": [0, 0, 0]}, "biomes": [{"name": "plain", "ground": "grass"}]}`
	w, err := ParseWorld("w.world.json", []byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	d := WorldDefaults
	if w.Cell != d.Cell || w.Chunk != d.Chunk || w.Extent != d.Extent || w.View != d.View || w.BiomeScale != d.BiomeScale || w.Seed != 0 {
		t.Fatalf("%+v", w)
	}
	if w.Biomes[0].Weight != 1 || w.Scatter != nil || w.Sites != nil || w.Places != nil || w.Entities != nil {
		t.Fatalf("%+v", w)
	}
	if float64(w.Extent)*float64(w.Chunk)*float64(w.Cell) != MaxWorldMeters {
		t.Fatalf("the defaults reach %v m, want exactly %d", float64(w.Extent)*float64(w.Chunk)*float64(w.Cell), MaxWorldMeters)
	}
}

func TestParseWorldErrors(t *testing.T) {
	cam := `"camera": {"position": [0, 5, 10], "look_at": [0, 0, 0]}`
	bio := `"biomes": [{"name": "plain", "ground": "grass"}, {"name": "forest", "ground": "moss"}]`
	world := func(extra string) string { return `{"veduta": "world/1", ` + cam + `, ` + bio + extra + `}` }
	cases := []struct {
		name, src string
		wants     []wantErr
	}{
		{"no biomes", `{"veduta": "world/1", ` + cam + `}`,
			[]wantErr{{`biomes: at least one biome is required`, ""}}},
		{"cell too large", world(`, "cell": 100`),
			[]wantErr{{`cell: 100 out of range (0, 64]`, `100`}}},
		{"chunk too small", world(`, "chunk": 2`),
			[]wantErr{{`chunk: 2 out of range [4, 64]`, `2`}}},
		{"view too large", world(`, "view": 9`),
			[]wantErr{{`view: 9 out of range [1, 4]`, `9`}}},
		{"too far", world(`, "extent": 600`),
			[]wantErr{{`extent: the world reaches 9600 m from the origin (extent 600 × chunk 16 × cell 1), more than 8192 m`, `600`}}},
		{"too far by cell", world(`, "cell": 2`),
			[]wantErr{{`cell: the world reaches 16384 m`, `2}`}}},
		{"duplicate biome", world(`, "scatter": [], "sites": []`) + "",
			nil},
		{"biome dup", `{"veduta": "world/1", ` + cam + `, "biomes": [{"name": "a", "ground": "g"}, {"name": "a", "ground": "g"}]}`,
			[]wantErr{{`biomes[1].name: duplicate biome "a" (first used by biomes[0])`, `"a", "ground": "g"}]`}}},
		{"biome weight", world(``)[:len(world(``))-1] + `}`,
			nil},
		{"weight range", `{"veduta": "world/1", ` + cam + `, "biomes": [{"name": "a", "ground": "g", "weight": 5000}]}`,
			[]wantErr{{`biomes[0].weight: 5000 out of range [1, 1000]`, `5000`}}},
		{"missing ground", `{"veduta": "world/1", ` + cam + `, "biomes": [{"name": "a"}]}`,
			[]wantErr{{`biomes[0].ground: is required`, `{"name": "a"}`}}},
		{"scatter density missing", world(`, "scatter": [{"prefab": "tree"}]`),
			[]wantErr{{`scatter[0].density: is required`, `{"prefab": "tree"}`}}},
		{"scatter density range", world(`, "scatter": [{"prefab": "tree", "density": 1.5}]`),
			[]wantErr{{`scatter[0].density: 1.5 out of range (0, 1]`, `1.5`}}},
		{"scatter unknown biome", world(`, "scatter": [{"prefab": "tree", "biomes": ["sea"], "density": 0.1}]`),
			[]wantErr{{`scatter[0].biomes[0]: unknown biome "sea" (want one of [plain forest])`, `"sea"`}}},
		{"scatter too big", world(`, "scatter": [{"prefab": "big", "density": 0.1}]`),
			[]wantErr{{`scatter[0].prefab: prefab "big" is 40×40 m, larger than one cell (1 m)`, `"big"`}}},
		{"site spacing missing", world(`, "sites": [{"tag": "city", "prefabs": ["city_small"]}]`),
			[]wantErr{{`sites[0].spacing: is required`, `{"tag"`}}},
		{"site spacing too small for prefab", world(`, "sites": [{"tag": "city", "prefabs": ["city_small"], "spacing": 12}]`),
			[]wantErr{{`sites[0].spacing: 12 cells is less than the 12×8 cell footprint of prefab "city_small" (prefabs[0]) plus its min_distance (4 m): need at least 16`, `12}`}}},
		{"site chance range", world(`, "sites": [{"tag": "city", "prefabs": ["city_small"], "spacing": 24, "chance": 0}]`),
			[]wantErr{{`sites[0].chance: 0 out of range (0, 1]`, `0}`}}},
		{"site no prefabs", world(`, "sites": [{"tag": "city", "prefabs": [], "spacing": 24}]`),
			[]wantErr{{`sites[0].prefabs: at least one prefab is required`, `[]`}}},
		{"place reserved", world(`, "places": [{"name": "site_x", "prefab": "city_small", "cell": [0, 0]}]`),
			[]wantErr{{`places[0].name: name "site_x" starts with "site_", which is reserved`, `"site_x"`}}},
		{"place duplicate", world(`, "places": [{"name": "a", "prefab": "tree", "cell": [0, 0]}, {"name": "a", "prefab": "tree", "cell": [5, 5]}]`),
			[]wantErr{{`places[1].name: duplicate place "a" (first used by places[0])`, `"a", "prefab": "tree", "cell": [5`}}},
		{"place rotation", world(`, "places": [{"name": "a", "prefab": "tree", "cell": [0, 0], "rotation": 45}]`),
			[]wantErr{{`places[0].rotation: 45 is not one of [0 90 180 270]`, `45`}}},
		{"place cell missing", world(`, "places": [{"name": "a", "prefab": "tree"}]`),
			[]wantErr{{`places[0].cell: is required ([x, z] in cells)`, `{"name": "a", "prefab": "tree"}`}}},
		{"place cell length", world(`, "places": [{"name": "a", "prefab": "tree", "cell": [1]}]`),
			[]wantErr{{`places[0].cell: want [x, z], got 1 numbers`, `[1]`}}},
		{"place outside", world(`, "places": [{"name": "a", "prefab": "tree", "cell": [8192, 0]}]`),
			[]wantErr{{`places[0].cell[0]: 8192 is outside the world (cells -8192 to 8191)`, `8192`}}},
		{"place footprint outside", world(`, "places": [{"name": "a", "prefab": "city_small", "cell": [8185, 8190]}]`),
			[]wantErr{{`places[0].cell[0]: the 12×8 cell footprint of "city_small" reaches cell 8196, outside the world (last cell 8191)`, `8185`},
				{`places[0].cell[1]: the 12×8 cell footprint of "city_small" reaches cell 8197`, `8190`}}},
		{"entity reserved", world(`, "entities": [{"name": "chunk_0_0_ground", "kind": "static"}]`),
			[]wantErr{{`entities[0].name: name "chunk_0_0_ground" starts with "chunk_", which is reserved`, `"chunk_0_0_ground"`}}},
		{"unknown field", world(`, "plane": "xy"`),
			[]wantErr{{`plane: unknown field`, `"plane"`}}},
	}
	for _, c := range cases {
		if c.wants == nil {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseWorld("w.world.json", []byte(c.src), testPrefabs())
			checkErrs(t, c.src, err, c.wants...)
		})
	}
}

func TestWorldWithoutPrefabsSkipsFootprintChecks(t *testing.T) {
	src := `{"veduta": "world/1", "camera": {"position": [0, 5, 10], "look_at": [0, 0, 0]}, "biomes": [{"name": "plain", "ground": "grass"}],
	  "scatter": [{"prefab": "big", "density": 0.1}], "sites": [{"tag": "city", "prefabs": ["city_small"], "spacing": 2}],
	  "places": [{"name": "a", "prefab": "city_small", "cell": [8190, 8190]}]}`
	if _, err := ParseWorld("w.world.json", []byte(src), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseWorld("w.world.json", []byte(src), testPrefabs()); err == nil {
		t.Fatal("footprint checks skipped with prefabs")
	}
}

func TestWorldDeps(t *testing.T) {
	var src WorldSource
	if err := json.Unmarshal([]byte(testWorld), &src); err != nil {
		t.Fatal(err)
	}
	src.Places = append(src.Places, PlaceSource{Prefab: "Bad Name"})
	want := []string{"prefabs/city_small.prefab.json", "prefabs/tree.prefab.json"}
	if got := WorldDeps(&src); !reflect.DeepEqual(got, want) {
		t.Fatalf("deps %v, want %v", got, want)
	}
}

func TestReserved(t *testing.T) {
	for name, want := range map[string]string{"chunk_1_2": "chunk_", "site_0_1_1_x": "site_", "place_capital": "place_", "chunky": "", "capital": ""} {
		if got := Reserved(name); got != want {
			t.Errorf("Reserved(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestPrefabAndWorldCodec(t *testing.T) {
	p, err := ParsePrefab("house.prefab.json", []byte(`{"veduta": "prefab/1", "footprint": [3, 2.5], "tags": ["house"],
	  "rules": {"biomes": ["plain"], "min_distance": {"city": 6, "house": 1}},
	  "entities": [{"name": "walls", "kind": "static", "model": "house", "hitbox": [[0, 0, 0], [3, 2, 2.5]], "layer": 2},
	               {"name": "roof", "kind": "static", "parent": "walls", "tags": ["roof"], "visible": false}]}`))
	if err != nil {
		t.Fatal(err)
	}
	w, err := ParseWorld("overworld.world.json", []byte(testWorld), testPrefabs())
	if err != nil {
		t.Fatal(err)
	}
	pc, wc := EncodePrefab(p), EncodeWorld(w)
	if !bytes.Equal(pc.Data, EncodePrefab(p).Data) || !bytes.Equal(wc.Data, EncodeWorld(w).Data) {
		t.Fatal("encoding is not deterministic")
	}
	p2, err := DecodePrefab(pc)
	if err != nil || !reflect.DeepEqual(p2, p) {
		t.Fatalf("prefab round trip: %v\n got  %+v\n want %+v", err, p2, p)
	}
	w2, err := DecodeWorld(wc)
	if err != nil || !reflect.DeepEqual(w2, w) {
		t.Fatalf("world round trip: %v\n got  %+v\n want %+v", err, w2, w)
	}
	if !bytes.Equal(EncodePrefab(p2).Data, pc.Data) || !bytes.Equal(EncodeWorld(w2).Data, wc.Data) {
		t.Fatal("re-encoding differs")
	}
	for _, k := range []Kind{KindPrefab, KindWorld} {
		body := pc
		if k == KindWorld {
			body = wc
		}
		meta := Meta{Kind: k, Name: "x", Source: k.Dir() + "/x" + k.Ext(), SourceHash: testHash, Compiler: CompilerVersion}
		file, err := PackVDA(meta, body)
		if err != nil {
			t.Fatal(err)
		}
		if _, got, err := UnpackVDA(file); err != nil || got.Type != body.Type {
			t.Fatalf("%s through vda: %v", k, err)
		}
	}
	// Decoders reject out-of-range values.
	bad := *w
	bad.Chunk = 3
	if _, err := DecodeWorld(EncodeWorld(&bad)); err == nil || !strings.Contains(err.Error(), "chunk 3 out of range") {
		t.Fatalf("bad chunk accepted: %v", err)
	}
	bad = *w
	bad.Places = []Place{{"a", "b", [2]int32{9000, 0}, 0}}
	if _, err := DecodeWorld(EncodeWorld(&bad)); err == nil || !strings.Contains(err.Error(), "outside the world") {
		t.Fatalf("bad place accepted: %v", err)
	}
	bp := *p
	bp.Footprint = gmath.V2(0, 1)
	if _, err := DecodePrefab(EncodePrefab(&bp)); err == nil || !strings.Contains(err.Error(), "footprint") {
		t.Fatalf("bad footprint accepted: %v", err)
	}
}

func TestScenarioWorld(t *testing.T) {
	sc, err := ParseScenario("w.scenario.json", []byte(`{"veduta": "scenario/1", "world": "overworld", "at": [120, -40], "ticks": 10}`))
	if err != nil {
		t.Fatal(err)
	}
	if sc.World != "overworld" || sc.Scene != "" || sc.At != [2]int32{120, -40} {
		t.Fatalf("%+v", sc)
	}
	cases := []struct {
		name, src string
		wants     []wantErr
	}{
		{"neither", `{"veduta": "scenario/1", "ticks": 10}`,
			[]wantErr{{`scene: is required (name of a scene in assets/scenes), or world`, ""}}},
		{"both", `{"veduta": "scenario/1", "scene": "main", "world": "overworld", "ticks": 10}`,
			[]wantErr{{`world: a scenario simulates a scene or a world, not both`, `"overworld"`}}},
		{"at with scene", `{"veduta": "scenario/1", "scene": "main", "at": [1, 2], "ticks": 10}`,
			[]wantErr{{`at: only used with world`, `[1, 2]`}}},
		{"at length", `{"veduta": "scenario/1", "world": "w", "at": [1], "ticks": 10}`,
			[]wantErr{{`at: want [x, z] in cells, got 1 numbers`, `[1]`}}},
		{"at range", `{"veduta": "scenario/1", "world": "w", "at": [1, 300000], "ticks": 10}`,
			[]wantErr{{`at[1]: 300000 out of range [-262144, 262143]`, `300000`}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseScenario("s.scenario.json", []byte(c.src))
			checkErrs(t, c.src, err, c.wants...)
		})
	}
}

func TestWorldDocExamples(t *testing.T) {
	for i, ex := range docJSON(t, "world.md") {
		var h struct{ Veduta string }
		if err := json.Unmarshal([]byte(ex), &h); err != nil {
			t.Fatalf("example %d: %v", i, err)
		}
		var err error
		switch h.Veduta {
		case TypeWorld:
			_, err = ParseWorld("example.world.json", []byte(ex), nil)
		case TypePrefab:
			_, err = ParsePrefab("example.prefab.json", []byte(ex))
		case TypeScenario:
			_, err = ParseScenario("example.scenario.json", []byte(ex))
		default:
			t.Errorf("example %d: no parser for %q", i, h.Veduta)
		}
		if err != nil {
			t.Errorf("docs/world.md example %d: %v", i, err)
		}
	}
}
