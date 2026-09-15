package world

import (
	"sort"
)

// Region describes a rectangle of the world for the tools: the biome of every cell,
// the structures touching it and the scatter inside it.
type Region struct {
	Rect    Rect
	Biomes  []int     // row-major, len Rect.W*Rect.D
	Shares  []float64 // fraction of the region's cells per biome
	Structs []Struct  // sites and places overlapping the region, by key
	Scatter []Struct  // scatter items inside the region, in chunk then cell order
}

// Region generates the description of the cells within radius of center, clipped to
// the world.
func (g *Gen) Region(center [2]int32, radius int32) *Region {
	r := Rect{center[0] - radius, center[1] - radius, 2*radius + 1, 2*radius + 1}
	b := g.Bounds()
	x0, z0 := max(r.X, b.X), max(r.Z, b.Z)
	x1, z1 := min(r.X+r.W, b.X+b.W), min(r.Z+r.D, b.Z+b.D)
	r = Rect{x0, z0, max(0, x1-x0), max(0, z1-z0)}
	out := &Region{Rect: r, Biomes: make([]int, r.W*r.D), Shares: make([]float64, len(g.W.Biomes))}
	for z := int32(0); z < r.D; z++ {
		for x := int32(0); x < r.W; x++ {
			bi := g.Biome(r.X+x, r.Z+z)
			out.Biomes[z*r.W+x] = bi
			out.Shares[bi]++
		}
	}
	if n := float64(r.W * r.D); n > 0 {
		for i := range out.Shares {
			out.Shares[i] /= n
		}
	}
	if r.W == 0 || r.D == 0 {
		return out
	}
	seen := map[string]bool{}
	cx0, cz0 := g.ChunkOf(r.X, r.Z)
	cx1, cz1 := g.ChunkOf(r.X+r.W-1, r.Z+r.D-1)
	for cz := cz0; cz <= cz1; cz++ {
		for cx := cx0; cx <= cx1; cx++ {
			for _, s := range g.Structures(cx, cz) {
				if !seen[s.Key] && s.Rect.Overlaps(r) {
					seen[s.Key] = true
					out.Structs = append(out.Structs, s)
				}
			}
			if len(g.scatter) == 0 {
				continue
			}
			for _, s := range g.Chunk(cx, cz).Scatter {
				if s.Rect.Overlaps(r) {
					out.Scatter = append(out.Scatter, s)
				}
			}
		}
	}
	sort.Slice(out.Structs, func(i, j int) bool { return out.Structs[i].Key < out.Structs[j].Key })
	return out
}

// Nearest is the closest structure carrying a tag, with the free cells between it and
// the queried cell.
type Nearest struct {
	Tag      string `json:"tag"`
	Key      string `json:"key"`
	Distance int32  `json:"distance"`
}

// CellInfo answers what is at a cell.
type CellInfo struct {
	Cell     [2]int32  `json:"cell"`
	Chunk    [2]int32  `json:"chunk"`
	Biome    string    `json:"biome"`
	Ground   string    `json:"ground"`
	Occupant *Struct   `json:"occupant,omitempty"` // the place, site or scatter item covering the cell
	Nearest  []Nearest `json:"nearest"`            // per place name and site tag, within reach
}

// Query describes cell (x, z): its biome, what occupies it, and the nearest place of
// each name and site of each rule tag within reach regions (searched in growing rings
// of site regions; places are always found).
func (g *Gen) Query(x, z int32, reach int32) *CellInfo {
	cx, cz := g.ChunkOf(x, z)
	b := g.Biome(x, z)
	info := &CellInfo{Cell: [2]int32{x, z}, Chunk: [2]int32{cx, cz}, Biome: g.W.Biomes[b].Name, Ground: g.W.Biomes[b].Ground, Nearest: []Nearest{}}
	here := Rect{x, z, 1, 1}
	for _, s := range g.Structures(cx, cz) {
		if s.Rect.Contains(x, z) {
			s := s
			info.Occupant = &s
			break
		}
	}
	if info.Occupant == nil {
		for _, s := range g.Chunk(cx, cz).Scatter {
			if s.Rect.Contains(x, z) {
				s := s
				info.Occupant = &s
				break
			}
		}
	}
	for i := range g.places {
		p := &g.places[i]
		info.Nearest = append(info.Nearest, Nearest{Tag: p.Place, Key: p.Key, Distance: here.Gap(p.Rect)})
	}
	for r := range g.sites {
		s := int32(g.sites[r].Spacing)
		var best *Struct
		g.eachSite(r, here.Grow(reach*s), func(t *Struct) bool {
			if best == nil || here.Gap(t.Rect) < here.Gap(best.Rect) {
				best = t
			}
			return false
		})
		if best != nil {
			info.Nearest = append(info.Nearest, Nearest{Tag: g.sites[r].Tag, Key: best.Key, Distance: here.Gap(best.Rect)})
		}
	}
	return info
}
