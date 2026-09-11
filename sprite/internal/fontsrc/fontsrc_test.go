package fontsrc

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"strings"
	"testing"
)

// blankSource returns a valid source where every glyph is blank.
func blankSource() string {
	var sb strings.Builder
	sb.WriteString("// test font\n")
	for c := First; c <= Last; c++ {
		fmt.Fprintf(&sb, "\n0x%02X\n", c)
		for y := 0; y < CellH; y++ {
			sb.WriteString("........\n")
		}
	}
	return sb.String()
}

func TestParseBlank(t *testing.T) {
	f, err := Parse([]byte(blankSource()))
	if err != nil {
		t.Fatal(err)
	}
	for i, g := range f.Glyphs {
		if g != (Bitmap{}) {
			t.Fatalf("glyph %#x not blank", i+First)
		}
	}
}

func TestParseErrors(t *testing.T) {
	src := blankSource()
	// Line numbers in blankSource: 1 comment, then per glyph: blank, header, 8 rows.
	// Glyph i's header is on line 3+10*i and its rows on lines 4+10*i .. 11+10*i.
	lines := strings.Split(src, "\n")
	edit := func(line int, text string) string {
		l := append([]string(nil), lines...)
		l[line-1] = text
		return strings.Join(l, "\n")
	}
	cases := []struct {
		name, src, want string
	}{
		{"bad header", edit(3, "space"), "line 3: glyph header"},
		{"out of range", edit(3, "0x7F"), "line 3: character code 0x7f outside"},
		{"duplicate", edit(13, "0x20"), "line 13: duplicate glyph 0x20"},
		{"short row", edit(4, "......."), "line 4: glyph 0x20 row 0 has 7 cells, want 8"},
		{"bad cell", edit(15, "...x...."), "line 15: glyph 0x21 row 1: invalid cell 'x'"},
		{"missing", strings.Join(lines[:len(lines)-11], "\n"), "missing glyph 0x7e"},
		{"truncated", strings.Join(lines[:len(lines)-4], "\n"), "glyph 0x7e has 5 rows, want 8"},
	}
	for _, c := range cases {
		_, err := Parse([]byte(c.src))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want containing %q", c.name, err, c.want)
		}
	}
}

func TestAtlasPlacement(t *testing.T) {
	src := strings.Replace(blankSource(), "0x41\n........\n........\n........\n........",
		"0x41 A\n........\n........\n........\n..#.....", 1)
	f, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if !f.Glyph('A').Ink(2, 3) {
		t.Fatal("pixel (2,3) of 'A' not parsed as ink")
	}
	img := f.Atlas()
	if img.Rect.Dx() != AtlasW || img.Rect.Dy() != AtlasH || AtlasW != 128 || AtlasH != 48 {
		t.Fatalf("atlas %v, want 128x48", img.Rect)
	}
	// 'A' is glyph 33: column 1, row 2.
	for y := 0; y < AtlasH; y++ {
		for x := 0; x < AtlasW; x++ {
			want := uint8(0)
			if x == 8+2 && y == 16+3 {
				want = 1
			}
			if got := img.ColorIndexAt(x, y); got != want {
				t.Fatalf("atlas (%d,%d) = %d, want %d", x, y, got, want)
			}
		}
	}
	data, err := Generate([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	dec, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, a := dec.At(10, 19).RGBA(); a != 0xffff {
		t.Fatalf("decoded ink alpha = %#x", a)
	}
	if r, g, b, a := dec.At(0, 0).RGBA(); r|g|b|a != 0 {
		t.Fatalf("decoded background = %#x %#x %#x %#x, want transparent black", r, g, b, a)
	}
}

// TestBuiltinDesignRules checks the committed font source against the design rules
// documented in its header: blank spacing column, descenders only in the last row,
// a blank space glyph, ink in every other glyph and no two identical glyphs.
func TestBuiltinDesignRules(t *testing.T) {
	src, err := os.ReadFile("../../font8x8.txt")
	if err != nil {
		t.Fatal(err)
	}
	f, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	const descenders = "gjpqy,;"
	seen := map[Bitmap]rune{}
	for r := rune(First); r <= Last; r++ {
		g := f.Glyph(r)
		for y := 0; y < CellH; y++ {
			if g.Ink(CellW-1, y) {
				t.Errorf("glyph %q: ink in spacing column %d", r, CellW-1)
			}
		}
		if g[CellH-1] != 0 && !strings.ContainsRune(descenders, r) {
			t.Errorf("glyph %q: ink in row %d, reserved for descenders %q", r, CellH-1, descenders)
		}
		if (*g == Bitmap{}) != (r == ' ') {
			t.Errorf("glyph %q: blank = %v", r, *g == Bitmap{})
		}
		if prev, ok := seen[*g]; ok {
			t.Errorf("glyphs %q and %q are identical", prev, r)
		}
		seen[*g] = r
	}
}
