package soft

import (
	"math"

	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/gmath"
)

func sqrtf(x float32) float32 { return float32(math.Sqrt(float64(x))) }

// post converts the auxiliary buffers into colors for the modes that need a full-frame
// view (depth range normalization, id palette, overdraw heat map).
func (c *core) post(dl *gfx.DrawList) {
	fb := c.target
	switch c.mode {
	case gfx.ModeOverdraw:
		for i, n := range c.overdraw {
			fb.Color[i] = gfx.HeatColor(int(n))
		}
	case gfx.ModeIDs:
		for i, id := range fb.ID {
			switch {
			case id != 0:
				fb.Color[i] = gfx.IDColor(id)
			case fb.Depth[i] < 1:
				fb.Color[i] = 0xff404040 // geometry without an entity
			}
		}
	case gfx.ModeDepth:
		c.postDepth(dl)
	}
}

// postDepth maps linear eye depth of covered pixels to gray: nearest = white, farthest
// = dark gray, background black. The range adapts to the frame.
func (c *core) postDepth(dl *gfx.DrawList) {
	fb := c.target
	var proj gmath.Mat4
	found := false
	for _, v := range dl.Views {
		if !v.Overlay {
			proj, found = v.Proj, true
			break
		}
	}
	if !found {
		return
	}
	persp := proj[11] != 0
	// For an OpenGL perspective matrix: m10 = (f+n)/(n-f), m14 = 2fn/(n-f).
	a, b := float64(proj[10]), float64(proj[14])
	eye := func(d float32) float64 {
		zn := float64(2*float64(d)) - 1 // rounded explicitly: arm64 must not fuse a multiply-sub
		if persp {
			return b / (zn + a) // -z_eye = m14/(z_ndc + m10), positive in front of the camera
		}
		return zn
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, d := range fb.Depth {
		if d < 1 {
			e := eye(d)
			lo, hi = min(lo, e), max(hi, e)
		}
	}
	if lo > hi {
		return
	}
	span := hi - lo
	for i, d := range fb.Depth {
		if d >= 1 {
			continue
		}
		t := 0.0
		if span > 0 {
			t = (eye(d) - lo) / span
		}
		g := uint32(255 - math.Round(t*200))
		fb.Color[i] = 0xff000000 | g<<16 | g<<8 | g
	}
}

// drawLines rasterizes the debug lines after all triangles, clipped against the frustum.
// Depth-tested lines pass when they are no farther than the stored depth plus a small
// bias, so lines lying on surfaces stay visible.
func (c *core) drawLines(dl *gfx.DrawList) {
	fb := c.target
	for _, l := range dl.Lines {
		if l.View < 0 || l.View >= len(dl.Views) {
			continue
		}
		view := &dl.Views[l.View]
		if view.Overlay && c.mode != gfx.ModeColor {
			continue
		}
		vp := view.Proj.Mul(view.View)
		a := vp.MulVec4(l.A.Vec4(1))
		b := vp.MulVec4(l.B.Vec4(1))
		va := cvert{p: [4]float32{a.X, a.Y, a.Z, a.W}}
		vb := cvert{p: [4]float32{b.X, b.Y, b.Z, b.W}}
		t0, t1 := float32(0), float32(1)
		visible := true
		for p := 0; p < 6 && visible; p++ {
			da, db := planeDist(&va, p), planeDist(&vb, p)
			switch {
			case da < 0 && db < 0:
				visible = false
			case da < 0:
				t0 = max(t0, da/(da-db))
			case db < 0:
				t1 = min(t1, da/(da-db))
			}
		}
		if !visible || t0 > t1 {
			continue
		}
		pa, pb := lerp4(a, b, t0), lerp4(a, b, t1)
		if !(pa.W > 0 && pb.W > 0) {
			continue
		}
		W, H := float32(fb.W), float32(fb.H)
		// Each product is rounded explicitly so arm64 cannot fuse it into a multiply-add
		// (see gmath.m32).
		sx0, sy0, z0 := float32((float32(pa.X/pa.W*0.5)+0.5)*W), float32((0.5-float32(pa.Y/pa.W*0.5))*H), float32(pa.Z/pa.W*0.5)+0.5
		sx1, sy1, z1 := float32((float32(pb.X/pb.W*0.5)+0.5)*W), float32((0.5-float32(pb.Y/pb.W*0.5))*H), float32(pb.Z/pb.W*0.5)+0.5
		dx, dy := sx1-sx0, sy1-sy0
		// Out-of-range float to int conversions differ between amd64 and arm64, so a
		// degenerate projection (NaN or huge coordinates) is refused in float32 first.
		span := max(gmath.Abs(dx), gmath.Abs(dy))
		if !(span < 1<<24) {
			continue
		}
		steps := int(span) + 1
		inv := 1 / float32(steps)
		for s := 0; s <= steps; s++ {
			f := float32(s) * inv
			xf, yf := floorf(sx0+float32(dx*f)), floorf(sy0+float32(dy*f))
			if !(xf >= 0 && yf >= 0 && xf < W && yf < H) {
				continue
			}
			x, y := int(xf), int(yf)
			i := y*fb.W + x
			if l.DepthTest {
				z := z0 + float32((z1-z0)*f)
				if z > fb.Depth[i]+2e-4 {
					continue
				}
			}
			fb.Color[i] = l.Color
		}
	}
}

func lerp4(a, b gmath.Vec4, t float32) gmath.Vec4 {
	return gmath.Vec4{
		X: a.X + float32((b.X-a.X)*t),
		Y: a.Y + float32((b.Y-a.Y)*t),
		Z: a.Z + float32((b.Z-a.Z)*t),
		W: a.W + float32((b.W-a.W)*t),
	}
}
