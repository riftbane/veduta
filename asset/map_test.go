package asset

import (
	"reflect"
	"strings"
	"testing"

	"github.com/riftbane/veduta/v2/gmath"
)

const mapSrc = `{
  "veduta": "map/1",
  "size": [4, 3],
  "tile": 2,
  "origin": [1, 2, 3],
  "terrains": [
    { "key": ".", "name": "grass", "texture": "grass", "tags": ["tillable"] },
    { "key": "~", "name": "water", "material": "water_mat", "tags": ["water", "solid"] }
  ],
  "layers": [
    { "name": "ground", "rows": ["..~~", "..~.", "...."] },
    { "name": "top", "z": 2, "layer": 5, "rows": ["    ", " .  ", "    "] }
  ],
  "objects": [
    { "name": "door", "at": [1, 0], "tags": ["door"], "props": { "to": "house", "x": 3, "f": 0.5, "big": true } },
    { "name": "field", "at": [0, 1], "size": [2, 2] }
  ]
}`

func TestMap(t *testing.T) {
	m, err := ParseMap("assets/maps/farm.vmap", []byte(mapSrc))
	if err != nil {
		t.Fatal(err)
	}
	want := &Map{
		Name: "farm", W: 4, H: 3, Tile: 2, Origin: gmath.V3(1, 2, 3),
		Terrains: []MapTerrain{
			{Key: '.', Name: "grass", Texture: "grass", Tags: []string{"tillable"}},
			{Key: '~', Name: "water", Material: "water_mat", Tags: []string{"water", "solid"}},
		},
		Layers: []MapLayer{
			{Name: "ground", Z: 0, Cells: []uint8{1, 1, 2, 2, 1, 1, 2, 1, 1, 1, 1, 1}},
			{Name: "top", Z: 2, Layer: 5, Cells: []uint8{0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0}},
		},
		Objects: []MapObject{
			{Name: "door", X: 1, Y: 0, W: 1, H: 1, Tags: []string{"door"},
				Props: []MapProp{{"big", true}, {"f", 0.5}, {"to", "house"}, {"x", 3.0}}},
			{Name: "field", X: 0, Y: 1, W: 2, H: 2},
		},
	}
	if !reflect.DeepEqual(m, want) {
		t.Fatalf("compiled\n%+v\nwant\n%+v", m, want)
	}
	if m.TerrainAt(0, 2, 1).Name != "water" || m.TerrainAt(1, 0, 0) != nil || m.TerrainAt(0, 4, 0) != nil {
		t.Errorf("TerrainAt wrong")
	}
	if m.Terrain("water") != 1 || m.Terrain("lava") != -1 || m.Layer("top") != 1 || m.Layer("x") != -1 {
		t.Errorf("lookups wrong")
	}
	back, err := DecodeMap(EncodeMap(m))
	if err != nil || !reflect.DeepEqual(back, m) {
		t.Fatalf("round trip: %v\n%+v", err, back)
	}
	// A default layer z is a tenth of its index.
	m2, err := ParseMap("m.vmap", []byte(strings.Replace(mapSrc, `"z": 2, `, "", 1)))
	if err != nil || m2.Layers[1].Z != 0.1 {
		t.Errorf("default z %v, %v", m2.Layers[1].Z, err)
	}
}

func TestMapErrors(t *testing.T) {
	cases := map[string][]string{
		`"size": [0, 2000], "terrains": [], "layers": []`: {
			`size[0]: 0 out of range [1, 1024]`, `size[1]: 2000 out of range [1, 1024]`,
			`terrains: at least one terrain is required`, `layers: at least one layer is required`},
		`"size": [2, 1], "terrains": [{"key": "ab", "name": "Grass"}, {"key": " ", "name": "w", "texture": "w", "material": "m"}, {"key": ".", "name": "w", "texture": "t"}, {"key": ".", "name": "x", "texture": "t", "tags": ["a", "a"]}], "layers": [{"name": "g", "rows": [".."]}]`: {
			`terrains[0].key: must be one printable ASCII character`,
			`terrains[0].name: name "Grass" may only contain`,
			`terrains[0]: needs a texture or a material`,
			`terrains[1].key: must be one printable ASCII character but space`,
			`terrains[1].material: not allowed with texture`,
			`terrains[2].name: duplicate terrain name "w" (first used by terrains[1])`,
			`terrains[3].key: duplicate key "." (first used by terrains[2])`,
			`terrains[3].tags[1]: duplicate "a"`},
		`"size": [3, 2], "terrains": [{"key": ".", "name": "g", "texture": "g"}], "layers": [{"name": "g", "rows": ["..", "..x"]}, {"name": "g", "z": 1e9, "layer": 2000, "rows": ["...", "..."]}, {"rows": ["...", "...", "..."]}]`: {
			`layers[0].rows[0]: 2 characters, want 3`,
			`layers[0].rows[1]: character "x" at column 2 is not a terrain's key`,
			`layers[1].name: duplicate layer name "g" (first used by layers[0])`,
			`layers[1].z: 1e+09 out of range`,
			`layers[1].layer: 2000 out of range [-1000, 1000]`,
			`layers[2].name: is required`,
			`layers[2].rows: 3 rows, want 2`},
		`"size": [3, 2], "terrains": [{"key": ".", "name": "g", "texture": "g"}], "layers": [{"name": "g", "rows": ["...", "..."]}], "objects": [{"name": "a", "at": [3, 0]}, {"name": "a", "at": [1, 1], "size": [3, 1]}, {"at": [0]}, {"name": "b", "at": [0, 0], "props": {"x.y": 1, "list": [1], "o": {}}}]`: {
			`objects[0].at: cell [3 0] is outside the 3 × 2 map`,
			`objects[1].name: duplicate object name "a"`,
			`objects[1].size: [3 1] cells from [1 1] leave the 3 × 2 map`,
			`objects[2].name: is required`,
			`objects[2].at: want [column, row], got 1 numbers`,
			`objects[3].props.list: must be a string, a number or a boolean`,
			`objects[3].props.o: must be a string, a number or a boolean`,
			`objects[3].props.x.y: a property name is 1-64 characters without spaces or '.'`},
	}
	for body, wants := range cases {
		_, err := ParseMap("m.vmap", []byte(`{"veduta": "map/1", `+body+`}`))
		if err == nil {
			t.Errorf("%s: no error", body)
			continue
		}
		for _, w := range wants {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s\nreports:\n%v\nwant %q", body, err, w)
			}
		}
	}
}

func TestSceneMap(t *testing.T) {
	s, err := ParseScene("main.vscene", []byte(`{"veduta": "scene/1", "camera": {"position": [0, 0, 5], "look_at": [0, 0, 0]}, "map": "farm", "entities": []}`))
	if err != nil || s.Map != "farm" {
		t.Fatalf("map %q, %v", s.Map, err)
	}
	back, err := DecodeScene(EncodeScene(s))
	if err != nil || back.Map != "farm" {
		t.Fatalf("round trip %q, %v", back.Map, err)
	}
	lib := NewLibrary(nil)
	lib.Scenes["main"] = s
	m, _ := ParseMap("farm.vmap", []byte(mapSrc))
	lib.Maps["other"] = m
	got := strings.Join(lib.References(), "\n")
	for _, w := range []string{`scene main: map "farm" not found`, `map other: terrain grass: texture "grass" not found`, `map other: terrain water: material "water_mat" not found`} {
		if !strings.Contains(got, w) {
			t.Errorf("references:\n%s\nwant %q", got, w)
		}
	}
}

// An empty key is a key like any other: it is reported, not a crash.
func TestEmptyKey(t *testing.T) {
	_, err := ParseMap("m.vmap", []byte(`{"veduta": "map/1", "size": [1, 1], "terrains": [{"key": ".", "name": "g", "texture": "g"}],
		"layers": [{"name": "g", "rows": ["."]}], "objects": [{"name": "o", "at": [0, 0], "props": {"": 1}}]}`))
	if err == nil || !strings.Contains(err.Error(), "objects[0].props.: a property name is 1-64 characters") {
		t.Errorf("empty property name: %v", err)
	}
	if _, err := ParseMap("m.vmap", []byte(`{"veduta": "map/1", "a": {"": 2}}`)); err == nil || !strings.Contains(err.Error(), `unknown field`) {
		t.Errorf("empty key in an unknown field: %v", err)
	}
}
