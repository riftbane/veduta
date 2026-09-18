package model

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/gmath"
	"github.com/riftbane/veduta/v2/internal/golden"
)

// compile parses src as "test.vmodel" and fails the test on error.
func compile(t testing.TB, src string) *asset.Model {
	t.Helper()
	m, err := Parse("test.vmodel", []byte(src))
	if err != nil {
		t.Fatalf("compile: %v\n%s", err, src)
	}
	return m
}

// model wraps comma-separated part objects into a model source.
func model(parts string) string {
	return `{"veduta": "model/1", "parts": [` + parts + `]}`
}

// compileErrs parses src and returns its validation errors, failing if there are none.
func compileErrs(t *testing.T, file, src string) asset.Errors {
	t.Helper()
	_, err := Parse(file, []byte(src))
	if err == nil {
		t.Fatalf("no error for %s", src)
	}
	var es asset.Errors
	if !errors.As(err, &es) {
		t.Fatalf("error %T (%v) is not asset.Errors", err, err)
	}
	return es
}

const badSrc = `{
  "veduta": "model/1",
  "name": "other",
  "units": "cm",
  "pivot": "top",
  "smooth_angle_deg": 200,
  "symmetry": "w",
  "triangle_budget": 0,
  "parts": [
    { "shape": "box", "size": [1, 0, 1], "radius": 2 },
    { "shape": "cylinder", "height": 1, "segments": 2, "rings": 4 },
    { "shape": "sphere", "radius": -1, "segments": 300, "rings": 1 },
    { "shape": "plane", "size": [1, 2, 3], "uv": "cubic" },
    { "shape": "extrude", "profile": [[0, 0], [1, 1], [1, 0], [0, 1]], "depth": 1 },
    { "shape": "lathe", "profile": [[0, 0], [-1, 1]], "segments": 0 },
    { "shape": "mirror", "axis": "x", "of": 7, "position": [0, 0, 0], "flip_normals": false },
    { "shape": "torus" },
    { "size": [1, 1, 1] },
    { "shape": "box", "size": [1, 1, 1], "rotation_deg": [0, 1], "scale": [1, 0, 1], "material": "Bad Name" }
  ]
}`

// TestValidationReportsEverything checks that every problem of a source is reported at
// once, in source order, with its JSON path and line.
func TestValidationReportsEverything(t *testing.T) {
	want := []struct {
		line int
		msg  string
	}{
		{3, `name: "other" does not match the file name (want "bad")`},
		{4, `units: unknown value "cm"`},
		{5, `pivot: unknown value "top"`},
		{6, `smooth_angle_deg: 200 out of range [0, 180]`},
		{7, `symmetry: unknown value "w"`},
		{8, `triangle_budget: 0 out of range [1, 1000000]`},
		{10, `parts[0].radius: not used by shape box`},
		{10, `parts[0].size[1]: must be a positive number, got 0`},
		{11, `parts[1].rings: not used by shape cylinder`},
		{11, `parts[1].radius: is required`},
		{11, `parts[1].segments: 2 out of range [3, 256]`},
		{12, `parts[2].radius: must be a positive number, got -1`},
		{12, `parts[2].segments: 300 out of range [3, 256]`},
		{12, `parts[2].rings: 1 out of range [2, 128]`},
		{13, `parts[3].size: want 2 numbers, got 3`},
		{13, `parts[3].uv: unknown value "cubic"`},
		{14, `parts[4].profile: not a simple polygon: edge 0-1 touches or crosses edge 2-3`},
		{15, `parts[5].profile[1][0]: radius must be >= 0, got -1`},
		{15, `parts[5].segments: 0 out of range [3, 256]`},
		{16, `parts[6].flip_normals: not used by shape mirror`},
		{16, `parts[6].position: not used by shape mirror`},
		{16, `parts[6].of: 7 is not the index of an earlier part (want 0..5)`},
		{17, `parts[7].shape: unknown value "torus"`},
		{18, `parts[8].shape: is required`},
		{19, `parts[9].rotation_deg: want 3 numbers, got 2`},
		{19, `parts[9].scale[1]: must be non-zero`},
		{19, `parts[9].material: name "Bad Name" may only contain`},
	}
	es := compileErrs(t, "models/bad.vmodel", badSrc)
	for i, e := range es {
		t.Logf("%v", e)
		if i >= len(want) {
			continue
		}
		if e.File != "models/bad.vmodel" || e.Line != want[i].line || !strings.HasPrefix(e.Msg, want[i].msg) {
			t.Errorf("error %d = %s:%d:%d %q, want line %d %q", i, e.File, e.Line, e.Col, e.Msg, want[i].line, want[i].msg)
		}
	}
	if len(es) != len(want) {
		t.Fatalf("got %d errors, want %d", len(es), len(want))
	}
	// Columns point at the value: "radius" of part 0 is the 2 after its key.
	if e := es[6]; e.Col != strings.Index(strings.Split(badSrc, "\n")[9], `2 }`)+1 {
		t.Errorf("parts[0].radius column = %d", e.Col)
	}
}

func TestShapeValidation(t *testing.T) {
	box := `{"shape": "box", "size": [1, 1, 1]}`
	cases := []struct{ parts, want string }{
		{`{"shape": "box"}`, "parts[0].size: is required"},
		{`{"shape": "box", "size": [1, 1, 1], "segments": 8}`, "parts[0].segments: not used by shape box"},
		{`{"shape": "box", "size": [1, 1]}`, "parts[0].size: want 3 numbers, got 2"},
		{`{"shape": "box", "size": [1, 1, 1], "profile": [[0, 0]]}`, "parts[0].profile: not used by shape box"},
		{`{"shape": "cylinder", "radius": 1}`, "parts[0].height: is required"},
		{`{"shape": "cylinder", "radius": 1, "height": 1, "size": [1, 1, 1]}`, "parts[0].size: not used by shape cylinder"},
		{`{"shape": "cylinder", "radius": 1, "height": 1, "segments": 257}`, "parts[0].segments: 257 out of range [3, 256]"},
		{`{"shape": "cylinder", "radius": 0, "height": 1}`, "parts[0].radius: must be a positive number, got 0"},
		{`{"shape": "sphere"}`, "parts[0].radius: is required"},
		{`{"shape": "sphere", "radius": 1, "height": 2}`, "parts[0].height: not used by shape sphere"},
		{`{"shape": "sphere", "radius": 1, "rings": 129}`, "parts[0].rings: 129 out of range [2, 128]"},
		{`{"shape": "plane"}`, "parts[0].size: is required"},
		{`{"shape": "plane", "size": [1, -1]}`, "parts[0].size[1]: must be a positive number, got -1"},
		{`{"shape": "plane", "size": [1, 1], "depth": 1}`, "parts[0].depth: not used by shape plane"},
		{`{"shape": "extrude", "profile": [[0, 0], [1, 0], [0, 1]]}`, "parts[0].depth: is required"},
		{`{"shape": "extrude", "depth": 1}`, "parts[0].profile: is required"},
		{`{"shape": "extrude", "profile": [[0, 0], [1, 0]], "depth": 1}`, "parts[0].profile: want at least 3 points, got 2"},
		{`{"shape": "extrude", "profile": [[0, 0], [1, 0], [1, 0], [0, 1]], "depth": 1}`, "parts[0].profile[2]: repeats point 1"},
		{`{"shape": "extrude", "profile": [[0, 0], [1, 0], [0, 1], [0, 0]], "depth": 1}`, "parts[0].profile[0]: repeats point 3"},
		{`{"shape": "extrude", "profile": [[0, 0], [1, 0, 2], [0, 1]], "depth": 1}`, "parts[0].profile[1]: want 2 numbers, got 3"},
		{`{"shape": "extrude", "profile": [[0, 0], [2, 0], [1, 0], [1, 1]], "depth": 1}`, "parts[0].profile: not a simple polygon"},
		{`{"shape": "extrude", "profile": [[0, 0], [1, 0], [0, 1]], "depth": 1, "segments": 4}`, "parts[0].segments: not used by shape extrude"},
		{`{"shape": "lathe", "profile": [[0, 0]]}`, "parts[0].profile: want at least 2 points, got 1"},
		{`{"shape": "lathe", "profile": [[0, 0], [0, 1]]}`, "parts[0].profile: every point lies on the axis"},
		{`{"shape": "lathe", "profile": [[0, 0], [1, 1]], "depth": 1}`, "parts[0].depth: not used by shape lathe"},
		{`{"shape": "lathe", "profile": [[0, 0], [1, 1], [1, 1]]}`, "parts[0].profile[2]: repeats point 1"},
		{`{"shape": "box", "size": [1, 1, 1], "uv": "cube"}`, `parts[0].uv: unknown value "cube"`},
		{`{"shape": "box", "size": [1, 1, 1], "scale": [0, 1, 1]}`, "parts[0].scale[0]: must be non-zero"},
		{`{"shape": "box", "size": [1, 1, 1], "position": [1, 2]}`, "parts[0].position: want 3 numbers, got 2"},
		{`{"shape": "box", "size": [1, 1, 1], "material": "Wood"}`, `parts[0].material: name "Wood"`},
		{`{"shape": "mirror", "axis": "x", "of": 0}`, "parts[0].of: a mirror needs an earlier part"},
		{box + `, {"shape": "mirror", "of": 0}`, "parts[1].axis: is required"},
		{box + `, {"shape": "mirror", "axis": "x"}`, "parts[1].of: is required"},
		{box + `, {"shape": "mirror", "axis": "w", "of": 0}`, `parts[1].axis: unknown value "w"`},
		{box + `, {"shape": "mirror", "axis": "x", "of": 1}`, "parts[1].of: 1 is not the index of an earlier part (want 0..0)"},
		{box + `, {"shape": "mirror", "axis": "x", "of": -1}`, "parts[1].of: -1 is not the index of an earlier part"},
		{box + `, {"shape": "mirror", "axis": "x", "of": 0, "rotation_deg": [0, 0, 0]}`, "parts[1].rotation_deg: not used by shape mirror"},
		{box + `, {"shape": "mirror", "axis": "x", "of": 0, "uv": "box"}`, "parts[1].uv: not used by shape mirror"},
		{box + `, {"shape": "mirror", "axis": "x", "of": 0, "size": [1, 1, 1]}`, "parts[1].size: not used by shape mirror"},
		{``, "parts: at least one part is required"},
	}
	for _, c := range cases {
		es := compileErrs(t, "m.vmodel", model(c.parts))
		found := false
		for _, e := range es {
			found = found || strings.Contains(e.Msg, c.want)
		}
		if !found {
			t.Errorf("%s: errors %v, want %q", c.parts, es, c.want)
		}
	}
	// Explicit zero counts are errors, not "use the default".
	for _, part := range []string{
		`{"shape": "cylinder", "radius": 1, "height": 1, "segments": 0}`,
		`{"shape": "sphere", "radius": 1, "rings": 0}`,
	} {
		compileErrs(t, "m.vmodel", model(part))
	}
}

func TestParseFileAndName(t *testing.T) {
	src := model(`{"shape": "box", "size": [1, 1, 1]}`)
	if _, err := Parse("assets/models/crate.json", []byte(src)); err == nil || !strings.Contains(err.Error(), `must end in ".vmodel"`) {
		t.Errorf("wrong suffix: %v", err)
	}
	if _, err := Parse("Crate.vmodel", []byte(src)); err == nil || !strings.Contains(err.Error(), `model name "Crate"`) {
		t.Errorf("bad name: %v", err)
	}
	m, err := Parse(filepath.Join("assets", "models", "crate.vmodel"), []byte(`{"veduta": "model/1", "name": "crate", "parts": [{"shape": "box", "size": [1, 1, 1]}]}`))
	if err != nil || m.Name != "crate" {
		t.Fatalf("named model: %v %+v", err, m)
	}
	// Decoding errors come from asset.Decode, located.
	_, err = Parse("m.vmodel", []byte("{\n\"veduta\": \"model/1\",\n\"parts\": [{\"shape\": \"box\", \"sise\": [1, 1, 1]}]}"))
	var se *asset.SourceError
	if !errors.As(err, &se) || se.Line != 3 || !strings.Contains(se.Msg, "parts[0].sise: unknown field") {
		t.Errorf("unknown field: %v", err)
	}
}

func TestCompileWithoutLocator(t *testing.T) {
	r := float32(0.5)
	src := &asset.ModelSource{Veduta: asset.TypeModel, Parts: []asset.PartSource{{Shape: "sphere", Radius: &r}}}
	m, err := Compile("ball", src, nil)
	if err != nil || m.Name != "ball" || len(m.Mesh.Indices) != 3*224 {
		t.Fatalf("compile: %v", err)
	}
	src.Parts = append(src.Parts, asset.PartSource{Shape: "box", Radius: &r})
	_, err = Compile("ball", src, nil)
	if err == nil || !strings.Contains(err.Error(), "parts[1].size: is required") || !strings.Contains(err.Error(), "parts[1].radius: not used") {
		t.Fatalf("errors: %v", err)
	}
	if _, err := Compile("ball", nil, nil); err == nil {
		t.Fatal("nil source accepted")
	}
}

func TestDefaultsAndCounts(t *testing.T) {
	cases := []struct {
		part       string
		tris, vert int // vert < 0: not checked
		uv         string
	}{
		{`{"shape": "box", "size": [1, 1, 1]}`, 12, 24, "box"},
		{`{"shape": "plane", "size": [1, 1]}`, 2, 4, "planar"},
		{`{"shape": "cylinder", "radius": 1, "height": 1}`, 4 * 16, -1, "cylindrical"},
		{`{"shape": "cylinder", "radius": 1, "height": 1, "segments": 12}`, 4 * 12, -1, "cylindrical"},
		{`{"shape": "sphere", "radius": 1}`, 2 * 16 * 7, -1, "spherical"},
		{`{"shape": "sphere", "radius": 1, "segments": 5, "rings": 2}`, 2 * 5, -1, "spherical"},
		{`{"shape": "extrude", "profile": [[0, 0], [1, 0], [0, 1]], "depth": 1}`, 2 + 6, -1, "box"},
		{`{"shape": "lathe", "profile": [[0, 0], [0.5, 0], [0.4, 1], [0, 1.2]]}`, 16 + 32 + 16, -1, "cylindrical"},
		{`{"shape": "box", "size": [1, 1, 1], "uv": "spherical"}`, 12, -1, "spherical"},
	}
	for _, c := range cases {
		m := compile(t, model(c.part))
		if n := len(m.Mesh.Indices) / 3; n != c.tris {
			t.Errorf("%s: %d triangles, want %d", c.part, n, c.tris)
		}
		if c.vert >= 0 && len(m.Mesh.Vertices) != c.vert {
			t.Errorf("%s: %d vertices, want %d", c.part, len(m.Mesh.Vertices), c.vert)
		}
		p := m.Parts[0]
		if p.UV != c.uv || p.Of != -1 || p.Index != 0 || p.First != 0 || p.Count != len(m.Mesh.Indices) || p.Material != "" {
			t.Errorf("%s: part info %+v", c.part, p)
		}
	}
	m := compile(t, model(`{"shape": "box", "size": [1, 1, 1]}`))
	if m.SmoothAngleDeg != 30 || m.TriangleBudget != 20000 || m.Pivot != "origin" || m.Symmetry != "" ||
		len(m.Materials) != 1 || m.Materials[0] != "" || m.PivotOffset != gmath.Zero3 || m.Name != "test" {
		t.Errorf("defaults: %+v", m)
	}
	m = compile(t, `{"veduta": "model/1", "units": "m", "pivot": "center", "smooth_angle_deg": 0, "symmetry": "z",
		"triangle_budget": 500, "parts": [{"shape": "box", "size": [1, 1, 1]}]}`)
	if m.SmoothAngleDeg != 0 || m.TriangleBudget != 500 || m.Pivot != "center" || m.Symmetry != "z" {
		t.Errorf("settings: %+v", m)
	}
}

func TestMaterials(t *testing.T) {
	m := compile(t, model(`
		{"shape": "box", "size": [1, 1, 1], "material": "wood"},
		{"shape": "box", "size": [1, 1, 1]},
		{"shape": "box", "size": [1, 1, 1], "material": "steel"},
		{"shape": "box", "size": [1, 1, 1], "material": "wood"},
		{"shape": "mirror", "axis": "x", "of": 2},
		{"shape": "mirror", "axis": "x", "of": 0, "material": "gold"}`))
	if !reflect.DeepEqual(m.Materials, []string{"wood", "", "steel", "gold"}) {
		t.Fatalf("materials %q", m.Materials)
	}
	wantIdx := []int{0, 1, 2, 0, 2, 3}
	wantName := []string{"wood", "", "steel", "wood", "steel", "gold"}
	for i, p := range m.Mesh.Parts {
		if p.Material != wantIdx[i] || m.Parts[i].Material != wantName[i] {
			t.Errorf("part %d: material %d %q", i, p.Material, m.Parts[i].Material)
		}
	}
	if m.Parts[4].Shape != "mirror" || m.Parts[4].Of != 2 || m.Parts[4].UV != "box" {
		t.Errorf("mirror info %+v", m.Parts[4])
	}
}

func TestPivot(t *testing.T) {
	part := `{"shape": "box", "size": [1, 2, 1], "position": [3, 1, -2]}`
	cases := []struct {
		pivot    string
		off      gmath.Vec3
		min, max gmath.Vec3
	}{
		{"origin", gmath.V3(0, 0, 0), gmath.V3(2.5, 0, -2.5), gmath.V3(3.5, 2, -1.5)},
		{"center", gmath.V3(-3, -1, 2), gmath.V3(-0.5, -1, -0.5), gmath.V3(0.5, 1, 0.5)},
		{"bottom-center", gmath.V3(-3, 0, 2), gmath.V3(-0.5, 0, -0.5), gmath.V3(0.5, 2, 0.5)},
	}
	for _, c := range cases {
		m := compile(t, `{"veduta": "model/1", "pivot": "`+c.pivot+`", "parts": [`+part+`]}`)
		if m.PivotOffset != c.off || m.Mesh.Bounds.Min != c.min || m.Mesh.Bounds.Max != c.max {
			t.Errorf("%s: offset %v bounds %v", c.pivot, m.PivotOffset, m.Mesh.Bounds)
		}
	}
}

func TestDeterministic(t *testing.T) {
	data := readModel(t, "crate")
	a, err := Parse("crate.vmodel", data)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Parse("crate.vmodel", data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("two compilations differ")
	}
	for _, v := range a.Mesh.Vertices {
		for _, x := range []float32{v.Pos.X, v.Pos.Y, v.Pos.Z, v.Normal.X, v.Normal.Y, v.Normal.Z, v.UV.X, v.UV.Y} {
			if !gmath.IsFinite(x) {
				t.Fatalf("non-finite vertex %+v", v)
			}
		}
	}
}

// TestCrateExample compiles the model example of spec §8.1.
func TestCrateExample(t *testing.T) {
	m, err := Parse("testdata/models/crate.vmodel", readModel(t, "crate"))
	if err != nil {
		t.Fatal(err)
	}
	shapes := []string{"box", "cylinder", "sphere", "plane", "extrude", "lathe", "mirror"}
	if len(m.Parts) != len(shapes) || len(m.Mesh.Parts) != len(shapes) {
		t.Fatalf("%d parts", len(m.Parts))
	}
	for i, s := range shapes {
		if m.Parts[i].Shape != s || m.Parts[i].Index != i {
			t.Errorf("part %d: %+v", i, m.Parts[i])
		}
	}
	if m.Parts[6].Of != 1 || m.Parts[6].UV != "cylindrical" || m.Parts[6].Count != m.Parts[1].Count {
		t.Errorf("mirror part %+v", m.Parts[6])
	}
	if !reflect.DeepEqual(m.Materials, []string{"crate_wood", ""}) || m.Mesh.Parts[0].Material != 0 || m.Mesh.Parts[3].Material != 1 {
		t.Errorf("materials %q %+v", m.Materials, m.Mesh.Parts)
	}
	// bottom-center: the plane spans x, z in [-5, 5]; the sphere reaches y = -0.3.
	if m.Pivot != "bottom-center" || m.Mesh.Bounds.Min.Y != 0 || !near(m.PivotOffset.Y, 0.3, 1e-6) ||
		m.Mesh.Bounds.Min.X != -5 || m.Mesh.Bounds.Max.X != 5 || m.Mesh.Bounds.Min.Z != -5 {
		t.Errorf("pivot offset %v bounds %v", m.PivotOffset, m.Mesh.Bounds)
	}
	for i := range m.Mesh.Parts {
		checkNoDegenerate(t, tris(m, i))
	}
	for _, i := range []int{0, 1, 2, 4, 5, 6} { // every part but the plane is closed
		checkClosed(t, tris(m, i))
	}
}

// TestSamples compiles every sample model in testdata/models.
func TestSamples(t *testing.T) {
	root, err := golden.Root()
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(root, "testdata", "models", "*.vmodel"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no samples: %v", err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Parse(f, data); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
}

func readModel(t testing.TB, name string) []byte {
	t.Helper()
	root, err := golden.Root()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "testdata", "models", name+".vmodel"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func near(a, b, tol float32) bool { return gmath.Abs(a-b) <= tol }

// TestGeometryBeyondFloat32 checks that parts whose finite numbers multiply past the
// float32 range are refused rather than cooked with infinities and architecture-dependent
// NaNs.
func TestGeometryBeyondFloat32(t *testing.T) {
	src := `{"veduta": "model/1", "pivot": "center", "parts": [
		{"shape": "box", "size": [1, 1, 1]},
		{"shape": "box", "size": [3e38, 1, 1], "position": [3e38, 0, 0]}
	]}`
	es := compileErrs(t, "test.vmodel", src)
	if len(es) != 1 || !strings.Contains(es[0].Error(), "parts[1]: geometry exceeds the float32 range") {
		t.Fatalf("errors = %v", es)
	}
}

// Levels of detail: each automatic level halves the segments and rings of level 0 once
// more (never below 3 and 2), keeps the base's parts, materials and pivot offset; a level
// naming another model has no geometry.
func TestLevelsOfDetail(t *testing.T) {
	m := compile(t, `{"veduta": "model/1", "pivot": "bottom-center",
		"lod": [{"distance": 10}, {"distance": 25.5}, {"distance": 40, "model": "far"}, {"distance": 60}], "draw_distance": 90,
		"parts": [
			{"shape": "cylinder", "radius": 0.2, "height": 1, "segments": 12, "material": "bark"},
			{"shape": "sphere", "radius": 0.8, "segments": 16, "rings": 8, "position": [0, 1.2, 0], "material": "leaf"},
			{"shape": "box", "size": [1, 1, 1]},
			{"shape": "mirror", "axis": "x", "of": 0}
		]}`)
	if m.DrawDistance != 90 || len(m.LODs) != 4 {
		t.Fatalf("draw distance %v, %d levels", m.DrawDistance, len(m.LODs))
	}
	// cylinder 4·seg, sphere 2·seg·(rings−1), box 12, mirror of the cylinder.
	want := []int{2*4*12 + 2*16*7 + 12, 2*4*6 + 2*8*3 + 12, 2*4*3 + 2*4*1 + 12, 0, 2*4*3 + 2*3*1 + 12}
	for level, n := range want {
		if got := m.Triangles(level); got != n {
			t.Errorf("level %d: %d triangles, want %d", level, got, n)
		}
	}
	for i, l := range m.LODs {
		if i == 2 {
			if l.Model != "far" || l.Distance != 40 || len(l.Mesh.Vertices) != 0 || len(l.Mesh.Parts) != 0 {
				t.Errorf("level 3: %+v", l)
			}
			continue
		}
		if l.Model != "" || len(l.Mesh.Parts) != len(m.Mesh.Parts) {
			t.Fatalf("level %d: model %q, %d parts", i+1, l.Model, len(l.Mesh.Parts))
		}
		for k, p := range l.Mesh.Parts {
			if p.Material != m.Mesh.Parts[k].Material {
				t.Errorf("level %d part %d material %d, base %d", i+1, k, p.Material, m.Mesh.Parts[k].Material)
			}
		}
		// Same pivot: the reduced model still stands on y = 0 and fits the base bounds.
		if l.Mesh.Bounds.Min.Y != 0 || !m.Mesh.Bounds.ContainsBox(l.Mesh.Bounds) {
			t.Errorf("level %d bounds %v, base %v", i+1, l.Mesh.Bounds, m.Mesh.Bounds)
		}
	}
	if m.LODs[0].Distance != 10 || m.LODs[1].Distance != 25.5 || m.LODs[3].Distance != 60 {
		t.Errorf("distances %v %v %v", m.LODs[0].Distance, m.LODs[1].Distance, m.LODs[3].Distance)
	}
	if m := compile(t, model(`{"shape": "box", "size": [1, 1, 1]}`)); m.LODs != nil || m.DrawDistance != 0 {
		t.Errorf("no lod: %+v %v", m.LODs, m.DrawDistance)
	}

	src := `{
  "veduta": "model/1",
  "lod": [{"distance": 0}, {}, {"distance": 5, "model": "lod"}, {"distance": 5, "model": "Bad"}, {"distance": 7}],
  "draw_distance": 6,
  "parts": [{"shape": "box", "size": [1, 1, 1]}]
}`
	wantErrs := []string{
		`draw_distance: 6 is not farther than the last level of detail (7)`,
		`lod: 5 levels, at most 4`,
		`lod[0].distance: 0 out of range (0, 100000]`,
		`lod[1].distance: is required`,
		`lod[2].model: "lod" is this model`,
		`lod[3].distance: 5 is not farther than the level before (5)`,
		`lod[3].model: name "Bad" may only contain`,
	}
	es := compileErrs(t, "models/lod.vmodel", src)
	var got []string
	for _, e := range es {
		got = append(got, e.Msg)
	}
	sort.Strings(got)
	if len(got) != len(wantErrs) {
		t.Fatalf("errors:\n%s", strings.Join(got, "\n"))
	}
	for i, w := range wantErrs {
		if !strings.HasPrefix(got[i], w) {
			t.Errorf("error %d = %q, want %q", i, got[i], w)
		}
	}
	if es := compileErrs(t, "models/d.vmodel", model(`{"shape": "box", "size": [1, 1, 1]}`)[:len(`{"veduta": "model/1",`)]+` "draw_distance": -2, "parts": [{"shape": "box", "size": [1, 1, 1]}]}`); len(es) != 1 || !strings.Contains(es[0].Msg, "-2 out of range") {
		t.Errorf("negative draw distance: %v", es)
	}
}
