package soft

import (
	"fmt"
	"math"

	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gmath"
)

// Vertex attributes interpolated across triangles.
const (
	aU  = iota // texture u
	aV         // texture v
	aR         // light × color, red
	aG         // light × color, green
	aB         // light × color, blue
	aNX        // world normal x
	aNY        // world normal y
	aNZ        // world normal z
	nattr
)

// maxPoly bounds a triangle clipped by six planes (each plane adds at most one vertex).
const maxPoly = 3 + 6

// cvert is a vertex in clip space with its attributes.
type cvert struct {
	p [4]float32
	a [nattr]float32
	// edge reports whether the polygon edge from this vertex to the next one lies on an
	// original triangle edge (false for edges created by clipping); used by wireframe.
	edge bool
}

// cmdState is a draw command resolved for rasterization.
type cmdState struct {
	tex     *texture
	alpha   float32 // color alpha × 255
	cutoff  float32 // alpha-test threshold × 255 (0 disables)
	state   gfx.PipelineState
	filter  gfx.Filter
	id      uint32
	overlay bool
}

// tri is a triangle after clipping, snapping and setup. Edge k is the edge opposite
// vertex k, so its function is proportional to the barycentric weight of vertex k.
type tri struct {
	cmd                    int32
	level                  int32
	minX, minY, maxX, maxY int32
	wire                   uint8 // bit k: edge k is an original triangle edge
	// E_k(x, y) = ec[k] + edx[k]*x + edy[k]*y at the center of pixel (x, y), in 1/256 px²
	// units, with the top-left bias already applied: a pixel is inside iff all E_k >= 0.
	ec, edx, edy [3]int64
	invArea      float32
	einv         [3]float32 // 1 / (16 × edge length in 28.4 units): E_k × einv = pixels
	z            [3]float32 // window depth in [0, 1]
	iw           [3]float32 // 1/w
	a            [3][nattr]float32
}

// xform holds per-command vertex transform and lighting parameters.
type xform struct {
	mvp     gmath.Mat4
	nrm     gmath.Mat3
	color   gmath.Vec3
	unlit   bool
	light   gmath.Vec3
	ambient gmath.Vec3
}

func (c *core) setup(dl *gfx.DrawList) error {
	c.tris = c.tris[:0]
	for i := range c.bins {
		c.bins[i] = c.bins[i][:0]
	}
	c.cmds = c.cmds[:0]
	for ci := range dl.Cmds {
		cmd := &dl.Cmds[ci]
		if cmd.View < 0 || cmd.View >= len(dl.Views) {
			return fmt.Errorf("soft: command %d: view %d out of range (%d views)", ci, cmd.View, len(dl.Views))
		}
		view := &dl.Views[cmd.View]
		if view.Overlay && c.mode != gfx.ModeColor {
			continue
		}
		verts, idx, err := c.source(dl, cmd)
		if err != nil {
			return fmt.Errorf("soft: command %d: %w", ci, err)
		}
		cs := cmdState{
			alpha:   gmath.Clamp01(cmd.Color.W) * 255,
			cutoff:  gmath.Clamp01(cmd.Cutoff) * 255,
			state:   cmd.State,
			filter:  cmd.Filter,
			id:      cmd.ID,
			overlay: view.Overlay,
		}
		if cmd.Texture != 0 {
			if int(cmd.Texture) > len(c.textures) {
				return fmt.Errorf("soft: command %d: unknown texture %d", ci, cmd.Texture)
			}
			cs.tex = c.textures[cmd.Texture-1]
		}
		c.cmds = append(c.cmds, cs)
		c.stats.Commands++

		x := xform{
			mvp:     view.Proj.Mul(view.View).Mul(cmd.Model),
			nrm:     cmd.Model.NormalMatrix(),
			color:   cmd.Color.XYZ(),
			unlit:   cmd.Unlit || view.Overlay,
			light:   dl.Light.Color,
			ambient: dl.Light.Ambient,
		}
		c.gen++
		if c.gen == 0 {
			clear(c.stamp)
			c.gen = 1
		}
		if len(c.stamp) < len(verts) {
			c.stamp = append(c.stamp, make([]uint32, len(verts)-len(c.stamp))...)
			c.xv = append(c.xv, make([]cvert, len(verts)-len(c.xv))...)
		}
		nv := uint32(len(verts))
		csi := int32(len(c.cmds) - 1)
		for t := 0; t+2 < len(idx); t += 3 {
			i0, i1, i2 := idx[t], idx[t+1], idx[t+2]
			if i0 >= nv || i1 >= nv || i2 >= nv {
				return fmt.Errorf("soft: command %d: index out of range (%d vertices)", ci, nv)
			}
			c.stats.Triangles++
			v0 := c.vertex(i0, verts, &x)
			v1 := c.vertex(i1, verts, &x)
			v2 := c.vertex(i2, verts, &x)
			c.clipAndSetup(csi, v0, v1, v2)
		}
	}
	return nil
}

// source returns the vertices and the index range a command draws.
func (c *core) source(dl *gfx.DrawList, cmd *gfx.DrawCmd) ([]gfx.Vertex, []uint32, error) {
	var verts []gfx.Vertex
	var all []uint32
	count := cmd.Count
	if cmd.Mesh == 0 {
		verts, all = dl.Verts, dl.Indices
	} else {
		if int(cmd.Mesh) > len(c.meshes) {
			return nil, nil, fmt.Errorf("unknown mesh %d", cmd.Mesh)
		}
		m := c.meshes[cmd.Mesh-1]
		verts, all = m.Vertices, m.Indices
		if count == 0 && cmd.First == 0 {
			count = len(all)
		}
	}
	if cmd.First < 0 || count < 0 || count%3 != 0 || cmd.First+count > len(all) {
		return nil, nil, fmt.Errorf("index range [%d, %d) invalid (%d indices)", cmd.First, cmd.First+count, len(all))
	}
	return verts, all[cmd.First : cmd.First+count], nil
}

// vertex transforms and lights vertex i once per command.
func (c *core) vertex(i uint32, verts []gfx.Vertex, x *xform) *cvert {
	v := &c.xv[i]
	if c.stamp[i] == c.gen {
		return v
	}
	c.stamp[i] = c.gen
	src := &verts[i]
	p := x.mvp.MulVec4(src.Pos.Vec4(1))
	v.p = [4]float32{p.X, p.Y, p.Z, p.W}
	n := x.nrm.MulVec3(src.Normal).Normalize()
	col := x.color
	if !x.unlit {
		d := max(0, n.Dot(c.lightDir))
		col = gmath.Vec3{
			X: col.X * (x.ambient.X + x.light.X*d),
			Y: col.Y * (x.ambient.Y + x.light.Y*d),
			Z: col.Z * (x.ambient.Z + x.light.Z*d),
		}
	}
	v.a = [nattr]float32{src.UV.X, src.UV.Y, col.X, col.Y, col.Z, n.X, n.Y, n.Z}
	v.edge = true
	return v
}

// planeDist returns the signed distance of v to clip plane p (inside when >= 0):
// 0: x >= -w, 1: x <= w, 2: y >= -w, 3: y <= w, 4: z >= -w (near), 5: z <= w (far).
func planeDist(v *cvert, p int) float32 {
	switch p {
	case 0:
		return v.p[3] + v.p[0]
	case 1:
		return v.p[3] - v.p[0]
	case 2:
		return v.p[3] + v.p[1]
	case 3:
		return v.p[3] - v.p[1]
	case 4:
		return v.p[3] + v.p[2]
	default:
		return v.p[3] - v.p[2]
	}
}

func outcode(v *cvert) uint8 {
	var oc uint8
	for p := 0; p < 6; p++ {
		if !(planeDist(v, p) >= 0) { // NaN counts as outside
			oc |= 1 << p
		}
	}
	return oc
}

func (c *core) clipAndSetup(cmd int32, v0, v1, v2 *cvert) {
	oc0, oc1, oc2 := outcode(v0), outcode(v1), outcode(v2)
	if oc0&oc1&oc2 != 0 {
		c.stats.Culled++
		return
	}
	if oc0|oc1|oc2 == 0 {
		c.setupTri(cmd, v0, v1, v2, 7)
		return
	}
	c.stats.Clipped++
	in := &c.poly[0]
	in[0], in[1], in[2] = *v0, *v1, *v2
	in[0].edge, in[1].edge, in[2].edge = true, true, true
	n := 3
	cur := 0
	planes := oc0 | oc1 | oc2
	for p := 0; p < 6; p++ {
		if planes&(1<<p) == 0 {
			continue
		}
		n = clipPoly(&c.poly[cur], n, &c.poly[1-cur], p)
		cur = 1 - cur
		if n < 3 {
			c.stats.Culled++
			return
		}
	}
	poly := &c.poly[cur]
	for i := 1; i+1 < n; i++ {
		var e uint8
		if i == 1 && poly[0].edge {
			e |= 1 // v0→v1
		}
		if poly[i].edge {
			e |= 2 // v1→v2
		}
		if i+1 == n-1 && poly[n-1].edge {
			e |= 4 // v2→v0
		}
		c.setupTri(cmd, &poly[0], &poly[i], &poly[i+1], e)
	}
}

// clipPoly clips polygon in[:n] against plane p into out and returns the new vertex
// count. Intersections are always computed from the inside vertex towards the outside
// one, so an edge shared by two triangles yields bit-identical points (no cracks).
func clipPoly(in *[maxPoly]cvert, n int, out *[maxPoly]cvert, p int) int {
	m := 0
	for i := 0; i < n; i++ {
		a := &in[i]
		j := i + 1
		if j == n {
			j = 0
		}
		b := &in[j]
		da, db := planeDist(a, p), planeDist(b, p)
		ain, bin := da >= 0, db >= 0
		switch {
		case ain && bin:
			out[m] = *a
			m++
		case ain:
			out[m] = *a
			m++
			lerpVert(&out[m], a, b, da/(da-db))
			out[m].edge = false
			m++
		case bin:
			lerpVert(&out[m], b, a, db/(db-da))
			out[m].edge = a.edge
			m++
		}
	}
	return m
}

func lerpVert(dst, a, b *cvert, t float32) {
	for k := range dst.p {
		dst.p[k] = a.p[k] + t*(b.p[k]-a.p[k])
	}
	for k := range dst.a {
		dst.a[k] = a.a[k] + t*(b.a[k]-a.a[k])
	}
}

// setupTri snaps a clipped triangle to the pixel grid, culls it and bins it into tiles.
// e holds original-edge flags: bit 0 v0→v1, bit 1 v1→v2, bit 2 v2→v0.
func (c *core) setupTri(cmd int32, v0, v1, v2 *cvert, e uint8) {
	fb := c.target
	W, H := float32(fb.W), float32(fb.H)
	vs := [3]*cvert{v0, v1, v2}
	var X, Y [3]int64
	var z, iw [3]float32
	for k, v := range vs {
		w := v.p[3]
		if !(w > 0) {
			c.stats.Culled++
			return
		}
		inv := 1 / w
		sx := (v.p[0]*inv*0.5 + 0.5) * W
		sy := (0.5 - v.p[1]*inv*0.5) * H
		if !(sx >= -1 && sx <= W+1 && sy >= -1 && sy <= H+1) { // also rejects NaN
			c.stats.Culled++
			return
		}
		X[k] = int64(math.Floor(float64(sx*16) + 0.5))
		Y[k] = int64(math.Floor(float64(sy*16) + 0.5))
		z[k] = v.p[2]*inv*0.5 + 0.5
		iw[k] = inv
	}
	area := (X[1]-X[0])*(Y[2]-Y[0]) - (Y[1]-Y[0])*(X[2]-X[0])
	if area == 0 {
		c.stats.Culled++
		return
	}
	cs := &c.cmds[cmd]
	e01, e12, e20 := e&1 != 0, e&2 != 0, e&4 != 0
	if area < 0 {
		// Counter-clockwise in NDC (y up) is negative in y-down window space: a front
		// face. Swap v1 and v2 so every rasterized triangle has positive area.
		vs[1], vs[2] = vs[2], vs[1]
		X[1], X[2] = X[2], X[1]
		Y[1], Y[2] = Y[2], Y[1]
		z[1], z[2] = z[2], z[1]
		iw[1], iw[2] = iw[2], iw[1]
		e01, e20 = e20, e01
		area = -area
	} else if cs.state.Cull == gfx.CullBack {
		c.stats.Culled++
		return
	}

	minX := max(0, min(X[0], X[1], X[2])>>4)
	maxX := min(int64(fb.W-1), max(X[0], X[1], X[2])>>4)
	minY := max(0, min(Y[0], Y[1], Y[2])>>4)
	maxY := min(int64(fb.H-1), max(Y[0], Y[1], Y[2])>>4)
	if minX > maxX || minY > maxY {
		c.stats.Culled++
		return
	}

	c.tris = append(c.tris, tri{})
	t := &c.tris[len(c.tris)-1]
	t.cmd = cmd
	t.minX, t.minY, t.maxX, t.maxY = int32(minX), int32(minY), int32(maxX), int32(maxY)
	if e12 {
		t.wire |= 1
	}
	if e20 {
		t.wire |= 2
	}
	if e01 {
		t.wire |= 4
	}
	for k := 0; k < 3; k++ {
		a, b := (k+1)%3, (k+2)%3
		ax, ay, bx, by := X[a], Y[a], X[b], Y[b]
		dx, dy := bx-ax, by-ay
		ec := dx*(8-ay) - dy*(8-ax)
		if !(dy < 0 || dy == 0 && dx > 0) { // not a top or left edge
			ec--
		}
		t.ec[k], t.edx[k], t.edy[k] = ec, -dy*16, dx*16
		t.einv[k] = float32(1 / (16 * math.Sqrt(float64(dx*dx+dy*dy))))
		t.z[k], t.iw[k] = z[k], iw[k]
		for j := 0; j < nattr; j++ {
			t.a[k][j] = vs[k].a[j] * iw[k]
		}
	}
	t.invArea = float32(1 / float64(area))

	if tex := cs.tex; tex != nil && len(tex.levels) > 1 {
		// Mip per triangle: texel area over pixel area, both as doubled triangle areas.
		l0 := &tex.levels[0]
		du1, dv1 := float64(vs[1].a[aU]-vs[0].a[aU]), float64(vs[1].a[aV]-vs[0].a[aV])
		du2, dv2 := float64(vs[2].a[aU]-vs[0].a[aU]), float64(vs[2].a[aV]-vs[0].a[aV])
		texels := math.Abs(du1*dv2-du2*dv1) * float64(l0.w) * float64(l0.h)
		ratio := texels / (float64(area) / 256)
		lv := int32(0)
		for ratio >= 4 && int(lv) < len(tex.levels)-1 {
			ratio /= 4
			lv++
		}
		t.level = lv
	}

	c.stats.Drawn++
	idx := int32(len(c.tris) - 1)
	tx0, tx1 := int(minX)/TileSize, int(maxX)/TileSize
	ty0, ty1 := int(minY)/TileSize, int(maxY)/TileSize
	for ty := ty0; ty <= ty1; ty++ {
		row := ty * c.tilesX
		for tx := tx0; tx <= tx1; tx++ {
			c.bins[row+tx] = append(c.bins[row+tx], idx)
		}
	}
}
