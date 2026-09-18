package tilemap

import (
	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/gfx"
)

// A terrain's border over a lower neighbour is drawn in the four quarters of the lower
// cell, each quarter with one of four shapes: a band along its horizontal side (the
// terrain is above or below), a band along its vertical side (left or right), both bands
// (an inner corner) or a quarter disk at its corner (the terrain touches only the
// diagonal). The edge atlas holds the 16 quarter images, the terrain's pixels cut by the
// shape, for every frame of the texture.

// Shapes of a quarter's border, the columns of the edge atlas.
const (
	shapeH      = iota // band along the horizontal side
	shapeV             // band along the vertical side
	shapeL             // both bands
	shapeCorner        // quarter disk at the corner
	shapes
)

// Quarters of a cell, the rows of the edge atlas.
const (
	quarterNW = iota
	quarterNE
	quarterSW
	quarterSE
	quarters
)

// Sides of a cell, each with its own wandering.
const (
	sideN = iota
	sideS
	sideW
	sideE
)

// wanderSteps is the number of segments of a side's wandering: its offset is 0 at both
// ends (the cell's corners, where borders meet) and random in between.
const wanderSteps = 4

// hash32 mixes a seed, a side and a step into 32 bits (the finalizer of MurmurHash3).
func hash32(seed int64, side, step int) uint32 {
	h := uint32(seed) ^ uint32(side)*0x9e3779b1 ^ uint32(step)*0x85ebca77
	h ^= h >> 16
	h *= 0x7feb352d
	h ^= h >> 15
	h *= 0x846ca68b
	h ^= h >> 16
	return h
}

// wander returns the offset in [-1, 1] of side at t in [0, 1] along it: 0 at t = 0 and
// t = 1, smoothly interpolated between seeded values at the steps.
func wander(seed int64, side int, t float64) float64 {
	x := float64(t * wanderSteps) // products rounded: arm64 must not fuse them
	i := int(x)
	if i >= wanderSteps {
		i = wanderSteps - 1
	}
	f := x - float64(i)
	v := func(k int) float64 {
		if k == 0 || k == wanderSteps {
			return 0
		}
		return float64(int64(hash32(seed, side, k)>>8)*2-(1<<24)) / (1 << 24) // exact: -1 to 1
	}
	a, b := v(i), v(i+1)
	s := f * f * (3 - float64(2*f))
	return a + float64((b-a)*s)
}

// reach returns how far the border along side reaches into the cell at t along it, in
// pixels: width wandering by roughness, within half the cell.
func reach(e *asset.Edge, side int, t, half float64) float64 {
	w := float64(e.Width)
	r := w * (1 + float64(float64(e.Roughness)*wander(e.Seed, side, t)))
	return min(max(r, 0), half)
}

// inShape reports whether pixel (px, py) of a fw × fh frame is covered by the border
// shape s of quarter q.
func inShape(e *asset.Edge, s, q, px, py, fw, fh int) bool {
	cx, cy := float64(px)+0.5, float64(py)+0.5
	w, h := float64(fw), float64(fh)
	half := min(w, h) / 2
	top := q == quarterNW || q == quarterNE
	left := q == quarterNW || q == quarterSW
	horizontal := func() bool { // the band along the top or bottom side
		if top {
			return cy < reach(e, sideN, cx/w, half)
		}
		return h-cy < reach(e, sideS, cx/w, half)
	}
	vertical := func() bool { // the band along the left or right side
		if left {
			return cx < reach(e, sideW, cy/h, half)
		}
		return w-cx < reach(e, sideE, cy/h, half)
	}
	switch s {
	case shapeH:
		return horizontal()
	case shapeV:
		return vertical()
	case shapeL:
		return horizontal() || vertical()
	}
	ox, oy := 0.0, 0.0 // the quarter's outer corner
	if !left {
		ox = w
	}
	if !top {
		oy = h
	}
	dx, dy := cx-ox, cy-oy
	r := min(float64(e.Width), half)
	return float64(dx*dx)+float64(dy*dy) < float64(r*r)
}

// EdgeAtlas returns the edge atlas of texture t (whose Edge is set): for every frame of t a
// band of 4 × 4 quarter images (shapes across, quarters down), each padded by a pixel
// that repeats its border. It has t's frames as a grid of one column, and t's clips.
func EdgeAtlas(name string, t *asset.Texture) *asset.Texture {
	src := t.Data.Levels[0]
	gc, gr := max(t.Grid[0], 1), max(t.Grid[1], 1)
	fw, fh := src.W/gc, src.H/gr
	qw, qh := fw/2, fh/2
	cw, ch := qw+2, qh+2
	frames := gc * gr
	img := gfx.NewImage(shapes*cw, quarters*ch*frames)
	for f := 0; f < frames; f++ {
		fx, fy := f%gc*fw, f/gc*fh
		for q := 0; q < quarters; q++ {
			qx, qy := q%2*qw, q/2*qh // the quarter within the frame
			for s := 0; s < shapes; s++ {
				ox, oy := s*cw, (f*quarters+q)*ch
				for j := -1; j <= qh; j++ {
					for i := -1; i <= qw; i++ {
						// Padding repeats the nearest pixel of the quarter.
						px, py := qx+min(max(i, 0), qw-1), qy+min(max(j, 0), qh-1)
						var c uint32
						if inShape(t.Edge, s, q, px, py, fw, fh) {
							c = src.Pix[(fy+py)*src.W+fx+px]
						}
						img.Pix[(oy+j+1)*img.W+ox+i+1] = c
					}
				}
			}
		}
	}
	out := &asset.Texture{Name: name, Data: gfx.TextureData{Levels: []*gfx.Image{img}, Wrap: gfx.WrapClamp}, Clips: t.Clips, Play: t.Play}
	if frames > 1 {
		out.Grid = [2]int{1, frames}
	}
	return out
}

// edgeUV returns the texture coordinates, within one frame's band of the edge atlas of a
// texture whose frames are fw × fh, of shape s in quarter q: left, top, right, bottom.
func edgeUV(s, q, fw, fh int) (u0, v0, u1, v1 float32) {
	qw, qh := fw/2, fh/2
	cw, ch := qw+2, qh+2
	aw, bh := float32(shapes*cw), float32(quarters*ch)
	x, y := float32(s*cw+1), float32(q*ch+1)
	return x / aw, y / bh, (x + float32(qw)) / aw, (y + float32(qh)) / bh
}
