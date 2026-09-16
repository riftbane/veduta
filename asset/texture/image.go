package texture

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"
	"io/fs"
	"math"

	"github.com/riftbane/veduta/v2/gfx"
)

// loadImage reads and decodes the PNG at p (a valid path) from fsys, refusing files
// that are not PNG and images larger than MaxImageSize per side before decoding them.
func loadImage(fsys fs.FS, p string) (*gfx.Image, error) {
	data, err := fs.ReadFile(fsys, p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("%q: file not found in the assets directory", p)
	case err != nil:
		return nil, fmt.Errorf("%q: %v", p, err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%q: not a PNG image (%v)", p, err)
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > MaxImageSize || cfg.Height > MaxImageSize {
		return nil, fmt.Errorf("%q: image is %dx%d, want 1 to %d pixels per side", p, cfg.Width, cfg.Height, MaxImageSize)
	}
	img, err := gfx.DecodePNG(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%q: decode PNG: %v", p, err)
	}
	return img, nil
}

// imagePainter draws a PNG image placed at (ox, oy) with size dw × dh texture pixels.
//
// Coverage is the exact area of the pixel square covered by the placed image (so
// letterbox edges are anti-aliased), times the image's own alpha. The color averages
// kx × ky bilinear samples spread evenly over the pixel (kx = ceil of the horizontal
// shrink factor, 1 when enlarging), clamped into the placed image, with premultiplied
// alpha so transparent texels do not darken their neighbours.
type imagePainter struct {
	img            *gfx.Image
	ox, oy, dw, dh float64
	fx, fy         float64 // image texels per texture pixel
	kx, ky         int
}

func newImagePainter(s *spec, l *layerSpec) *imagePainter {
	W, H := float64(s.w), float64(s.h)
	iw, ih := float64(l.img.W), float64(l.img.H)
	p := &imagePainter{img: l.img}
	switch l.fit {
	case "stretch":
		p.dw, p.dh = W, H
	default:
		k := min(W/iw, H/ih)
		if l.fit == "cover" {
			k = max(W/iw, H/ih)
		}
		// Products and halves are rounded explicitly so arm64 cannot fuse them into a
		// multiply-add (see gmath.m32).
		p.dw, p.dh = float64(iw*k), float64(ih*k)
		p.ox, p.oy = float64((W-p.dw)/2), float64((H-p.dh)/2)
	}
	p.fx, p.fy = iw/p.dw, ih/p.dh
	p.kx = max(1, int(math.Ceil(p.fx)))
	p.ky = max(1, int(math.Ceil(p.fy)))
	return p
}

// overlap returns the length of [a, a+1] ∩ [lo, lo+n].
func overlap(a, lo, n float64) float64 {
	return max(0, min(a+1, lo+n)-max(a, lo))
}

func (p *imagePainter) at(x, y int) ([3]float32, float32) {
	fx, fy := float64(x), float64(y)
	cov := overlap(fx, p.ox, p.dw) * overlap(fy, p.oy, p.dh)
	if !(cov > 0) {
		return [3]float32{}, 0
	}
	var acc [4]float64
	for j := 0; j < p.ky; j++ {
		sy := min(max(fy+(float64(j)+0.5)/float64(p.ky), p.oy), p.oy+p.dh)
		v := float64((sy-p.oy)*p.fy) - 0.5
		for i := 0; i < p.kx; i++ {
			sx := min(max(fx+(float64(i)+0.5)/float64(p.kx), p.ox), p.ox+p.dw)
			u := float64((sx-p.ox)*p.fx) - 0.5
			c := p.bilinear(u, v)
			for k := range acc {
				acc[k] += c[k]
			}
		}
	}
	rgb, a := premulAvg(acc, p.kx*p.ky)
	return rgb, a * float32(cov)
}

// bilinear samples the image at texel coordinates (u, v) (texel centers at integers)
// with clamp-to-edge addressing and returns premultiplied RGBA.
func (p *imagePainter) bilinear(u, v float64) [4]float64 {
	fu, fv := math.Floor(u), math.Floor(v)
	tu, tv := u-fu, v-fv
	i0, j0 := int(fu), int(fv)
	c00 := p.texel(i0, j0)
	c10 := p.texel(i0+1, j0)
	c01 := p.texel(i0, j0+1)
	c11 := p.texel(i0+1, j0+1)
	var out [4]float64
	for k := range out {
		top := c00[k] + float64((c10[k]-c00[k])*tu)
		bottom := c01[k] + float64((c11[k]-c01[k])*tu)
		out[k] = top + float64((bottom-top)*tv)
	}
	return out
}

// texel returns the premultiplied color of texel (i, j), clamped to the image.
func (p *imagePainter) texel(i, j int) [4]float64 {
	i = min(max(i, 0), p.img.W-1)
	j = min(max(j, 0), p.img.H-1)
	r, g, b, a8 := gfx.UnpackRGBA(p.img.Pix[j*p.img.W+i])
	a := float64(a8) / 255
	return [4]float64{float64(float64(r) / 255 * a), float64(float64(g) / 255 * a), float64(float64(b) / 255 * a), a}
}
