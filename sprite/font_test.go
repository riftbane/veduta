package sprite

import (
	"bytes"
	"image"
	"os"
	"strings"
	"testing"

	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gmath"
	"github.com/riftbane/veduta/sprite/internal/fontsrc"
)

func TestDefaultFont(t *testing.T) {
	f := DefaultFont()
	if f != DefaultFont() {
		t.Fatal("DefaultFont returned different fonts")
	}
	if f.Atlas.W != 128 || f.Atlas.H != 48 || f.CellW != 8 || f.CellH != 8 || f.Cols != 16 ||
		f.First != 32 || f.Last != 126 {
		t.Fatalf("font = %+v (atlas %dx%d)", *f, f.Atlas.W, f.Atlas.H)
	}
	for i, c := range f.Atlas.Pix {
		if c != 0 && c != 0xFFFFFFFF {
			t.Fatalf("atlas pixel %d = %#08x, want 0 or 0xFFFFFFFF", i, c)
		}
	}
	td := f.TextureData()
	if td.Levels[0].W != 128 || td.Levels[0].H != 48 || len(td.Levels) != 8 || td.Wrap != gfx.WrapClamp {
		t.Fatalf("texture data: %d levels, wrap %v", len(td.Levels), td.Wrap)
	}
	if &td.Levels[0].Pix[0] == &f.Atlas.Pix[0] {
		t.Fatal("TextureData shares the atlas memory")
	}
}

// TestEmbeddedAtlasMatchesSource guarantees that the committed font8x8.png is exactly
// what the generator produces from font8x8.txt (run `go generate ./sprite` after
// editing the source).
func TestEmbeddedAtlasMatchesSource(t *testing.T) {
	src, err := os.ReadFile("font8x8.txt")
	if err != nil {
		t.Fatal(err)
	}
	fs, err := fontsrc.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	f := DefaultFont()
	for r := rune(fontsrc.First); r <= fontsrc.Last; r++ {
		g := f.Glyph(r)
		bm := fs.Glyph(r)
		for y := 0; y < fontsrc.CellH; y++ {
			for x := 0; x < fontsrc.CellW; x++ {
				want := uint32(0)
				if bm.Ink(x, y) {
					want = 0xFFFFFFFF
				}
				if got := f.Atlas.At(g.Min.X+x, g.Min.Y+y); got != want {
					t.Fatalf("glyph %q pixel (%d,%d) = %#08x in font8x8.png, %#08x in font8x8.txt; run go generate ./sprite",
						r, x, y, got, want)
				}
			}
		}
	}
	gen, err := fontsrc.Generate(src)
	if err != nil {
		t.Fatal(err)
	}
	img, err := gfx.DecodePNG(bytes.NewReader(gen))
	if err != nil {
		t.Fatal(err)
	}
	if img.W != f.Atlas.W || img.H != f.Atlas.H || !equalPix(img.Pix, f.Atlas.Pix) {
		t.Fatal("regenerated atlas differs from font8x8.png; run go generate ./sprite")
	}
}

func TestDefaultFontLicense(t *testing.T) {
	l := DefaultFontLicense()
	for _, want := range []string{"original design", "CC0 1.0", "public domain"} {
		if !strings.Contains(l, want) {
			t.Errorf("license note does not mention %q", want)
		}
	}
}

func equalPix(a, b []uint32) bool {
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

func TestGlyph(t *testing.T) {
	f := DefaultFont()
	cases := []struct {
		r    rune
		want image.Rectangle
	}{
		{' ', image.Rect(0, 0, 8, 8)},
		{'A', image.Rect(8, 16, 16, 24)}, // 65-32 = 33: column 1, row 2
		{'~', image.Rect(112, 40, 120, 48)},
		{'?', image.Rect(120, 8, 128, 16)},
		{'\n', image.Rect(120, 8, 128, 16)},
		{'é', image.Rect(120, 8, 128, 16)},
		{-1, image.Rect(120, 8, 128, 16)},
	}
	for _, c := range cases {
		if got := f.Glyph(c.r); got != c.want {
			t.Errorf("Glyph(%q) = %v, want %v", c.r, got, c.want)
		}
	}
	digits := &Font{Atlas: gfx.NewImage(80, 8), CellW: 8, CellH: 8, Cols: 10, First: '0', Last: '9'}
	if got := digits.Glyph('x'); got != image.Rect(0, 0, 8, 8) {
		t.Errorf("font without '?': Glyph('x') = %v, want the first glyph", got)
	}
}

func TestTextGlyphUV(t *testing.T) {
	f := DefaultFont()
	var dl gfx.DrawList
	b := Begin(&dl, 640, 360)
	w := b.Text(f, 9, 5, 6, 2, "A", 0xFF00FF00)
	b.End()
	if w != 16 {
		t.Fatalf("width = %v, want 16", w)
	}
	if len(dl.Cmds) != 1 {
		t.Fatalf("%d cmds", len(dl.Cmds))
	}
	c := checkCmd(t, &dl, 0, b.View())
	if c.Texture != 9 || c.Filter != gfx.FilterNearest || c.Color != gfx.ColorVec4(0xFF00FF00) || c.Count != 6 {
		t.Fatalf("cmd = %+v", c)
	}
	tl, br := dl.Verts[0], dl.Verts[2]
	if tl.Pos != gmath.V3(5, 6, 0) || br.Pos != gmath.V3(21, 22, 0) {
		t.Errorf("glyph quad %v..%v, want (5,6)..(21,22)", tl.Pos, br.Pos)
	}
	if tl.UV != gmath.V2(8.0/128, 16.0/48) || br.UV != gmath.V2(16.0/128, 24.0/48) {
		t.Errorf("glyph UVs %v..%v", tl.UV, br.UV)
	}
}

func TestTextLayout(t *testing.T) {
	f := DefaultFont()
	var dl gfx.DrawList
	b := Begin(&dl, 640, 360)
	s := "AB\nC D\n"
	w := b.Text(f, 1, 10, 20, 2, s, 0xFFFFFFFF)
	b.End()
	if w != 48 {
		t.Fatalf("width = %v, want 48 (3 cells × 16 px)", w)
	}
	if mw, mh := MeasureText(f, s, 2); float32(mw) != w || mh != 48 {
		t.Fatalf("MeasureText = %d×%d, want %v×48", mw, mh, w)
	}
	// A B / C _ D: the space advances without a quad; all quads share one command.
	want := []struct {
		r    rune
		x, y float32
	}{{'A', 10, 20}, {'B', 26, 20}, {'C', 10, 36}, {'D', 42, 36}}
	if len(dl.Cmds) != 1 || len(dl.Verts) != 4*len(want) {
		t.Fatalf("%d cmds, %d verts", len(dl.Cmds), len(dl.Verts))
	}
	for k, g := range want {
		v := dl.Verts[4*k]
		r := f.Glyph(g.r)
		if v.Pos != gmath.V3(g.x, g.y, 0) || v.UV != gmath.V2(float32(r.Min.X)/128, float32(r.Min.Y)/48) {
			t.Errorf("glyph %q at %v uv %v, want (%v,%v) uv of %v", g.r, v.Pos, v.UV, g.x, g.y, r)
		}
	}
}

func TestTextUnknownRuneAndScale(t *testing.T) {
	f := DefaultFont()
	var dl gfx.DrawList
	b := Begin(&dl, 640, 360)
	w := b.Text(f, 1, 0, 0, 0, "é", 0xFFFFFFFF) // scale 0 counts as 1
	b.End()
	if w != 8 || len(dl.Verts) != 4 {
		t.Fatalf("width %v, %d verts", w, len(dl.Verts))
	}
	q := f.Glyph('?')
	if dl.Verts[0].UV != gmath.V2(float32(q.Min.X)/128, float32(q.Min.Y)/48) || dl.Verts[2].Pos != gmath.V3(8, 8, 0) {
		t.Fatalf("unknown rune drew %+v .. %+v", dl.Verts[0], dl.Verts[2])
	}
}

func TestMeasureText(t *testing.T) {
	f := DefaultFont()
	cases := []struct {
		s           string
		scale, w, h int
	}{
		{"", 1, 0, 0},
		{"A", 1, 8, 8},
		{"Hello", 2, 80, 16},
		{"ab\nlonger\n", 1, 48, 24},
		{"\n", 3, 0, 48},
		{"héllo", 1, 40, 8}, // runes, not bytes
		{"x", -3, 8, 8},     // scale below 1 counts as 1
	}
	for _, c := range cases {
		if w, h := MeasureText(f, c.s, c.scale); w != c.w || h != c.h {
			t.Errorf("MeasureText(%q, %d) = %d×%d, want %d×%d", c.s, c.scale, w, h, c.w, c.h)
		}
	}
}
