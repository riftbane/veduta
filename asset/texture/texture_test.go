package texture

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/gfx"
)

// tex wraps comma-separated layer objects into a texture source of size w×h.
func tex(size, extra, layers string) string {
	return `{"veduta": "texture/1", "size": ` + size + extra + `, "layers": [` + layers + `]}`
}

// compile parses src as "test.tex.json" with the testdata FS and fails on error.
func compile(t testing.TB, src string, opt Options) *asset.Texture {
	t.Helper()
	if opt.FS == nil {
		opt.FS = os.DirFS(testdataDir(t))
	}
	tx, err := Parse("test.tex.json", []byte(src), opt)
	if err != nil {
		t.Fatalf("compile: %v\n%s", err, src)
	}
	return tx
}

// compileErrs parses src and returns its validation errors, failing if there are none.
func compileErrs(t *testing.T, file, src string, opt Options) asset.Errors {
	t.Helper()
	_, err := Parse(file, []byte(src), opt)
	if err == nil {
		t.Fatalf("no error for %s", src)
	}
	var es asset.Errors
	if !errors.As(err, &es) {
		t.Fatalf("error %T (%v) is not asset.Errors", err, err)
	}
	return es
}

// hex formats a pixel as "#rrggbbaa".
func hex(c uint32) string {
	s := gfx.FormatColor(c)
	if len(s) == 7 {
		s += "ff"
	}
	return s
}

// pixels returns the base level as "#rrggbbaa" strings, row by row.
func pixels(tx *asset.Texture) []string {
	img := tx.Data.Levels[0]
	out := make([]string, len(img.Pix))
	for i, c := range img.Pix {
		out[i] = hex(c)
	}
	return out
}

const badSrc = `{
  "veduta": "texture/1",
  "size": [0, 5000],
  "mipmaps": false,
  "layers": [
    { "type": "solid" },
    { "type": "noise", "color": "#fff", "octaves": 9, "scale": 0, "radius": 3 },
    { "type": "stripes", "width": -1, "colors": ["#000000"] },
    { "type": "rect", "xy": [0], "size": [4, 0], "color": "#ff0000", "corner": -1, "cells": 0 },
    { "type": "circle", "center": [1, 2], "color": "#00ff00", "outline": null },
    { "type": "gradient", "from": "#000000", "angle_deg": 45, "opacity": 1.5 },
    { "type": "checker", "cells": 0, "colors": ["#000000", "#ffffff", "#808080"] },
    { "type": "image", "path": "../secret.png", "fit": "fill", "blend": "overlay" },
    { "type": "blur" },
    { "color": "#000000" }
  ]
}`

// TestValidationReportsEverything checks that every problem of a source is reported at
// once, in source order, with its JSON path and line.
func TestValidationReportsEverything(t *testing.T) {
	want := []struct {
		line int
		msg  string
	}{
		{3, `size[0]: 0 out of range [1, 4096]`},
		{3, `size[1]: 5000 out of range [1, 4096]`},
		{6, `layers[0].color: is required`},
		{7, `layers[1].radius: not used by layer type noise`},
		{7, `layers[1].scale: 0 out of range (0, 4096]`},
		{7, `layers[1].octaves: 9 out of range [1, 8]`},
		{7, `layers[1].color: color "#fff": want #RRGGBB or #RRGGBBAA`},
		{8, `layers[2].width: must be a positive number, got -1`},
		{8, `layers[2].colors: want 2 to 64 colors, got 1`},
		{9, `layers[3].cells: not used by layer type rect`},
		{9, `layers[3].xy: want 2 numbers, got 1`},
		{9, `layers[3].size[1]: must be a positive number, got 0`},
		{9, `layers[3].corner: must be a number >= 0, got -1`},
		{10, `layers[4].radius: is required`},
		{11, `layers[5].to: is required`},
		{11, `layers[5].opacity: 1.5 out of range [0, 1]`},
		{12, `layers[6].cells: 0 out of range [1, 4096]`},
		{12, `layers[6].colors: want exactly 2 colors, got 3`},
		{13, `layers[7].fit: unknown value "fill"`},
		{13, `layers[7].path: "../secret.png": must not leave the assets directory`},
		{13, `layers[7].blend: unknown value "overlay"`},
		{14, `layers[8].type: unknown value "blur"`},
		{15, `layers[9].type: is required`},
	}
	es := compileErrs(t, "textures/bad.tex.json", badSrc, Options{})
	for i, e := range es {
		t.Logf("%v", e)
		if i >= len(want) {
			continue
		}
		if e.File != "textures/bad.tex.json" || e.Line != want[i].line || !strings.HasPrefix(e.Msg, want[i].msg) {
			t.Errorf("error %d = %s:%d:%d %q, want line %d %q", i, e.File, e.Line, e.Col, e.Msg, want[i].line, want[i].msg)
		}
	}
	if len(es) != len(want) {
		t.Fatalf("got %d errors, want %d", len(es), len(want))
	}
	// Columns point at the value.
	lines := strings.Split(badSrc, "\n")
	cols := []struct {
		err  int
		line int
		at   string
	}{
		{1, 2, `5000]`},
		{3, 6, `3 }`},
		{5, 6, `9,`},
		{19, 12, `"../secret.png"`},
		{13, 9, `"#00ff00"`}, // radius is missing: the error points at the layer object
	}
	for _, c := range cols {
		want := strings.Index(lines[c.line], c.at) + 1
		if c.err == 13 {
			want = strings.Index(lines[c.line], "{") + 1
		}
		if e := es[c.err]; e.Col != want {
			t.Errorf("error %d (%s) column = %d, want %d", c.err, e.Msg, e.Col, want)
		}
	}
}

func TestLayerValidation(t *testing.T) {
	cases := []struct{ layers, want string }{
		{`{"type": "solid", "color": "#000000", "width": 0}`, "layers[0].width: not used by layer type solid"},
		{`{"type": "solid", "color": "#000000", "colors": null}`, "layers[0].colors: not used by layer type solid"},
		{`{"type": "solid", "color": "#000000", "fit": ""}`, "layers[0].fit: not used by layer type solid"},
		{`{"type": "solid", "color": ""}`, "layers[0].color: empty color"},
		{`{"type": "solid", "color": "#0000000"}`, `layers[0].color: color "#0000000": want #RRGGBB or #RRGGBBAA`},
		{`{"type": "solid", "color": "#000000", "opacity": -0.1}`, "layers[0].opacity: -0.1 out of range [0, 1]"},
		{`{"type": "noise"}`, "layers[0].color: is required"},
		{`{"type": "noise", "color": "#000000", "octaves": 0}`, "layers[0].octaves: 0 out of range [1, 8]"},
		{`{"type": "noise", "color": "#000000", "scale": 5000}`, "layers[0].scale: 5000 out of range (0, 4096]"},
		{`{"type": "noise", "color": "#000000", "cells": 2}`, "layers[0].cells: not used by layer type noise"},
		{`{"type": "stripes", "colors": ["#000000", "#ffffff"]}`, "layers[0].width: is required"},
		{`{"type": "stripes", "width": 2}`, "layers[0].colors: is required"},
		{`{"type": "stripes", "width": 2, "colors": ["#000000", ""]}`, "layers[0].colors[1]: empty color"},
		{`{"type": "stripes", "width": 2, "colors": ["#000000", "#fffffg"]}`, `layers[0].colors[1]: color "#fffffg": invalid hex digits`},
		{`{"type": "stripes", "width": 2, "colors": ["#000000", "#ffffff"], "color": "#ffffff"}`, "layers[0].color: not used by layer type stripes"},
		{`{"type": "rect", "size": [1, 1], "color": "#000000"}`, "layers[0].xy: is required"},
		{`{"type": "rect", "xy": [1, 1], "color": "#000000"}`, "layers[0].size: is required"},
		{`{"type": "rect", "xy": [1, 1], "size": [1, 1]}`, "layers[0].color: is required"},
		{`{"type": "rect", "xy": [1, 1], "size": [1, 1, 1], "color": "#000000"}`, "layers[0].size: want 2 numbers, got 3"},
		{`{"type": "rect", "xy": [1, 1], "size": [1, 1], "color": "#000000", "outline": -2}`, "layers[0].outline: must be a number >= 0, got -2"},
		{`{"type": "rect", "xy": [1, 1], "size": [1, 1], "color": "#000000", "center": [0, 0]}`, "layers[0].center: not used by layer type rect"},
		{`{"type": "circle", "radius": 1, "color": "#000000"}`, "layers[0].center: is required"},
		{`{"type": "circle", "center": [1, 1], "radius": 0, "color": "#000000"}`, "layers[0].radius: must be a positive number, got 0"},
		{`{"type": "circle", "center": [1, 1], "radius": 1, "color": "#000000", "corner": 1}`, "layers[0].corner: not used by layer type circle"},
		{`{"type": "gradient", "to": "#000000"}`, "layers[0].from: is required"},
		{`{"type": "gradient", "from": "#000000", "to": "#000000", "color": "#000000"}`, "layers[0].color: not used by layer type gradient"},
		{`{"type": "checker", "colors": ["#000000", "#ffffff"]}`, "layers[0].cells: is required"},
		{`{"type": "checker", "cells": 4097, "colors": ["#000000", "#ffffff"]}`, "layers[0].cells: 4097 out of range [1, 4096]"},
		{`{"type": "checker", "cells": 2, "colors": ["#000000"]}`, "layers[0].colors: want exactly 2 colors, got 1"},
		{`{"type": "checker", "cells": 2, "colors": ["#000000", "#ffffff"], "angle_deg": 0}`, "layers[0].angle_deg: not used by layer type checker"},
		{`{"type": "image"}`, "layers[0].path: is required"},
		{`{"type": "image", "path": "textures/src/logo.png", "seed": 1}`, "layers[0].seed: not used by layer type image"},
		{`{"type": "Solid", "color": "#000000"}`, `layers[0].type: unknown value "Solid"`},
	}
	for _, c := range cases {
		es := compileErrs(t, "test.tex.json", tex("[4, 4]", "", c.layers), Options{FS: os.DirFS(testdataDir(t))})
		if len(es) != 1 || !strings.HasPrefix(es[0].Msg, c.want) {
			t.Errorf("%s:\n got %v\nwant %s", c.layers, es, c.want)
		}
	}
}

func TestTopLevelValidation(t *testing.T) {
	solid := `{"type": "solid", "color": "#000000"}`
	cases := []struct{ src, want string }{
		{`{"veduta": "texture/1", "layers": [` + solid + `]}`, "size: is required"},
		{tex("[4]", "", solid), "size: want 2 integers [width, height], got 1"},
		{tex("[4, 4097]", "", solid), "size[1]: 4097 out of range [1, 4096]"},
		{tex("[4, 4]", "", ""), "layers: at least one layer is required"},
		{`{"veduta": "texture/1", "size": [4, 4]}`, "layers: at least one layer is required"},
		{tex("[4, 4]", "", strings.Repeat(solid+",", 64)+solid), "layers: 65 layers, want at most 64"},
	}
	for _, c := range cases {
		es := compileErrs(t, "test.tex.json", c.src, Options{})
		if len(es) != 1 || !strings.HasPrefix(es[0].Msg, c.want) {
			t.Errorf("%s:\n got %v\nwant %s", c.src, es, c.want)
		}
	}
	// Decoding errors are located single errors.
	for src, want := range map[string]string{
		tex("[4.5, 4]", "", solid):                "cannot use JSON number 4.5 as int",
		tex("[4, 4]", `, "tilling": true`, solid): `tilling: unknown field`,
		`{"veduta": "model/1"}`:                   `header is "model/1", want "texture/1"`,
	} {
		_, err := Parse("test.tex.json", []byte(src), Options{})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want %q", src, err, want)
		}
	}
}

func TestNames(t *testing.T) {
	src := tex("[1, 1]", "", `{"type": "solid", "color": "#000000"}`)
	for file, want := range map[string]string{
		"textures/crate.tex.json": "",
		"crate.json":              `file name "crate.json" must end in ".tex.json"`,
		".tex.json":               `file name ".tex.json" must end in ".tex.json" with a non-empty name`,
		"Crate.tex.json":          `file name: texture name "Crate" may only contain`,
	} {
		tx, err := Parse(file, []byte(src), Options{})
		switch {
		case want == "" && err != nil:
			t.Errorf("%s: %v", file, err)
		case want == "" && tx.Name != "crate":
			t.Errorf("%s: name %q", file, tx.Name)
		case want != "" && (err == nil || !strings.Contains(err.Error(), want)):
			t.Errorf("%s: got %v, want %q", file, err, want)
		}
	}
	var src2 asset.TextureSource
	if _, err := asset.Decode("x", []byte(src), asset.TypeTexture, &src2); err != nil {
		t.Fatal(err)
	}
	if _, err := Compile("bad name", &src2, nil, Options{}); err == nil || !strings.Contains(err.Error(), `texture name "bad name"`) {
		t.Errorf("Compile with a bad name: %v", err)
	}
	if _, err := Compile("x", nil, nil, Options{}); err == nil {
		t.Error("Compile(nil source) succeeded")
	}
}

// TestBlendModes composites a 1×1 layer over a known canvas; expected bytes are worked
// out by hand from the formulas in docs/texture.md.
func TestBlendModes(t *testing.T) {
	// d = #ff8000 = (1, 128/255, 0), s = #4080c0 = (64/255, 128/255, 192/255).
	cases := []struct {
		name, base, layer, want string
	}{
		{"normal", "#ff8000", `"color": "#4080c0"`, "#4080c0ff"},
		{"multiply", "#ff8000", `"color": "#4080c0", "blend": "multiply"`, "#404000ff"},                          // g 128·128/255 = 64.25
		{"screen", "#ff8000", `"color": "#4080c0", "blend": "screen"`, "#ffc0c0ff"},                              // g 255·(1 − (127/255)²) = 191.75
		{"add", "#ff8000", `"color": "#4080c0", "blend": "add"`, "#ffffc0ff"},                                    // g min(1, 256/255)
		{"normal quarter", "#ff8000", `"color": "#4080c0", "opacity": 0.25`, "#cf8030ff"},                        // r 255 − 191/4 = 207.25
		{"multiply quarter", "#ff8000", `"color": "#4080c0", "blend": "multiply", "opacity": 0.25`, "#cf7000ff"}, // g 128 − 63.75/4 = 112.06
		{"screen quarter", "#ff8000", `"color": "#4080c0", "blend": "screen", "opacity": 0.25`, "#ff9030ff"},     // g 128 + 63.75/4 = 143.94
		{"add quarter", "#ff8000", `"color": "#4080c0", "blend": "add", "opacity": 0.25`, "#ffa030ff"},           // g 128 + 127/4 = 159.75
		{"color alpha", "#ff8000", `"color": "#4080c040"`, "#cf8030ff"},                                          // 64/255 alpha ≈ 0.25: r 255 − 191·0.251 = 207.06
		{"zero opacity", "#ff8000", `"color": "#4080c0", "opacity": 0`, "#ff8000ff"},
		// Over a transparent canvas a layer keeps its own color (no darkening).
		{"transparent canvas", "", `"color": "#ffffff80"`, "#ffffff80"},
		{"transparent multiply", "", `"color": "#ffffff80", "blend": "multiply"`, "#ffffff80"},
		// Two half-transparent layers: αo = a + dA·(1 − a) = 0.75195; r = a/αo, b = (1 − a)·dA/αo.
		{"half over half", "#0000ff80", `"color": "#ff000080"`, "#aa0055c0"},
	}
	for _, c := range cases {
		layers := `{"type": "solid", ` + c.layer + `}`
		if c.base != "" {
			layers = `{"type": "solid", "color": "` + c.base + `"}, ` + layers
		}
		got := pixels(compile(t, tex("[1, 1]", "", layers), Options{}))[0]
		if got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

// TestShapes checks coverage, clamping and anti-aliasing of rect, circle, stripes,
// gradient and checker on tiny textures, pixel by pixel.
func TestShapes(t *testing.T) {
	const W, B, T, H = "#ffffffff", "#000000ff", "#00000000", "#ffffff80"
	cases := []struct {
		name, size, layer string
		want              []string
	}{
		{"rect", "[4, 1]", `{"type": "rect", "xy": [1, 0], "size": [2, 1], "color": "#ffffff"}`, []string{T, W, W, T}},
		{"rect half pixel", "[4, 1]", `{"type": "rect", "xy": [0.5, 0], "size": [2, 1], "color": "#ffffff"}`, []string{H, W, H, T}},
		{"rect outside", "[2, 1]", `{"type": "rect", "xy": [-10, 0], "size": [11, 1], "color": "#ffffff"}`, []string{W, T}},
		{"rect outline", "[4, 4]", `{"type": "rect", "xy": [0, 0], "size": [4, 4], "color": "#ffffff", "outline": 1}`,
			[]string{W, W, W, W, W, T, T, W, W, T, T, W, W, W, W, W}},
		{"rect outline too wide", "[3, 1]", `{"type": "rect", "xy": [0, 0], "size": [3, 1], "color": "#ffffff", "outline": 0.5}`, []string{W, W, W}},
		{"stripes", "[4, 1]", `{"type": "stripes", "width": 1, "colors": ["#000000", "#ffffff"]}`, []string{B, W, B, W}},
		{"stripes aa", "[3, 1]", `{"type": "stripes", "width": 1.5, "colors": ["#000000", "#ffffff"]}`, []string{B, "#808080ff", W}},
		{"stripes 90", "[1, 3]", `{"type": "stripes", "width": 1, "angle_deg": 90, "colors": ["#000000", "#ffffff", "#ff0000"]}`, []string{B, W, "#ff0000ff"}},
		{"stripes -90", "[1, 3]", `{"type": "stripes", "width": 1, "angle_deg": -90, "colors": ["#000000", "#ffffff", "#ff0000"]}`, []string{"#ff0000ff", W, B}},
		{"stripes alpha", "[2, 1]", `{"type": "stripes", "width": 1, "colors": ["#00000000", "#ff000080"]}`, []string{T, "#ff000080"}},
		{"stripes alpha aa", "[2, 1]", `{"type": "stripes", "width": 0.5, "colors": ["#00000000", "#ff0000"]}`, []string{"#ff000080", "#ff000080"}},
		{"gradient", "[5, 1]", `{"type": "gradient", "from": "#000000", "to": "#ffffff"}`, []string{B, "#404040ff", "#808080ff", "#bfbfbfff", W}},
		{"gradient 180", "[3, 1]", `{"type": "gradient", "from": "#000000", "to": "#ffffff", "angle_deg": 180}`, []string{W, "#808080ff", B}},
		{"gradient 1px", "[1, 1]", `{"type": "gradient", "from": "#ff0000", "to": "#0000ff"}`, []string{"#ff0000ff"}},
		{"gradient premultiplied", "[3, 1]", `{"type": "gradient", "from": "#ff000000", "to": "#0000ffff"}`, []string{T, "#0000ff80", "#0000ffff"}},
		{"checker", "[4, 2]", `{"type": "checker", "cells": 2, "colors": ["#000000", "#ffffff"]}`, []string{B, B, W, W, W, W, B, B}},
		// 3 cells per side on a 1-pixel-high texture: the pixel row is in cell row 1.
		{"checker odd", "[3, 1]", `{"type": "checker", "cells": 3, "colors": ["#000000", "#ffffff"]}`, []string{W, B, W}},
		{"noise color", "[2, 1]", `{"type": "noise", "color": "#ff0000", "opacity": 0}`, []string{T, T}},
	}
	for _, c := range cases {
		got := pixels(compile(t, tex(c.size, "", c.layer), Options{}))
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got %v\nwant %v", c.name, got, c.want)
		}
	}
}

// TestShapeEquivalences checks clamping rules by comparing whole images.
func TestShapeEquivalences(t *testing.T) {
	same := []struct{ name, a, b string }{
		{"corner clamps to half the smaller side",
			`{"type": "rect", "xy": [3.5, 2.25], "size": [20, 20], "color": "#ffffff", "corner": 1000}`,
			`{"type": "circle", "center": [13.5, 12.25], "radius": 10, "color": "#ffffff"}`},
		{"rect outline wider than half the rect is filled",
			`{"type": "rect", "xy": [3, 4], "size": [20, 10], "color": "#ffffff", "corner": 3, "outline": 5}`,
			`{"type": "rect", "xy": [3, 4], "size": [20, 10], "color": "#ffffff", "corner": 3}`},
		{"circle outline >= radius is filled",
			`{"type": "circle", "center": [12, 12], "radius": 7.3, "color": "#ffffff", "outline": 7.3}`,
			`{"type": "circle", "center": [12, 12], "radius": 7.3, "color": "#ffffff"}`},
		{"angle 360 = angle 0",
			`{"type": "stripes", "width": 3, "angle_deg": 360, "colors": ["#000000", "#ffffff"]}`,
			`{"type": "stripes", "width": 3, "colors": ["#000000", "#ffffff"]}`},
		{"angle 450 = angle 90",
			`{"type": "gradient", "from": "#000000", "to": "#ffffff", "angle_deg": 450}`,
			`{"type": "gradient", "from": "#000000", "to": "#ffffff", "angle_deg": 90}`},
	}
	for _, c := range same {
		a := compile(t, tex("[24, 24]", "", c.a), Options{})
		b := compile(t, tex("[24, 24]", "", c.b), Options{})
		if !reflect.DeepEqual(a.Data.Levels[0].Pix, b.Data.Levels[0].Pix) {
			t.Errorf("%s: images differ", c.name)
		}
	}
	// A rect outline draws inside the rect: the outer edge matches the filled rect, the
	// center is empty, and the stroke is outline pixels wide.
	o := pixels(compile(t, tex("[12, 12]", "", `{"type": "rect", "xy": [1, 1], "size": [10, 10], "color": "#ffffff", "corner": 4, "outline": 2}`), Options{}))
	if o[6*12+6] != "#00000000" || o[6*12+1] != "#ffffffff" || o[6*12+2] != "#ffffffff" || o[6*12+3] != "#00000000" || o[6*12] != "#00000000" {
		t.Errorf("outline row 6 = %v", o[6*12:7*12])
	}
}

// TestStripesAnyAngle checks that stripes at arbitrary angles use only the listed colors
// or blends of neighbours and that every band color appears.
func TestStripesAnyAngle(t *testing.T) {
	for _, angle := range []string{"0", "17.5", "45", "-33", "123.4", "270", "1e6"} {
		tx := compile(t, tex("[32, 32]", "", `{"type": "stripes", "width": 4, "angle_deg": `+angle+`, "colors": ["#ff0000", "#0000ff"]}`), Options{})
		seen := map[uint32]bool{}
		for _, c := range tx.Data.Levels[0].Pix {
			r, g, b, a := gfx.UnpackRGBA(c)
			if a != 255 || g != 0 || int(r)+int(b) < 254 || int(r)+int(b) > 256 {
				t.Fatalf("angle %s: pixel %s is not a red/blue blend", angle, hex(c))
			}
			seen[c] = true
		}
		if !seen[0xffff0000] || !seen[0xff0000ff] {
			t.Errorf("angle %s: pure band colors missing", angle)
		}
	}
}

// seamDelta returns the largest per-channel difference between horizontally or
// vertically adjacent pixels inside img, and across the wrap from the last column (row)
// to the first — the seam a 2×2 repeat of img shows.
func seamDelta(img *gfx.Image, vertical bool) (inside, seam int) {
	diff := func(a, b uint32) int {
		d := 0
		for s := 0; s < 32; s += 8 {
			x, y := int(a>>s&0xff), int(b>>s&0xff)
			d = max(d, x-y, y-x)
		}
		return d
	}
	w, h := img.W, img.H
	if vertical {
		for x := 0; x < w; x++ {
			for y := 0; y+1 < h; y++ {
				inside = max(inside, diff(img.At(x, y), img.At(x, y+1)))
			}
			seam = max(seam, diff(img.At(x, h-1), img.At(x, 0)))
		}
		return
	}
	for y := 0; y < h; y++ {
		for x := 0; x+1 < w; x++ {
			inside = max(inside, diff(img.At(x, y), img.At(x+1, y)))
		}
		seam = max(seam, diff(img.At(w-1, y), img.At(0, y)))
	}
	return
}

// TestNoiseTiling checks that tiling noise repeated 2×2 has no seam: the jump across the
// wrap is no larger than the largest jump between neighbours inside the texture. Sizes,
// scales and octaves include non-square, non-power-of-two and non-integer cases.
func TestNoiseTiling(t *testing.T) {
	for _, c := range []struct{ size, scale, octaves string }{
		{"[64, 64]", "4", "1"},
		{"[64, 64]", "8", "8"},
		{"[96, 40]", "3.4", "4"},
		{"[37, 53]", "5", "3"},
		{"[200, 17]", "0.2", "6"},
	} {
		src := tex(c.size, `, "tiling": true`, `{"type": "noise", "seed": -3, "scale": `+c.scale+`, "octaves": `+c.octaves+`, "color": "#ffffff"}`)
		img := compile(t, src, Options{}).Data.Levels[0]
		for _, vertical := range []bool{false, true} {
			inside, seam := seamDelta(img, vertical)
			// The repeat itself: its internal seam must be no worse than the wrap.
			if in2, _ := seamDelta(tiled2x2(img), vertical); in2 != max(inside, seam) {
				t.Errorf("2×2 repeat: largest jump %d, want %d", in2, max(inside, seam))
			}
			if seam > inside {
				t.Errorf("%s scale %s octaves %s vertical=%v: seam jump %d > largest inside jump %d", c.size, c.scale, c.octaves, vertical, seam, inside)
			}
		}
	}
	// Without tiling the same noise does not wrap: its seam is visibly larger.
	img := compile(t, tex("[64, 64]", "", `{"type": "noise", "seed": 1, "scale": 4, "color": "#ffffff"}`), Options{}).Data.Levels[0]
	if inside, seam := seamDelta(img, false); seam <= inside {
		t.Errorf("non-tiling noise: seam %d <= inside %d; expected a visible seam", seam, inside)
	}
}

// TestNoiseProperties checks the noise range, that seeds change the pattern, and that
// more octaves add detail.
func TestNoiseProperties(t *testing.T) {
	noise := func(seed, octaves string) *gfx.Image {
		return compile(t, tex("[64, 64]", `, "tiling": true`, `{"type": "noise", "seed": `+seed+`, "octaves": `+octaves+`, "color": "#ffffff"}`), Options{}).Data.Levels[0]
	}
	a, b := noise("1", "1"), noise("2", "1")
	if reflect.DeepEqual(a.Pix, b.Pix) {
		t.Error("seeds 1 and 2 give the same noise")
	}
	lo, hi := 255, 0
	for _, c := range a.Pix {
		v := int(c >> 24)
		lo, hi = min(lo, v), max(hi, v)
		if c&0xffffff != 0xffffff {
			t.Fatalf("noise pixel %s: color changed", hex(c))
		}
	}
	if hi-lo < 128 {
		t.Errorf("noise alpha spans only [%d, %d]", lo, hi)
	}
	one, _ := seamDelta(noise("1", "1"), false)
	many, _ := seamDelta(noise("1", "6"), false)
	if many <= one {
		t.Errorf("6 octaves: largest neighbour jump %d, 1 octave: %d; want more detail", many, one)
	}
	// Cells much smaller than a pixel (4096·2^7 cells across 16 pixels) are legal: the
	// result is per-pixel white noise, still in range and still wrapping integer periods.
	for _, extra := range []string{"", `, "tiling": true`} {
		fine := compile(t, tex("[16, 16]", extra, `{"type": "noise", "seed": 9223372036854775807, "scale": 4096, "octaves": 8, "color": "#ffffff"}`), Options{}).Data.Levels[0]
		lo, hi := 255, 0
		for _, c := range fine.Pix {
			lo, hi = min(lo, int(c>>24)), max(hi, int(c>>24))
		}
		if hi == lo {
			t.Errorf("fine noise%s is flat (%d)", extra, lo)
		}
	}
}

func TestDeterminism(t *testing.T) {
	data, err := os.ReadFile(testdataDir(t) + "/textures/example.tex.json")
	if err != nil {
		t.Fatal(err)
	}
	a := compile(t, string(data), Options{})
	prev := runtime.GOMAXPROCS(1)
	b := compile(t, string(data), Options{})
	runtime.GOMAXPROCS(prev)
	c := compile(t, string(data), Options{})
	for i := range a.Data.Levels {
		if !reflect.DeepEqual(a.Data.Levels[i], b.Data.Levels[i]) || !reflect.DeepEqual(a.Data.Levels[i], c.Data.Levels[i]) {
			t.Fatalf("level %d differs between compilations", i)
		}
	}
}

func TestWrapAndMips(t *testing.T) {
	solid := `{"type": "solid", "color": "#336699"}`
	cases := []struct {
		extra  string
		wrap   gfx.Wrap
		tiling bool
		levels int
	}{
		{"", gfx.WrapClamp, false, 7},
		{`, "tiling": false`, gfx.WrapClamp, false, 7},
		{`, "tiling": true`, gfx.WrapRepeat, true, 7},
		{`, "mipmaps": true`, gfx.WrapClamp, false, 7},
		{`, "mipmaps": false, "tiling": true`, gfx.WrapRepeat, true, 1},
	}
	for _, c := range cases {
		tx := compile(t, tex("[64, 40]", c.extra, solid), Options{})
		if tx.Data.Wrap != c.wrap || tx.Tiling != c.tiling || len(tx.Data.Levels) != c.levels || tx.Layers != 1 {
			t.Errorf("%q: wrap %d tiling %v levels %d layers %d", c.extra, tx.Data.Wrap, tx.Tiling, len(tx.Data.Levels), tx.Layers)
		}
		last := tx.Data.Levels[len(tx.Data.Levels)-1]
		if c.levels > 1 && (last.W != 1 || last.H != 1 || tx.Data.Levels[1].W != 32 || tx.Data.Levels[1].H != 20) {
			t.Errorf("%q: mip chain sizes wrong", c.extra)
		}
		if tx.Data.Levels[0].W != 64 || tx.Data.Levels[0].H != 40 {
			t.Errorf("%q: base %dx%d", c.extra, tx.Data.Levels[0].W, tx.Data.Levels[0].H)
		}
	}
	// Extreme but legal sizes.
	for _, size := range []string{"[1, 1]", "[4096, 1]", "[1, 4096]"} {
		tx := compile(t, tex(size, "", solid), Options{})
		if n := len(tx.Data.Levels); n != 1 && n != 13 {
			t.Errorf("%s: %d levels", size, n)
		}
	}
}

func TestSkip(t *testing.T) {
	two := tex("[2, 2]", "", `{"type": "solid", "color": "#ff0000"}, {"type": "solid", "color": "#0000ff", "opacity": 0.5}`)
	one := tex("[2, 2]", "", `{"type": "solid", "color": "#ff0000"}`)
	a := compile(t, two, Options{Skip: []int{1}})
	b := compile(t, one, Options{})
	if !reflect.DeepEqual(a.Data.Levels, b.Data.Levels) {
		t.Error("skipping layer 1 differs from the source without it")
	}
	if a.Layers != 2 {
		t.Errorf("Layers = %d, want 2 (skipped layers still count)", a.Layers)
	}
	all := compile(t, two, Options{Skip: []int{0, 1, 1}})
	if all.Data.Levels[0].Pix[0] != 0 {
		t.Errorf("skipping every layer gives %s, want transparent", hex(all.Data.Levels[0].Pix[0]))
	}
	for _, skip := range [][]int{{2}, {-1}} {
		_, err := Parse("t.tex.json", []byte(two), Options{Skip: skip})
		if err == nil || !strings.Contains(err.Error(), "out of range [0, 1]") {
			t.Errorf("skip %v: %v", skip, err)
		}
	}
	// Skipped layers are still validated.
	bad := tex("[2, 2]", "", `{"type": "solid", "color": "#ff0000"}, {"type": "solid"}`)
	if _, err := Parse("t.tex.json", []byte(bad), Options{Skip: []int{1}}); err == nil {
		t.Error("an invalid skipped layer was accepted")
	}
}

// TestImageFit places testdata/textures/src/rb.png (2×1: red, blue) in a 4×4 texture.
func TestImageFit(t *testing.T) {
	const R, Bl, T = "#ff0000ff", "#0000ffff", "#00000000"
	// Bilinear between texel centers: x = 1 is 3/4 red + 1/4 blue.
	const rb, br = "#bf0040ff", "#4000bfff"
	cases := []struct {
		fit  string
		want []string
	}{
		// Scaled 2× to 4×2 and centered: rows 0 and 3 are letterbox.
		{"contain", []string{T, T, T, T, R, rb, br, Bl, R, rb, br, Bl, T, T, T, T}},
		{"", []string{T, T, T, T, R, rb, br, Bl, R, rb, br, Bl, T, T, T, T}},
		// Scaled 4× to 8×4 and centered: the middle half of the image is visible.
		{"cover", []string{"#df0020ff", "#9f0060ff", "#60009fff", "#2000dfff"}},
		{"stretch", []string{R, rb, br, Bl}},
	}
	for _, c := range cases {
		fit := ""
		if c.fit != "" {
			fit = `, "fit": "` + c.fit + `"`
		}
		got := pixels(compile(t, tex("[4, 4]", "", `{"type": "image", "path": "textures/src/rb.png"`+fit+`}`), Options{}))
		want := c.want
		if len(want) == 4 { // same for every row
			want = append(append(append(append([]string{}, want...), want...), want...), want...)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("fit %q:\n got %v\nwant %v", c.fit, got, want)
		}
	}
	// Letterbox edges are anti-aliased by area: 2×1 in 4×3 → image rows 0.5 to 2.5.
	got := pixels(compile(t, tex("[4, 3]", "", `{"type": "image", "path": "textures/src/rb.png"}`), Options{}))
	if got[0] != "#ff000080" || got[4] != R || got[8] != "#ff000080" {
		t.Errorf("letterbox edge column 0 = %s %s %s", got[0], got[4], got[8])
	}
	// The logo at its own size is copied exactly (alpha included).
	logo := readPNG(t, testdataDir(t)+"/textures/src/logo.png")
	tx := compile(t, tex("[48, 32]", "", `{"type": "image", "path": "textures/src/logo.png"}`), Options{})
	for i, c := range tx.Data.Levels[0].Pix {
		if want := logo.Pix[i]; c != want && !(c>>24 == 0 && want>>24 == 0) {
			t.Fatalf("pixel %d: %s, want %s", i, hex(c), hex(want))
		}
	}
	// Shrinking 2× averages 2×2 blocks (premultiplied): same as a box filter.
	half := compile(t, tex("[24, 16]", "", `{"type": "image", "path": "textures/src/logo.png", "fit": "stretch"}`), Options{}).Data.Levels[0]
	for y := 0; y < 16; y++ {
		for x := 0; x < 24; x++ {
			var sum [4]float64
			for k := 0; k < 4; k++ {
				r, g, b, a := gfx.UnpackRGBA(logo.Pix[(2*y+k/2)*48+2*x+k%2])
				af := float64(a) / 255
				sum[0] += float64(r) * af
				sum[1] += float64(g) * af
				sum[2] += float64(b) * af
				sum[3] += af
			}
			r, g, b, a := gfx.UnpackRGBA(half.Pix[y*24+x])
			if d := float64(a) - sum[3]/4*255; d > 0.51 || d < -0.51 {
				t.Fatalf("(%d,%d) alpha %d, want %.2f", x, y, a, sum[3]/4*255)
			}
			if sum[3] > 0 {
				for k, v := range []uint8{r, g, b} {
					if d := float64(v) - sum[k]/sum[3]; d > 0.51 || d < -0.51 {
						t.Fatalf("(%d,%d) channel %d = %d, want %.2f", x, y, k, v, sum[k]/sum[3])
					}
				}
			}
		}
	}
}

func readPNG(t *testing.T, path string) *gfx.Image {
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

func encodePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	img.SetNRGBA(0, 0, color.NRGBA{1, 2, 3, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestImagePaths(t *testing.T) {
	bad := map[string]string{
		"../secret.png":             `must not leave the assets directory`,
		"textures/../../secret.png": `must not leave the assets directory`,
		"/etc/logo.png":             `must be relative to the assets directory`,
		"C:/logo.png":               `must be relative to the assets directory`,
		`textures\src\logo.png`:     `use forward slashes`,
		"./textures/src/logo.png":   `not a clean relative path`,
		"textures//src/logo.png":    `not a clean relative path`,
		"textures/src/":             `not a clean relative path`,
		"textures/src/logo.jpg":     `must be a .png file`,
		"textures/src/missing.png":  `file not found in the assets directory`,
	}
	for p, want := range bad {
		src := tex("[4, 4]", "", `{"type": "image", "path": "`+strings.ReplaceAll(p, `\`, `\\`)+`"}`)
		es := compileErrs(t, "test.tex.json", src, Options{FS: os.DirFS(testdataDir(t))})
		if len(es) != 1 || !strings.HasPrefix(es[0].Msg, "layers[0].path: ") || !strings.Contains(es[0].Msg, want) {
			t.Errorf("%s: got %v, want %q", p, es, want)
		}
		if es[0].Line != 1 || es[0].Col != strings.Index(src, `"path": `)+len(`"path": `)+1 {
			t.Errorf("%s: located at %d:%d", p, es[0].Line, es[0].Col)
		}
	}
	mfs := fstest.MapFS{
		"a/text.png":  {Data: []byte("GIF89a not a png")},
		"a/big.png":   {Data: encodePNG(t, MaxImageSize+1, 1)},
		"a/tall.png":  {Data: encodePNG(t, 1, MaxImageSize+1)},
		"a/ok.png":    {Data: encodePNG(t, MaxImageSize, 1)},
		"a/trunc.png": {Data: encodePNG(t, 64, 64)[:60]},
	}
	for p, want := range map[string]string{
		"a/text.png":  "not a PNG image",
		"a/big.png":   "image is 8193x1, want 1 to 8192 pixels per side",
		"a/tall.png":  "image is 1x8193",
		"a/trunc.png": "decode PNG",
		"a/ok.png":    "",
	} {
		src := tex("[4, 4]", "", `{"type": "image", "path": "`+p+`", "fit": "stretch"}`)
		tx, err := Parse("test.tex.json", []byte(src), Options{FS: mfs})
		switch {
		case want == "" && err != nil:
			t.Errorf("%s: %v", p, err)
		case want == "" && tx.Data.Levels[0].Pix[0] != 0xffffffff:
			t.Errorf("%s: pixel 0 = %s, want the 8192:4 average of white", p, hex(tx.Data.Levels[0].Pix[0]))
		case want != "" && (err == nil || !strings.Contains(err.Error(), want)):
			t.Errorf("%s: got %v, want %q", p, err, want)
		}
	}
	// Without a filesystem, image layers are errors.
	es := compileErrs(t, "test.tex.json", tex("[4, 4]", "", `{"type": "image", "path": "textures/src/logo.png"}`), Options{})
	if len(es) != 1 || !strings.Contains(es[0].Msg, "no filesystem") {
		t.Errorf("nil FS: %v", es)
	}
}

func TestDeps(t *testing.T) {
	src := &asset.TextureSource{Layers: []asset.LayerSource{
		{Type: "image", Path: "textures/src/b.png"},
		{Type: "solid", Color: "#000000", Path: "textures/src/solid.png"},
		{Type: "image", Path: "textures/src/a.png"},
		{Type: "image", Path: "../escape.png"},
		{Type: "image", Path: "/abs.png"},
		{Type: "image", Path: `textures\win.png`},
		{Type: "image", Path: ""},
		{Type: "image", Path: "textures/src/b.png", Fit: "bogus"},
		{Type: "image", Path: "textures/src/a.jpg"},
	}}
	want := []string{"textures/src/a.png", "textures/src/b.png"}
	if got := Deps(src); !reflect.DeepEqual(got, want) {
		t.Errorf("Deps = %q, want %q", got, want)
	}
	if got := Deps(&asset.TextureSource{}); len(got) != 0 {
		t.Errorf("Deps(empty) = %q", got)
	}
	if got := Deps(nil); got != nil {
		t.Errorf("Deps(nil) = %q", got)
	}
}

// TestSpecExample compiles the example of spec §8.2 (testdata/textures/example.tex.json).
func TestSpecExample(t *testing.T) {
	tx := parseSample(t, "example", nil)
	if tx.Name != "example" || tx.Layers != 8 || !tx.Tiling || tx.Data.Wrap != gfx.WrapRepeat || len(tx.Data.Levels) != 9 {
		t.Fatalf("example: name %q layers %d tiling %v wrap %d levels %d", tx.Name, tx.Layers, tx.Tiling, tx.Data.Wrap, len(tx.Data.Levels))
	}
	// The checker (layer 6) is opaque and covers everything below it; the logo (layer 7)
	// is letterboxed in the middle rows.
	img := tx.Data.Levels[0]
	if c := img.At(0, 0); c != 0xffc0c0c0 {
		t.Errorf("corner pixel %s, want the light checker color", hex(c))
	}
	if c := img.At(128/3, 128); c == 0xffc0c0c0 || c == 0xff404040 {
		t.Errorf("logo disk pixel %s is a checker color", hex(c))
	}
	if got := Deps(mustDecode(t, "example")); !reflect.DeepEqual(got, []string{"textures/src/logo.png"}) {
		t.Errorf("example deps %q", got)
	}
	// Skipping the checker shows the layers below it.
	sk, err := Parse("example.tex.json", mustRead(t, "example"), Options{FS: os.DirFS(testdataDir(t)), Skip: []int{6}})
	if err != nil {
		t.Fatal(err)
	}
	if c := sk.Data.Levels[0].At(0, 0); c == 0xffc0c0c0 {
		t.Errorf("with the checker skipped the corner is still %s", hex(c))
	}
}

func mustRead(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(testdataDir(t) + "/textures/" + name + ".tex.json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mustDecode(t *testing.T, name string) *asset.TextureSource {
	t.Helper()
	var src asset.TextureSource
	if _, err := asset.Decode(name, mustRead(t, name), asset.TypeTexture, &src); err != nil {
		t.Fatal(err)
	}
	return &src
}

// TestCompileInCode compiles a source built in Go (no locator): fields set in code are
// validated like parsed ones.
func TestCompileInCode(t *testing.T) {
	r := float32(2)
	src := &asset.TextureSource{Size: []int{8, 8}, Layers: []asset.LayerSource{
		{Type: "solid", Color: "#ffffff", Radius: &r},
	}}
	_, err := Compile("x", src, nil, Options{})
	if err == nil || !strings.Contains(err.Error(), "layers[0].radius: not used by layer type solid") {
		t.Errorf("got %v", err)
	}
	src.Layers[0].Radius = nil
	tx, err := Compile("x", src, nil, Options{})
	if err != nil || tx.Data.Levels[0].Pix[0] != 0xffffffff {
		t.Errorf("got %v", err)
	}
}
