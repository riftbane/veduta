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
	for k := 0; k < c.nchunks; k++ { // chunk order is submission order
		ch := &c.chunks[k]
		for _, ti := range ch.bins[t] {
			tr := &ch.tris[ti]
			rx0, ry0 := max(int(tr.minX), x0), max(int(tr.minY), y0)
			rx1, ry1 := min(int(tr.maxX), x1), min(int(tr.maxY), y1)
			if rx0 > rx1 || ry0 > ry1 {
				continue
			}
			frags += c.rasterTri(tr, rx0, ry0, rx1, ry1)
		}
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
	if cs.fast && c.mode == gfx.ModeColor && fb.Normal == nil {
		return c.rasterTriOpaque(t, cs, x0, y0, x1, y1)
	}
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
			l0 := float32(e0+t.eb[0]) * t.invArea
			l1 := float32(e1+t.eb[1]) * t.invArea
			l2 := float32(e2+t.eb[2]) * t.invArea
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
			// Each product is rounded explicitly so arm64 cannot fuse it into a multiply-add
			// (see gmath.m32); this includes products that reach the +0.5 inside unorm.
			z := float32(l0*t.z[0]) + float32(l1*t.z[1]) + float32(l2*t.z[2])
			if cs.state.DepthTest && !(z < fb.Depth[i]) {
				continue
			}
			w := 1 / (float32(l0*t.iw[0]) + float32(l1*t.iw[1]) + float32(l2*t.iw[2]))
			p0, p1, p2 := l0*w, l1*w, l2*w
			u := float32(p0*t.a[0][aU]) + float32(p1*t.a[1][aU]) + float32(p2*t.a[2][aU])
			v := float32(p0*t.a[0][aV]) + float32(p1*t.a[1][aV]) + float32(p2*t.a[2][aV])

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
				alpha := float32(float32(texel>>24) * (cs.alpha * (1.0 / 255)))
				if alpha < cs.cutoff {
					continue
				}
				r := float32(float32(texel>>16&0xff) * (float32(p0*t.a[0][aR]) + float32(p1*t.a[1][aR]) + float32(p2*t.a[2][aR])))
				g := float32(float32(texel>>8&0xff) * (float32(p0*t.a[0][aG]) + float32(p1*t.a[1][aG]) + float32(p2*t.a[2][aG])))
				b := float32(float32(texel&0xff) * (float32(p0*t.a[0][aB]) + float32(p1*t.a[1][aB]) + float32(p2*t.a[2][aB])))
				if mode == gfx.ModeCollision {
					r, g, b = float32(r*0.5), float32(g*0.5), float32(b*0.5)
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
				if cs.state.DepthWrite && !cs.overlay {
					fb.ID[i] = cs.id
					if fb.Normal != nil {
						fb.Normal[i] = packNormal(t, p0, p1, p2)
					}
				}
			default:
				// Debug modes apply the color mode's rules: the same alpha test, depth
				// written only with DepthWrite, id and normal only with DepthWrite outside
				// overlays, so ids/depth sheets agree with color-mode buffers.
				if !passAlpha(cs, t, u, v) {
					continue
				}
				switch mode {
				case gfx.ModeWireframe:
					fb.Color[i] = wireColor(t, e0-t.edx[0], e1-t.edx[1], e2-t.edx[2])
				case gfx.ModeNormals:
					fb.Color[i] = normalColor(t, cs, p0, p1, p2, x, y)
				case gfx.ModeSilhouette:
					fb.Color[i] = 0xffffffff
				}
				if cs.state.DepthWrite {
					fb.Depth[i] = z
					if !cs.overlay {
						fb.ID[i] = cs.id
						if fb.Normal != nil {
							fb.Normal[i] = packNormal(t, p0, p1, p2)
						}
					}
				}
			}
			frags++
		}
	}
	return frags
}

// rasterTriOpaque is rasterTri specialized for cmdState.fast in ModeColor without a normal
// target. Per-triangle values are hoisted into locals and each row is resliced, so the
// pixel loop has no mode or blend switches and no framebuffer bounds checks. It must
// produce exactly the bits of the generic path (same expressions, same order, same
// explicit rounding of every product).
func (c *core) rasterTriOpaque(t *tri, cs *cmdState, x0, y0, x1, y1 int) int64 {
	fb := c.target
	lv := &cs.tex.levels[t.level]
	pix, tw, wm, hm, fw, fh := lv.pix, lv.w, lv.wmask, lv.hmask, lv.fw, lv.fh
	alphaScale, cutoff := cs.alpha*(1.0/255), cs.cutoff
	alphaTest := cutoff > 0 // alpha >= 0, so a zero (or NaN) cutoff never discards
	depthWrite, id := cs.state.DepthWrite, cs.id
	inv := t.invArea
	b0, b1, b2 := t.eb[0], t.eb[1], t.eb[2]
	dx0, dx1, dx2 := t.edx[0], t.edx[1], t.edx[2]
	z0, z1, z2 := t.z[0], t.z[1], t.z[2]
	w0, w1, w2 := t.iw[0], t.iw[1], t.iw[2]
	a0, a1, a2 := &t.a[0], &t.a[1], &t.a[2]
	var frags int64
	for y := y0; y <= y1; y++ {
		yy, xx := int64(y), int64(x0)
		e0 := t.ec[0] + dx0*xx + t.edy[0]*yy
		e1 := t.ec[1] + dx1*xx + t.edy[1]*yy
		e2 := t.ec[2] + dx2*xx + t.edy[2]*yy
		lo, hi := int64(0), int64(x1-x0)
		if hi >= wideRow {
			if !span(e0, dx0, &lo, &hi) || !span(e1, dx1, &lo, &hi) || !span(e2, dx2, &lo, &hi) || lo > hi {
				continue
			}
			e0 += dx0 * lo
			e1 += dx1 * lo
			e2 += dx2 * lo
		} else {
			for lo <= hi && e0|e1|e2 < 0 {
				e0 += dx0
				e1 += dx1
				e2 += dx2
				lo++
			}
			if lo > hi {
				continue
			}
		}
		start := y*fb.W + x0 + int(lo)
		colors := fb.Color[start : start+int(hi-lo)+1]
		depths := fb.Depth[start:]
		depths = depths[:len(colors)]
		ids := fb.ID[start:]
		ids = ids[:len(colors)]
		for k := range colors {
			if e0|e1|e2 < 0 {
				break // triangles are convex: the row's span has ended
			}
			l0 := float32(e0+b0) * inv
			l1 := float32(e1+b1) * inv
			l2 := float32(e2+b2) * inv
			e0 += dx0
			e1 += dx1
			e2 += dx2
			z := float32(l0*z0) + float32(l1*z1) + float32(l2*z2)
			if !(z < depths[k]) {
				continue
			}
			w := 1 / (float32(l0*w0) + float32(l1*w1) + float32(l2*w2))
			p0, p1, p2 := l0*w, l1*w, l2*w
			u := float32(p0*a0[aU]) + float32(p1*a1[aU]) + float32(p2*a2[aU])
			v := float32(p0*a0[aV]) + float32(p1*a1[aV]) + float32(p2*a2[aV])

			// Bilinear sample, as texture.bilinear with the power-of-two mask wrap.
			fu := bound(float32(u*fw) - 0.5)
			fv := bound(float32(v*fh) - 0.5)
			xf, xi := floorfi(fu)
			yf, yi := floorfi(fv)
			fx := min(uint32((fu-xf)*256), 255)
			fy := min(uint32((fv-yf)*256), 255)
			tx0, tx1 := xi&wm, (xi+1)&wm
			r0, r1 := (yi&hm)*tw, ((yi+1)&hm)*tw
			top := lerpPacked(pix[r0+tx0], pix[r0+tx1], fx)
			bot := lerpPacked(pix[r1+tx0], pix[r1+tx1], fx)
			texel := lerpPacked(top, bot, fy)

			if alphaTest && float32(texel>>24)*alphaScale < cutoff {
				continue
			}
			r := float32(float32(texel>>16&0xff) * (float32(p0*a0[aR]) + float32(p1*a1[aR]) + float32(p2*a2[aR])))
			g := float32(float32(texel>>8&0xff) * (float32(p0*a0[aG]) + float32(p1*a1[aG]) + float32(p2*a2[aG])))
			b := float32(float32(texel&0xff) * (float32(p0*a0[aB]) + float32(p1*a1[aB]) + float32(p2*a2[aB])))
			colors[k] = 0xff000000 | pack(r, g, b)
			if depthWrite {
				depths[k] = z
				ids[k] = id
			}
			frags++
		}
	}
	return frags
}

// passAlpha reports whether a fragment survives the alpha test, computing alpha exactly
// as the color path does (texture sampled with the command's filter, times the tint).
func passAlpha(cs *cmdState, t *tri, u, v float32) bool {
	if cs.cutoff <= 0 {
		return true
	}
	texel := uint32(0xffffffff)
	if cs.tex != nil {
		if cs.filter == gfx.FilterNearest {
			texel = cs.tex.nearest(t.level, u, v)
		} else {
			texel = cs.tex.bilinear(t.level, u, v)
		}
	}
	return !(float32(texel>>24)*(cs.alpha*(1.0/255)) < cs.cutoff)
}

// Hatch colors of ModeNormals.
const (
	hatchBack  = 0xffff00ff // magenta: back-facing triangle (inside-out or wrong winding)
	hatchAway  = 0xffff8800 // orange: normal points away from the viewer (flipped normal)
	hatchDark  = 0xff200020
	awayCosine = -0.25 // n·toViewer below this counts as pointing away
)

// normalColor is the ModeNormals color: the packed world normal, or diagonal stripes when
// the fragment comes from a back-facing triangle or its normal points away from the
// viewer, so flipped normals and inside-out parts are visibly wrong.
func normalColor(t *tri, cs *cmdState, p0, p1, p2 float32, x, y int) uint32 {
	// Products are rounded explicitly so arm64 cannot fuse them into a multiply-add.
	nx := float32(p0*t.a[0][aNX]) + float32(p1*t.a[1][aNX]) + float32(p2*t.a[2][aNX])
	ny := float32(p0*t.a[0][aNY]) + float32(p1*t.a[1][aNY]) + float32(p2*t.a[2][aNY])
	nz := float32(p0*t.a[0][aNZ]) + float32(p1*t.a[1][aNZ]) + float32(p2*t.a[2][aNZ])
	flag := uint32(0)
	switch {
	case t.back:
		flag = hatchBack
	case float32(nx*cs.vz.X)+float32(ny*cs.vz.Y)+float32(nz*cs.vz.Z) < awayCosine*sqrtf(float32(nx*nx)+float32(ny*ny)+float32(nz*nz)):
		flag = hatchAway
	}
	if flag != 0 {
		if (x+y)>>2&1 == 0 {
			return flag
		}
		return hatchDark
	}
	return packNormal(t, p0, p1, p2)
}

func pack(r, g, b float32) uint32 {
	return uint32(unorm(r))<<16 | uint32(unorm(g))<<8 | uint32(unorm(b))
}

// unorm rounds a channel value in [0, 255] (clamping) to an integer. The conversion of x
// keeps a caller's unrounded product from fusing with the + 0.5 once this is inlined.
func unorm(x float32) uint32 {
	if !(x > 0) {
		return 0
	}
	if x >= 255 {
		return 255
	}
	return uint32(float32(x) + 0.5)
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
	nx := float32(p0*t.a[0][aNX]) + float32(p1*t.a[1][aNX]) + float32(p2*t.a[2][aNX])
	ny := float32(p0*t.a[0][aNY]) + float32(p1*t.a[1][aNY]) + float32(p2*t.a[2][aNY])
	nz := float32(p0*t.a[0][aNZ]) + float32(p1*t.a[1][aNZ]) + float32(p2*t.a[2][aNZ])
	l := float32(nx*nx) + float32(ny*ny) + float32(nz*nz)
	if l > 0 {
		s := 1 / sqrtf(l)
		nx, ny, nz = nx*s, ny*s, nz*s
	}
	// The outer products are rounded too: they reach the +0.5 inside the inlined unorm.
	return 0xff000000 | pack(
		float32((float32(nx*0.5)+0.5)*255),
		float32((float32(ny*0.5)+0.5)*255),
		float32((float32(nz*0.5)+0.5)*255),
	)
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
