package inspect

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/asset/cook"
	"github.com/riftbane/veduta/asset/model"
	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gmath"
	"github.com/riftbane/veduta/internal/golden"
)

// Models added to the template library: a 2×2 plane facing +Y (a decal) and a box whose
// part names a material that does not exist.
var scnFixtureModels = map[string]string{
	"decal":    `{"veduta": "model/1", "name": "decal", "parts": [{"shape": "plane", "size": [2, 2]}]}`,
	"ghostmat": `{"veduta": "model/1", "name": "ghostmat", "pivot": "bottom-center", "parts": [{"shape": "box", "size": [1, 1, 1], "position": [0, 0.5, 0], "material": "ghost"}]}`,
}

var scnFixtureMaterials = []*asset.Material{
	{Name: "painted", Albedo: 0xffffffff, Texture: "paint", Alpha: "opaque", Cutoff: 0.5},
	{Name: "flat", Albedo: 0xffe0a040, Unlit: true, Alpha: "opaque", Cutoff: 0.5},
	{Name: "twosided", Albedo: 0xffd04060, Alpha: "opaque", Cutoff: 0.5, Cull: gfx.CullNone},
}

// Crafted scenes, each exercising one check.
var scnFixtures = map[string]string{
	// A model name with a typo (two entities), an unknown entity material, a material
	// whose texture is missing, and a model whose part names an unknown material.
	"missing": `{"veduta": "scene/1", "camera": {"position": [0, 6, 8], "look_at": [0, 0, 0]}, "entities": [
		{"name": "ground", "kind": "static", "model": "ground"},
		{"name": "barrel", "kind": "static", "model": "crat", "position": [-2, 0, 0]},
		{"name": "barrel_2", "kind": "static", "model": "crat", "position": [-2, 0, 2]},
		{"name": "box", "kind": "static", "model": "crate", "material": "nope", "position": [2, 0, 0]},
		{"name": "sign", "kind": "static", "model": "decal", "material": "painted", "position": [0, 0.5, 2]},
		{"name": "odd", "kind": "static", "model": "ghostmat", "position": [0, 0, -2]}]}`,
	// A crate far outside, one whose AABB crosses the +x bound, a model-less marker
	// below the floor.
	"bounds": `{"veduta": "scene/1", "camera": {"position": [0, 9, 11], "look_at": [0, 0, -1]}, "entities": [
		{"name": "ground", "kind": "static", "model": "ground"},
		{"name": "far_crate", "kind": "static", "model": "crate", "position": [150, 0, 0]},
		{"name": "edge_crate", "kind": "static", "model": "crate", "position": [99.8, 0, 0]},
		{"name": "marker", "kind": "spawn", "position": [0, -60, 0]}]}`,
	// crate_a and crate_b interpenetrate; crate_c rests on crate_d and every crate rests
	// on the ground (touching); pushable is not static; lid is a child of crate_a.
	"overlap": `{"veduta": "scene/1", "camera": {"position": [0, 6, 7], "look_at": [-1, 0.5, 0]}, "entities": [
		{"name": "ground", "kind": "static", "model": "ground"},
		{"name": "crate_a", "kind": "static", "model": "crate"},
		{"name": "crate_b", "kind": "static", "model": "crate", "position": [0.7, 0, 0]},
		{"name": "crate_d", "kind": "static", "model": "crate", "position": [-3, 0, 0]},
		{"name": "crate_c", "kind": "static", "model": "crate", "position": [-3, 1, 0]},
		{"name": "pushable", "kind": "pickup", "model": "crate", "position": [0, 0, 0.3]},
		{"name": "lid", "kind": "static", "model": "crate", "parent": "crate_a", "position": [-0.2, 0.8, 0], "scale": [0.5, 0.5, 0.5]}]}`,
	// The camera looks up and away from everything.
	"away": `{"veduta": "scene/1", "camera": {"position": [0, 9, 11], "look_at": [0, 9, 30]}, "entities": [
		{"name": "ground", "kind": "static", "model": "ground"},
		{"name": "player", "kind": "player", "model": "hero"},
		{"name": "crate_1", "kind": "static", "model": "crate", "position": [-3, 0, -4]},
		{"name": "gem_1", "kind": "collectible", "model": "gem", "position": [0, 0.3, -3]}]}`,
	// treasure is behind the wall, beacon out of view, ghost invisible; hero is seen.
	"hidden": `{"veduta": "scene/1", "camera": {"position": [0, 3, 10], "look_at": [0, 1, 0]}, "entities": [
		{"name": "ground", "kind": "static", "model": "ground"},
		{"name": "wall", "kind": "static", "model": "crate", "position": [0, 0, 4], "scale": [8, 4, 0.4]},
		{"name": "treasure", "kind": "collectible", "model": "gem", "position": [0, 0.3, 0], "tags": ["important"]},
		{"name": "beacon", "kind": "collectible", "model": "gem", "position": [60, 0.3, 0], "tags": ["important"]},
		{"name": "ghost", "kind": "collectible", "model": "gem", "position": [1, 0.3, 6], "visible": false, "tags": ["important"]},
		{"name": "hero", "kind": "player", "model": "hero", "position": [2, 0, 6], "tags": ["important", "player"]}]}`,
	// speck is in view but far below one pixel.
	"speck": `{"veduta": "scene/1", "camera": {"position": [0, 3, 10], "look_at": [0, 1, 0]}, "entities": [
		{"name": "ground", "kind": "static", "model": "ground"},
		{"name": "speck", "kind": "collectible", "model": "gem", "position": [0, 1, 0], "scale": [0.001, 0.001, 0.001], "tags": ["important"]}]}`,
	// decal lies on the ground facing up (risk); flipped lies on it facing down but is
	// double-sided (risk); the crate rests on it back to back (no risk); raised floats
	// 1 cm above (no risk).
	"zfight": `{"veduta": "scene/1", "camera": {"position": [0, 6, 8], "look_at": [0, 0, 0]}, "entities": [
		{"name": "ground", "kind": "static", "model": "ground"},
		{"name": "decal", "kind": "static", "model": "decal", "material": "hero_nose", "position": [2, 0, 0]},
		{"name": "crate", "kind": "static", "model": "crate", "position": [-2, 0, 0]},
		{"name": "raised", "kind": "static", "model": "decal", "material": "gem", "position": [-1, 0.01, 3]},
		{"name": "flipped", "kind": "static", "model": "decal", "material": "twosided", "position": [0, 0, -2.5], "rotation_deg": [180, 0, 0]}]}`,
	"dark": `{"veduta": "scene/1", "camera": {"position": [0, 9, 11], "look_at": [0, 0, -1]},
		"light": {"direction": [-0.4, -1, -0.3], "color": "#202020", "ambient": "#101010"}, "entities": [
		{"name": "ground", "kind": "static", "model": "ground"},
		{"name": "player", "kind": "player", "model": "hero"},
		{"name": "crate_1", "kind": "static", "model": "crate", "position": [-3, 0, -4]}]}`,
	"black": `{"veduta": "scene/1", "camera": {"position": [0, 9, 11], "look_at": [0, 0, -1]},
		"light": {"direction": [-0.4, -1, -0.3], "color": "#000000", "ambient": "#000000"}, "entities": [
		{"name": "ground", "kind": "static", "model": "ground"},
		{"name": "crate_1", "kind": "static", "model": "crate", "position": [-3, 0, -4]}]}`,
	"uplight": `{"veduta": "scene/1", "camera": {"position": [0, 9, 11], "look_at": [0, 0, -1]},
		"light": {"direction": [0, 1, 0], "color": "#ffffff", "ambient": "#000000"}, "entities": [
		{"name": "ground", "kind": "static", "model": "ground"},
		{"name": "crate_1", "kind": "static", "model": "crate", "position": [-3, 0, -4]}]}`,
	"unlit": `{"veduta": "scene/1", "camera": {"position": [0, 4, 4], "look_at": [0, 0, 0]}, "entities": [
		{"name": "sticker", "kind": "static", "model": "decal", "material": "flat"}]}`,
	"empty": `{"veduta": "scene/1", "camera": {"position": [0, 4, 4], "look_at": [0, 0, 0]}, "entities": []}`,
}

var (
	scnTemplateOnce sync.Once
	scnTemplateLib  *asset.Library
	scnTemplateErr  error
)

// scnLibrary returns a copy of the template library plus the fixture models, materials
// and scenes.
func scnLibrary(t *testing.T) *asset.Library {
	t.Helper()
	scnTemplateOnce.Do(func() { scnTemplateLib, scnTemplateErr = cook.Load(filepath.Join("..", "template")) })
	if scnTemplateErr != nil {
		t.Fatal(scnTemplateErr)
	}
	base := scnTemplateLib
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
	for _, n := range asset.Names(base.Scenes) {
		lib.Scenes[n] = base.Scenes[n]
	}
	for _, n := range asset.Names(scnFixtureModels) {
		m, err := model.Parse("models/"+n+".model.json", []byte(scnFixtureModels[n]))
		if err != nil {
			t.Fatalf("fixture model %s: %v", n, err)
		}
		lib.Models[n] = m
	}
	for _, m := range scnFixtureMaterials {
		lib.Materials[m.Name] = m
	}
	for _, n := range asset.Names(scnFixtures) {
		s, err := asset.ParseScene("assets/scenes/"+n+".scene.json", []byte(scnFixtures[n]))
		if err != nil {
			t.Fatalf("fixture scene %s: %v", n, err)
		}
		lib.Scenes[n] = s
	}
	return lib
}

func scnRenderer(t *testing.T, lib *asset.Library) *Renderer {
	t.Helper()
	ir, err := NewRenderer(lib)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ir.Close)
	return ir
}

// scnInspect runs Scene with no sheets unless opt asks for some.
func scnInspect(t *testing.T, ir *Renderer, name string, opt Options) *Report {
	t.Helper()
	if opt.OutDir == "" {
		opt.OutDir = t.TempDir()
	}
	if opt.Sheets == nil {
		opt.Sheets = []string{"none"}
	}
	r, err := Scene(ir, name, opt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := json.Marshal(r); err != nil {
		t.Fatalf("report does not encode: %v", err)
	}
	if testing.Verbose() {
		b, _ := json.MarshalIndent(r, "", "  ")
		t.Logf("%s", b)
	}
	return r
}

func scnFind(r *Report, code string) []Issue {
	var out []Issue
	for _, is := range r.Issues {
		if is.Code == code {
			out = append(out, is)
		}
	}
	return out
}

func scnCodes(r *Report) []string {
	var out []string
	for _, is := range r.Issues {
		out = append(out, is.Code)
	}
	return out
}

// scnSheet loads the sheet kind written for scene name.
func scnSheet(t *testing.T, r *Report, kind string) *gfx.Image {
	t.Helper()
	for _, p := range r.Sheets {
		if strings.HasSuffix(p, "."+kind+".png") {
			img, err := LoadImage(p)
			if err != nil {
				t.Fatal(err)
			}
			return img
		}
	}
	t.Fatalf("no %s sheet in %v", kind, r.Sheets)
	return nil
}

func TestSceneTemplateMain(t *testing.T) {
	ir := scnRenderer(t, scnLibrary(t))
	r := scnInspect(t, ir, "main", Options{Sheets: []string{"summary", "ids"}})
	if r.Subject != "scene:main" {
		t.Errorf("subject %q", r.Subject)
	}
	if r.Summary.Errors != 0 || r.Summary.Warnings != 0 {
		t.Errorf("template main scene: %+v %v", r.Summary, scnCodes(r))
	}
	m := r.Metrics
	if m["entities"] != 10 || m["drawn_entities"] != 10 || m["visible_entities"] != 10 {
		t.Errorf("entities %v drawn %v visible %v", m["entities"], m["drawn_entities"], m["visible_entities"])
	}
	if tri, _ := m["triangles"].(int); tri <= 0 {
		t.Errorf("triangles %v", m["triangles"])
	}
	if br, _ := m["background_ratio"].(float64); !(br > 0 && br < 1) {
		t.Errorf("background_ratio %v", m["background_ratio"])
	}
	cam := m["camera"].(map[string]any)
	if cam["type"] != "perspective" || cam["fov_deg"] != 55.0 {
		t.Errorf("camera %v", cam)
	}
	px := m["entity_pixels"].([]scnEntityPixels)
	if len(px) != 10 || px[0].Name != "ground" || px[0].Pixels == 0 {
		t.Errorf("entity_pixels %+v", px)
	}
	for i := 1; i < len(px); i++ {
		if px[i].ID <= px[i-1].ID {
			t.Errorf("entity_pixels not sorted by id: %+v", px)
		}
	}
	if len(r.Sheets) != 2 {
		t.Fatalf("sheets %v", r.Sheets)
	}
	// Views are framed 4:3 like the console panel: two 317×238 tiles per row fill 640
	// pixels, and a single ids view is 640×480 above its legend.
	sum := scnSheet(t, r, "summary")
	if sum.W != 640 || sum.H != 2*238+3*scnPad {
		t.Errorf("summary sheet is %dx%d, want 640x%d", sum.W, sum.H, 2*238+3*scnPad)
	}
	golden.Image(t, "inspect_scene_main_summary", sum)
	ids := scnSheet(t, r, "ids")
	if ids.W != 640 || ids.H <= 480 {
		t.Errorf("ids sheet is %dx%d, want 640 wide and taller than 480", ids.W, ids.H)
	}
	golden.Image(t, "inspect_scene_main_ids", ids)
}

// TestSceneSheetSize pins the size of scene views: 4:3 by default, the height following
// the width at 4:3 when only the width is given, and both clamped.
func TestSceneSheetSize(t *testing.T) {
	for _, c := range []struct {
		opt          Options
		tile         bool
		wantW, wantH int
	}{
		{Options{}, false, 640, 480},
		{Options{}, true, 317, 238},
		{Options{Width: 320}, false, 320, 240},
		{Options{Width: 320}, true, 317, 240},
		{Options{Width: 200}, true, 200, 150},
		{Options{Width: 400, Height: 100}, false, 400, 100},
		{Options{Height: 300}, false, 640, 300},
		{Options{Height: 300}, true, 317, 300},
		{Options{Width: 10}, false, 64, 48},
		{Options{Width: 100, Height: 1}, true, 100, 48},
		{Options{Width: 4000}, false, 640, 640},
		{Options{Width: 640, Height: 5000}, false, 640, 640},
	} {
		if w, h := scnSize(c.opt, c.tile); w != c.wantW || h != c.wantH {
			t.Errorf("scnSize(%+v, tile=%v) = %dx%d, want %dx%d", c.opt, c.tile, w, h, c.wantW, c.wantH)
		}
	}
}

func TestSceneMissingAsset(t *testing.T) {
	ir := scnRenderer(t, scnLibrary(t))
	r := scnInspect(t, ir, "missing", Options{})
	is := scnFind(r, "SCENE_MISSING_ASSET")
	if len(is) != 4 {
		t.Fatalf("want 4 SCENE_MISSING_ASSET, got %v", scnCodes(r))
	}
	want := []struct {
		kind, file, field, name string
		count                   int
		hint                    string
	}{
		{"model", "assets/scenes/missing.scene.json", "entities[1].model", "crat", 2, `did you mean "crate"?`},
		{"material", "assets/scenes/missing.scene.json", "entities[3].material", "nope", 1, "entities[3].material"},
		{"texture", "assets/materials/painted.mat.json", "texture", "paint", 1, `Material "painted"`},
		{"material", "assets/models/ghostmat.model.json", "parts[0].material", "ghost", 1, "parts[0].material"},
	}
	for i, w := range want {
		got := is[i]
		if got.Severity != Error || got.Where["kind"] != w.kind || got.Where["file"] != w.file || got.Where["field"] != w.field ||
			got.Where["name"] != w.name || got.Count != w.count || !strings.Contains(got.Hint, w.hint) {
			t.Errorf("issue %d: got %+v, want %+v", i, got, w)
		}
	}
	if ents := is[0].Where["entities"].([]string); len(ents) != 2 || ents[0] != "barrel" || ents[1] != "barrel_2" {
		t.Errorf("entities %v", ents)
	}
	// Entities with a missing model are not drawn and have no AABB: nothing else fires.
	if r.Summary.Errors != 4 || r.Summary.Warnings != 0 {
		t.Errorf("summary %+v %v", r.Summary, scnCodes(r))
	}
}

func TestSceneOutsideBounds(t *testing.T) {
	ir := scnRenderer(t, scnLibrary(t))
	r := scnInspect(t, ir, "bounds", Options{Focus: "SCENE_ENTITY_OUTSIDE_BOUNDS"})
	is := r.Issues
	if len(is) != 3 {
		t.Fatalf("want 3 issues, got %+v", is)
	}
	check := func(i int, entity string, posOut bool, sides []string, by float64) {
		t.Helper()
		w := is[i].Where
		if w["entity"] != entity || w["position_outside"] != posOut || !equalStrings(w["sides"].([]string), sides) || w["outside_by"] != by {
			t.Errorf("issue %d: %+v", i, is[i])
		}
		if !strings.Contains(is[i].Hint, "position") || !strings.Contains(is[i].Hint, `"bounds" in veduta.json`) {
			t.Errorf("hint %q", is[i].Hint)
		}
	}
	check(0, "far_crate", true, []string{"+x"}, 50.5)
	check(1, "edge_crate", false, []string{"+x"}, 0.3)
	check(2, "marker", true, []string{"-y"}, 10.0)
	if !strings.Contains(is[0].Hint, "within_bounds") || strings.Contains(is[1].Hint, "within_bounds") {
		t.Errorf("within_bounds note: %q / %q", is[0].Hint, is[1].Hint)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSceneOverlap(t *testing.T) {
	ir := scnRenderer(t, scnLibrary(t))
	r := scnInspect(t, ir, "overlap", Options{Sheets: []string{"top"}})
	is := scnFind(r, "SCENE_OVERLAP")
	if len(is) != 1 {
		t.Fatalf("want 1 SCENE_OVERLAP (crate_a, crate_b), got %+v", is)
	}
	w := is[0].Where
	if w["a"] != "crate_a" || w["b"] != "crate_b" || w["overlap"] != gmath.V3(0.3, 1, 1) {
		t.Errorf("where %+v", w)
	}
	if !strings.Contains(is[0].Hint, `Move "crate_b" by +0.3 m along x (entities[2].position [0.7, 0, 0] → [1, 0, 0])`) {
		t.Errorf("hint %q", is[0].Hint)
	}
	if r.Metrics["overlap_pairs"] != 1 {
		t.Errorf("overlap_pairs %v", r.Metrics["overlap_pairs"])
	}
	golden.Image(t, "inspect_scene_overlap_top", scnSheet(t, r, "top"))
}

func TestSceneCameraSeesNothing(t *testing.T) {
	ir := scnRenderer(t, scnLibrary(t))
	r := scnInspect(t, ir, "away", Options{Sheets: []string{"top"}, Width: 320})
	is := scnFind(r, "SCENE_CAMERA_SEES_NOTHING")
	if len(is) != 1 || is[0].Severity != Error {
		t.Fatalf("want one SCENE_CAMERA_SEES_NOTHING error, got %v", scnCodes(r))
	}
	if !strings.Contains(is[0].Hint, "Set camera.look_at to") || is[0].Where["entity_pixels"] != 0 {
		t.Errorf("issue %+v", is[0])
	}
	if r.Metrics["background_ratio"] != 1.0 || r.Metrics["visible_entities"] != 0 {
		t.Errorf("metrics %v %v", r.Metrics["background_ratio"], r.Metrics["visible_entities"])
	}
	golden.Image(t, "inspect_scene_away_top", scnSheet(t, r, "top"))

	// An empty scene sees nothing either, with a hint about the entities list.
	r = scnInspect(t, ir, "empty", Options{})
	if is := scnFind(r, "SCENE_CAMERA_SEES_NOTHING"); len(is) != 1 || !strings.Contains(is[0].Hint, "no entities") {
		t.Errorf("empty scene: %+v", r.Issues)
	}
}

func TestSceneEntityOffscreen(t *testing.T) {
	ir := scnRenderer(t, scnLibrary(t))
	r := scnInspect(t, ir, "hidden", Options{Sheets: []string{"camera"}, Width: 320})
	is := scnFind(r, "SCENE_ENTITY_OFFSCREEN")
	if len(is) != 3 {
		t.Fatalf("want 3 SCENE_ENTITY_OFFSCREEN, got %+v", is)
	}
	reasons := map[string]string{}
	for _, i := range is {
		reasons[i.Where["entity"].(string)] = i.Where["reason"].(string)
	}
	if reasons["treasure"] != "occluded" || reasons["beacon"] != "outside_view" || reasons["ghost"] != "hidden" {
		t.Errorf("reasons %v", reasons)
	}
	occ := is[0].Where
	if occ["entity"] != "treasure" || occ["projected_pixels"].(int) == 0 {
		t.Errorf("treasure %+v", occ)
	}
	if list, _ := occ["occluders"].([]struct {
		Entity string `json:"entity"`
		ID     uint32 `json:"id"`
		Pixels int    `json:"pixels"`
	}); len(list) != 0 {
		_ = list
	}
	if !strings.Contains(is[0].Hint, `"wall" (entities[1])`) || !strings.Contains(is[0].Hint, "entities[1].position") {
		t.Errorf("treasure hint %q", is[0].Hint)
	}
	if !strings.Contains(is[1].Hint, "camera.look_at") || !strings.Contains(is[2].Hint, "entities[4].visible") {
		t.Errorf("hints %q / %q", is[1].Hint, is[2].Hint)
	}
	golden.Image(t, "inspect_scene_hidden_camera", scnSheet(t, r, "camera"))

	r = scnInspect(t, ir, "speck", Options{Focus: "SCENE_ENTITY_OFFSCREEN"})
	if len(r.Issues) != 1 || r.Issues[0].Where["reason"] != "no_pixels" || !strings.Contains(r.Issues[0].Hint, "entities[1].scale") {
		t.Errorf("speck: %+v", r.Issues)
	}
}

func TestSceneZFight(t *testing.T) {
	ir := scnRenderer(t, scnLibrary(t))
	r := scnInspect(t, ir, "zfight", Options{Sheets: []string{"camera"}, Width: 320})
	is := scnFind(r, "SCENE_ZFIGHT_RISK")
	if len(is) != 2 {
		t.Fatalf("want 2 SCENE_ZFIGHT_RISK (ground/decal, ground/flipped), got %+v", is)
	}
	d, f := is[0].Where, is[1].Where
	if d["a"] != "ground" || d["b"] != "decal" || d["facing"] != "same" || d["normal"] != gmath.V3(0, 1, 0) || d["area"] != 4.0 {
		t.Errorf("decal %+v", d)
	}
	if !strings.Contains(is[0].Hint, `Move "decal" at least 0.01 m along [0, 1, 0] (entities[1].position [2, 0, 0] → [2, 0.01, 0])`) {
		t.Errorf("decal hint %q", is[0].Hint)
	}
	if f["a"] != "ground" || f["b"] != "flipped" || f["facing"] != "opposite" || !strings.Contains(is[1].Hint, `"twosided"`) {
		t.Errorf("flipped %+v %q", f, is[1].Hint)
	}
	if r.Metrics["zfight_truncated"] != false {
		t.Errorf("truncated")
	}
	golden.Image(t, "inspect_scene_zfight_camera", scnSheet(t, r, "camera"))
}

func TestSceneUnlit(t *testing.T) {
	ir := scnRenderer(t, scnLibrary(t))
	for _, tc := range []struct {
		scene, severity, hint string
	}{
		{"dark", Warning, "raise light.color (now #202020) and light.ambient (now #101010)"},
		{"black", Warning, "both near black"},
		{"uplight", Warning, "light.direction [0, 1, 0] points up"},
		{"unlit", Info, `"unlit": false in assets/materials/flat.mat.json`},
	} {
		r := scnInspect(t, ir, tc.scene, Options{Focus: "SCENE_UNLIT"})
		if len(r.Issues) != 1 || r.Issues[0].Severity != tc.severity || !strings.Contains(r.Issues[0].Hint, tc.hint) {
			t.Errorf("%s: %+v", tc.scene, r.Issues)
		}
	}
	// The template scene is well lit.
	if r := scnInspect(t, ir, "main", Options{}); r.Has("SCENE_UNLIT") {
		t.Errorf("main: %+v", r.Issues)
	}
}

func TestSceneFocusAndSheets(t *testing.T) {
	ir := scnRenderer(t, scnLibrary(t))
	dir := t.TempDir()
	r, err := Scene(ir, "overlap", Options{OutDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Sheets) != 1 || r.Sheets[0] != filepath.Join(dir, "overlap.summary.png") {
		t.Errorf("default sheets %v", r.Sheets)
	}
	if _, err := os.Stat(r.Sheets[0]); err != nil {
		t.Error(err)
	}
	all := r.Summary

	r = scnInspect(t, ir, "overlap", Options{Focus: "SCENE_OVERLAP"})
	if len(r.Issues) != 1 || r.Issues[0].Code != "SCENE_OVERLAP" || r.Summary != all {
		t.Errorf("focus: %+v %+v (all %+v)", r.Issues, r.Summary, all)
	}
	if r := scnInspect(t, ir, "overlap", Options{Sheets: []string{"none"}}); len(r.Sheets) != 0 {
		t.Errorf("none: %v", r.Sheets)
	}
	r = scnInspect(t, ir, "overlap", Options{Sheets: []string{"ids", "camera"}})
	if len(r.Sheets) != 2 || !strings.HasSuffix(r.Sheets[0], "overlap.camera.png") || !strings.HasSuffix(r.Sheets[1], "overlap.ids.png") {
		t.Errorf("selection: %v", r.Sheets)
	}
	if r := scnInspect(t, ir, "overlap", Options{Sheets: []string{"all"}}); len(r.Sheets) != 4 {
		t.Errorf("all: %v", r.Sheets)
	}
	for _, opt := range []Options{{Sheets: []string{"turntable"}}, {Focus: "MESH_FLIPPED_NORMALS"}} {
		opt.OutDir = t.TempDir()
		_, err := Scene(ir, "overlap", opt)
		if err == nil || !strings.Contains(err.Error(), "valid:") {
			t.Errorf("%+v: %v", opt, err)
		}
	}
	if _, err := Scene(ir, "nosuch", Options{}); err == nil || !strings.Contains(err.Error(), "have:") {
		t.Errorf("unknown scene: %v", err)
	}
	if _, err := Scene(nil, "main", Options{}); err == nil {
		t.Error("nil renderer accepted")
	}
	if _, err := Scene(&Renderer{Lib: ir.Lib}, "main", Options{}); err == nil {
		t.Error("renderer without backend accepted")
	}
}

func TestSceneDeterministic(t *testing.T) {
	lib := scnLibrary(t)
	run := func() ([]byte, [][]byte) {
		ir := scnRenderer(t, lib)
		dir := t.TempDir()
		var reps bytes.Buffer
		var pngs [][]byte
		for _, name := range []string{"main", "zfight", "hidden", "overlap"} {
			r, err := Scene(ir, name, Options{OutDir: dir, Sheets: []string{"all"}})
			if err != nil {
				t.Fatal(err)
			}
			b, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			// Each run writes to its own directory; on Windows the path appears
			// JSON-escapes (backslashes doubled) in the report.
			esc, _ := json.Marshal(dir)
			b = bytes.ReplaceAll(b, esc[1:len(esc)-1], []byte("OUT"))
			reps.Write(bytes.ReplaceAll(b, []byte(dir), []byte("OUT")))
			for _, p := range r.Sheets {
				data, err := os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				pngs = append(pngs, data)
			}
		}
		return reps.Bytes(), pngs
	}
	r1, p1 := run()
	r2, p2 := run()
	if !bytes.Equal(r1, r2) {
		t.Error("reports differ between runs")
	}
	if len(p1) != len(p2) {
		t.Fatalf("sheet counts differ")
	}
	for i := range p1 {
		if !bytes.Equal(p1[i], p2[i]) {
			t.Errorf("sheet %d differs between runs", i)
		}
	}
}

func TestSceneEdgeCases(t *testing.T) {
	lib := scnLibrary(t)
	// A non-finite position (only possible from Go code) still yields an encodable report.
	nan := *lib.Scenes["bounds"]
	nan.Name = "nan"
	nan.Entities = append([]asset.Entity(nil), nan.Entities...)
	nan.Entities[1].Position = gmath.V3(float32(math.NaN()), 0, 0)
	lib.Scenes["nan"] = &nan
	// 400 crates on a 0.9 m grid: every neighbour pair overlaps.
	grid := &asset.Scene{Name: "grid", Camera: lib.Scenes["main"].Camera, Light: gfx.DefaultLight, Background: 0xff202830}
	for i := 0; i < 400; i++ {
		grid.Entities = append(grid.Entities, asset.Entity{Name: "crate_" + string(rune('a'+i/26)) + string(rune('a'+i%26)), Kind: "static", Model: "crate",
			Position: gmath.V3(float32(float32(i%20)*0.9)-9, 0, float32(float32(i/20)*0.9)-9), Scale: gmath.One3, Visible: true})
	}
	lib.Scenes["grid"] = grid
	ir := scnRenderer(t, lib)

	r := scnInspect(t, ir, "nan", Options{Focus: "SCENE_ENTITY_OUTSIDE_BOUNDS"})
	if len(r.Issues) == 0 || r.Issues[0].Where["entity"] != "far_crate" || !strings.Contains(r.Issues[0].Hint, "non-finite") {
		t.Errorf("nan: %+v", r.Issues)
	}

	start := time.Now()
	r = scnInspect(t, ir, "grid", Options{Focus: "SCENE_OVERLAP"})
	if d := time.Since(start); d > 20*time.Second {
		t.Errorf("400-entity scene took %v", d)
	}
	pairs := 19*20*2 + 19*19*2
	if r.Metrics["overlap_pairs"] != pairs || len(r.Issues) != scnIssueMax+1 {
		t.Fatalf("grid: %v pairs, %d issues", r.Metrics["overlap_pairs"], len(r.Issues))
	}
	last := r.Issues[scnIssueMax]
	if last.Where["omitted"] != pairs-scnIssueMax || last.Count != pairs-scnIssueMax {
		t.Errorf("omitted: %+v", last)
	}
	if m := r.Metrics; m["entity_pixels_omitted"] != 400-scnEntityMax || m["zfight_truncated"] != false {
		t.Errorf("grid metrics: omitted %v truncated %v", m["entity_pixels_omitted"], m["zfight_truncated"])
	}

	// Without a project manifest the default bounds apply.
	lib2 := scnLibrary(t)
	lib2.Project = nil
	r = scnInspect(t, scnRenderer(t, lib2), "bounds", Options{Focus: "SCENE_ENTITY_OUTSIDE_BOUNDS"})
	if len(r.Issues) != 3 {
		t.Errorf("no project: %+v", r.Issues)
	}
}
