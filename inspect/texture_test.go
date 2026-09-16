package inspect

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/asset/cook"
	"github.com/riftbane/veduta/v2/asset/texture"
	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/internal/golden"
)

// Crafted texture sources, each exercising one check.
var texFixtures = map[string]string{
	// A left-to-right gradient declared tiling: the left and right edges never match.
	"gradient": `{"veduta": "texture/1", "size": [64, 64], "tiling": true, "layers": [
		{"type": "gradient", "from": "#000000", "to": "#ffffff", "angle_deg": 0}]}`,
	// Vertical stripes whose period divides the width: the edge on the border is a stripe
	// edge like the others, not a seam.
	"stripes": `{"veduta": "texture/1", "size": [64, 64], "tiling": true, "layers": [
		{"type": "stripes", "width": 8, "angle_deg": 0, "colors": ["#202020", "#e0e0e0"]}]}`,
	// Layer 0 is painted over by the opaque solid of layer 1.
	"covered": `{"veduta": "texture/1", "size": [32, 32], "layers": [
		{"type": "checker", "cells": 4, "colors": ["#ff0000", "#0000ff"]},
		{"type": "solid", "color": "#808080"},
		{"type": "stripes", "width": 4, "angle_deg": 90, "colors": ["#00000000", "#00000040"]}]}`,
	// Layer 2 has opacity 0.
	"invisible": `{"veduta": "texture/1", "size": [32, 32], "layers": [
		{"type": "solid", "color": "#406080"},
		{"type": "stripes", "width": 4, "angle_deg": 45, "colors": ["#ffffff", "#000000"], "opacity": 0.5},
		{"type": "circle", "center": [16, 16], "radius": 8, "color": "#ff0000", "opacity": 0}]}`,
	"flat": `{"veduta": "texture/1", "size": [32, 32], "layers": [{"type": "solid", "color": "#808080"}]}`,
	"npot": `{"veduta": "texture/1", "size": [100, 60], "layers": [
		{"type": "solid", "color": "#336699"},
		{"type": "noise", "seed": 1, "scale": 4, "color": "#000000", "opacity": 0.5}]}`,
	"npot_nomip": `{"veduta": "texture/1", "size": [100, 60], "mipmaps": false, "layers": [
		{"type": "solid", "color": "#336699"},
		{"type": "noise", "seed": 1, "scale": 4, "color": "#000000", "opacity": 0.5}]}`,
	// A checker of 1-texel cells: mip 1 is already uniform gray.
	"checker1": `{"veduta": "texture/1", "size": [64, 64], "layers": [
		{"type": "solid", "color": "#606060"},
		{"type": "checker", "cells": 64, "colors": ["#000000", "#ffffff"]}]}`,
	// A leaf on a transparent background, used by an opaque material.
	"leaf": `{"veduta": "texture/1", "size": [32, 32], "layers": [
		{"type": "circle", "center": [16, 16], "radius": 12, "color": "#3a8a2a"},
		{"type": "circle", "center": [13, 12], "radius": 5, "color": "#8ccf5a"}]}`,
	// An opaque texture used by blend and cutout materials.
	"tile": `{"veduta": "texture/1", "size": [32, 32], "tiling": true, "layers": [
		{"type": "solid", "color": "#c0b090"},
		{"type": "rect", "xy": [2, 2], "size": [28, 28], "color": "#00000060", "outline": 2}]}`,
	"one": `{"veduta": "texture/1", "size": [1, 1], "tiling": true, "layers": [{"type": "solid", "color": "#ff8000"}]}`,
	"line": `{"veduta": "texture/1", "size": [1, 64], "tiling": true, "layers": [
		{"type": "gradient", "from": "#000000", "to": "#ffffff", "angle_deg": 90}]}`,
}

// Materials added to the test game library for the alpha checks.
var texFixtureMaterials = []*asset.Material{
	{Name: "leaf_opaque", Albedo: 0xffffffff, Texture: "leaf", Alpha: "opaque", Cutoff: 0.5},
	{Name: "tile_blend", Albedo: 0xffffffff, Texture: "tile", Alpha: "blend", Cutoff: 0.5},
	{Name: "tile_cut", Albedo: 0xffffffff, Texture: "tile", Alpha: "cutout", Cutoff: 0.5},
	{Name: "tile_tinted", Albedo: 0x80ffffff, Texture: "tile", Alpha: "blend", Cutoff: 0.5},
}

var (
	texTemplateOnce sync.Once
	texTemplateLib  *asset.Library
	texTemplateErr  error
)

// texTestRenderer returns a renderer over the test game library plus the crafted
// textures and materials, and the source of every texture.
func texTestRenderer(t *testing.T) (*Renderer, map[string]*TexSource) {
	t.Helper()
	texTemplateOnce.Do(func() { texTemplateLib, texTemplateErr = cook.Load(filepath.Join("..", "internal", "testgame")) })
	if texTemplateErr != nil {
		t.Fatal(texTemplateErr)
	}
	base := texTemplateLib
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
	srcs := map[string]*TexSource{}
	assets := filepath.Join("..", "internal", "testgame", "assets")
	for _, n := range []string{"grass", "crate"} {
		file := filepath.Join(assets, "textures", n+".tex.json")
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		srcs[n] = &TexSource{File: file, Data: data, FS: os.DirFS(assets)}
	}
	for _, n := range asset.Names(texFixtures) {
		file := "textures/" + n + ".tex.json"
		data := []byte(texFixtures[n])
		tex, err := texture.Parse(file, data, texture.Options{})
		if err != nil {
			t.Fatalf("fixture %s: %v", n, err)
		}
		lib.Textures[n] = tex
		srcs[n] = &TexSource{File: file, Data: data}
	}
	for _, m := range texFixtureMaterials {
		lib.Materials[m.Name] = m
	}
	ir, err := NewRenderer(lib)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ir.Close)
	return ir, srcs
}

// texInspect runs Texture with sheets disabled unless opt asks for some.
func texInspect(t *testing.T, ir *Renderer, name string, src *TexSource, opt Options) *Report {
	t.Helper()
	if opt.OutDir == "" {
		opt.OutDir = t.TempDir()
	}
	if opt.Sheets == nil {
		opt.Sheets = []string{"none"}
	}
	r, err := Texture(ir, name, src, opt)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func texIssues(r *Report, code string) []Issue {
	var out []Issue
	for _, is := range r.Issues {
		if is.Code == code {
			out = append(out, is)
		}
	}
	return out
}

func texJSON(t *testing.T, r *Report) string {
	t.Helper()
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func texReadSheet(t *testing.T, path string) *gfx.Image {
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

func TestTextureTemplate(t *testing.T) {
	ir, srcs := texTestRenderer(t)
	for _, name := range []string{"grass", "crate"} {
		r := texInspect(t, ir, name, srcs[name], Options{})
		if r.Subject != "texture:"+name {
			t.Errorf("%s: subject %q", name, r.Subject)
		}
		if r.Summary.Errors != 0 || r.Summary.Warnings != 0 {
			t.Errorf("%s: want a clean report, got\n%s", name, texJSON(t, r))
		}
		if r.Has("TEX_LAYERS_NOT_CHECKED") {
			t.Errorf("%s: layer analysis did not run:\n%s", name, texJSON(t, r))
		}
		for _, k := range []string{"size", "levels", "tiling", "wrap", "layers", "mean_color", "luminance_mean",
			"luminance_std", "alpha_min", "alpha_max", "seam_delta_lr", "seam_delta_tb", "inside_delta", "mip2_contrast_ratio"} {
			if _, ok := r.Metrics[k]; !ok {
				t.Errorf("%s: metric %s missing", name, k)
			}
		}
		if r.Metrics["size"] != [2]int{64, 64} || r.Metrics["levels"] != 7 || r.Metrics["tiling"] != true || r.Metrics["wrap"] != "repeat" {
			t.Errorf("%s: metrics %v", name, r.Metrics)
		}
		if got := r.Metrics["materials"].([]string); len(got) != 1 || got[0] != name {
			t.Errorf("%s: materials %v", name, got)
		}
		t.Logf("%s:\n%s", name, texJSON(t, r))
	}
}

func TestTextureSummaryGolden(t *testing.T) {
	ir, srcs := texTestRenderer(t)
	r, err := Texture(ir, "grass", srcs["grass"], Options{OutDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Sheets) != 1 || filepath.Base(r.Sheets[0]) != "grass.summary.png" {
		t.Fatalf("default sheets %v, want one summary", r.Sheets)
	}
	img := texReadSheet(t, r.Sheets[0])
	if img.W > 640 {
		t.Errorf("summary is %d px wide", img.W)
	}
	golden.Image(t, "inspect_texture_grass_summary", img)
}

func TestTextureSeam(t *testing.T) {
	ir, srcs := texTestRenderer(t)
	opt := Options{OutDir: t.TempDir(), Sheets: []string{"tiled_2x2"}}
	r := texInspect(t, ir, "gradient", srcs["gradient"], opt)
	seams := texIssues(r, "TEX_SEAM")
	if len(seams) != 1 {
		t.Fatalf("want one TEX_SEAM, got\n%s", texJSON(t, r))
	}
	s := seams[0]
	if s.Severity != Warning || s.Where["edge"] != "left-right" || s.Where["layer"] != 0 || s.Where["type"] != "gradient" || s.Count != 64 {
		t.Errorf("seam issue %+v", s)
	}
	if md := s.Where["mean_delta"].(float64); md < 200 {
		t.Errorf("mean_delta %v", md)
	}
	if !strings.Contains(s.Hint, `"tiling": false`) || !strings.Contains(s.Hint, "layers[0]") {
		t.Errorf("hint %q", s.Hint)
	}
	golden.Image(t, "inspect_texture_gradient_tiled", texReadSheet(t, r.Sheets[0]))

	// Without the source the seam is still found, just not traced to a layer.
	r = texInspect(t, ir, "gradient", nil, Options{})
	if s := texIssues(r, "TEX_SEAM"); len(s) != 1 || s[0].Where["layer"] != nil {
		t.Errorf("no source: %s", texJSON(t, r))
	}
}

func TestTextureSeamFalsePositives(t *testing.T) {
	ir, srcs := texTestRenderer(t)
	for _, name := range []string{"stripes", "grass", "crate", "tile", "one"} {
		r := texInspect(t, ir, name, srcs[name], Options{})
		if r.Has("TEX_SEAM") {
			t.Errorf("%s: unexpected seam:\n%s", name, texJSON(t, r))
		}
	}
	// A non-tiling gradient is never a seam.
	r := texInspect(t, ir, "npot", srcs["npot"], Options{})
	if r.Has("TEX_SEAM") {
		t.Errorf("non-tiling texture reported a seam")
	}
	// A 1×64 vertical gradient that tiles: seam top-bottom only.
	r = texInspect(t, ir, "line", srcs["line"], Options{})
	if s := texIssues(r, "TEX_SEAM"); len(s) != 1 || s[0].Where["edge"] != "top-bottom" {
		t.Errorf("line: %s", texJSON(t, r))
	}
}

func TestTextureLayerNoEffect(t *testing.T) {
	ir, srcs := texTestRenderer(t)
	r := texInspect(t, ir, "covered", srcs["covered"], Options{})
	ne := texIssues(r, "TEX_LAYER_NO_EFFECT")
	if len(ne) != 1 || ne[0].Where["layer"] != 0 || ne[0].Where["type"] != "checker" || ne[0].Severity != Warning {
		t.Fatalf("covered: %s", texJSON(t, r))
	}
	if !strings.Contains(ne[0].Hint, "layer 1 (solid)") || !strings.Contains(ne[0].Hint, "layers[0]") {
		t.Errorf("hint %q", ne[0].Hint)
	}

	r = texInspect(t, ir, "invisible", srcs["invisible"], Options{})
	ne = texIssues(r, "TEX_LAYER_NO_EFFECT")
	if len(ne) != 1 || ne[0].Where["layer"] != 2 || ne[0].Where["type"] != "circle" || !strings.Contains(ne[0].Hint, "layers[2].opacity") {
		t.Fatalf("invisible: %s", texJSON(t, r))
	}
	for _, name := range []string{"grass", "crate", "leaf", "npot"} {
		if r := texInspect(t, ir, name, srcs[name], Options{}); r.Has("TEX_LAYER_NO_EFFECT") {
			t.Errorf("%s: unexpected TEX_LAYER_NO_EFFECT:\n%s", name, texJSON(t, r))
		}
	}
}

func TestTextureLowContrast(t *testing.T) {
	ir, srcs := texTestRenderer(t)
	r := texInspect(t, ir, "flat", srcs["flat"], Options{})
	lc := texIssues(r, "TEX_LOW_CONTRAST")
	if len(lc) != 1 || lc[0].Where["luminance_std"] != 0.0 || lc[0].Where["luminance_range"] != 0 {
		t.Fatalf("flat: %s", texJSON(t, r))
	}
	if r.Has("TEX_MIP_ILLEGIBLE") || r.Has("TEX_SEAM") {
		t.Errorf("flat texture reported more than low contrast:\n%s", texJSON(t, r))
	}
	if r.Metrics["mip2_contrast_ratio"] != nil {
		t.Errorf("mip2_contrast_ratio of a flat texture = %v, want null", r.Metrics["mip2_contrast_ratio"])
	}
	// A leaf: flat-ish color but the alpha carries the shape.
	if r := texInspect(t, ir, "leaf", srcs["leaf"], Options{}); r.Has("TEX_LOW_CONTRAST") {
		t.Errorf("leaf reported low contrast:\n%s", texJSON(t, r))
	}
}

func TestTextureNotPowerOfTwo(t *testing.T) {
	ir, srcs := texTestRenderer(t)
	r := texInspect(t, ir, "npot", srcs["npot"], Options{})
	p := texIssues(r, "TEX_NOT_POWER_OF_TWO")
	if len(p) != 1 || p[0].Severity != Warning || p[0].Count != 2 || p[0].Where["suggested"] != [2]int{128, 64} {
		t.Fatalf("npot: %s", texJSON(t, r))
	}
	if !strings.Contains(p[0].Hint, `"size" to [128, 64]`) {
		t.Errorf("hint %q", p[0].Hint)
	}
	r = texInspect(t, ir, "npot_nomip", srcs["npot_nomip"], Options{})
	if p := texIssues(r, "TEX_NOT_POWER_OF_TWO"); len(p) != 1 || p[0].Severity != Info {
		t.Fatalf("npot_nomip: %s", texJSON(t, r))
	}
	if r.Metrics["levels"] != 1 || r.Metrics["mip2_contrast_ratio"] != nil {
		t.Errorf("npot_nomip metrics %v", r.Metrics)
	}
}

func TestTextureMipIllegible(t *testing.T) {
	ir, srcs := texTestRenderer(t)
	opt := Options{OutDir: t.TempDir(), Sheets: []string{"mips"}}
	r := texInspect(t, ir, "checker1", srcs["checker1"], opt)
	mi := texIssues(r, "TEX_MIP_ILLEGIBLE")
	if len(mi) != 1 || mi[0].Where["layer"] != 1 || mi[0].Where["type"] != "checker" || mi[0].Where["ratio"].(float64) > 0.05 {
		t.Fatalf("checker1: %s", texJSON(t, r))
	}
	if !strings.Contains(mi[0].Hint, "layers[1].cells to 8 or fewer") {
		t.Errorf("hint %q", mi[0].Hint)
	}
	img := texReadSheet(t, r.Sheets[0])
	if img.W > 640 {
		t.Errorf("mips sheet is %d px wide", img.W)
	}
	golden.Image(t, "inspect_texture_checker_mips", img)
	for _, name := range []string{"grass", "crate", "gradient", "npot"} {
		if r := texInspect(t, ir, name, srcs[name], Options{}); r.Has("TEX_MIP_ILLEGIBLE") {
			t.Errorf("%s: unexpected TEX_MIP_ILLEGIBLE:\n%s", name, texJSON(t, r))
		}
	}
}

func TestTextureAlphaUnused(t *testing.T) {
	ir, srcs := texTestRenderer(t)
	opt := Options{OutDir: t.TempDir(), Sheets: []string{"channels"}}
	r := texInspect(t, ir, "leaf", srcs["leaf"], opt)
	au := texIssues(r, "TEX_ALPHA_UNUSED")
	if len(au) != 1 || au[0].Severity != Warning || au[0].Count == 0 || !strings.Contains(au[0].Hint, "materials/leaf_opaque.mat.json") {
		t.Fatalf("leaf: %s", texJSON(t, r))
	}
	if au[0].Count != r.Metrics["nonopaque_texels"] || r.Metrics["alpha_min"] != 0 {
		t.Errorf("leaf count %d, metrics %v", au[0].Count, r.Metrics)
	}
	golden.Image(t, "inspect_texture_leaf_channels", texReadSheet(t, r.Sheets[0]))

	r = texInspect(t, ir, "tile", srcs["tile"], Options{})
	au = texIssues(r, "TEX_ALPHA_UNUSED")
	if len(au) != 1 || au[0].Severity != Info || au[0].Count != 2 {
		t.Fatalf("tile: %s", texJSON(t, r))
	}
	if got := au[0].Where["materials"].([]string); strings.Join(got, ",") != "tile_blend,tile_cut" {
		t.Errorf("tile materials %v (tile_tinted brings alpha through its albedo)", got)
	}
}

func TestTextureNoSource(t *testing.T) {
	ir, _ := texTestRenderer(t)
	r := texInspect(t, ir, "covered", nil, Options{})
	nc := texIssues(r, "TEX_LAYERS_NOT_CHECKED")
	if len(nc) != 1 || nc[0].Severity != Info || nc[0].Count != 3 || r.Has("TEX_LAYER_NO_EFFECT") {
		t.Fatalf("no source: %s", texJSON(t, r))
	}
}

func TestTextureFocus(t *testing.T) {
	ir, srcs := texTestRenderer(t)
	all := texInspect(t, ir, "gradient", srcs["gradient"], Options{})
	r := texInspect(t, ir, "gradient", srcs["gradient"], Options{Focus: "TEX_SEAM"})
	if len(r.Issues) != 1 || r.Issues[0].Code != "TEX_SEAM" {
		t.Fatalf("focus kept %v", r.Issues)
	}
	if r.Summary != all.Summary {
		t.Errorf("focus summary %+v, want %+v", r.Summary, all.Summary)
	}
	r = texInspect(t, ir, "grass", srcs["grass"], Options{Focus: "TEX_SEAM"})
	if len(r.Issues) != 0 || r.Issues == nil {
		t.Errorf("grass focus: %v", r.Issues)
	}
}

func TestTextureSheets(t *testing.T) {
	ir, srcs := texTestRenderer(t)
	dir := t.TempDir()
	base := func(paths []string) string {
		var b []string
		for _, p := range paths {
			if _, err := os.Stat(p); err != nil {
				t.Errorf("sheet %s: %v", p, err)
			}
			b = append(b, filepath.Base(p))
		}
		return strings.Join(b, " ")
	}
	cases := []struct {
		sheets []string
		want   string
	}{
		{nil, "crate.summary.png"},
		{[]string{"none"}, ""},
		{[]string{}, ""},
		{[]string{"on_model:crate", "mips", "single", "on_model:crate"}, "crate.single.png crate.mips.png crate.on_model_crate.png"},
		{[]string{"all"}, "crate.summary.png crate.single.png crate.tiled_2x2.png crate.channels.png crate.mips.png"},
	}
	for _, c := range cases {
		r, err := Texture(ir, "crate", srcs["crate"], Options{OutDir: dir, Sheets: c.sheets})
		if err != nil {
			t.Fatalf("%v: %v", c.sheets, err)
		}
		if got := base(r.Sheets); got != c.want {
			t.Errorf("sheets %v: got %q, want %q", c.sheets, got, c.want)
		}
		for _, p := range r.Sheets {
			img := texReadSheet(t, p)
			if img.W > 640 {
				t.Errorf("%s is %d px wide", p, img.W)
			}
			if strings.HasSuffix(p, "on_model_crate.png") {
				golden.Image(t, "inspect_texture_crate_on_model", img)
			}
		}
	}
	for _, bad := range [][]string{{"bogus"}, {"on_model:"}, {"on_model:nope"}, {"single", "turntable"}} {
		_, err := Texture(ir, "crate", srcs["crate"], Options{OutDir: dir, Sheets: bad})
		if err == nil {
			t.Errorf("sheets %v: no error", bad)
			continue
		}
		if !strings.Contains(err.Error(), "single") && !strings.Contains(err.Error(), "have: crate, ") {
			t.Errorf("sheets %v: error does not list the valid values: %v", bad, err)
		}
	}
	// A non-tiling texture's summary has no tiled tile but tiled_2x2 can be asked for.
	r, err := Texture(ir, "npot", srcs["npot"], Options{OutDir: dir, Sheets: []string{"summary", "tiled_2x2"}})
	if err != nil || len(r.Sheets) != 2 {
		t.Fatalf("npot sheets %v: %v", r.Sheets, err)
	}
}

func TestTextureDeterministic(t *testing.T) {
	ir, srcs := texTestRenderer(t)
	dir := t.TempDir()
	sheets := []string{"all", "on_model:crate"}
	run := func() (string, [][]byte) {
		r, err := Texture(ir, "crate", srcs["crate"], Options{OutDir: dir, Sheets: sheets})
		if err != nil {
			t.Fatal(err)
		}
		var pngs [][]byte
		for _, p := range r.Sheets {
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			pngs = append(pngs, b)
		}
		return texJSON(t, r), pngs
	}
	j1, p1 := run()
	j2, p2 := run()
	if j1 != j2 {
		t.Errorf("reports differ:\n%s\n%s", j1, j2)
	}
	for i := range p1 {
		if !bytes.Equal(p1[i], p2[i]) {
			t.Errorf("sheet %d differs between runs", i)
		}
	}
}

func TestTextureErrorsAndEdges(t *testing.T) {
	ir, srcs := texTestRenderer(t)
	if _, err := Texture(ir, "nope", nil, Options{Sheets: []string{"none"}}); err == nil || !strings.Contains(err.Error(), "crate") {
		t.Errorf("unknown texture: %v", err)
	}
	if _, err := Texture(nil, "crate", nil, Options{}); err == nil {
		t.Errorf("nil renderer: no error")
	}
	// A source that does not parse is an error, not a silent skip.
	bad := &TexSource{File: "textures/crate.tex.json", Data: []byte(`{"veduta": "texture/1", "size": [64, 64], "bogus": 1, "layers": []}`)}
	if _, err := Texture(ir, "crate", bad, Options{Sheets: []string{"none"}}); err == nil {
		t.Errorf("bad source: no error")
	}
	// A stale source (different size) skips the layer checks with a reason.
	stale := &TexSource{File: "textures/crate.tex.json", Data: []byte(`{"veduta": "texture/1", "size": [32, 32], "layers": [{"type": "solid", "color": "#ffffff"}]}`)}
	r := texInspect(t, ir, "crate", stale, Options{})
	if nc := texIssues(r, "TEX_LAYERS_NOT_CHECKED"); len(nc) != 1 || !strings.Contains(nc[0].Where["reason"].(string), "cook again") ||
		!strings.Contains(nc[0].Hint, "Cook the project again") {
		t.Errorf("stale source: %s", texJSON(t, r))
	}
	// 1×1: every metric is defined, nothing but low contrast is reported.
	r = texInspect(t, ir, "one", srcs["one"], Options{Sheets: []string{"all"}})
	for _, is := range r.Issues {
		if is.Code != "TEX_LOW_CONTRAST" {
			t.Errorf("1x1: unexpected %s", is.Code)
		}
	}
	if len(r.Sheets) != 5 {
		t.Errorf("1x1 sheets %v", r.Sheets)
	}
	if _, err := json.Marshal(r); err != nil {
		t.Errorf("1x1 report does not encode: %v", err)
	}
	// Every hint of every fixture names something to change in a source file.
	for _, name := range asset.Names(srcs) {
		r := texInspect(t, ir, name, srcs[name], Options{})
		for _, is := range r.Issues {
			if is.Hint == "" || (!strings.Contains(is.Hint, `"`) && !strings.Contains(is.Hint, "layers[")) {
				t.Errorf("%s %s: hint names no field: %q", name, is.Code, is.Hint)
			}
		}
	}
}
