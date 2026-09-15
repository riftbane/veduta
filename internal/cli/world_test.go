package cli

import (
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/riftbane/veduta/asset"
)

// templateSession copies the template into a temp dir (no build) and opens it.
func templateSession(t *testing.T) (*Session, string) {
	t.Helper()
	src := filepath.Join("..", "..", "template")
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
	s, dir := templateSession(t)
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
	s, dir := templateSession(t)
	file := filepath.Join(dir, "assets", "worlds", "overworld.world.json")
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
	w, err := asset.ParseWorld("overworld.world.json", after, nil)
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
	for _, want := range []string{"world_map", "world_query", "world_place", "world_remove", "inspect"} {
		if !names[want] {
			t.Errorf("missing tool %s", want)
		}
	}
	if !reflect.DeepEqual(InspectKinds, []string{"model", "texture", "scene", "prefab", "world"}) {
		t.Fatal(InspectKinds)
	}
}
