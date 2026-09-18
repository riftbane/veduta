package texture

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/internal/golden"
	"github.com/riftbane/veduta/v2/internal/sheet"
)

// testdataDir returns the module's testdata directory, the assets directory of the
// sample textures (their image layers read textures/src/*.png).
func testdataDir(t testing.TB) string {
	t.Helper()
	root, err := golden.Root()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "testdata")
}

// parseSample compiles testdata/textures/<name>.vtex, after edit (when not nil)
// changed the decoded source, leaving out the layers in skip.
func parseSample(t testing.TB, name string, edit func(*asset.TextureSource), skip ...int) *asset.Texture {
	t.Helper()
	dir := testdataDir(t)
	file := filepath.Join(dir, "textures", name+".vtex")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	opt := Options{FS: os.DirFS(dir), Skip: skip}
	if edit == nil {
		tex, err := Parse(file, data, opt)
		if err != nil {
			t.Fatal(err)
		}
		return tex
	}
	var src asset.TextureSource
	loc, err := asset.Decode(file, data, asset.TypeTexture, &src)
	if err != nil {
		t.Fatal(err)
	}
	edit(&src)
	tex, err := Compile(name, &src, loc, opt)
	if err != nil {
		t.Fatal(err)
	}
	return tex
}

// onBackdrop composites img (straight alpha) over an 8-pixel gray checkerboard so
// transparency is visible.
func onBackdrop(img *gfx.Image) *gfx.Image {
	out := gfx.NewImage(img.W, img.H)
	for y := 0; y < img.H; y++ {
		for x := 0; x < img.W; x++ {
			bg := uint32(0x70)
			if (x/8+y/8)&1 == 1 {
				bg = 0x98
			}
			c := img.Pix[y*img.W+x]
			a := c >> 24
			var px uint32 = 0xff000000
			for s := 0; s < 24; s += 8 {
				v := ((c>>s&0xff)*a + bg*(255-a) + 127) / 255
				px |= v << s
			}
			out.Pix[y*img.W+x] = px
		}
	}
	return out
}

// tiled2x2 repeats img twice in each direction.
func tiled2x2(img *gfx.Image) *gfx.Image {
	out := gfx.NewImage(2*img.W, 2*img.H)
	for k := 0; k < 4; k++ {
		out.Blit(img, k%2*img.W, k/2*img.H)
	}
	return out
}

// zoom enlarges images up to 160 pixels 2× (nearest) so their pixels are easy to look
// at.
func zoom(img *gfx.Image) *gfx.Image {
	if img.W > 160 || img.H > 160 {
		return img
	}
	return sheet.Resize(img, 2*img.W, 2*img.H)
}

// TestGoldenSamples renders every sample texture in testdata/textures and compares it
// with testdata/golden/texture_<name>.png: the texture over a gray checkerboard (to show
// alpha), followed for tiling textures by a 2×2 repeat that exposes seams. The image
// sample shows the three fit modes (contain, cover, stretch) of a 3:2 logo in a square
// texture; the spec example is shown again with its opaque checker (layer 6) skipped so
// the layers below it can be checked. Textures up to 160 pixels are shown at 2×.
func TestGoldenSamples(t *testing.T) {
	for _, name := range []string{"solid", "noise", "stripes", "rect", "circle", "gradient", "checker", "image", "example", "crate_wood"} {
		t.Run(name, func(t *testing.T) {
			var tiles []*gfx.Image
			switch name {
			case "example":
				var labels []string
				for _, skip := range [][]int{nil, {6}} {
					base := parseSample(t, name, nil, skip...).Data.Levels[0]
					tiles = append(tiles, onBackdrop(base), onBackdrop(tiled2x2(base)))
					label := "example"
					if skip != nil {
						label = "layer 6 skipped"
					}
					labels = append(labels, label, "2x2")
				}
				golden.Image(t, "texture_"+name, sheet.Grid(tiles, labels, 2, 4))
				return
			case "image":
				for _, fit := range []string{"contain", "cover", "stretch"} {
					tex := parseSample(t, name, func(s *asset.TextureSource) { s.Layers[0].Fit = fit })
					tiles = append(tiles, zoom(onBackdrop(tex.Data.Levels[0])))
				}
			default:
				tex := parseSample(t, name, nil)
				base := tex.Data.Levels[0]
				tiles = append(tiles, zoom(onBackdrop(base)))
				if tex.Tiling {
					tiles = append(tiles, zoom(onBackdrop(tiled2x2(base))))
				}
			}
			golden.Image(t, "texture_"+name, sheet.HStack(4, tiles...))
		})
	}
}

// baseSamples are the sample textures the preview of the VS Code extension is compared
// with, and the fit modes of the image sample, whose source names only one.
var baseSamples = []struct {
	golden string
	sample string
	fit    string
}{
	{"solid", "solid", ""},
	{"noise", "noise", ""},
	{"stripes", "stripes", ""},
	{"rect", "rect", ""},
	{"circle", "circle", ""},
	{"gradient", "gradient", ""},
	{"checker", "checker", ""},
	{"image", "image", ""},
	{"image_cover", "image", "cover"},
	{"image_stretch", "image", "stretch"},
	{"example", "example", ""},
	{"crate_wood", "crate_wood", ""},
}

// TestGoldenBase writes the base level of every sample texture as it is, with no backdrop
// and no zoom, to testdata/golden/texture_base_<name>.png. Nothing else in Go reads them:
// they are what the preview of the VS Code extension is compared with
// (editors/vscode/test/texture.test.js), which draws the same sources in JavaScript. A
// change to the renderer here therefore fails that test too, until both are updated.
func TestGoldenBase(t *testing.T) {
	for _, s := range baseSamples {
		t.Run(s.golden, func(t *testing.T) {
			var edit func(*asset.TextureSource)
			if s.fit != "" {
				edit = func(src *asset.TextureSource) { src.Layers[0].Fit = s.fit }
			}
			golden.Image(t, "texture_base_"+s.golden, parseSample(t, s.sample, edit).Data.Levels[0])
		})
	}
}
