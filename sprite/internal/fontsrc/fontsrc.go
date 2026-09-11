// Package fontsrc parses the human-readable source of the built-in 8×8 bitmap font
// (sprite/font8x8.txt) and renders it into the atlas image embedded by package sprite.
//
// It is shared by the generator (sprite/internal/genfont) and by the tests that check
// the committed PNG against the text source, so the two can never drift apart.
//
// Source format: blank lines and lines starting with "//" are ignored. Each glyph is a
// header line whose first field is the character code (decimal or 0x-prefixed hex; the
// rest of the line is a free comment) followed by exactly CellH rows of exactly CellW
// cells, top to bottom, where '#' is ink and '.' is transparent. Every code from First to
// Last must appear exactly once.
package fontsrc

import (
	"bufio"
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strconv"
	"strings"
)

// Atlas layout of the built-in font.
const (
	CellW = 8   // glyph cell width in pixels
	CellH = 8   // glyph cell height in pixels
	Cols  = 16  // cells per atlas row
	First = 32  // first rune in the atlas (space)
	Last  = 126 // last rune in the atlas ('~')

	// Count is the number of glyphs.
	Count = Last - First + 1
	// Rows is the number of cell rows in the atlas.
	Rows = (Count + Cols - 1) / Cols
	// AtlasW and AtlasH are the atlas size in pixels.
	AtlasW = Cols * CellW
	AtlasH = Rows * CellH
)

// Bitmap is one glyph: Bitmap[y] holds row y, bit (CellW-1-x) set when pixel x is ink.
type Bitmap [CellH]uint8

// Ink reports whether pixel (x, y) of the glyph is ink.
func (b *Bitmap) Ink(x, y int) bool { return b[y]&(1<<(CellW-1-x)) != 0 }

// Font is a parsed font source: Glyphs[r-First] is the bitmap of rune r.
type Font struct {
	Glyphs [Count]Bitmap
}

// Glyph returns the bitmap of r, which must lie in [First, Last].
func (f *Font) Glyph(r rune) *Bitmap { return &f.Glyphs[r-First] }

// Parse parses a font source. Errors carry the 1-based line number.
func Parse(src []byte) (*Font, error) {
	var f Font
	var seen [Count]bool
	sc := bufio.NewScanner(bytes.NewReader(src))
	line := 0
	cur := -1 // glyph index being read, -1 between glyphs
	row := 0
	hdr := 0 // line of the current header
	for sc.Scan() {
		line++
		text := strings.TrimRight(sc.Text(), " \t\r")
		if cur < 0 {
			if text == "" || strings.HasPrefix(text, "//") {
				continue
			}
			field, _, _ := strings.Cut(strings.TrimLeft(text, " \t"), " ")
			code, err := strconv.ParseInt(field, 0, 32)
			if err != nil {
				return nil, fmt.Errorf("line %d: glyph header %q: want a character code such as 0x41", line, text)
			}
			if code < First || code > Last {
				return nil, fmt.Errorf("line %d: character code %#x outside %#x..%#x", line, code, First, Last)
			}
			cur = int(code - First)
			if seen[cur] {
				return nil, fmt.Errorf("line %d: duplicate glyph %#x", line, code)
			}
			seen[cur] = true
			row, hdr = 0, line
			continue
		}
		if len(text) != CellW {
			return nil, fmt.Errorf("line %d: glyph %#x row %d has %d cells, want %d", line, cur+First, row, len(text), CellW)
		}
		var bits uint8
		for x := 0; x < CellW; x++ {
			switch text[x] {
			case '#':
				bits |= 1 << (CellW - 1 - x)
			case '.':
			default:
				return nil, fmt.Errorf("line %d: glyph %#x row %d: invalid cell %q (want '#' or '.')", line, cur+First, row, text[x])
			}
		}
		f.Glyphs[cur][row] = bits
		row++
		if row == CellH {
			cur = -1
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if cur >= 0 {
		return nil, fmt.Errorf("line %d: glyph %#x has %d rows, want %d", hdr, cur+First, row, CellH)
	}
	for i, ok := range seen {
		if !ok {
			return nil, fmt.Errorf("missing glyph %#x", i+First)
		}
	}
	return &f, nil
}

// Ink is the atlas color of glyph pixels; everything else is fully transparent.
var Ink = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}

// Atlas renders the font into a Cols×Rows grid of CellW×CellH cells, rune First at the
// top-left, left to right then top to bottom. The image has a two-entry palette:
// transparent black and opaque white (Ink).
func (f *Font) Atlas() *image.Paletted {
	img := image.NewPaletted(image.Rect(0, 0, AtlasW, AtlasH), color.Palette{color.NRGBA{}, Ink})
	for i := range f.Glyphs {
		ox, oy := i%Cols*CellW, i/Cols*CellH
		for y := 0; y < CellH; y++ {
			for x := 0; x < CellW; x++ {
				if f.Glyphs[i].Ink(x, y) {
					img.SetColorIndex(ox+x, oy+y, 1)
				}
			}
		}
	}
	return img
}

// Generate parses a font source and returns the atlas encoded as PNG.
func Generate(src []byte) ([]byte, error) {
	f, err := Parse(src)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, f.Atlas()); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
