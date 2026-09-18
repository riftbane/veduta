package asset

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/riftbane/veduta/v2/gfx"
)

// wantErr describes one expected located error: Msg must contain msg; when at is not
// empty, the error must be located at the first occurrence of at in the source.
type wantErr struct {
	msg string
	at  string
}

// posOf returns the 1-based line and column of the first occurrence of needle in src.
func posOf(t *testing.T, src, needle string) (int, int) {
	t.Helper()
	i := strings.Index(src, needle)
	if i < 0 {
		t.Fatalf("needle %q not in source", needle)
	}
	line := 1 + strings.Count(src[:i], "\n")
	col := i + 1
	if nl := strings.LastIndexByte(src[:i], '\n'); nl >= 0 {
		col = i - nl
	}
	return line, col
}

// sourceErrors flattens err into its SourceErrors.
func sourceErrors(t *testing.T, err error) Errors {
	t.Helper()
	var es Errors
	if errors.As(err, &es) {
		return es
	}
	var se *SourceError
	if errors.As(err, &se) {
		return Errors{se}
	}
	t.Fatalf("error %v (%T) is not a SourceError", err, err)
	return nil
}

// checkErrs asserts that err holds exactly len(wants) errors matching wants in order.
func checkErrs(t *testing.T, src string, err error, wants ...wantErr) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error, want %d", len(wants))
	}
	es := sourceErrors(t, err)
	if len(es) != len(wants) {
		t.Fatalf("got %d errors, want %d:\n%v", len(es), len(wants), err)
	}
	for i, w := range wants {
		e := es[i]
		if !strings.Contains(e.Msg, w.msg) {
			t.Errorf("error %d = %q, want it to contain %q", i, e.Msg, w.msg)
		}
		if w.at != "" {
			line, col := posOf(t, src, w.at)
			if e.Line != line || e.Col != col {
				t.Errorf("error %d %q at %d:%d, want %d:%d (%q)", i, e.Msg, e.Line, e.Col, line, col, w.at)
			}
		}
	}
}

// docJSON returns the ```json code blocks of docs/<name>.
func docJSON(t *testing.T, name string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "docs", name))
	if err != nil {
		t.Fatal(err)
	}
	var blocks []string
	rest := string(data)
	for {
		i := strings.Index(rest, "```json\n")
		if i < 0 {
			break
		}
		rest = rest[i+len("```json\n"):]
		j := strings.Index(rest, "\n```")
		if j < 0 {
			t.Fatalf("%s: unterminated json block", name)
		}
		blocks = append(blocks, rest[:j])
		rest = rest[j:]
	}
	if len(blocks) == 0 {
		t.Fatalf("%s has no json examples", name)
	}
	return blocks
}

func readTestdata(t *testing.T, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "testdata", "assets", rel))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseMaterialDefaults(t *testing.T) {
	m, err := ParseMaterial("assets/materials/plain.vmat", []byte(`{"veduta": "material/1"}`))
	if err != nil {
		t.Fatal(err)
	}
	want := DefaultMaterial
	want.Name = "plain"
	if *m != want {
		t.Fatalf("got %+v, want %+v", *m, want)
	}
}

func TestParseMaterialFull(t *testing.T) {
	src := `{ "veduta": "material/1", "albedo": "#80c0ff60", "texture": "leaves", "unlit": true,
	  "alpha": "cutout", "cutoff": 0.25, "cull": "none", "filter": "nearest" }`
	m, err := ParseMaterial("leaf.vmat", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	want := Material{Name: "leaf", Albedo: 0x6080c0ff, Texture: "leaves", Unlit: true, Alpha: "cutout",
		Cutoff: 0.25, Cull: gfx.CullNone, Filter: gfx.FilterNearest}
	if *m != want {
		t.Fatalf("got %+v, want %+v", *m, want)
	}
	if m.AlphaCutoff() != 0.25 || m.State().Cull != gfx.CullNone {
		t.Fatal("pipeline helpers disagree with the compiled material")
	}
	blend, err := ParseMaterial("glass.vmat", []byte(`{"veduta": "material/1", "alpha": "blend"}`))
	if err != nil {
		t.Fatal(err)
	}
	if blend.State() != gfx.StateTransparent || blend.AlphaCutoff() != 0 {
		t.Fatalf("blend state %+v", blend.State())
	}
}

func TestParseMaterialErrors(t *testing.T) {
	cases := []struct {
		name, file, src string
		wants           []wantErr
	}{
		{"bad alpha", "m.vmat", `{"veduta": "material/1", "alpha": "glass"}`,
			[]wantErr{{`alpha: unknown value "glass" (want one of [opaque blend cutout])`, `"glass"`}}},
		{"cutoff without cutout", "m.vmat", `{"veduta": "material/1", "alpha": "blend", "cutoff": 0.3}`,
			[]wantErr{{`cutoff: only allowed when alpha is "cutout"`, `0.3`}}},
		{"cutoff without alpha", "m.vmat", `{"veduta": "material/1", "cutoff": 0.3}`,
			[]wantErr{{`cutoff: only allowed`, `0.3`}}},
		{"cutoff zero", "m.vmat", `{"veduta": "material/1", "alpha": "cutout", "cutoff": 0}`,
			[]wantErr{{`cutoff: 0 out of range (0, 1]`, `0}`}}},
		{"cutoff above one", "m.vmat", `{"veduta": "material/1", "alpha": "cutout", "cutoff": 1.5}`,
			[]wantErr{{`out of range (0, 1]`, `1.5`}}},
		{"bad albedo", "m.vmat", `{"veduta": "material/1", "albedo": "#fff"}`,
			[]wantErr{{`albedo: color "#fff": want #RRGGBB or #RRGGBBAA`, `"#fff"`}}},
		{"bad texture name", "m.vmat", `{"veduta": "material/1", "texture": "Wood.png"}`,
			[]wantErr{{`texture: name "Wood.png"`, `"Wood.png"`}}},
		{"bad cull", "m.vmat", `{"veduta": "material/1", "cull": "front"}`,
			[]wantErr{{`cull: unknown value "front"`, `"front"`}}},
		{"grid without texture", "m.vmat", `{"veduta": "material/1", "grid": [4, 2]}`,
			[]wantErr{{`grid: needs a texture`, `[4, 2]`}}},
		{"grid of three", "m.vmat", `{"veduta": "material/1", "texture": "t", "grid": [4, 2, 1]}`,
			[]wantErr{{`grid: must be [columns, rows]`, `[4, 2, 1]`}}},
		{"grid too big", "m.vmat", `{"veduta": "material/1", "texture": "t", "grid": [4, 300]}`,
			[]wantErr{{`grid[1]: 300 out of range [1, 256]`, `300`}}},
		{"bad filter", "m.vmat", `{"veduta": "material/1", "filter": "trilinear"}`,
			[]wantErr{{`filter: unknown value "trilinear"`, `"trilinear"`}}},
		{"unknown field", "m.vmat", `{"veduta": "material/1", "colour": "#ffffff"}`,
			[]wantErr{{`colour: unknown field`, `"colour"`}}},
		{"wrong header", "m.vmat", `{"veduta": "texture/1"}`,
			[]wantErr{{`want "material/1"`, `"texture/1"`}}},
		{"bad file name", "Crate.vmat", `{"veduta": "material/1"}`,
			[]wantErr{{`file name: material name "Crate"`, ""}}},
		{"wrong suffix", "crate.json", `{"veduta": "material/1"}`,
			[]wantErr{{`must end in ".vmat"`, ""}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseMaterial(c.file, []byte(c.src))
			checkErrs(t, c.src, err, c.wants...)
		})
	}
}

// Every problem in a file is reported, each at its own line and column.
func TestParseMaterialReportsAll(t *testing.T) {
	src := "{\n  \"veduta\": \"material/1\",\n  \"albedo\": \"red\",\n  \"alpha\": \"glass\",\n  \"cutoff\": 2,\n  \"cull\": \"front\",\n  \"texture\": \"-x\"\n}"
	_, err := ParseMaterial("m.vmat", []byte(src))
	es := sourceErrors(t, err)
	want := []struct {
		line, col int
		msg       string
	}{
		{3, 13, "albedo:"},
		{4, 12, "alpha:"},
		{7, 14, "texture:"},
		{5, 13, "cutoff: only allowed"},
		{6, 11, "cull:"},
	}
	if len(es) != len(want) {
		t.Fatalf("got %d errors, want %d:\n%v", len(es), len(want), err)
	}
	for i, w := range want {
		if es[i].Line != w.line || es[i].Col != w.col || !strings.Contains(es[i].Msg, w.msg) || es[i].File != "m.vmat" {
			t.Errorf("error %d = %v, want %d:%d %q", i, es[i], w.line, w.col, w.msg)
		}
	}
}

func TestCompileMaterialWithoutLocator(t *testing.T) {
	m, err := CompileMaterial("gem", &MaterialSource{Albedo: "#ff0000", Alpha: "cutout", Cutoff: ptr(float32(1))}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.Albedo != 0xffff0000 || m.Cutoff != 1 {
		t.Fatalf("got %+v", m)
	}
	_, err = CompileMaterial("Bad Name", &MaterialSource{Cull: "x"}, nil)
	es := sourceErrors(t, err)
	if len(es) != 2 || es[0].Line != 1 || es[0].Col != 1 {
		t.Fatalf("errors: %v", err)
	}
}

func TestMaterialSamples(t *testing.T) {
	m, err := ParseMaterial("materials/crate_wood.vmat", readTestdata(t, "materials/crate_wood.vmat"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "crate_wood" || m.Texture != "crate_wood" {
		t.Fatalf("got %+v", m)
	}
	for i, ex := range docJSON(t, "material.md") {
		if _, err := ParseMaterial("example.vmat", []byte(ex)); err != nil {
			t.Errorf("docs/material.md example %d: %v", i, err)
		}
	}
}

// Compiling the same source twice gives equal results.
func TestMaterialDeterministic(t *testing.T) {
	data := readTestdata(t, "materials/crate_wood.vmat")
	a, _ := ParseMaterial("crate_wood.vmat", data)
	b, _ := ParseMaterial("crate_wood.vmat", data)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("material compilation is not deterministic")
	}
}
