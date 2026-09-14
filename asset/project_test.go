package asset

import (
	"reflect"
	"strings"
	"testing"

	"github.com/riftbane/veduta/gmath"
)

func TestParseProjectDefaults(t *testing.T) {
	p, err := ParseProject("veduta.json", []byte(`{"veduta": "project/1", "name": "mygame", "engine": "v0.1.0"}`))
	if err != nil {
		t.Fatal(err)
	}
	want := DefaultProject
	want.Name, want.Engine = "mygame", "v0.1.0"
	want.Title = want.Name // a game that names no title is shown by its name
	if !reflect.DeepEqual(*p, want) {
		t.Fatalf("got  %+v\nwant %+v", *p, want)
	}
	if want.Resolution != [2]int{320, 240} || want.InspectResolution != [2]int{320, 240} || want.TickRate != 20 ||
		want.Assets != "assets" || want.Cooked != "assets/.cooked" || want.Entry != "./cmd/game" || want.DefaultSeed != 1 ||
		want.Bounds != (gmath.AABB{Min: gmath.V3(-100, -50, -100), Max: gmath.V3(100, 100, 100)}) {
		t.Fatalf("defaults drifted from spec §5.2: %+v", want)
	}
	// Zero values mean "absent" too.
	p, err = ParseProject("veduta.json", []byte(`{"veduta": "project/1", "name": "g", "engine": "v1.0.0",
	  "tick_rate": 0, "default_seed": 0, "entry": "", "assets": "", "cooked": "", "default_scene": ""}`))
	if err != nil {
		t.Fatal(err)
	}
	if p.TickRate != 20 || p.DefaultSeed != 1 || p.Entry != "./cmd/game" || p.DefaultScene != "main" || p.Cooked != "assets/.cooked" {
		t.Fatalf("zero values did not take defaults: %+v", p)
	}
}

func TestParseProjectSample(t *testing.T) {
	p, err := ParseProject("veduta.json", readTestdata(t, "veduta.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := DefaultProject
	want.Name, want.Engine = "mygame", "v0.1.0"
	want.Title = want.Name
	want.Invariants = []string{"finite_positions", "within_bounds"}
	if !reflect.DeepEqual(*p, want) {
		t.Fatalf("got  %+v\nwant %+v", *p, want)
	}
}

func TestParseProjectCustom(t *testing.T) {
	src := `{"veduta": "project/1", "name": "space-race", "engine": "v0.2.0-rc.1+build.7", "entry": ".",
	  "resolution": [800, 600], "inspect_resolution": [320, 180], "tick_rate": 30, "default_scene": "level_1",
	  "default_seed": 99, "assets": "data/src", "cooked": "data/bin", "invariants": ["entity_count_max:010", "no_crash"],
	  "bounds": [[-1, -2, -3], [1, 2, 3]]}`
	p, err := ParseProject("veduta.json", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	want := Project{Name: "space-race", Title: "space-race", Engine: "v0.2.0-rc.1+build.7", Entry: ".", Resolution: [2]int{800, 600},
		InspectResolution: [2]int{320, 180}, TickRate: 30, DefaultScene: "level_1", DefaultSeed: 99,
		Assets: "data/src", Cooked: "data/bin", Invariants: []string{"entity_count_max:10", "no_crash"},
		Bounds: gmath.AABB{Min: gmath.V3(-1, -2, -3), Max: gmath.V3(1, 2, 3)}}
	if !reflect.DeepEqual(*p, want) {
		t.Fatalf("got  %+v\nwant %+v", *p, want)
	}
}

// TestParseProjectTitleAndIcon covers what a console dashboard shows: a readable name and
// a picture, neither of which the identifier in name can carry.
func TestParseProjectTitleAndIcon(t *testing.T) {
	head := `{"veduta": "project/1", "name": "mygame", "engine": "v0.1.0", `
	p, err := ParseProject("veduta.json", []byte(head+`"title": "Caverna delle Gemme", "icon": "icon.png"}`))
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Caverna delle Gemme" || p.Icon != "icon.png" {
		t.Fatalf("title %q icon %q", p.Title, p.Icon)
	}
	// An icon may sit in a subdirectory, as long as it stays inside the project.
	if p, err = ParseProject("veduta.json", []byte(head+`"icon": "art/tile.png"}`)); err != nil || p.Icon != "art/tile.png" {
		t.Fatalf("icon in a subdirectory: %v %v", p, err)
	}
	for _, c := range []struct{ body, errHas string }{
		{`"title": "` + strings.Repeat("x", 65) + `"`, "want 1 to 64"},
		{`"title": "two\nlines"`, "cannot be printed"},
		{`"icon": "icon.jpg"`, "must be a .png file"},
		{`"icon": "../outside.png"`, "clean relative path"},
		{`"icon": "/etc/icon.png"`, "must be relative"},
	} {
		_, err := ParseProject("veduta.json", []byte(head+c.body+`}`))
		if err == nil || !strings.Contains(err.Error(), c.errHas) {
			t.Errorf("%s: err = %v, want one containing %q", c.body, err, c.errHas)
		}
	}
}

func TestParseProjectErrors(t *testing.T) {
	head := `{"veduta": "project/1", "name": "g", "engine": "v0.1.0", `
	with := func(body string) string { return head + body + `}` }
	cases := []struct {
		name, src string
		wants     []wantErr
	}{
		{"name and engine missing", `{"veduta": "project/1"}`,
			[]wantErr{{`name: is required`, ""}, {`engine: is required`, ""}}},
		{"name invalid", `{"veduta": "project/1", "name": "My Game", "engine": "v0.1.0"}`,
			[]wantErr{{`name: name "My Game"`, `"My Game"`}}},
		{"engine without v", `{"veduta": "project/1", "name": "g", "engine": "0.1.0"}`,
			[]wantErr{{`engine: "0.1.0" is not a version like v0.1.0`, `"0.1.0"`}}},
		{"engine two parts", `{"veduta": "project/1", "name": "g", "engine": "v0.1"}`,
			[]wantErr{{`is not a version`, `"v0.1"`}}},
		{"engine leading zero", `{"veduta": "project/1", "name": "g", "engine": "v0.01.0"}`,
			[]wantErr{{`is not a version`, `"v0.01.0"`}}},
		{"engine empty prerelease", `{"veduta": "project/1", "name": "g", "engine": "v0.1.0-"}`,
			[]wantErr{{`is not a version`, `"v0.1.0-"`}}},
		{"engine bad build", `{"veduta": "project/1", "name": "g", "engine": "v0.1.0+a..b"}`,
			[]wantErr{{`is not a version`, `"v0.1.0+a..b"`}}},
		{"entry without dot", with(`"entry": "cmd/game"`),
			[]wantErr{{`entry: "cmd/game" must be a Go package path relative to the project root starting with "./"`, `"cmd/game"`}}},
		{"entry escapes", with(`"entry": "./../game"`),
			[]wantErr{{`entry: "../game" must be a clean relative path`, `"./../game"`}}},
		{"resolution length", with(`"resolution": [1280]`),
			[]wantErr{{`resolution: want [width, height], got 1 numbers`, `[1280]`}}},
		{"resolution range", with(`"resolution": [0, 9000], "inspect_resolution": [-1, 10]`),
			[]wantErr{{`resolution[0]: 0 out of range [1, 8192]`, `0, 9000`}, {`resolution[1]: 9000 out of range [1, 8192]`, `9000`},
				{`inspect_resolution[0]: -1 out of range`, `-1`}}},
		{"tick rate", with(`"tick_rate": 1001`),
			[]wantErr{{`tick_rate: 1001 out of range [1, 1000]`, `1001`}}},
		{"tick rate negative", with(`"tick_rate": -5`),
			[]wantErr{{`tick_rate: -5 out of range [1, 1000]`, `-5`}}},
		{"default scene", with(`"default_scene": "Main"`),
			[]wantErr{{`default_scene: name "Main"`, `"Main"`}}},
		{"default seed negative", with(`"default_seed": -1`),
			[]wantErr{{`default_seed: cannot use JSON number`, `-1`}}},
		{"paths", with(`"assets": "/abs", "cooked": "a\\b"`),
			[]wantErr{{`assets: "/abs" must be relative to the project root`, `"/abs"`}, {`cooked: "a\\b": use forward slashes`, `"a\\b"`}}},
		{"path segments", with(`"assets": "x/../y", "cooked": "out/"`),
			[]wantErr{{`assets: "x/../y" must be a clean relative path`, `"x/../y"`}, {`cooked: "out/" must be a clean`, `"out/"`}}},
		{"drive letter", with(`"assets": "C:/assets"`),
			[]wantErr{{`must be relative to the project root`, `"C:/assets"`}}},
		{"cooked equals assets", with(`"assets": "a", "cooked": "a"`),
			[]wantErr{{`cooked: must differ from assets ("a")`, `"a"}`}}},
		{"invariants", with(`"invariants": ["within_bounds", "no_overlap:x"]`),
			[]wantErr{{`invariants[1]: invariant "no_overlap:x"`, `"no_overlap:x"`}}},
		{"bounds count", with(`"bounds": [[0, 0, 0]]`),
			[]wantErr{{`bounds: want [[min_x, min_y, min_z], [max_x, max_y, max_z]], got 1 vectors`, `[[0, 0, 0]]`}}},
		{"bounds vector", with(`"bounds": [[0, 0], [1, 1, 1]]`),
			[]wantErr{{`bounds[0]: want 3 numbers, got 2`, `[0, 0]`}}},
		{"bounds null vector", with(`"bounds": [null, [1, 1, 1]]`),
			[]wantErr{{`bounds[0]: is required`, `null`}}},
		{"bounds order", with(`"bounds": [[-100, -50, -100], [100, -60, -100]]`),
			[]wantErr{{`bounds[1][1]: max y (-60) must be greater than min y (-50)`, `-60`}, {`bounds[1][2]: max z (-100) must be greater than min z (-100)`, `-100]]`}}},
		{"unknown field", with(`"tickrate": 30`),
			[]wantErr{{`tickrate: unknown field`, `"tickrate"`}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseProject("veduta.json", []byte(c.src))
			checkErrs(t, c.src, err, c.wants...)
		})
	}
}

func TestProjectDocExamples(t *testing.T) {
	for i, ex := range docJSON(t, "project.md") {
		if _, err := ParseProject("veduta.json", []byte(ex)); err != nil {
			t.Errorf("docs/project.md example %d: %v", i, err)
		}
	}
}
