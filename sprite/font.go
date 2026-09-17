package sprite

import (
	"bytes"
	_ "embed"
	"image"
	"strings"
	"sync"

	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/gmath"
)

// Font is a monospaced bitmap font stored in an atlas of equal cells. The glyph of rune
// r (First <= r <= Last) is cell r-First, counted left to right, top to bottom, Cols
// cells per row. Every glyph advances by CellW pixels; lines are CellH pixels apart.
type Font struct {
	Atlas        *gfx.Image // glyph cells; ink is typically white so Text's color tints it
	CellW, CellH int        // cell size in pixels
	Cols         int        // cells per atlas row
	First, Last  rune       // inclusive range of runes present in the atlas
	// Cells draws runes beyond the atlas's range with the cell of another code: the
	// default font keeps the characters of Windows-1252's 0x80..0x9F (€, ’, …) there.
	Cells map[rune]rune
}

// Glyph returns the atlas rectangle, in texels, of the glyph for r. A rune of Cells
// takes its cell; control characters (0x7F..0x9F) and runes outside [First, Last] map to
// '?' (or to First if '?' is not in the font either).
func (f *Font) Glyph(r rune) image.Rectangle {
	if c, ok := f.Cells[r]; ok {
		r = c
	} else if r >= 0x7f && r <= 0x9f || r < f.First || r > f.Last {
		r = '?'
		if r < f.First || r > f.Last {
			r = f.First
		}
	}
	return f.Cell(r)
}

// Cell returns the atlas rectangle of the cell of code c, which must be in [First, Last].
func (f *Font) Cell(c rune) image.Rectangle {
	i := int(c - f.First)
	x, y := i%f.Cols*f.CellW, i/f.Cols*f.CellH
	return image.Rect(x, y, x+f.CellW, y+f.CellH)
}

// cp1252 maps the characters Windows-1252 puts at 0x80..0x9F to those cells of the
// default font.
var cp1252 = map[rune]rune{
	'€': 0x80, '‚': 0x82, 'ƒ': 0x83, '„': 0x84, '…': 0x85, '†': 0x86, '‡': 0x87, 'ˆ': 0x88,
	'‰': 0x89, 'Š': 0x8a, '‹': 0x8b, 'Œ': 0x8c, 'Ž': 0x8e, '‘': 0x91, '’': 0x92, '“': 0x93,
	'”': 0x94, '•': 0x95, '–': 0x96, '—': 0x97, '˜': 0x98, '™': 0x99, 'š': 0x9a, '›': 0x9b,
	'œ': 0x9c, 'ž': 0x9e, 'Ÿ': 0x9f,
}

// TextureData returns the atlas as texture data for gfx.Backend.CreateTexture: a full
// mip chain (gfx.BuildMips) with clamped addressing. The returned levels are copies, so
// the font is not modified by the backend.
func (f *Font) TextureData() *gfx.TextureData {
	return &gfx.TextureData{Levels: gfx.BuildMips(f.Atlas.Clone()), Wrap: gfx.WrapClamp}
}

// MeasureText returns the size in pixels of s drawn with f at an integer scale (values
// below 1 count as 1): w is the widest line's rune count times CellW*scale, h the number
// of lines times CellH*scale. '\n' separates lines; the empty string measures 0×0.
func MeasureText(f *Font, s string, scale int) (w, h int) {
	if s == "" {
		return 0, 0
	}
	scale = max(scale, 1)
	lines, n, widest := 1, 0, 0
	for _, r := range s {
		if r == '\n' {
			widest = max(widest, n)
			n = 0
			lines++
			continue
		}
		n++
	}
	widest = max(widest, n)
	return widest * f.CellW * scale, lines * f.CellH * scale
}

// Wrap breaks s into lines no wider than width pixels when drawn with f at an integer
// scale, and returns them joined by '\n'. Lines break between words (at spaces, which the
// break swallows) and at the '\n' already in s; a word wider than the line is cut between
// runes. A width narrower than one glyph still fits one glyph per line.
func Wrap(f *Font, s string, width, scale int) string {
	scale = max(scale, 1)
	perLine := max(width/(f.CellW*scale), 1) // glyphs in a line
	var out strings.Builder
	for i, para := range strings.Split(s, "\n") {
		if i > 0 {
			out.WriteByte('\n')
		}
		n := 0 // glyphs on the current line
		for _, word := range strings.Split(para, " ") {
			runes := []rune(word)
			switch {
			case n > 0 && n+1+len(runes) <= perLine:
				out.WriteByte(' ')
				n++
			case n > 0:
				out.WriteByte('\n')
				n = 0
			}
			for len(runes) > perLine-n {
				out.WriteString(string(runes[:perLine-n]))
				out.WriteByte('\n')
				runes, n = runes[perLine-n:], 0
			}
			out.WriteString(string(runes))
			n += len(runes)
		}
	}
	return out.String()
}

// Text draws s with font f, whose atlas has been uploaded as texture tex, with the
// top-left corner of the first glyph cell at (x, y). Glyphs are magnified by the integer
// scale (values below 1 count as 1) and sampled with nearest filtering; integer x and y
// keep every font pixel on whole screen pixels. color tints the glyphs (0xAARRGGBB).
// '\n' starts a new line CellH*scale below at x; runes missing from the font draw as '?';
// spaces advance without drawing. The result is the width in pixels of the widest line,
// as MeasureText reports.
func (b *Batch) Text(f *Font, tex gfx.TextureID, x, y float32, scale int, s string, color uint32) float32 {
	scale = max(scale, 1)
	cw, ch := float32(f.CellW*scale), float32(f.CellH*scale)
	tw, th := f.Atlas.W, f.Atlas.H
	line, n, widest := 0, 0, 0
	for _, r := range s {
		if r == '\n' {
			widest = max(widest, n)
			n = 0
			line++
			continue
		}
		if r != ' ' {
			// Each product is rounded explicitly so arm64 cannot fuse it into a multiply-add
			// (see gmath.m32).
			gx := x + float32(float32(n)*cw)
			gy := y + float32(float32(line)*ch)
			dst := gmath.Rect{Min: gmath.Vec2{X: gx, Y: gy}, Max: gmath.Vec2{X: gx + cw, Y: gy + ch}}
			b.Image(tex, tw, th, f.Glyph(r), dst, color, gfx.FilterNearest)
		}
		n++
	}
	widest = max(widest, n)
	return float32(widest * f.CellW * scale)
}

//go:embed font8x8.png
var defaultAtlasPNG []byte

//go:embed FONT-LICENSE.md
var defaultFontLicense string

// DefaultFontLicense returns the license note of the built-in font (FONT-LICENSE.md),
// embedded next to its atlas: the font is an original design dedicated to the public
// domain under CC0 1.0.
func DefaultFontLicense() string { return defaultFontLicense }

var (
	defaultOnce sync.Once
	defaultFont *Font
)

// DefaultFont returns the built-in 8×8 font covering ASCII and Latin-1 (space to 'ÿ':
// the accented letters of Western European languages) and the characters of
// Windows-1252 (€, typographic quotes and dashes, …), decoded once from the embedded atlas
// font8x8.png: 16 columns × 14 rows of 8×8 cells (128×112 pixels) with white glyph pixels
// (0xFFFFFFFF) on transparent (0x00000000). Capitals and digits are 5×7; column 7 of
// almost every cell and row 7 (except descenders) are blank, so text stays legible at
// scale 1 and 2.
//
// Every call returns the same *Font; treat it as read-only. The font is an original
// design dedicated to the public domain (CC0 1.0, see FONT-LICENSE.md); its source is
// font8x8.txt.
func DefaultFont() *Font {
	defaultOnce.Do(func() {
		atlas, err := gfx.DecodePNG(bytes.NewReader(defaultAtlasPNG))
		if err != nil {
			panic("sprite: embedded font atlas: " + err.Error())
		}
		defaultFont = &Font{Atlas: atlas, CellW: 8, CellH: 8, Cols: 16, First: 32, Last: 255, Cells: cp1252}
	})
	return defaultFont
}
