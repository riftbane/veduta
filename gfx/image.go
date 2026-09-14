package gfx

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"strconv"

	"github.com/riftbane/veduta/gmath"
)

// Colors are BGRA8 packed in a uint32 as 0xAARRGGBB, which is B, G, R, A in little-endian
// memory: the layout of a 32-bit Linux framebuffer, so the player stores such a pixel as
// it stands (the console's 16-bit panel is packed to RGB565 instead).

// RGBA packs 8-bit channels into a color.
func RGBA(r, g, b, a uint8) uint32 {
	return uint32(a)<<24 | uint32(r)<<16 | uint32(g)<<8 | uint32(b)
}

// UnpackRGBA splits a color into 8-bit channels.
func UnpackRGBA(c uint32) (r, g, b, a uint8) {
	return uint8(c >> 16), uint8(c >> 8), uint8(c), uint8(c >> 24)
}

// ParseColor parses "#RRGGBB" (opaque) or "#RRGGBBAA".
func ParseColor(s string) (uint32, error) {
	if len(s) != 7 && len(s) != 9 || s[0] != '#' {
		return 0, fmt.Errorf("color %q: want #RRGGBB or #RRGGBBAA", s)
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return 0, fmt.Errorf("color %q: invalid hex digits", s)
	}
	if len(s) == 7 {
		return 0xff000000 | uint32(v), nil
	}
	return uint32(v)>>8 | uint32(v)<<24, nil
}

// FormatColor formats c as "#rrggbb" when opaque, else "#rrggbbaa".
func FormatColor(c uint32) string {
	r, g, b, a := UnpackRGBA(c)
	if a == 0xff {
		return fmt.Sprintf("#%02x%02x%02x", r, g, b)
	}
	return fmt.Sprintf("#%02x%02x%02x%02x", r, g, b, a)
}

// ColorVec4 converts a packed color to RGBA components in [0, 1].
func ColorVec4(c uint32) gmath.Vec4 {
	r, g, b, a := UnpackRGBA(c)
	return gmath.Vec4{X: float32(r) / 255, Y: float32(g) / 255, Z: float32(b) / 255, W: float32(a) / 255}
}

// ColorVec3 converts the RGB part of a packed color to components in [0, 1].
func ColorVec3(c uint32) gmath.Vec3 { return ColorVec4(c).XYZ() }

// Vec4Color converts RGBA components in [0, 1] to a packed color, clamping and rounding.
func Vec4Color(v gmath.Vec4) uint32 {
	return RGBA(unorm8(v.X), unorm8(v.Y), unorm8(v.Z), unorm8(v.W))
}

func unorm8(x float32) uint8 {
	if !(x > 0) {
		return 0
	}
	if x >= 1 {
		return 255
	}
	return uint8(float32(x*255) + 0.5) // rounded explicitly: arm64 must not fuse a multiply-add
}

// Image is a BGRA8 image stored row-major, top row first.
type Image struct {
	W, H int
	Pix  []uint32
}

// NewImage allocates a transparent black w×h image.
func NewImage(w, h int) *Image { return &Image{W: w, H: h, Pix: make([]uint32, w*h)} }

// At returns the pixel at (x, y); out-of-range coordinates return 0.
func (m *Image) At(x, y int) uint32 {
	if x < 0 || y < 0 || x >= m.W || y >= m.H {
		return 0
	}
	return m.Pix[y*m.W+x]
}

// Set writes the pixel at (x, y); out-of-range coordinates are ignored.
func (m *Image) Set(x, y int, c uint32) {
	if x < 0 || y < 0 || x >= m.W || y >= m.H {
		return
	}
	m.Pix[y*m.W+x] = c
}

// Fill sets every pixel to c.
func (m *Image) Fill(c uint32) {
	for i := range m.Pix {
		m.Pix[i] = c
	}
}

// Clone returns a deep copy.
func (m *Image) Clone() *Image {
	return &Image{W: m.W, H: m.H, Pix: append([]uint32(nil), m.Pix...)}
}

// Blit copies src into m with its top-left corner at (x, y), clipping to m.
func (m *Image) Blit(src *Image, x, y int) {
	for sy := 0; sy < src.H; sy++ {
		dy := y + sy
		if dy < 0 || dy >= m.H {
			continue
		}
		for sx := 0; sx < src.W; sx++ {
			dx := x + sx
			if dx < 0 || dx >= m.W {
				continue
			}
			m.Pix[dy*m.W+dx] = src.Pix[sy*src.W+sx]
		}
	}
}

// NRGBA converts the image to a standard library image (straight alpha).
func (m *Image) NRGBA() *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, m.W, m.H))
	for i, c := range m.Pix {
		r, g, b, a := UnpackRGBA(c)
		o := i * 4
		out.Pix[o], out.Pix[o+1], out.Pix[o+2], out.Pix[o+3] = r, g, b, a
	}
	return out
}

// FromImage converts any standard library image to an Image with straight alpha.
func FromImage(src image.Image) *Image {
	b := src.Bounds()
	m := NewImage(b.Dx(), b.Dy())
	for y := 0; y < m.H; y++ {
		for x := 0; x < m.W; x++ {
			c := color.NRGBAModel.Convert(src.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			m.Pix[y*m.W+x] = RGBA(c.R, c.G, c.B, c.A)
		}
	}
	return m
}

// EncodePNG writes the image as PNG. The output is a pure function of the pixels for a
// given Go release.
func (m *Image) EncodePNG(w io.Writer) error {
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	return enc.Encode(w, m.NRGBA())
}

// DecodePNG reads a PNG into an Image.
func DecodePNG(r io.Reader) (*Image, error) {
	src, err := png.Decode(r)
	if err != nil {
		return nil, err
	}
	return FromImage(src), nil
}

// BuildMips returns the mip chain for base: base itself followed by successively halved
// levels down to 1×1. Each texel of level n+1 averages the 2×2 block of level n (odd
// dimensions clamp the last row/column) with premultiplied alpha, so the color hidden in
// fully transparent texels never bleeds into visible ones. Integer math: deterministic.
func BuildMips(base *Image) []*Image {
	levels := []*Image{base}
	cur := base
	for cur.W > 1 || cur.H > 1 {
		w, h := max(cur.W/2, 1), max(cur.H/2, 1)
		next := NewImage(w, h)
		for y := 0; y < h; y++ {
			y0, y1 := min(2*y, cur.H-1), min(2*y+1, cur.H-1)
			for x := 0; x < w; x++ {
				x0, x1 := min(2*x, cur.W-1), min(2*x+1, cur.W-1)
				next.Pix[y*w+x] = avg4(cur.Pix[y0*cur.W+x0], cur.Pix[y0*cur.W+x1], cur.Pix[y1*cur.W+x0], cur.Pix[y1*cur.W+x1])
			}
		}
		levels = append(levels, next)
		cur = next
	}
	return levels
}

// avg4 averages four straight-alpha colors with premultiplied weights: alpha is the
// plain average, color channels are Σ(c·a)/Σa (all texels weigh the same when opaque).
func avg4(a, b, c, d uint32) uint32 {
	px := [4]uint32{a, b, c, d}
	var sa uint32
	for _, p := range px {
		sa += p >> 24
	}
	out := ((sa + 2) / 4) << 24
	if sa == 0 {
		return out
	}
	for s := 0; s < 24; s += 8 {
		var sum uint32
		for _, p := range px {
			sum += (p >> s & 0xff) * (p >> 24)
		}
		out |= ((sum + sa/2) / sa) << s
	}
	return out
}
