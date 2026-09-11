package soft

import "github.com/riftbane/veduta/gfx"

type texture struct {
	levels []level
	wrap   gfx.Wrap
	fast   bool // repeat wrap and power-of-two size: wrapping is a mask
}

type level struct {
	w, h         int32
	fw, fh       float32
	wmask, hmask int32 // w-1 / h-1 for power-of-two sizes, else -1
	pix          []uint32
}

func newLevel(img *gfx.Image) level {
	l := level{w: int32(img.W), h: int32(img.H), fw: float32(img.W), fh: float32(img.H), wmask: -1, hmask: -1, pix: img.Pix}
	if img.W&(img.W-1) == 0 {
		l.wmask = l.w - 1
	}
	if img.H&(img.H-1) == 0 {
		l.hmask = l.h - 1
	}
	return l
}

// floorf is an exact float32 floor that avoids a float64 round trip. Values with
// |x| >= 2^23 are already integers (or NaN/Inf) and are returned unchanged.
func floorf(x float32) float32 {
	if x > -8388608 && x < 8388608 {
		i := float32(int32(x))
		if i > x {
			i--
		}
		return i
	}
	return x
}

func wrapCoord(x, size, mask int32, wrap gfx.Wrap) int32 {
	if wrap == gfx.WrapClamp {
		if x < 0 {
			return 0
		}
		if x >= size {
			return size - 1
		}
		return x
	}
	if mask >= 0 {
		return x & mask
	}
	x %= size
	if x < 0 {
		x += size
	}
	return x
}

func (t *texture) nearest(lv int32, u, v float32) uint32 {
	l := &t.levels[lv]
	x := wrapCoord(int32(floorf(u*l.fw)), l.w, l.wmask, t.wrap)
	y := wrapCoord(int32(floorf(v*l.fh)), l.h, l.hmask, t.wrap)
	return l.pix[y*l.w+x]
}

func (t *texture) bilinear(lv int32, u, v float32) uint32 {
	l := &t.levels[lv]
	fu := u*l.fw - 0.5
	fv := v*l.fh - 0.5
	xf, yf := floorf(fu), floorf(fv)
	fx := uint32((fu - xf) * 256)
	fy := uint32((fv - yf) * 256)
	fx, fy = min(fx, 255), min(fy, 255)
	xi, yi := int32(xf), int32(yf)
	var x0, x1, r0, r1 int32
	if t.fast {
		x0, x1 = xi&l.wmask, (xi+1)&l.wmask
		r0, r1 = (yi&l.hmask)*l.w, ((yi+1)&l.hmask)*l.w
	} else {
		x0 = wrapCoord(xi, l.w, l.wmask, t.wrap)
		x1 = wrapCoord(xi+1, l.w, l.wmask, t.wrap)
		r0 = wrapCoord(yi, l.h, l.hmask, t.wrap) * l.w
		r1 = wrapCoord(yi+1, l.h, l.hmask, t.wrap) * l.w
	}
	pix := l.pix
	top := lerpPacked(pix[r0+x0], pix[r0+x1], fx)
	bot := lerpPacked(pix[r1+x0], pix[r1+x1], fx)
	return lerpPacked(top, bot, fy)
}

// lerpPacked blends two BGRA8 colors with weight f/256 for b, two channels at a time.
func lerpPacked(a, b, f uint32) uint32 {
	g := 256 - f
	rb := ((a&0x00ff00ff)*g + (b&0x00ff00ff)*f) >> 8 & 0x00ff00ff
	ga := ((a>>8&0x00ff00ff)*g + (b>>8&0x00ff00ff)*f) & 0xff00ff00
	return rb | ga
}

// uvChecker is the procedural texture of ModeUVChecker: an 8×8 checkerboard per UV unit
// whose red channel grows with u and green with v, so orientation, stretching and seams
// are all visible.
func uvChecker(u, v float32) uint32 {
	fu, fv := u-floorf(u), v-floorf(v)
	base := float32(235)
	if (int32(fu*8)+int32(fv*8))&1 != 0 {
		base = 120
	}
	r := uint32(base * (0.45 + 0.55*fu))
	g := uint32(base * (0.45 + 0.55*fv))
	b := uint32(base * 0.75)
	return 0xff000000 | r<<16 | g<<8 | b
}
