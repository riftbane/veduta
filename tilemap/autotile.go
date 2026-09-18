package tilemap

import (
	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/gfx"
)

// An autotile frame is 6 × 3 square tiles: on the left an island, the terrain in 3 × 3
// cells with nothing around it (convex corners), on the right a lake, the terrain in the
// 8 cells around one empty cell (concave corners); the lake's middle tile is not used.
// A cell of the terrain looks at its 8 neighbours (the same terrain on its layer, or off
// the map, counts as itself) and each of its quarters, from the two sides and the corner
// it touches, falls in a class. A cell whose four classes are those of a tile, as the tile
// sits in its drawing, is drawn with the whole tile; any other with four quarters, each
// from the tile that stands for its class.

// Classes of a quarter of an autotile cell.
const (
	classFull      = iota // the sides and the corner are the terrain
	classInner            // the sides are, the corner is not: a concave corner
	classConvex           // neither side is: a convex corner
	classVStraight        // the side above or below is not, the other side is, the corner is not: a straight edge
	classVConcave         // the side above or below is not, the other side and the corner are: the edge turns in
	classHStraight        // as classVStraight, with the side left or right
	classHConcave         // as classVConcave, with the side left or right
)

// Autotile tiles: column + 6 × row of the frame.
const (
	autoCols  = 6
	autoRows  = 3
	lakeEmpty = 1*autoCols + 4 // the lake's middle
)

// Neighbours, as bits of a cell's mask: set when the neighbour is the same terrain.
const (
	nbN = 1 << iota
	nbNE
	nbE
	nbSE
	nbS
	nbSW
	nbW
	nbNW
)

// neighbours are the offsets of a cell's neighbours, in the order of their bits.
var neighbours = [8][2]int{{0, -1}, {1, -1}, {1, 0}, {1, 1}, {0, 1}, {-1, 1}, {-1, 0}, {-1, -1}}

// quarterSides are, per quarter, its side above or below, its side left or right and its
// corner.
var quarterSides = [quarters][3]int{
	quarterNW: {nbN, nbW, nbNW}, quarterNE: {nbN, nbE, nbNE}, quarterSW: {nbS, nbW, nbSW}, quarterSE: {nbS, nbE, nbSE},
}

// quarterClass returns the class of quarter q of a cell whose neighbours are mask.
func quarterClass(mask, q int) int {
	s := quarterSides[q]
	v, h, d := mask&s[0] != 0, mask&s[1] != 0, mask&s[2] != 0
	switch {
	case v && h && d:
		return classFull
	case v && h:
		return classInner
	case !v && !h:
		return classConvex
	case !v && d:
		return classVConcave
	case !v:
		return classVStraight
	case d:
		return classHConcave
	}
	return classHStraight
}

// classTile is, per class and quarter, the tile a quarter of that class is taken from.
var classTile = [7][quarters]int{
	classFull:      {7, 7, 7, 7},   // the island's middle
	classInner:     {17, 15, 5, 3}, // the lake's corner diagonal to the empty cell
	classConvex:    {0, 2, 12, 14}, // the island's corners
	classVStraight: {1, 1, 13, 13}, // the island's top and bottom
	classVConcave:  {16, 16, 4, 4}, // the lake's bottom and top
	classHStraight: {6, 8, 6, 8},   // the island's left and right
	classHConcave:  {11, 9, 11, 9}, // the lake's right and left
}

// tileClasses is, per tile, the classes of its quarters as it sits in its drawing (-1 for
// the lake's middle): a cell with these classes is drawn with the whole tile.
var tileClasses = func() (out [autoCols * autoRows][quarters]int) {
	// same reports whether cell (x, y) of the drawing is the terrain, for tile t: around
	// the island there is nothing, around the lake the terrain goes on.
	same := func(t, x, y int) bool {
		if t%autoCols < 3 {
			return x >= 0 && x < 3 && y >= 0 && y < 3
		}
		return x != 4 || y != 1
	}
	for t := range out {
		x, y := t%autoCols, t/autoCols
		mask := 0
		for k, o := range neighbours {
			if same(t, x+o[0], y+o[1]) {
				mask |= 1 << k
			}
		}
		for q := range out[t] {
			out[t][q] = quarterClass(mask, q)
		}
	}
	out[lakeEmpty] = [quarters]int{-1, -1, -1, -1}
	return out
}()

// autoPick returns how a cell whose neighbours are mask is drawn: the whole tile (and
// whole true), or the tile of each quarter.
func autoPick(mask int) (tiles [quarters]int, whole bool) {
	var cls [quarters]int
	for q := range cls {
		cls[q] = quarterClass(mask, q)
	}
	for t, c := range tileClasses {
		if c == cls {
			return [quarters]int{t, t, t, t}, true
		}
	}
	for q, c := range cls {
		tiles[q] = classTile[c][q]
	}
	return tiles, false
}

// autoMask returns the neighbours of cell (x, y) of layer l that are its own terrain v; a
// neighbour off the map counts as v.
func (m *Map) autoMask(l, x, y, v int) int {
	mask := 0
	for k, o := range neighbours {
		nx, ny := x+o[0], y+o[1]
		if !m.Inside(nx, ny) || m.at(l, nx, ny) == v {
			mask |= 1 << k
		}
	}
	return mask
}

// AutoAtlas returns the atlas the map draws autotile texture t with: for every frame of t a
// band of its 6 × 3 tiles, each padded by a pixel that repeats its border. It has t's
// frames as a grid of one column, and t's clips.
func AutoAtlas(name string, t *asset.Texture) *asset.Texture {
	src := t.Data.Levels[0]
	gc, gr := max(t.Grid[0], 1), max(t.Grid[1], 1)
	fw, fh := src.W/gc, src.H/gr
	ts := fw / autoCols
	cs := ts + 2
	frames := gc * gr
	img := gfx.NewImage(autoCols*cs, autoRows*cs*frames)
	for f := 0; f < frames; f++ {
		fx, fy := f%gc*fw, f/gc*fh
		for tile := 0; tile < autoCols*autoRows; tile++ {
			tx, ty := tile%autoCols*ts, tile/autoCols*ts
			ox, oy := tile%autoCols*cs, (f*autoRows+tile/autoCols)*cs
			for j := -1; j <= ts; j++ {
				for i := -1; i <= ts; i++ {
					px, py := tx+min(max(i, 0), ts-1), ty+min(max(j, 0), ts-1)
					img.Pix[(oy+j+1)*img.W+ox+i+1] = src.Pix[(fy+py)*src.W+fx+px]
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

// autoUV returns the texture coordinates, within one frame's band of the autotile atlas of
// tiles ts pixels large, of tile: the whole tile, or its quarter q when q ≥ 0.
func autoUV(tile, q, ts int) (u0, v0, u1, v1 float32) {
	cs := ts + 2
	aw, bh := float32(autoCols*cs), float32(autoRows*cs)
	x, y, w := tile%autoCols*cs+1, tile/autoCols*cs+1, ts
	if q >= 0 {
		w = ts / 2
		x += q % 2 * w
		y += q / 2 * w
	}
	return float32(x) / aw, float32(y) / bh, float32(x+w) / aw, float32(y+w) / bh
}
