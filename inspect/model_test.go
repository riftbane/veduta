package inspect

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/asset/cook"
	"github.com/riftbane/veduta/v2/asset/model"
	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/gmath"
	"github.com/riftbane/veduta/v2/internal/golden"
	"github.com/riftbane/veduta/v2/scene"
)

// mdlTestLib loads the test game project plus fixtures from testdata/inspect/models.
func mdlTestLib(t *testing.T, fixtures ...string) *asset.Library {
	t.Helper()
	lib, err := cook.Load(filepath.Join("..", "internal", "testgame"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		p := filepath.Join("..", "testdata", "inspect", "models", f+".vmodel")
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		mdlTestAdd(t, lib, p, data)
	}
	return lib
}

// mdlTestAdd compiles a model source into lib.
func mdlTestAdd(t *testing.T, lib *asset.Library, file string, src []byte) *asset.Model {
	t.Helper()
	m, err := model.Parse(file, src)
	if err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	lib.Models[m.Name] = m
	return m
}

func mdlTestRenderer(t *testing.T, lib *asset.Library) *Renderer {
	t.Helper()
	ir, err := NewRenderer(lib)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ir.Close)
	return ir
}

func mdlTestInspect(t *testing.T, ir *Renderer, name string, opt Options) *Report {
	t.Helper()
	if opt.OutDir == "" {
		opt.OutDir = t.TempDir()
	}
	rep, err := Model(ir, name, opt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := json.Marshal(rep); err != nil {
		t.Fatalf("report does not encode: %v", err)
	}
	return rep
}

func mdlTestIssues(r *Report, code string) []Issue {
	var out []Issue
	for _, is := range r.Issues {
		if is.Code == code {
			out = append(out, is)
		}
	}
	return out
}

func mdlTestJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// mdlTestClone deep-copies a model's mesh and parts under a new name.
func mdlTestClone(m *asset.Model, name string) *asset.Model {
	c := *m
	c.Name = name
	c.Mesh.Vertices = append([]gfx.Vertex(nil), m.Mesh.Vertices...)
	c.Mesh.Indices = append([]uint32(nil), m.Mesh.Indices...)
	c.Mesh.Parts = append([]gfx.MeshPart(nil), m.Mesh.Parts...)
	c.Parts = append([]asset.PartInfo(nil), m.Parts...)
	c.Materials = append([]string(nil), m.Materials...)
	return &c
}

// mdlTestSetCount changes the index count of a part in both part lists.
func mdlTestSetCount(m *asset.Model, part, count int) {
	m.Parts[part].Count = count
	m.Mesh.Parts[part].Count = count
}

func mdlTestDecode(t *testing.T, path string) *gfx.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := gfx.DecodePNG(f)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// mdlTestRect returns the screen rectangle covered by the vertices of one part seen by
// cam in a w×h tile.
func mdlTestRect(m *asset.Model, part int, cam scene.Camera, w, h int) (x0, y0, x1, y1 int) {
	vp := cam.Proj(float32(w) / float32(h)).Mul(cam.View())
	fx0, fy0, fx1, fy1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	pi := m.Parts[part]
	for _, vi := range m.Mesh.Indices[pi.First : pi.First+pi.Count] {
		c := vp.MulVec4(m.Mesh.Vertices[vi].Pos.Vec4(1))
		x := float64((float32(c.X/c.W*0.5) + 0.5) * float32(w))
		y := float64((0.5 - float32(c.Y/c.W*0.5)) * float32(h))
		fx0, fy0, fx1, fy1 = min(fx0, x), min(fy0, y), max(fx1, x), max(fy1, y)
	}
	return int(fx0), int(fy0), int(math.Ceil(fx1)), int(math.Ceil(fy1))
}

// mdlTestHatch counts ModeNormals hatch pixels (back-facing magenta and its dark stripe)
// in a rectangle of img.
func mdlTestHatch(img *gfx.Image, x0, y0, x1, y1 int) int {
	n := 0
	for y := max(y0, 0); y < min(y1, img.H); y++ {
		for x := max(x0, 0); x < min(x1, img.W); x++ {
			if c := img.At(x, y); c == 0xffff00ff || c == 0xff200020 {
				n++
			}
		}
	}
	return n
}

func TestModelTemplateClean(t *testing.T) {
	lib := mdlTestLib(t)
	ir := mdlTestRenderer(t, lib)
	for _, name := range []string{"ground", "hero", "gem", "crate", "quad"} {
		t.Run(name, func(t *testing.T) {
			rep := mdlTestInspect(t, ir, name, Options{})
			t.Logf("%s", mdlTestJSON(t, rep))
			if rep.Summary.Errors != 0 {
				t.Errorf("unmodified test game model %s has %d error(s)", name, rep.Summary.Errors)
			}
			if rep.Subject != "model:"+name {
				t.Errorf("subject %q", rep.Subject)
			}
			if rep.Metrics["watertight"] != true {
				t.Errorf("watertight = %v, want true", rep.Metrics["watertight"])
			}
			for _, k := range []string{"triangles", "vertices", "parts", "materials", "aabb", "size", "pivot_offset",
				"surface_area", "volume", "watertight", "texel_density_cv", "symmetry_x", "part_stats"} {
				if _, ok := rep.Metrics[k]; !ok {
					t.Errorf("metric %s missing", k)
				}
			}
			if len(rep.Sheets) != 1 || !strings.HasSuffix(rep.Sheets[0], name+".summary.png") {
				t.Errorf("sheets = %v, want one summary", rep.Sheets)
			}
			if name == "hero" {
				if s := rep.Metrics["symmetry_x"].(float64); s < mdlSymMin {
					t.Errorf("hero symmetry_x = %v", s)
				}
				if rep.Has("MESH_ASYMMETRIC") {
					t.Error("hero reported asymmetric")
				}
				img := mdlTestDecode(t, rep.Sheets[0])
				if img.W > 640 {
					t.Errorf("summary is %d px wide", img.W)
				}
				golden.Image(t, "inspect_model_hero_summary", img)
			}
		})
	}
}

// TestModelFlippedNormals is acceptance criterion §15.5: a hero with one part's normals
// flipped reports MESH_FLIPPED_NORMALS with the part index, and the normals sheet shows
// the part hatched.
func TestModelFlippedNormals(t *testing.T) {
	lib := mdlTestLib(t, "hero_flipped")
	ir := mdlTestRenderer(t, lib)
	dir := t.TempDir()
	rep := mdlTestInspect(t, ir, "hero_flipped", Options{OutDir: dir, Sheets: []string{"normals", "sections"}})
	t.Logf("%s", mdlTestJSON(t, rep))
	flips := mdlTestIssues(rep, "MESH_FLIPPED_NORMALS")
	if len(flips) != 1 {
		t.Fatalf("MESH_FLIPPED_NORMALS issues = %d, want 1", len(flips))
	}
	is := flips[0]
	if is.Severity != Error || is.Where["part"] != 0 || is.Where["shape"] != "cylinder" || is.Count != 64 {
		t.Errorf("issue = %+v", is)
	}
	if want := `Part 0 (cylinder) has normals pointing inward. Set "flip_normals": false or check winding.`; is.Hint != want {
		t.Errorf("hint = %q, want %q", is.Hint, want)
	}
	if tris := is.Where["triangles"].([]int); len(tris) != mdlListMax || tris[0] != 0 {
		t.Errorf("triangles = %v", tris)
	}
	if rep.Issues[0].Code != "MESH_FLIPPED_NORMALS" {
		t.Errorf("first issue %s, want the error first", rep.Issues[0].Code)
	}
	if rep.Metrics["watertight"] != true {
		t.Error("flipped hero is still closed")
	}

	// The normals sheet hatches the flipped part: count hatch pixels inside part 0's
	// screen rectangle in the first tile, against the unmodified hero.
	w, h, _ := mdlLayout("normals", Options{})
	m := lib.Models["hero_flipped"]
	cam := mdlTiles("normals", m.Mesh.Bounds, float32(w)/float32(h))[0].cam
	x0, y0, x1, y1 := mdlTestRect(m, 0, cam, w, h)
	img := mdlTestDecode(t, filepath.Join(dir, "hero_flipped.normals.png"))
	flipped := mdlTestHatch(img, x0+mdlPad, y0+mdlPad, x1+mdlPad, y1+mdlPad)
	plain := mdlTestInspect(t, ir, "hero", Options{OutDir: dir, Sheets: []string{"normals"}})
	if plain.Has("MESH_FLIPPED_NORMALS") {
		t.Error("unmodified hero reports MESH_FLIPPED_NORMALS")
	}
	control := mdlTestHatch(mdlTestDecode(t, plain.Sheets[0]), x0+mdlPad, y0+mdlPad, x1+mdlPad, y1+mdlPad)
	t.Logf("part 0 rect [%d,%d]-[%d,%d]: hatch pixels flipped %d, unmodified %d", x0, y0, x1, y1, flipped, control)
	if flipped < 200 || control*20 > flipped {
		t.Errorf("hatch pixels in part 0: flipped %d, unmodified %d", flipped, control)
	}
	golden.Image(t, "inspect_model_hero_flipped_normals", img)
	golden.Image(t, "inspect_model_hero_flipped_sections", mdlTestDecode(t, filepath.Join(dir, "hero_flipped.sections.png")))

	// A flipped sphere is named by its own index.
	src, err := os.ReadFile(filepath.Join("..", "internal", "testgame", "assets", "models", "hero.vmodel"))
	if err != nil {
		t.Fatal(err)
	}
	s := strings.Replace(string(src), `"position": [0, 0.3, 0], "material": "hero" }`, `"position": [0, 0.3, 0], "material": "hero", "flip_normals": true }`, 1)
	s = strings.Replace(s, `"name": "hero"`, `"name": "hero_sphere"`, 1)
	if s == string(src) {
		t.Fatal("hero source changed; update the test")
	}
	mdlTestAdd(t, lib, "hero_sphere.vmodel", []byte(s))
	rep = mdlTestInspect(t, &Renderer{Lib: lib}, "hero_sphere", Options{Sheets: []string{"none"}})
	flips = mdlTestIssues(rep, "MESH_FLIPPED_NORMALS")
	if len(flips) != 1 || flips[0].Where["part"] != 2 || !strings.HasPrefix(flips[0].Hint, "Part 2 (sphere) has normals pointing inward.") {
		t.Errorf("flipped sphere: %+v", flips)
	}
}

func TestModelOpenBox(t *testing.T) {
	lib := mdlTestLib(t)
	m := mdlTestClone(lib.Models["crate"], "open_box")
	m.Mesh.Indices = m.Mesh.Indices[:30] // drop the two triangles of the -Z face
	mdlTestSetCount(m, 0, 30)
	lib.Models["open_box"] = m
	ir := mdlTestRenderer(t, lib)
	dir := t.TempDir()
	rep := mdlTestInspect(t, ir, "open_box", Options{OutDir: dir, Sheets: []string{"wireframe"}})
	t.Logf("%s", mdlTestJSON(t, rep))
	open := mdlTestIssues(rep, "MESH_OPEN_BOUNDARY")
	if len(open) != 1 {
		t.Fatalf("MESH_OPEN_BOUNDARY issues = %d", len(open))
	}
	is := open[0]
	if is.Severity != Warning || is.Count != 1 || is.Where["loops"] != 1 || is.Where["edges"] != 4 || is.Where["part"] != 0 {
		t.Errorf("issue = %+v", is)
	}
	if c := is.Where["centers"].([]gmath.Vec3)[0]; c.Z != -0.5 || c.Y != 0.5 {
		t.Errorf("hole center = %v, want the -Z face center", c)
	}
	if rep.Metrics["watertight"] != false || rep.Has("MESH_FLIPPED_NORMALS") || rep.Has("MESH_MIXED_WINDING") {
		t.Errorf("open box: watertight %v, issues %v", rep.Metrics["watertight"], rep.Issues)
	}
	img := mdlTestDecode(t, rep.Sheets[0])
	red := 0
	for _, c := range img.Pix {
		if c == mdlHoleColor {
			red++
		}
	}
	if red < 50 {
		t.Errorf("wireframe sheet has %d hole-edge pixels", red)
	}
	golden.Image(t, "inspect_model_open_box_wireframe", img)

	// A plane is open by construction: info, not warning.
	mdlTestAdd(t, lib, "floor.vmodel", []byte(`{"veduta": "model/1", "parts": [{"shape": "plane", "size": [2, 2], "material": "grass"}]}`))
	rep = mdlTestInspect(t, &Renderer{Lib: lib}, "floor", Options{Sheets: []string{"none"}})
	open = mdlTestIssues(rep, "MESH_OPEN_BOUNDARY")
	if len(open) != 1 || open[0].Severity != Info || open[0].Where["edges"] != 4 {
		t.Errorf("plane: %+v", open)
	}
}

func TestModelMixedWinding(t *testing.T) {
	lib := mdlTestLib(t)
	m := mdlTestClone(lib.Models["crate"], "mixed")
	m.Mesh.Indices[1], m.Mesh.Indices[2] = m.Mesh.Indices[2], m.Mesh.Indices[1]
	lib.Models["mixed"] = m
	rep := mdlTestInspect(t, &Renderer{Lib: lib}, "mixed", Options{Sheets: []string{"none"}})
	t.Logf("%s", mdlTestJSON(t, rep.Issues))
	mixed := mdlTestIssues(rep, "MESH_MIXED_WINDING")
	if len(mixed) != 1 {
		t.Fatalf("MESH_MIXED_WINDING issues = %d", len(mixed))
	}
	is := mixed[0]
	if is.Severity != Error || is.Count != 1 || is.Where["edges"] != 3 || len(is.Where["triangles"].([]int)) != 1 || is.Where["triangles"].([]int)[0] != 0 {
		t.Errorf("issue = %+v", is)
	}
	if rep.Has("MESH_FLIPPED_NORMALS") || rep.Has("MESH_OPEN_BOUNDARY") {
		t.Errorf("one reversed triangle is only mixed winding: %v", rep.Issues)
	}
}

func TestModelDegenerate(t *testing.T) {
	lib := mdlTestLib(t)
	m := mdlTestClone(lib.Models["crate"], "degen")
	base := uint32(len(m.Mesh.Vertices))
	for _, x := range []float32{-0.5, 0, 0.5} { // three collinear points on the bottom front edge
		v := m.Mesh.Vertices[0]
		v.Pos.X, v.Pos.Y, v.Pos.Z = x, 0, 0.5
		m.Mesh.Vertices = append(m.Mesh.Vertices, v)
	}
	m.Mesh.Indices = append(m.Mesh.Indices, 0, 0, 1, base, base+1, base+2)
	mdlTestSetCount(m, 0, len(m.Mesh.Indices))
	lib.Models["degen"] = m
	rep := mdlTestInspect(t, &Renderer{Lib: lib}, "degen", Options{Sheets: []string{"none"}})
	d := mdlTestIssues(rep, "MESH_DEGENERATE_TRIANGLE")
	if len(d) != 1 {
		t.Fatalf("MESH_DEGENERATE_TRIANGLE issues = %d (%v)", len(d), rep.Issues)
	}
	why := d[0].Where["reasons"].(map[string]int)
	if d[0].Count != 2 || why["repeated_index"] != 1 || why["zero_area"] != 1 {
		t.Errorf("issue = %+v", d[0])
	}
	if tris := d[0].Where["triangles"].([]int); len(tris) != 2 || tris[0] != 12 || tris[1] != 13 {
		t.Errorf("triangles = %v, want [12 13]", tris)
	}
	if rep.Has("MESH_OPEN_BOUNDARY") || rep.Has("MESH_NONMANIFOLD_EDGE") {
		t.Errorf("degenerate triangles must not break the topology: %v", rep.Issues)
	}
}

func TestModelBudgetScalePivot(t *testing.T) {
	lib := mdlTestLib(t, "budget", "big", "tiny", "pivot_away")
	ir := &Renderer{Lib: lib}
	opt := Options{Sheets: []string{"none"}}

	rep := mdlTestInspect(t, ir, "budget", opt)
	b := mdlTestIssues(rep, "MESH_TRIANGLE_BUDGET")
	if len(b) != 1 || b[0].Count != 972 || b[0].Where["budget"] != 500 || b[0].Where["largest_part"] != 0 ||
		!strings.Contains(b[0].Hint, `"triangle_budget"`) || !strings.Contains(b[0].Hint, `"segments"`) {
		t.Errorf("budget: %+v", b)
	}

	for _, name := range []string{"big", "tiny"} {
		rep = mdlTestInspect(t, ir, name, opt)
		s := mdlTestIssues(rep, "MESH_SCALE_SUSPICIOUS")
		if len(s) != 1 || s[0].Severity != Warning {
			t.Errorf("%s: %+v", name, rep.Issues)
			continue
		}
		t.Logf("%s: %s", name, s[0].Hint)
	}
	for _, name := range []string{"crate", "ground", "budget"} {
		if rep = mdlTestInspect(t, ir, name, opt); rep.Has("MESH_SCALE_SUSPICIOUS") || rep.Has("MESH_PIVOT_OFF") {
			t.Errorf("%s: %v", name, rep.Issues)
		}
	}

	rep = mdlTestInspect(t, ir, "pivot_away", opt)
	p := mdlTestIssues(rep, "MESH_PIVOT_OFF")
	if len(p) != 1 || p[0].Where["pivot"] != "origin" || math.Abs(p[0].Where["distance"].(float64)-2.9155) > 1e-3 ||
		!strings.Contains(p[0].Hint, `"pivot": "bottom-center"`) {
		t.Errorf("pivot: %+v", p)
	}
}

func TestModelAsymmetric(t *testing.T) {
	lib := mdlTestLib(t, "asym")
	rep := mdlTestInspect(t, &Renderer{Lib: lib}, "asym", Options{Sheets: []string{"none"}})
	a := mdlTestIssues(rep, "MESH_ASYMMETRIC")
	if len(a) != 1 {
		t.Fatalf("MESH_ASYMMETRIC issues = %d (%v)", len(a), rep.Issues)
	}
	t.Logf("%s", mdlTestJSON(t, a[0]))
	score := a[0].Where["score"].(float64)
	if a[0].Severity != Warning || score >= mdlSymMin || score != rep.Metrics["symmetry_x"] || a[0].Where["axis"] != "x" {
		t.Errorf("issue = %+v, symmetry_x %v", a[0], rep.Metrics["symmetry_x"])
	}
	if parts := a[0].Where["parts"].([]int); len(parts) != 1 || parts[0] != 1 || !strings.Contains(a[0].Hint, `"mirror"`) {
		t.Errorf("parts = %v, hint %q", parts, a[0].Hint)
	}
	// Without "symmetry" the score is still measured but not an issue.
	lib.Models["asym"].Symmetry = ""
	rep = mdlTestInspect(t, &Renderer{Lib: lib}, "asym", Options{Sheets: []string{"none"}})
	if rep.Has("MESH_ASYMMETRIC") || rep.Metrics["symmetry_x"].(float64) >= mdlSymMin {
		t.Errorf("no symmetry requested: %v, symmetry_x %v", rep.Issues, rep.Metrics["symmetry_x"])
	}
	// A requested axis adds its own metric.
	lib.Models["asym"].Symmetry = "z"
	rep = mdlTestInspect(t, &Renderer{Lib: lib}, "asym", Options{Sheets: []string{"none"}})
	if rep.Metrics["symmetry_z"] != 1.0 || rep.Has("MESH_ASYMMETRIC") {
		t.Errorf("symmetry z: %v %v", rep.Metrics["symmetry_z"], rep.Issues)
	}
}

func TestModelUVAndDensity(t *testing.T) {
	lib := mdlTestLib(t, "planar_box")
	lib.Textures["decal"] = &asset.Texture{Name: "decal", Tiling: false}
	lib.Materials["decal"] = &asset.Material{Name: "decal", Texture: "decal", Albedo: 0xffffffff, Alpha: "opaque"}
	mdlTestAdd(t, lib, "decal_box.vmodel", []byte(`{"veduta": "model/1", "pivot": "bottom-center", "parts": [{"shape": "box", "size": [2, 1, 1], "material": "decal"}]}`))
	ir := &Renderer{Lib: lib}
	opt := Options{Sheets: []string{"none"}}

	rep := mdlTestInspect(t, ir, "decal_box", opt)
	t.Logf("%s", mdlTestJSON(t, rep.Issues))
	out, over := mdlTestIssues(rep, "MESH_UV_OUT_OF_RANGE"), mdlTestIssues(rep, "MESH_UV_OVERLAP")
	if len(out) != 1 || out[0].Severity != Warning || !strings.Contains(out[0].Hint, `"tiling": true`) {
		t.Errorf("out of range with a clamped texture: %+v", out)
	}
	if len(over) != 1 || over[0].Severity != Warning {
		t.Errorf("overlap with a clamped texture: %+v", over)
	}

	rep = mdlTestInspect(t, ir, "crate", opt) // tiling texture, UVs within [0, 1]
	over = mdlTestIssues(rep, "MESH_UV_OVERLAP")
	if len(over) != 1 || over[0].Severity != Info || rep.Has("MESH_UV_OUT_OF_RANGE") {
		t.Errorf("crate: %v", rep.Issues)
	}
	rep = mdlTestInspect(t, ir, "ground", opt)
	if out = mdlTestIssues(rep, "MESH_UV_OUT_OF_RANGE"); len(out) != 1 || out[0].Severity != Info {
		t.Errorf("ground: %v", rep.Issues)
	}
	rep = mdlTestInspect(t, ir, "gem", opt) // untextured material: no UV issue
	if rep.Has("MESH_UV_OUT_OF_RANGE") || rep.Has("MESH_UV_OVERLAP") || rep.Has("MESH_TEXEL_DENSITY_UNEVEN") {
		t.Errorf("gem: %v", rep.Issues)
	}

	rep = mdlTestInspect(t, ir, "planar_box", opt)
	d := mdlTestIssues(rep, "MESH_TEXEL_DENSITY_UNEVEN")
	cv := rep.Metrics["texel_density_cv"].(float64)
	if len(d) != 1 || d[0].Severity != Warning || math.Abs(cv-math.Sqrt2) > 1e-3 || !strings.Contains(d[0].Hint, `"uv"`) {
		t.Errorf("planar box: cv %v, %+v", cv, d)
	}
	if rep = mdlTestInspect(t, ir, "crate", opt); rep.Metrics["texel_density_cv"] != 0.0 {
		t.Errorf("crate texel_density_cv = %v", rep.Metrics["texel_density_cv"])
	}
}

func TestModelDuplicateVertex(t *testing.T) {
	lib := mdlTestLib(t)
	// A part pasted twice coincides with itself.
	mdlTestAdd(t, lib, "twin.vmodel", []byte(`{"veduta": "model/1", "parts": [
		{"shape": "box", "size": [1, 1, 1], "material": "hero"},
		{"shape": "box", "size": [1, 1, 1], "material": "hero_nose"}]}`))
	rep := mdlTestInspect(t, &Renderer{Lib: lib}, "twin", Options{Sheets: []string{"none"}})
	d := mdlTestIssues(rep, "MESH_DUPLICATE_VERTEX")
	if len(d) != 1 || d[0].Severity != Warning || d[0].Count != 24 || len(d[0].Where["parts"].([]int)) != 2 {
		t.Errorf("twin: %+v", d)
	}
	m := mdlTestClone(lib.Models["crate"], "unwelded")
	m.Mesh.Vertices = append(m.Mesh.Vertices, m.Mesh.Vertices[0])
	m.Mesh.Indices[0] = uint32(len(m.Mesh.Vertices) - 1)
	lib.Models["unwelded"] = m
	rep = mdlTestInspect(t, &Renderer{Lib: lib}, "unwelded", Options{Sheets: []string{"none"}})
	if d = mdlTestIssues(rep, "MESH_DUPLICATE_VERTEX"); len(d) != 1 || d[0].Severity != Info || d[0].Count != 1 {
		t.Errorf("unwelded: %+v", d)
	}
	if rep.Has("MESH_OPEN_BOUNDARY") {
		t.Error("welding must hide the split vertex from the topology check")
	}
}

// TestModelShapes checks that well-formed sources of every shape (the examples of
// docs/model.md and corner cases of the compiler) produce no false positives.
func TestModelShapes(t *testing.T) {
	lib := mdlTestLib(t)
	srcs := map[string]string{
		"spec_crate": `{"veduta": "model/1", "pivot": "bottom-center", "parts": [
			{"shape": "box", "size": [1, 1, 1], "position": [0, 0.5, 0], "material": "crate"},
			{"shape": "cylinder", "radius": 0.1, "height": 1.2, "segments": 12, "position": [0.6, 0.6, 0]},
			{"shape": "sphere", "radius": 0.3, "segments": 16, "rings": 8},
			{"shape": "plane", "size": [10, 10]},
			{"shape": "extrude", "profile": [[0,0],[1,0],[1,2],[0,2]], "depth": 0.5},
			{"shape": "lathe", "profile": [[0,0],[0.5,0],[0.4,1],[0,1.2]], "segments": 16},
			{"shape": "mirror", "axis": "x", "of": 1}]}`,
		"robot": `{"veduta": "model/1", "pivot": "bottom-center", "symmetry": "x", "parts": [
			{"shape": "box", "size": [0.5, 0.6, 0.4], "position": [0, 0.3, 0], "material": "hero"},
			{"shape": "cylinder", "radius": 0.07, "height": 0.7, "segments": 12, "position": [0.45, 0.55, 0], "rotation_deg": [0, 0, -50], "material": "hero"},
			{"shape": "mirror", "axis": "x", "of": 1},
			{"shape": "sphere", "radius": 0.12, "segments": 12, "rings": 6, "position": [0.72, 0.8, 0.1], "material": "hero"},
			{"shape": "mirror", "axis": "x", "of": 3},
			{"shape": "lathe", "profile": [[0, 0.6], [0.18, 0.6], [0.16, 0.8], [0, 0.85]], "segments": 16, "material": "hero"}]}`,
		"corners": `{"veduta": "model/1", "smooth_angle_deg": 180, "parts": [
			{"shape": "lathe", "profile": [[1, 0], [1.5, 0], [1.5, 1], [1, 1], [1, 0]], "segments": 5},
			{"shape": "lathe", "profile": [[0, 0], [0.5, 0.25], [0, 0.5], [0.5, 0.75], [0, 1]], "segments": 7, "position": [3, 0, 0]},
			{"shape": "extrude", "profile": [[0,0],[1,0],[2,0],[2,1],[1,0.2],[0,1]], "depth": 0.01, "position": [0, 2, 0]},
			{"shape": "extrude", "profile": [[0,0],[0,1],[1,1],[1,0]], "depth": 2, "scale": [-1, 1, 0.5], "rotation_deg": [30, 45, 10]},
			{"shape": "sphere", "radius": 0.2, "segments": 3, "rings": 2, "position": [-2, 0, 0]},
			{"shape": "cylinder", "radius": 0.2, "height": 0.001, "segments": 256, "position": [-3, 0, 0]},
			{"shape": "box", "size": [1, 1, 1], "scale": [1, -2, 1], "position": [0, -3, 0]},
			{"shape": "mirror", "axis": "y", "of": 6},
			{"shape": "lathe", "profile": [[0.5, 0], [0.5, 1]], "segments": 8, "position": [5, 0, 0]}]}`,
	}
	ir := &Renderer{Lib: lib}
	for _, name := range asset.Names(srcs) {
		mdlTestAdd(t, lib, name+".vmodel", []byte(srcs[name]))
		rep := mdlTestInspect(t, ir, name, Options{Sheets: []string{"none"}})
		t.Logf("%s: %s", name, mdlTestJSON(t, rep.Issues))
		for _, is := range rep.Issues {
			switch {
			case is.Severity == Error:
				t.Errorf("%s: error %s: %s", name, is.Code, is.Hint)
			case is.Code == "MESH_OPEN_BOUNDARY" && is.Severity == Info:
			case is.Code == "MESH_UV_OVERLAP" || is.Code == "MESH_UV_OUT_OF_RANGE" || is.Code == "MESH_TEXEL_DENSITY_UNEVEN":
			default:
				t.Errorf("%s: unexpected %s %s: %s", name, is.Severity, is.Code, is.Hint)
			}
		}
	}
}

func TestModelLarge(t *testing.T) {
	if testing.Short() {
		t.Skip("large mesh")
	}
	lib := mdlTestLib(t)
	mdlTestAdd(t, lib, "large.vmodel", []byte(`{"veduta": "model/1", "symmetry": "x", "triangle_budget": 1000000, "parts": [
		{"shape": "sphere", "radius": 1, "segments": 256, "rings": 128, "material": "crate"},
		{"shape": "sphere", "radius": 1, "segments": 256, "rings": 128, "position": [3, 0, 0], "material": "crate"},
		{"shape": "mirror", "axis": "x", "of": 1}]}`))
	start := time.Now()
	rep := mdlTestInspect(t, &Renderer{Lib: lib}, "large", Options{Sheets: []string{"none"}})
	t.Logf("%d triangles analysed in %v: %s", rep.Metrics["triangles"], time.Since(start), mdlTestJSON(t, rep.Issues))
	if rep.Summary.Errors != 0 || rep.Has("MESH_ASYMMETRIC") {
		t.Errorf("large: %v", rep.Issues)
	}
}

func TestModelFocus(t *testing.T) {
	lib := mdlTestLib(t, "hero_flipped")
	ir := &Renderer{Lib: lib}
	rep := mdlTestInspect(t, ir, "hero_flipped", Options{Sheets: []string{"none"}, Focus: "MESH_FLIPPED_NORMALS"})
	if len(rep.Issues) != 1 || rep.Issues[0].Code != "MESH_FLIPPED_NORMALS" || rep.Summary.Errors != 1 {
		t.Errorf("focus: %+v", rep)
	}
	rep = mdlTestInspect(t, ir, "crate", Options{Sheets: []string{"none"}, Focus: "MESH_FLIPPED_NORMALS"})
	if len(rep.Issues) != 0 || rep.Summary.Info == 0 {
		t.Errorf("focus keeps the summary of every issue: %+v", rep)
	}
}

func TestModelSheets(t *testing.T) {
	lib := mdlTestLib(t)
	ir := mdlTestRenderer(t, lib)
	dir := t.TempDir()
	rep := mdlTestInspect(t, ir, "crate", Options{OutDir: dir, Sheets: []string{"none"}})
	if len(rep.Sheets) != 0 {
		t.Errorf("none: %v", rep.Sheets)
	}
	rep = mdlTestInspect(t, ir, "crate", Options{OutDir: dir, Sheets: []string{"sections", "normals"}})
	want := []string{filepath.Join(dir, "crate.normals.png"), filepath.Join(dir, "crate.sections.png")}
	if strings.Join(rep.Sheets, ",") != strings.Join(want, ",") {
		t.Errorf("sheets = %v, want %v", rep.Sheets, want)
	}
	if _, err := Model(ir, "crate", Options{OutDir: dir, Sheets: []string{"bogus"}}); err == nil || !strings.Contains(err.Error(), "turntable") {
		t.Errorf("unknown sheet: %v", err)
	}
	if _, err := Model(ir, "nope", Options{OutDir: dir}); err == nil || !strings.Contains(err.Error(), "crate") {
		t.Errorf("unknown model: %v", err)
	}
	rep = mdlTestInspect(t, ir, "gem", Options{OutDir: dir, Sheets: []string{"all"}})
	if len(rep.Sheets) != len(ModelSheets) {
		t.Fatalf("all: %v", rep.Sheets)
	}
	for i, p := range rep.Sheets {
		if !strings.HasSuffix(p, "gem."+ModelSheets[i]+".png") {
			t.Errorf("sheet %d = %s", i, p)
		}
		img := mdlTestDecode(t, p)
		if img.W > 640 || img.H > 360 {
			t.Errorf("%s is %dx%d", p, img.W, img.H)
		}
	}
	golden.Image(t, "inspect_model_gem_turntable", mdlTestDecode(t, rep.Sheets[1]))
	// An explicit tile size is honoured; the height follows the width at 4:3.
	rep = mdlTestInspect(t, ir, "gem", Options{OutDir: dir, Sheets: []string{"silhouette"}, Width: 100})
	if img := mdlTestDecode(t, rep.Sheets[0]); img.W != 2*100+3*mdlPad || img.H != 2*75+3*mdlPad {
		t.Errorf("silhouette with Width 100 is %dx%d", img.W, img.H)
	}
}

// TestModelLayoutSize pins the tile size of model sheets given a width alone: the width
// is clamped first and the height follows it at 4:3, so a clamped width stays 4:3.
func TestModelLayoutSize(t *testing.T) {
	for _, c := range []struct {
		opt          Options
		wantW, wantH int
	}{
		{Options{Width: 100}, 100, 75},
		{Options{Width: 317}, 317, 238},
		{Options{Width: 4000}, 1024, 768},
		{Options{Width: 200, Height: 50}, 200, 50},
		{Options{Width: 1}, 16, 16},
	} {
		if w, h, _ := mdlLayout("silhouette", c.opt); w != c.wantW || h != c.wantH {
			t.Errorf("mdlLayout(%+v) = %dx%d, want %dx%d", c.opt, w, h, c.wantW, c.wantH)
		}
	}
}

func TestModelDeterministic(t *testing.T) {
	lib := mdlTestLib(t, "hero_flipped")
	ir := mdlTestRenderer(t, lib)
	dir := t.TempDir()
	run := func() (string, [][]byte) {
		rep := mdlTestInspect(t, ir, "hero_flipped", Options{OutDir: dir, Sheets: []string{"all"}})
		var pngs [][]byte
		for _, p := range rep.Sheets {
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			pngs = append(pngs, b)
		}
		return mdlTestJSON(t, rep), pngs
	}
	j1, p1 := run()
	j2, p2 := run()
	if j1 != j2 {
		t.Errorf("reports differ:\n%s\n%s", j1, j2)
	}
	for i := range p1 {
		if !bytes.Equal(p1[i], p2[i]) {
			t.Errorf("sheet %s differs between runs", ModelSheets[i])
		}
	}
}

func TestModelEdgeCases(t *testing.T) {
	lib := asset.NewLibrary(nil)
	nan := float32(math.NaN())
	v := func(x, y, z float32) gfx.Vertex { return gfx.Vertex{Pos: gmath.V3(x, y, z), Normal: gmath.Up} }
	lib.Models["empty"] = &asset.Model{Name: "empty", Parts: []asset.PartInfo{{Shape: "box", Of: -1}}, Mesh: gfx.MeshData{Parts: []gfx.MeshPart{{}}}}
	lib.Models["nothing"] = &asset.Model{Name: "nothing"}
	lib.Models["single"] = &asset.Model{Name: "single", Mesh: gfx.MeshData{Vertices: []gfx.Vertex{v(0, 0, 0), v(1, 0, 0), v(0, 0, -1)}, Indices: []uint32{0, 1, 2}}}
	lib.Models["nan"] = &asset.Model{Name: "nan", Symmetry: "y", TriangleBudget: 1, Mesh: gfx.MeshData{
		Vertices: []gfx.Vertex{v(0, 0, 0), v(1, 0, 0), v(0, 0, -1), v(nan, 0, 0)},
		Indices:  []uint32{0, 1, 2, 0, 3, 1, 0, 1, 9, 2, 1}}}
	ir := &Renderer{Lib: lib}
	for _, name := range []string{"empty", "nothing", "single", "nan"} {
		rep := mdlTestInspect(t, ir, name, Options{Sheets: []string{"none"}})
		t.Logf("%s: %s", name, mdlTestJSON(t, rep.Issues))
	}
	rep := mdlTestInspect(t, ir, "single", Options{Sheets: []string{"none"}})
	if o := mdlTestIssues(rep, "MESH_OPEN_BOUNDARY"); len(o) != 1 || o[0].Where["edges"] != 3 || o[0].Where["shape"] != "mesh" {
		t.Errorf("single triangle: %v", rep.Issues)
	}
	rep = mdlTestInspect(t, ir, "nan", Options{Sheets: []string{"none"}})
	d := mdlTestIssues(rep, "MESH_DEGENERATE_TRIANGLE")
	if len(d) != 1 || d[0].Count != 2 || d[0].Where["reasons"].(map[string]int)["non_finite"] != 1 || d[0].Where["reasons"].(map[string]int)["index_out_of_range"] != 1 {
		t.Errorf("nan: %v", rep.Issues)
	}
	if rep.Metrics["triangles"] != 3 || !rep.Has("MESH_TRIANGLE_BUDGET") {
		t.Errorf("nan metrics: %v", rep.Metrics)
	}
	if _, err := Model(ir, "empty", Options{Sheets: []string{"summary"}}); err == nil {
		t.Error("sheets without a backend must fail")
	}
	if _, err := Model(nil, "empty", Options{}); err == nil {
		t.Error("nil renderer must fail")
	}
}

// Levels of detail: the triangles of every level, a level that saves nothing and a level
// naming a missing model.
func TestModelLevelsOfDetail(t *testing.T) {
	lib := mdlTestLib(t)
	mdlTestAdd(t, lib, "models/pine.vmodel", []byte(`{"veduta": "model/1", "pivot": "bottom-center",
		"lod": [{"distance": 10}, {"distance": 20, "model": "crate"}, {"distance": 30}, {"distance": 40, "model": "nowhere"}], "draw_distance": 50,
		"parts": [{"shape": "cylinder", "radius": 0.2, "height": 1, "segments": 8}, {"shape": "box", "size": [1, 1, 1], "position": [0, 1, 0]}]}`))
	ir := &Renderer{Lib: lib}
	rep := mdlTestInspect(t, ir, "pine", Options{Sheets: []string{"none"}})
	crate := len(lib.Models["crate"].Mesh.Indices) / 3
	if got, want := rep.Metrics["lod_triangles"], []int{44, 28, crate, 24, 0}; !reflect.DeepEqual(got, want) {
		t.Errorf("lod_triangles %v, want %v", got, want)
	}
	if rep.Metrics["draw_distance"] != float32(50) {
		t.Errorf("draw_distance %v", rep.Metrics["draw_distance"])
	}
	missing := mdlTestIssues(rep, "MESH_LOD_MODEL_MISSING")
	if len(missing) != 1 || missing[0].Severity != Error || missing[0].Where["level"] != 4 {
		t.Errorf("missing model: %v", rep.Issues)
	}
	// Level 3 (24 triangles) follows the crate level: only a gain against its own model counts.
	if gain := mdlTestIssues(rep, "MESH_LOD_NO_GAIN"); crate > 24 && len(gain) != 0 || crate <= 24 && len(gain) != 1 {
		t.Errorf("no gain with crate %d: %v", crate, rep.Issues)
	}
	mdlTestAdd(t, lib, "models/cube.vmodel", []byte(`{"veduta": "model/1", "lod": [{"distance": 10}], "parts": [{"shape": "box", "size": [1, 1, 1]}]}`))
	rep = mdlTestInspect(t, ir, "cube", Options{Sheets: []string{"none"}})
	if gain := mdlTestIssues(rep, "MESH_LOD_NO_GAIN"); len(gain) != 1 || gain[0].Where["triangles"] != 12 || gain[0].Where["previous"] != 12 {
		t.Errorf("box level: %v", rep.Issues)
	}
}
