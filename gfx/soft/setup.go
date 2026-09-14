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

// maxPoly bounds a clipped polygon. In exact arithmetic each of the six planes adds at
// most one vertex (3 + 6 = 9); rounding can make a plane add two, so the scratch arrays
// have room to spare and clipPoly never writes past them.
const maxPoly = 16

// cvert is a vertex in clip space with its attributes.
type cvert struct {
	p [4]float32
	a [nattr]float32
	// edge reports whether the polygon edge from this vertex to the next one lies on an
	// original triangle edge (false for edges created by clipping); used by wireframe.
	edge bool
	oc   uint8 // outcode, cached by vertex (stale on clipped vertices, which never read it)
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
	flip    bool       // the model matrix mirrors (negative determinant): front faces are clockwise
	vz      gmath.Vec3 // world direction towards the viewer (row 2 of the view matrix)
	// fast selects rasterTriOpaque in ModeColor: opaque, depth-tested, bilinear-filtered
	// with a power-of-two repeat texture, not an overlay.
	fast bool
}

// tri is a triangle after clipping, snapping and setup. Edge k is the edge opposite
// vertex k, so its function is proportional to the barycentric weight of vertex k.
type tri struct {
	cmd                    int32
	level                  int32
	minX, minY, maxX, maxY int32
	wire                   uint8 // bit k: edge k is an original triangle edge
	back                   bool  // back-facing (drawn because culling is off)
	// E_k(x, y) = ec[k] + edx[k]*x + edy[k]*y at the center of pixel (x, y), in 1/256 px²
	// units, with the top-left bias already applied: a pixel is inside iff all E_k >= 0.
	// eb[k] is that bias (0 or 1), added back before E_k weighs a vertex: the three weights
	// then sum to one, which for a triangle of a pixel or less is not a rounding matter.
	ec, edx, edy, eb [3]int64
	invArea          float32
	einv             [3]float32 // 1 / (16 × edge length in 28.4 units): E_k × einv = pixels
	z                [3]float32 // window depth in [0, 1]
	iw               [3]float32 // 1/w
	a                [3][nattr]float32
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

// cmdSrc is the validated geometry of a command and the frame-wide index of its first
// triangle.
type cmdSrc struct {
	verts []gfx.Vertex
	idx   []uint32
	first int
}

// setupCtx is one setup chunk: the triangles [lo, hi) of the frame, in submission order.
// A chunk owns its triangles, bins, vertex cache, clip scratch and counters, so chunks
// are set up concurrently. Tiles consume chunk 0's triangles, then chunk 1's, and so on,
// which is submission order; a triangle's setup depends only on its own vertices, so the
// number of chunks never changes the image.
type setupCtx struct {
	lo, hi int
	tris   []tri
	bins   [][]int32
	xv     []cvert
	stamp  []uint32
	gen    uint32
	poly   [2][maxPoly]cvert
	stats  gfx.FrameStats // Culled, Clipped and Drawn
}

// minChunkTris is the smallest number of triangles per chunk worth a parallel phase.
const minChunkTris = 256

func (c *core) setup(dl *gfx.DrawList) error {
	c.cmds = c.cmds[:0]
	c.xforms = c.xforms[:0]
	c.srcs = c.srcs[:0]
	total, maxVerts := 0, 0
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
			flip:    cmd.Model.Mat3().Det() < 0,
			vz:      gmath.V3(view.View[2], view.View[6], view.View[10]),
		}
		if cmd.Texture != 0 {
			if int(cmd.Texture) > len(c.textures) {
				return fmt.Errorf("soft: command %d: unknown texture %d", ci, cmd.Texture)
			}
			cs.tex = c.textures[cmd.Texture-1]
		}
		cs.fast = cs.tex != nil && cs.tex.fast && cs.filter != gfx.FilterNearest &&
			cs.state.Blend == gfx.BlendOpaque && cs.state.DepthTest && !cs.overlay
		nv := uint32(len(verts))
		for _, i := range idx {
			if i >= nv {
				return fmt.Errorf("soft: command %d: index out of range (%d vertices)", ci, nv)
			}
		}
		c.cmds = append(c.cmds, cs)
		c.stats.Commands++
		c.xforms = append(c.xforms, xform{
			mvp:     view.Proj.Mul(view.View).Mul(cmd.Model),
			nrm:     cmd.Model.NormalMatrix(),
			color:   cmd.Color.XYZ(),
			unlit:   cmd.Unlit || view.Overlay,
			light:   dl.Light.Color,
			ambient: dl.Light.Ambient,
		})
		c.srcs = append(c.srcs, cmdSrc{verts: verts, idx: idx, first: total})
		total += len(idx) / 3
		maxVerts = max(maxVerts, len(verts))
	}
	c.stats.Triangles = total

	n := min(len(c.chunks), max(1, total/minChunkTris))
	c.nchunks = n
	tiles := c.tilesX * c.tilesY
	for k := 0; k < n; k++ {
		ch := &c.chunks[k]
		ch.lo, ch.hi = total*k/n, total*(k+1)/n
		ch.tris = ch.tris[:0]
		// Keep per-tile capacity across target sizes: switching between two framebuffer
		// sizes every frame must not reallocate the bins.
		if cap(ch.bins) < tiles {
			ch.bins = append(ch.bins[:cap(ch.bins)], make([][]int32, tiles-cap(ch.bins))...)
		}
		ch.bins = ch.bins[:tiles]
		for i := range ch.bins {
			ch.bins[i] = ch.bins[i][:0]
		}
		if len(ch.stamp) < maxVerts {
			ch.stamp = append(ch.stamp, make([]uint32, maxVerts-len(ch.stamp))...)
			ch.xv = append(ch.xv, make([]cvert, maxVerts-len(ch.xv))...)
		}
		ch.stats = gfx.FrameStats{}
	}
	if n == 1 {
		c.setupChunk(&c.chunks[0])
	} else {
		c.run(phaseSetup)
	}
	for k := 0; k < n; k++ {
		s := &c.chunks[k].stats
		c.stats.Culled += s.Culled
		c.stats.Clipped += s.Clipped
		c.stats.Drawn += s.Drawn
	}
	return nil
}

// setupChunk transforms, clips, sets up and bins the triangles of chunk ch.
func (c *core) setupChunk(ch *setupCtx) {
	for si := range c.srcs {
		s := &c.srcs[si]
		lo, hi := max(ch.lo-s.first, 0), min(ch.hi-s.first, len(s.idx)/3)
		if lo >= hi {
			continue
		}
		ch.gen++
		if ch.gen == 0 {
			clear(ch.stamp)
			ch.gen = 1
		}
		x := &c.xforms[si]
		for t := lo; t < hi; t++ {
			i0, i1, i2 := s.idx[3*t], s.idx[3*t+1], s.idx[3*t+2]
			v0 := c.vertex(ch, i0, s.verts, x)
			v1 := c.vertex(ch, i1, s.verts, x)
			v2 := c.vertex(ch, i2, s.verts, x)
			c.clipAndSetup(ch, int32(si), v0, v1, v2)
		}
	}
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

// vertex transforms and lights vertex i once per command and chunk.
func (c *core) vertex(ch *setupCtx, i uint32, verts []gfx.Vertex, x *xform) *cvert {
	v := &ch.xv[i]
	if ch.stamp[i] == ch.gen {
		return v
	}
	ch.stamp[i] = ch.gen
	src := &verts[i]
	p := x.mvp.MulVec4(src.Pos.Vec4(1))
	v.p = [4]float32{p.X, p.Y, p.Z, p.W}
	v.oc = outcode(v)
	n := x.nrm.MulVec3(src.Normal).Normalize()
	col := x.color
	if !x.unlit {
		d := max(0, n.Dot(c.lightDir))
		// Each product is rounded explicitly so arm64 cannot fuse it into a multiply-add
		// (see gmath.m32).
		col = gmath.Vec3{
			X: col.X * (x.ambient.X + float32(x.light.X*d)),
			Y: col.Y * (x.ambient.Y + float32(x.light.Y*d)),
			Z: col.Z * (x.ambient.Z + float32(x.light.Z*d)),
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

// planeDist64 is planeDist in float64, used for clipping precision.
func planeDist64(v *cvert, p int) float64 {
	w := float64(v.p[3])
	switch p {
	case 0:
		return w + float64(v.p[0])
	case 1:
		return w - float64(v.p[0])
	case 2:
		return w + float64(v.p[1])
	case 3:
		return w - float64(v.p[1])
	case 4:
		return w + float64(v.p[2])
	default:
		return w - float64(v.p[2])
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

// clipAndSetup clips a triangle against the frustum and sets up the resulting pieces.
// Stats count submitted triangles: Culled when nothing of it is rasterized.
func (c *core) clipAndSetup(ch *setupCtx, cmd int32, v0, v1, v2 *cvert) {
	oc0, oc1, oc2 := v0.oc, v1.oc, v2.oc
	if oc0&oc1&oc2 != 0 {
		ch.stats.Culled++
		return
	}
	if oc0|oc1|oc2 == 0 {
		if !c.setupTri(ch, cmd, v0, v1, v2, 7, -1) {
			ch.stats.Culled++
		}
		return
	}
	ch.stats.Clipped++
	in := &ch.poly[0]
	in[0], in[1], in[2] = *v0, *v1, *v2
	in[0].edge, in[1].edge, in[2].edge = true, true, true
	n := 3
	cur := 0
	planes := oc0 | oc1 | oc2
	for p := 0; p < 6; p++ {
		if planes&(1<<p) == 0 {
			continue
		}
		n = clipPoly(&ch.poly[cur], n, &ch.poly[1-cur], p)
		cur = 1 - cur
		if n < 3 {
			ch.stats.Culled++
			return
		}
	}
	poly := &ch.poly[cur]
	lv := c.polyLevel(cmd, poly, n)
	drawn := false
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
		if c.setupTri(ch, cmd, &poly[0], &poly[i], &poly[i+1], e, lv) {
			drawn = true
		}
	}
	if !drawn {
		ch.stats.Culled++
	}
}

// clipPoly clips polygon in[:n] against plane p into out and returns the new vertex
// count. Distances and intersections are computed in float64 (clipped vertices must land
// on the plane even for kilometre-long triangles) and always from the inside vertex
// towards the outside one, so an edge shared by two triangles yields bit-identical points.
func clipPoly(in *[maxPoly]cvert, n int, out *[maxPoly]cvert, p int) int {
	m := 0
	for i := 0; i < n && m < maxPoly-1; i++ {
		a := &in[i]
		j := i + 1
		if j == n {
			j = 0
		}
		b := &in[j]
		da, db := planeDist64(a, p), planeDist64(b, p)
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

// lerpVert interpolates in float64 and rounds once to float32. The product is rounded
// explicitly (in float64) so arm64 cannot fuse it into a multiply-add.
func lerpVert(dst, a, b *cvert, t float64) {
	for k := range dst.p {
		x := float64(a.p[k])
		dst.p[k] = float32(x + float64(t*(float64(b.p[k])-x)))
	}
	for k := range dst.a {
		x := float64(a.a[k])
		dst.a[k] = float32(x + float64(t*(float64(b.a[k])-x)))
	}
}

// polyLevel picks one mip level for a whole clipped polygon (so the pieces of one source
// triangle never disagree), from its total texel area over its total window area.
// It returns -1 when the command has no mip chain.
func (c *core) polyLevel(cmd int32, poly *[maxPoly]cvert, n int) int32 {
	tex := c.cmds[cmd].tex
	if tex == nil || len(tex.levels) <= 1 {
		return -1
	}
	W, H := float64(c.target.W), float64(c.target.H)
	// Each product is rounded explicitly so arm64 cannot fuse it into a multiply-add.
	var sx, sy [maxPoly]float64
	for i := 0; i < n; i++ {
		w := float64(poly[i].p[3])
		if !(w > 0) {
			return 0
		}
		sx[i] = float64((float64(float64(poly[i].p[0])/w*0.5) + 0.5) * W)
		sy[i] = float64((0.5 - float64(float64(poly[i].p[1])/w*0.5)) * H)
	}
	var uv, px float64
	for i := 1; i+1 < n; i++ {
		du1, dv1 := float64(poly[i].a[aU]-poly[0].a[aU]), float64(poly[i].a[aV]-poly[0].a[aV])
		du2, dv2 := float64(poly[i+1].a[aU]-poly[0].a[aU]), float64(poly[i+1].a[aV]-poly[0].a[aV])
		uv += math.Abs(float64(du1*dv2) - float64(du2*dv1))
		px += math.Abs(float64((sx[i]-sx[0])*(sy[i+1]-sy[0])) - float64((sy[i]-sy[0])*(sx[i+1]-sx[0])))
	}
	return mipLevel(tex, uv, px)
}

// mipLevel returns ⌊log₄(texel area / pixel area)⌋ clamped to the chain, from doubled
// UV-space and window-space areas.
func mipLevel(tex *texture, uvArea2, pixelArea2 float64) int32 {
	if !(pixelArea2 > 0) {
		return int32(len(tex.levels) - 1)
	}
	l0 := &tex.levels[0]
	ratio := uvArea2 * float64(l0.w) * float64(l0.h) / pixelArea2
	lv := int32(0)
	for ratio >= 4 && int(lv) < len(tex.levels)-1 {
		ratio /= 4
		lv++
	}
	return lv
}

// setupTri snaps a (clipped) triangle to the pixel grid, culls it and bins it into the
// tiles of chunk ch. e holds original-edge flags: bit 0 v0→v1, bit 1 v1→v2, bit 2 v2→v0.
// lv is the mip level, or -1 to compute it from this triangle. It reports whether the
// triangle was binned.
func (c *core) setupTri(ch *setupCtx, cmd int32, v0, v1, v2 *cvert, e uint8, lv int32) bool {
	fb := c.target
	W, H := float32(fb.W), float32(fb.H)
	vs := [3]*cvert{v0, v1, v2}
	var X, Y [3]int64
	var z, iw [3]float32
	for k, v := range vs {
		w := v.p[3]
		if !(w > 0) {
			return false
		}
		inv := 1 / w
		// Each product is rounded explicitly so arm64 cannot fuse it into a multiply-add.
		sx := (float32(v.p[0]*inv*0.5) + 0.5) * W
		sy := (0.5 - float32(v.p[1]*inv*0.5)) * H
		if !gmath.IsFinite(sx) || !gmath.IsFinite(sy) {
			return false
		}
		// Vertices are inside the frustum by construction; any excess is rounding.
		sx, sy = gmath.Clamp(sx, 0, W), gmath.Clamp(sy, 0, H)
		X[k] = int64(math.Floor(float64(sx*16) + 0.5))
		Y[k] = int64(math.Floor(float64(sy*16) + 0.5))
		z[k] = float32(v.p[2]*inv*0.5) + 0.5
		iw[k] = inv
	}
	area := (X[1]-X[0])*(Y[2]-Y[0]) - (Y[1]-Y[0])*(X[2]-X[0])
	if area == 0 {
		return false
	}
	cs := &c.cmds[cmd]
	// Counter-clockwise in NDC (y up) is negative in y-down window space: a front face,
	// unless the model matrix mirrors.
	front := (area < 0) != cs.flip
	cull := cs.state.Cull
	if c.mode == gfx.ModeNormals {
		cull = gfx.CullNone // show back faces so inside-out parts are flagged
	}
	if !front && cull == gfx.CullBack {
		return false
	}
	e01, e12, e20 := e&1 != 0, e&2 != 0, e&4 != 0
	if area < 0 {
		// Swap v1 and v2 so every rasterized triangle has positive area.
		vs[1], vs[2] = vs[2], vs[1]
		X[1], X[2] = X[2], X[1]
		Y[1], Y[2] = Y[2], Y[1]
		z[1], z[2] = z[2], z[1]
		iw[1], iw[2] = iw[2], iw[1]
		e01, e20 = e20, e01
		area = -area
	}

	minX := max(0, min(X[0], X[1], X[2])>>4)
	maxX := min(int64(fb.W-1), max(X[0], X[1], X[2])>>4)
	minY := max(0, min(Y[0], Y[1], Y[2])>>4)
	maxY := min(int64(fb.H-1), max(Y[0], Y[1], Y[2])>>4)
	if minX > maxX || minY > maxY {
		return false
	}

	ch.tris = append(ch.tris, tri{})
	t := &ch.tris[len(ch.tris)-1]
	t.cmd = cmd
	t.back = !front
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
		var bias int64
		if !(dy < 0 || dy == 0 && dx > 0) { // not a top or left edge
			bias = 1
		}
		t.ec[k], t.edx[k], t.edy[k], t.eb[k] = ec-bias, -dy*16, dx*16, bias
		if c.mode == gfx.ModeWireframe { // einv is only read by wireColor
			t.einv[k] = float32(1 / (16 * math.Sqrt(float64(dx*dx+dy*dy))))
		}
		t.z[k], t.iw[k] = z[k], iw[k]
		for j := 0; j < nattr; j++ {
			t.a[k][j] = vs[k].a[j] * iw[k]
		}
	}
	t.invArea = float32(1 / float64(area))

	if tex := cs.tex; tex != nil && len(tex.levels) > 1 {
		if lv >= 0 {
			t.level = lv
		} else {
			// Mip per triangle: texel area over pixel area, both as doubled triangle
			// areas (area is in 1/256 px²).
			du1, dv1 := float64(vs[1].a[aU]-vs[0].a[aU]), float64(vs[1].a[aV]-vs[0].a[aV])
			du2, dv2 := float64(vs[2].a[aU]-vs[0].a[aU]), float64(vs[2].a[aV]-vs[0].a[aV])
			t.level = mipLevel(tex, math.Abs(float64(du1*dv2)-float64(du2*dv1)), float64(area)/256)
		}
	}

	ch.stats.Drawn++
	idx := int32(len(ch.tris) - 1)
	tx0, tx1 := int(minX)/TileSize, int(maxX)/TileSize
	ty0, ty1 := int(minY)/TileSize, int(maxY)/TileSize
	for ty := ty0; ty <= ty1; ty++ {
		row := ty * c.tilesX
		for tx := tx0; tx <= tx1; tx++ {
			ch.bins[row+tx] = append(ch.bins[row+tx], idx)
		}
	}
	return true
}
