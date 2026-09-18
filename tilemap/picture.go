package tilemap

import (
	"sort"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/gfx"
)

// CellSize returns the pixels of a cell in Picture: the largest frame of the map's
// terrains' textures (at least 1 × 1).
func (m *Map) CellSize() (w, h int) {
	w, h = 1, 1
	for i := range m.draws {
		if t := m.draws[i].tex; t != nil && len(t.Data.Levels) > 0 {
			fw, fh := t.Data.Levels[0].W/max(t.Grid[0], 1), t.Data.Levels[0].H/max(t.Grid[1], 1)
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
				// Quarters split the cell: an odd cell's right and bottom ones are a pixel larger.
				hw, hh := cw/2, ch/2
				dx, dy, dw, dh := e.x*cw, e.y*ch, hw, hh
				if e.quarter%2 == 1 {
					dx, dw = dx+hw, cw-hw
				}
				if e.quarter/2 == 1 {
					dy, dh = dy+hh, ch-hh
				}
				blit(a, sx, sy, qw, qh, dx, dy, dw, dh)
			}
		}
	}
	return img
}
