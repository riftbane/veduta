package texture

import (
	"math"
	"runtime"
	"sync"

	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/gmath"
)

// blendMode selects how a layer's color combines with the canvas.
type blendMode uint8

const (
	blendNormal blendMode = iota
	blendMultiply
	blendScreen
	blendAdd
)

// ss is the supersampling grid per axis: shapes and stripe edges use ss×ss samples per
// pixel at offsets (i+0.5)/ss.
const ss = 4

// ssMargin bounds the distance from a pixel center to its farthest sample,
// √2·(0.5 − 0.5/ss) ≈ 0.530, plus a safety margin: when a signed distance at the center
// is beyond it, every sample agrees with the center and the pixel is not supersampled.
const ssMargin = 0.54

// painter evaluates one layer. at returns the layer's straight RGB and its alpha before
// opacity (color alpha × coverage) at pixel (x, y). Painters are read-only after
// construction and safe for concurrent use.
type painter interface {
	at(x, y int) (rgb [3]float32, a float32)
}

// pass is one layer ready to composite.
type pass struct {
	p       painter
	opacity float32
	blend   blendMode
}

// render runs the layer program of s, leaving out the layers marked in skip, and returns
// the quantized image. Rows are split between goroutines; every pixel is computed
// independently, so the result does not depend on scheduling.
func render(s *spec, skip []bool) *gfx.Image {
	var passes []pass
	for i := range s.layers {
		if skip[i] {
			continue
		}
		l := &s.layers[i]
		passes = append(passes, pass{p: newPainter(s, l), opacity: l.opacity, blend: l.blend})
	}
	img := gfx.NewImage(s.w, s.h)
	workers := max(1, min(runtime.GOMAXPROCS(0), s.h))
	var wg sync.WaitGroup
	for k := 0; k < workers; k++ {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			for y := k; y < s.h; y += workers {
				row := img.Pix[y*s.w : (y+1)*s.w]
				for x := range row {
					var px [4]float32
					for i := range passes {
						ps := &passes[i]
						rgb, a := ps.p.at(x, y)
						composite(&px, rgb, float32(a*ps.opacity), ps.blend)
					}
					row[x] = pack(px)
				}
			}
		}(k)
	}
	wg.Wait()
	return img
}

// composite applies a layer with straight color s and effective alpha a to the canvas
// pixel d (straight RGB + alpha). It is W3C source-over compositing with a separable
// blend function B:
//
//	co = a·(1−dA)·s + a·dA·B(d, s) + (1−a)·dA·d,  αo = a + dA·(1−a),  rgb = co/αo
//
// which on an opaque canvas (dA = 1) is exactly rgb = d + (B(d, s) − d)·a, and over a
// transparent canvas (dA = 0) gives rgb = s, so partly transparent layers never pick up
// the canvas's initial black.
func composite(d *[4]float32, s [3]float32, a float32, mode blendMode) {
	if !(a > 0) {
		return
	}
	if a > 1 {
		a = 1
	}
	da := d[3]
	if da == 0 {
		d[0], d[1], d[2], d[3] = s[0], s[1], s[2], a
		return
	}
	var b [3]float32
	for i := 0; i < 3; i++ {
		b[i] = blendFn(mode, d[i], s[i])
	}
	if da == 1 {
		for i := 0; i < 3; i++ {
			d[i] += float32((b[i] - d[i]) * a)
		}
		return
	}
	ao := a + float32(da*(1-a))
	for i := 0; i < 3; i++ {
		co := float32(float32(a*(1-da))*s[i]) + float32(float32(a*da)*b[i]) + float32(float32((1-a)*da)*d[i])
		d[i] = co / ao
	}
	d[3] = min(ao, 1)
}

// blendFn is the blend function B(d, s) of one channel.
func blendFn(mode blendMode, d, s float32) float32 {
	switch mode {
	case blendMultiply:
		return d * s
	case blendScreen:
		return 1 - float32((1-d)*(1-s))
	case blendAdd:
		return min(1, d+s)
	}
	return s
}

// pack quantizes a canvas pixel to BGRA8: each channel clamped to [0, 1], times 255,
// rounded half up.
func pack(px [4]float32) uint32 {
	return gfx.RGBA(q8(px[0]), q8(px[1]), q8(px[2]), q8(px[3]))
}

func q8(x float32) uint8 {
	if !(x > 0) {
		return 0
	}
	if x >= 1 {
		return 255
	}
	return uint8(float32(x*255) + 0.5)
}

// newPainter builds the painter of a validated layer.
func newPainter(s *spec, l *layerSpec) painter {
	switch l.typ {
	case "solid":
		return &solid{rgb: rgbOf(l.color), a: l.color[3]}
	case "noise":
		return &noise{rgb: rgbOf(l.color), a: l.color[3], f: newNoiseField(s, l)}
	case "stripes":
		cos, sin := direction(l.angle)
		return &stripes{colors: l.colors, cos: cos, sin: sin, width: l.width}
	case "rect":
		// The halves are rounded explicitly so arm64 cannot fuse them into the sums below.
		hx, hy := float64(l.size[0]/2), float64(l.size[1]/2)
		r := min(l.corner, hx, hy)
		sh := &shape{rgb: rgbOf(l.color), a: l.color[3],
			outer: roundRect{cx: l.xy[0] + hx, cy: l.xy[1] + hy, hx: hx, hy: hy, r: r}}
		if o := l.outline; o > 0 && o < hx && o < hy {
			sh.inner = roundRect{cx: sh.outer.cx, cy: sh.outer.cy, hx: hx - o, hy: hy - o, r: max(r-o, 0)}
			sh.hole = true
		}
		return sh
	case "circle":
		r := l.radius
		sh := &shape{rgb: rgbOf(l.color), a: l.color[3],
			outer: roundRect{cx: l.center[0], cy: l.center[1], hx: r, hy: r, r: r}}
		if o := l.outline; o > 0 && o < r {
			sh.inner = roundRect{cx: l.center[0], cy: l.center[1], hx: r - o, hy: r - o, r: r - o}
			sh.hole = true
		}
		return sh
	case "gradient":
		return newGradient(s, l)
	case "checker":
		return &checker{c0: l.colors[0], c1: l.colors[1], n: l.cells, w: s.w, h: s.h}
	case "image":
		return newImagePainter(s, l)
	}
	panic("texture: unknown layer type " + l.typ)
}

func rgbOf(c rgba) [3]float32 { return [3]float32{c[0], c[1], c[2]} }

// direction returns the unit vector at angle deg in pixel space (0° = +x, right; 90° =
// +y, down). Multiples of 90° are exact.
func direction(deg float64) (cos, sin float64) {
	if q := math.Mod(deg, 90); q == 0 {
		switch int(math.Mod(deg/90, 4)+4) % 4 {
		case 0:
			return 1, 0
		case 1:
			return 0, 1
		case 2:
			return -1, 0
		default:
			return 0, -1
		}
	}
	sin, cos = gmath.SinCos64(deg * (math.Pi / 180))
	return cos, sin
}

// premulAvg averages ss×ss premultiplied samples: acc holds the sums of a·rgb and of a.
// It returns the straight color and the mean alpha.
func premulAvg(acc [4]float64, n int) ([3]float32, float32) {
	if !(acc[3] > 0) {
		return [3]float32{}, 0
	}
	inv := 1 / acc[3]
	return [3]float32{clamp01(acc[0] * inv), clamp01(acc[1] * inv), clamp01(acc[2] * inv)}, float32(acc[3] / float64(n))
}

func clamp01(x float64) float32 {
	switch {
	case !(x > 0):
		return 0
	case x >= 1:
		return 1
	}
	return float32(x)
}

// solid is a constant color.
type solid struct {
	rgb [3]float32
	a   float32
}

func (p *solid) at(x, y int) ([3]float32, float32) { return p.rgb, p.a }

// noise is a constant color whose alpha is multiplied by a value-noise field.
type noise struct {
	rgb [3]float32
	a   float32
	f   *noiseField
}

func (p *noise) at(x, y int) ([3]float32, float32) { return p.rgb, p.a * p.f.at(x, y) }

// stripes are parallel bands of width pixels measured along the direction (cos, sin)
// from the texture origin; band k has color k mod len(colors).
type stripes struct {
	colors   []rgba
	cos, sin float64
	width    float64
}

func (p *stripes) band(x, y float64) float64 {
	return math.Floor((float64(x*p.cos) + float64(y*p.sin)) / p.width)
}

func (p *stripes) color(k float64) rgba {
	n := float64(len(p.colors))
	i := math.Mod(k, n)
	if i < 0 {
		i += n
	}
	return p.colors[min(int(i), len(p.colors)-1)]
}

func (p *stripes) at(x, y int) ([3]float32, float32) {
	fx, fy := float64(x), float64(y)
	// The band index is monotonic along the direction, so when the pixel's four corners
	// are in one band every sample is too.
	k0, k1 := p.band(fx, fy), p.band(fx+1, fy)
	k2, k3 := p.band(fx, fy+1), p.band(fx+1, fy+1)
	if k0 == k1 && k0 == k2 && k0 == k3 {
		c := p.color(k0)
		return rgbOf(c), c[3]
	}
	var acc [4]float64
	first, same := rgba{}, true
	for j := 0; j < ss; j++ {
		sy := fy + float64((float64(j)+0.5)/ss)
		for i := 0; i < ss; i++ {
			c := p.color(p.band(fx+float64((float64(i)+0.5)/ss), sy))
			if i == 0 && j == 0 {
				first = c
			} else if c != first {
				same = false
			}
			a := float64(c[3])
			acc[0] += float64(a * float64(c[0]))
			acc[1] += float64(a * float64(c[1]))
			acc[2] += float64(a * float64(c[2]))
			acc[3] += a
		}
	}
	if same {
		return rgbOf(first), first[3]
	}
	return premulAvg(acc, ss*ss)
}

// roundRect is a rectangle with rounded corners: center (cx, cy), half extents (hx, hy)
// and corner radius r ≤ min(hx, hy). A circle is a roundRect with hx = hy = r.
type roundRect struct{ cx, cy, hx, hy, r float64 }

// dist is the exact signed Euclidean distance from (x, y) to the shape's outline
// (negative inside).
func (s *roundRect) dist(x, y float64) float64 {
	qx := math.Abs(x-s.cx) - (s.hx - s.r)
	qy := math.Abs(y-s.cy) - (s.hy - s.r)
	ox, oy := max(qx, 0), max(qy, 0)
	return math.Sqrt(float64(ox*ox)+float64(oy*oy)) + min(max(qx, qy), 0) - s.r
}

// shape is a filled or outlined rectangle or circle with ss×ss supersampled coverage. A
// sample is covered when it lies inside outer (distance ≤ 0) and, for outlines, not
// strictly inside inner.
type shape struct {
	rgb   [3]float32
	a     float32
	outer roundRect
	inner roundRect
	hole  bool
}

func (p *shape) covered(x, y float64) bool {
	return p.outer.dist(x, y) <= 0 && !(p.hole && p.inner.dist(x, y) < 0)
}

func (p *shape) at(x, y int) ([3]float32, float32) {
	fx, fy := float64(x), float64(y)
	do := p.outer.dist(fx+0.5, fy+0.5)
	if do > ssMargin {
		return p.rgb, 0
	}
	di := math.Inf(1)
	if p.hole {
		di = p.inner.dist(fx+0.5, fy+0.5)
		if di < -ssMargin {
			return p.rgb, 0
		}
	}
	if do < -ssMargin && di > ssMargin {
		return p.rgb, p.a
	}
	n := 0
	for j := 0; j < ss; j++ {
		sy := fy + float64((float64(j)+0.5)/ss)
		for i := 0; i < ss; i++ {
			if p.covered(fx+float64((float64(i)+0.5)/ss), sy) {
				n++
			}
		}
	}
	return p.rgb, p.a * float32(n) / (ss * ss)
}

// gradient interpolates from → to along a direction across the texture: t = 0 at the
// pixel center that is farthest back along the direction, t = 1 at the one farthest
// forward. Colors are interpolated with premultiplied alpha.
type gradient struct {
	from, to rgba
	cos, sin float64
	t0, span float64
}

func newGradient(s *spec, l *layerSpec) *gradient {
	cos, sin := direction(l.angle)
	g := &gradient{from: l.from, to: l.to, cos: cos, sin: sin}
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, c := range [4][2]float64{{0.5, 0.5}, {float64(s.w) - 0.5, 0.5}, {0.5, float64(s.h) - 0.5}, {float64(s.w) - 0.5, float64(s.h) - 0.5}} {
		t := float64(c[0]*cos) + float64(c[1]*sin)
		lo, hi = min(lo, t), max(hi, t)
	}
	g.t0, g.span = lo, hi-lo
	return g
}

func (p *gradient) at(x, y int) ([3]float32, float32) {
	var t float32
	if p.span > 0 {
		t = clamp01((float64((float64(x)+0.5)*p.cos) + float64((float64(y)+0.5)*p.sin) - p.t0) / p.span)
	}
	u := 1 - t
	c0, c1 := p.from, p.to
	if c0[3] == c1[3] {
		return [3]float32{
			float32(c0[0]*u) + float32(c1[0]*t),
			float32(c0[1]*u) + float32(c1[1]*t),
			float32(c0[2]*u) + float32(c1[2]*t),
		}, c0[3]
	}
	a := float32(c0[3]*u) + float32(c1[3]*t)
	if !(a > 0) {
		return [3]float32{}, 0
	}
	var rgb [3]float32
	for i := 0; i < 3; i++ {
		rgb[i] = min((float32(float32(c0[i]*c0[3])*u)+float32(float32(c1[i]*c1[3])*t))/a, 1)
	}
	return rgb, a
}

// checker is an n×n grid of cells over the whole texture; each pixel takes the color of
// the cell containing its center, c0 when column + row is even.
type checker struct {
	c0, c1 rgba
	n      int
	w, h   int
}

func (p *checker) at(x, y int) ([3]float32, float32) {
	col := (2*x + 1) * p.n / (2 * p.w)
	row := (2*y + 1) * p.n / (2 * p.h)
	c := p.c0
	if (col+row)&1 == 1 {
		c = p.c1
	}
	return rgbOf(c), c[3]
}
