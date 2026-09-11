// Package sheet composes inspection images: labeled grids of tiles (contact sheets),
// side-by-side strips, text drawn with the built-in font, and deterministic resizing.
package sheet

import (
	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/sprite"
)

// Colors used by sheets.
const (
	Background = 0xff15181d
	LabelFG    = 0xffe8e8e8
	LabelBG    = 0xc0000000
)

// Text draws s at pixel (x, y) with the built-in 8×8 font scaled by an integer factor
// and returns the drawn width. Newlines start a new line.
func Text(img *gfx.Image, x, y, scale int, s string, color uint32) int {
	f := sprite.DefaultFont()
	if scale < 1 {
		scale = 1
	}
	cx, widest := x, 0
	for _, r := range s {
		if r == '\n' {
			cx = x
			y += f.CellH * scale
			continue
		}
		g := f.Glyph(r)
		if r != ' ' {
			for gy := 0; gy < g.Dy(); gy++ {
				for gx := 0; gx < g.Dx(); gx++ {
					if f.Atlas.At(g.Min.X+gx, g.Min.Y+gy)>>24 < 128 {
						continue
					}
					for sy := 0; sy < scale; sy++ {
						for sx := 0; sx < scale; sx++ {
							img.Set(cx+gx*scale+sx, y+gy*scale+sy, color)
						}
					}
				}
			}
		}
		cx += f.CellW * scale
		widest = max(widest, cx-x)
	}
	return widest
}

// TextWidth returns the width of s in pixels at scale.
func TextWidth(s string, scale int) int {
	w, _ := sprite.MeasureText(sprite.DefaultFont(), s, max(scale, 1))
	return w
}

// Label draws s over a translucent dark box for legibility on any background.
func Label(img *gfx.Image, x, y, scale int, s string) {
	w, h := sprite.MeasureText(sprite.DefaultFont(), s, max(scale, 1))
	FillRect(img, x-2, y-2, w+3, h+3, LabelBG)
	Text(img, x, y, scale, s, LabelFG)
}

// FillRect blends a w×h rectangle of color c (straight alpha) at (x, y).
func FillRect(img *gfx.Image, x, y, w, h int, c uint32) {
	a := c >> 24
	for py := max(y, 0); py < min(y+h, img.H); py++ {
		for px := max(x, 0); px < min(x+w, img.W); px++ {
			i := py*img.W + px
			img.Pix[i] = blend(img.Pix[i], c, a)
		}
	}
}

func blend(dst, src, a uint32) uint32 {
	if a == 255 {
		return src
	}
	ia := 255 - a
	r := ((src>>16&0xff)*a + (dst>>16&0xff)*ia + 127) / 255
	g := ((src>>8&0xff)*a + (dst>>8&0xff)*ia + 127) / 255
	b := ((src&0xff)*a + (dst&0xff)*ia + 127) / 255
	return 0xff000000 | r<<16 | g<<8 | b
}

// Resize returns img scaled to w×h: area averaging when shrinking, nearest neighbour when
// enlarging. Integer math only, so the result is deterministic.
func Resize(img *gfx.Image, w, h int) *gfx.Image {
	out := gfx.NewImage(w, h)
	if img.W == 0 || img.H == 0 {
		return out
	}
	for y := 0; y < h; y++ {
		y0 := y * img.H / h
		y1 := max((y+1)*img.H/h, y0+1)
		for x := 0; x < w; x++ {
			x0 := x * img.W / w
			x1 := max((x+1)*img.W/w, x0+1)
			var sum [4]uint32
			n := uint32(0)
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					c := img.Pix[sy*img.W+sx]
					sum[0] += c & 0xff
					sum[1] += c >> 8 & 0xff
					sum[2] += c >> 16 & 0xff
					sum[3] += c >> 24
					n++
				}
			}
			out.Pix[y*w+x] = (sum[3]+n/2)/n<<24 | (sum[2]+n/2)/n<<16 | (sum[1]+n/2)/n<<8 | (sum[0]+n/2)/n
		}
	}
	return out
}

// Fit scales img to fit inside w×h keeping its aspect ratio.
func Fit(img *gfx.Image, w, h int) *gfx.Image {
	if img.W <= w && img.H <= h {
		return img
	}
	nw, nh := w, img.H*w/img.W
	if nh > h {
		nh, nw = h, img.W*h/img.H
	}
	return Resize(img, max(nw, 1), max(nh, 1))
}

// Grid lays out tiles in cols columns, each cell as large as the largest tile, separated
// by pad pixels, with labels[i] drawn in the bottom-left corner of cell i ("" for none),
// away from game HUDs, which usually sit at the top.
func Grid(tiles []*gfx.Image, labels []string, cols, pad int) *gfx.Image {
	if len(tiles) == 0 {
		return gfx.NewImage(1, 1)
	}
	cols = max(1, min(cols, len(tiles)))
	rows := (len(tiles) + cols - 1) / cols
	cw, ch := 0, 0
	for _, t := range tiles {
		cw, ch = max(cw, t.W), max(ch, t.H)
	}
	out := gfx.NewImage(cols*cw+(cols+1)*pad, rows*ch+(rows+1)*pad)
	out.Fill(Background)
	for i, t := range tiles {
		x := pad + (i%cols)*(cw+pad)
		y := pad + (i/cols)*(ch+pad)
		out.Blit(t, x+(cw-t.W)/2, y+(ch-t.H)/2)
		if i < len(labels) && labels[i] != "" {
			Label(out, x+3, y+ch-11, 1, labels[i])
		}
	}
	return out
}

// HStack places images side by side, top aligned, separated by pad pixels.
func HStack(pad int, imgs ...*gfx.Image) *gfx.Image {
	w, h := pad, 0
	for _, m := range imgs {
		w += m.W + pad
		h = max(h, m.H)
	}
	out := gfx.NewImage(w, h+2*pad)
	out.Fill(Background)
	x := pad
	for _, m := range imgs {
		out.Blit(m, x, pad)
		x += m.W + pad
	}
	return out
}
