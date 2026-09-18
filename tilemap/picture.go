package tilemap

import (
	"sort"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/gfx"
)

// CellSize returns the pixels of a cell in Picture: the largest frame of the map's
// terrains' textures, or tile of an autotile (at least 1 × 1).
func (m *Map) CellSize() (w, h int) {
	w, h = 1, 1
	for i := range m.draws {
		if t := m.draws[i].tex; t != nil && len(t.Data.Levels) > 0 {
			fw, fh := t.Data.Levels[0].W/max(t.Grid[0], 1), t.Data.Levels[0].H/max(t.Grid[1], 1)
			if ts := m.draws[i].auto; ts > 0 {
				fw, fh = ts, ts // a tile of it
			}
			w, h = max(w, fw), max(h, fh)
		}
	}
	return w, h
}

// Picture composes the map as seen from above, a cell every CellSize pixels, with the play
// clips of its textures at tick (rate ticks per second): the layers in draw order (by
// layer, then z, then file order), each its cells and then its borders by priority. A frame
// smaller than a cell is scaled to it, nearest texel. Every texel is cut out as the map's
// materials do: alpha of 0.5 or more is drawn opaque, less is not drawn. Empty pixels are
// transparent. It is the picture the inspector shows and the VS Code map editor draws.
func (m *Map) Picture(tick uint64, rate int) *gfx.Image {
	src := m.src
	cw, ch := m.CellSize()
	img := gfx.NewImage(src.W*cw, src.H*ch)
	order := make([]int, len(src.Layers))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		la, lb := &src.Layers[order[a]], &src.Layers[order[b]]
		if la.Layer != lb.Layer {
			return la.Layer < lb.Layer
		}
		return la.Z < lb.Z
	})
	frame := func(t *asset.Texture) int {
		if c := t.Clip(t.Play); c != nil && rate > 0 {
			f, _ := c.Frame(int(tick), rate)
			return f
		}
		return 0
	}
	// blit draws the part (sx, sy, sw, sh) of image s into (dx, dy, dw, dh) of the picture.
	blit := func(s *gfx.Image, sx, sy, sw, sh, dx, dy, dw, dh int) {
		for j := 0; j < dh; j++ {
			ty := sy + j*sh/dh
			for i := 0; i < dw; i++ {
				c := s.Pix[ty*s.W+sx+i*sw/dw]
				if c>>24 >= 128 {
					img.Pix[(dy+j)*img.W+dx+i] = c | 0xff000000
				}
			}
		}
	}
	for _, l := range order {
		for y := 0; y < src.H; y++ {
			for x := 0; x < src.W; x++ {
				v := m.at(l, x, y)
				if v == 0 || m.draws[v-1].tex == nil {
					continue
				}
				t := m.draws[v-1].tex
				base := t.Data.Levels[0]
				gc, gr := max(t.Grid[0], 1), max(t.Grid[1], 1)
				fw, fh := base.W/gc, base.H/gr
				f := frame(t) % (gc * gr)
				if ts := m.draws[v-1].auto; ts > 0 {
					tiles, whole := autoPick(m.autoMask(l, x, y, v))
					for q, tile := range tiles {
						sx, sy := f%gc*fw+tile%autoCols*ts, f/gc*fh+tile/autoCols*ts
						if whole {
							blit(base, sx, sy, ts, ts, x*cw, y*ch, cw, ch)
							break
						}
						dx, dy, dw, dh := quarterRect(q, x*cw, y*ch, cw, ch)
						blit(base, sx+q%2*ts/2, sy+q/2*ts/2, ts/2, ts/2, dx, dy, dw, dh)
					}
					continue
				}
				blit(base, f%gc*fw, f/gc*fh, fw, fh, x*cw, y*ch, cw, ch)
			}
		}
		type quad struct{ x, y, shape, quarter int }
		byTerrain := make([][]quad, len(src.Terrains))
		for y := 0; y < src.H; y++ {
			for x := 0; x < src.W; x++ {
				m.cellEdges(l, x, y, func(u, shape, quarter int) {
					byTerrain[u-1] = append(byTerrain[u-1], quad{x, y, shape, quarter})
				})
			}
		}
		ranked := make([]int, 0, len(byTerrain))
		for i := range byTerrain {
			if len(byTerrain[i]) > 0 {
				ranked = append(ranked, i)
			}
		}
		sort.SliceStable(ranked, func(a, b int) bool { return m.draws[ranked[a]].rank < m.draws[ranked[b]].rank })
		for _, i := range ranked {
			d := &m.draws[i]
			atlas := m.textures["map:edge:"+d.tex.Name]
			a := atlas.Data.Levels[0]
			qw, qh := d.frame[0]/2, d.frame[1]/2
			band := quarters * (qh + 2)
			f := frame(d.tex) % max(atlas.Grid[1], 1)
			for _, e := range byTerrain[i] {
				sx, sy := e.shape*(qw+2)+1, f*band+e.quarter*(qh+2)+1
				dx, dy, dw, dh := quarterRect(e.quarter, e.x*cw, e.y*ch, cw, ch)
				blit(a, sx, sy, qw, qh, dx, dy, dw, dh)
			}
		}
	}
	return img
}

// quarterRect returns quarter q of the cw × ch cell at (x, y) of the picture: an odd cell's
// right and bottom quarters are a pixel larger.
func quarterRect(q, x, y, cw, ch int) (dx, dy, dw, dh int) {
	hw, hh := cw/2, ch/2
	dx, dy, dw, dh = x, y, hw, hh
	if q%2 == 1 {
		dx, dw = dx+hw, cw-hw
	}
	if q/2 == 1 {
		dy, dh = dy+hh, ch-hh
	}
	return dx, dy, dw, dh
}
