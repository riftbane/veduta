package soft

import "github.com/riftbane/veduta/gfx"

// rasterTile clears (if requested) and rasterizes every triangle binned into tile t.
// Only this call writes the tile's pixels, so tiles can run concurrently.
func (c *core) rasterTile(t int) {
	fb := c.target
	x0 := (t % c.tilesX) * TileSize
	y0 := (t / c.tilesX) * TileSize
	x1 := min(x0+TileSize, fb.W) - 1
	y1 := min(y0+TileSize, fb.H) - 1
	if c.clear {
		// Fill the first row, then copy it (memmove) into the others.
		n := x1 - x0 + 1
		first := y0*fb.W + x0
		col, dep := fb.Color[first:first+n], fb.Depth[first:first+n]
		for i := range col {
			col[i] = c.clearColor
		}
		for i := range dep {
			dep[i] = 1
		}
		for y := y0; y <= y1; y++ {
			row := y*fb.W + x0
			if y > y0 {
				copy(fb.Color[row:row+n], col)
				copy(fb.Depth[row:row+n], dep)
			}
			clear(fb.ID[row : row+n])
			if fb.Normal != nil {
				clear(fb.Normal[row : row+n])
			}
		}
	}
	var frags int64
	for _, ti := range c.bins[t] {
		tr := &c.tris[ti]
		rx0, ry0 := max(int(tr.minX), x0), max(int(tr.minY), y0)
		rx1, ry1 := min(int(tr.maxX), x1), min(int(tr.maxY), y1)
		if rx0 > rx1 || ry0 > ry1 {
			continue
		}
		frags += c.rasterTri(tr, rx0, ry0, rx1, ry1)
	}
	if frags != 0 {
		c.fragments.Add(frags)
	}
}

// wideRow is the row width (in pixels, minus one) from which spans are solved by division
// instead of stepping pixel by pixel.
const wideRow = 24

// span narrows [lo, hi] (pixel offsets along a row) to where e + dx*k >= 0.
// It reports false when no offset satisfies the edge.
func span(e, dx int64, lo, hi *int64) bool {
	switch {
	case dx == 0:
		return e >= 0
	case dx > 0:
		if e < 0 {
			if k := (-e + dx - 1) / dx; k > *lo {
				*lo = k
			}
		}
	default:
		if e < 0 {
			return false
		}
		if k := e / -dx; k < *hi {
			*hi = k
		}
	}
	return true
}

// rasterTri rasterizes triangle t inside the pixel rectangle [x0,x1]×[y0,y1] and returns
// the number of fragments written.
func (c *core) rasterTri(t *tri, x0, y0, x1, y1 int) int64 {
	fb := c.target
	cs := &c.cmds[t.cmd]
	mode := c.mode
	var frags int64
	for y := y0; y <= y1; y++ {
		yy, xx := int64(y), int64(x0)
		e0 := t.ec[0] + t.edx[0]*xx + t.edy[0]*yy
		e1 := t.ec[1] + t.edx[1]*xx + t.edy[1]*yy
		e2 := t.ec[2] + t.edx[2]*xx + t.edy[2]*yy
		lo, hi := int64(0), int64(x1-x0)
		if hi >= wideRow {
			// Wide row: solve the span exactly with one division per edge.
			if !span(e0, t.edx[0], &lo, &hi) || !span(e1, t.edx[1], &lo, &hi) || !span(e2, t.edx[2], &lo, &hi) || lo > hi {
				continue
			}
			e0 += t.edx[0] * lo
			e1 += t.edx[1] * lo
			e2 += t.edx[2] * lo
		} else {
			// Narrow row: step to the first covered pixel; cheaper than dividing.
			for lo <= hi && e0|e1|e2 < 0 {
				e0 += t.edx[0]
				e1 += t.edx[1]
				e2 += t.edx[2]
				lo++
			}
			if lo > hi {
				continue
			}
		}
		row := y * fb.W
		for x := x0 + int(lo); x <= x0+int(hi); x++ {
			if e0|e1|e2 < 0 {
				break // triangles are convex: the row's span has ended
			}
			i := row + x
			l0 := float32(e0) * t.invArea
			l1 := float32(e1) * t.invArea
			l2 := float32(e2) * t.invArea
			e0 += t.edx[0]
			e1 += t.edx[1]
			e2 += t.edx[2]

			if mode == gfx.ModeOverdraw {
				if c.overdraw[i] < 0xffff {
					c.overdraw[i]++
				}
				frags++
				continue
			}
			z := l0*t.z[0] + l1*t.z[1] + l2*t.z[2]
			if cs.state.DepthTest && !(z < fb.Depth[i]) {
				continue
			}
			w := 1 / (l0*t.iw[0] + l1*t.iw[1] + l2*t.iw[2])
			p0, p1, p2 := l0*w, l1*w, l2*w
			u := p0*t.a[0][aU] + p1*t.a[1][aU] + p2*t.a[2][aU]
			v := p0*t.a[0][aV] + p1*t.a[1][aV] + p2*t.a[2][aV]

			switch mode {
			case gfx.ModeColor, gfx.ModeCollision, gfx.ModeUVChecker:
				texel := uint32(0xffffffff)
				if mode == gfx.ModeUVChecker {
					texel = uvChecker(u, v)
				} else if cs.tex != nil {
					if cs.filter == gfx.FilterNearest {
						texel = cs.tex.nearest(t.level, u, v)
					} else {
						texel = cs.tex.bilinear(t.level, u, v)
					}
				}
				alpha := float32(texel>>24) * (cs.alpha * (1.0 / 255))
				if alpha < cs.cutoff {
					continue
				}
				r := float32(texel>>16&0xff) * (p0*t.a[0][aR] + p1*t.a[1][aR] + p2*t.a[2][aR])
				g := float32(texel>>8&0xff) * (p0*t.a[0][aG] + p1*t.a[1][aG] + p2*t.a[2][aG])
				b := float32(texel&0xff) * (p0*t.a[0][aB] + p1*t.a[1][aB] + p2*t.a[2][aB])
				if mode == gfx.ModeCollision {
					r, g, b = r*0.5, g*0.5, b*0.5
				}
				src := pack(r, g, b)
				switch cs.state.Blend {
				case gfx.BlendOpaque:
					fb.Color[i] = 0xff000000 | src
				case gfx.BlendAlpha:
					fb.Color[i] = blendAlpha(fb.Color[i], src, unorm(alpha))
				case gfx.BlendAdd:
					fb.Color[i] = blendAdd(fb.Color[i], src, unorm(alpha))
				}
				if cs.state.DepthWrite {
					fb.Depth[i] = z
				}
				if !cs.overlay && (cs.state.Blend == gfx.BlendOpaque || alpha >= 128) {
					fb.ID[i] = cs.id
					if fb.Normal != nil {
						fb.Normal[i] = packNormal(t, p0, p1, p2)
					}
				}
			default:
				if cs.cutoff > 0 && cs.tex != nil {
					if float32(cs.tex.nearest(t.level, u, v)>>24)*(cs.alpha*(1.0/255)) < cs.cutoff {
						continue
					}
				}
				switch mode {
				case gfx.ModeWireframe:
					fb.Color[i] = wireColor(t, e0-t.edx[0], e1-t.edx[1], e2-t.edx[2])
				case gfx.ModeNormals:
					fb.Color[i] = packNormal(t, p0, p1, p2)
				case gfx.ModeSilhouette:
					fb.Color[i] = 0xffffffff
				}
				fb.Depth[i] = z
				fb.ID[i] = cs.id
				if fb.Normal != nil {
					fb.Normal[i] = packNormal(t, p0, p1, p2)
				}
			}
			frags++
		}
	}
	return frags
}

func pack(r, g, b float32) uint32 {
	return uint32(unorm(r))<<16 | uint32(unorm(g))<<8 | uint32(unorm(b))
}

// unorm rounds a channel value in [0, 255] (clamping) to an integer.
func unorm(x float32) uint32 {
	if !(x > 0) {
		return 0
	}
	if x >= 255 {
		return 255
	}
	return uint32(x + 0.5)
}

func blendAlpha(dst, src, a uint32) uint32 {
	ia := 255 - a
	r := ((src>>16&0xff)*a + (dst>>16&0xff)*ia + 127) / 255
	g := ((src>>8&0xff)*a + (dst>>8&0xff)*ia + 127) / 255
	b := ((src&0xff)*a + (dst&0xff)*ia + 127) / 255
	da := a + ((dst>>24)*ia+127)/255
	return da<<24 | r<<16 | g<<8 | b
}

func blendAdd(dst, src, a uint32) uint32 {
	r := min(255, (dst>>16&0xff)+((src>>16&0xff)*a+127)/255)
	g := min(255, (dst>>8&0xff)+((src>>8&0xff)*a+127)/255)
	b := min(255, (dst&0xff)+((src&0xff)*a+127)/255)
	return dst&0xff000000 | r<<16 | g<<8 | b
}

// packNormal interpolates and packs the world normal as 0xFFRRGGBB, n*0.5+0.5 per channel.
func packNormal(t *tri, p0, p1, p2 float32) uint32 {
	nx := p0*t.a[0][aNX] + p1*t.a[1][aNX] + p2*t.a[2][aNX]
	ny := p0*t.a[0][aNY] + p1*t.a[1][aNY] + p2*t.a[2][aNY]
	nz := p0*t.a[0][aNZ] + p1*t.a[1][aNZ] + p2*t.a[2][aNZ]
	l := nx*nx + ny*ny + nz*nz
	if l > 0 {
		s := 1 / sqrtf(l)
		nx, ny, nz = nx*s, ny*s, nz*s
	}
	return 0xff000000 | pack((nx*0.5+0.5)*255, (ny*0.5+0.5)*255, (nz*0.5+0.5)*255)
}

// Wireframe colors: pixels whose center is within one pixel of an original edge (on the
// triangle's side) are drawn bright over a dark fill. Shared edges get pixels from both
// neighbours, silhouette edges from one, so every edge is at least one pixel wide; the
// depth test provides hidden-line removal.
const (
	wireLine = 0xffe8e8e8
	wireFill = 0xff262a33
)

func wireColor(t *tri, e0, e1, e2 int64) uint32 {
	const width = 1.0
	if t.wire&1 != 0 && float32(e0)*t.einv[0] < width ||
		t.wire&2 != 0 && float32(e1)*t.einv[1] < width ||
		t.wire&4 != 0 && float32(e2)*t.einv[2] < width {
		return wireLine
	}
	return wireFill
}
