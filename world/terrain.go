package world

import (
	"hash/fnv"
	"math"
	"sort"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/gmath"
)

// NoWater marks a vertex without water.
const NoWater = math.MinInt32

// Terrain shaping constants (docs/world.md).
const (
	padBlend = 2  // cells over which a structure's pad blends into the ground around it
	rimCells = 2  // cells of dry rim around a lake or a sea
	rimMM    = 50 // millimeters the rim stands above the water, so it never z-fights with it
)

// Salts of the terrain's hash streams.
const (
	saltRelief  = 0x72656c696566 // "relief"
	saltFeature = 0x66656174     // "feat"
)

// feature is a compiled terrain feature in integer units.
type feature struct {
	*asset.Feature
	level, height, depth int64 // millimeters
	r, fall              int64 // cells
	rough                int64 // Q16
	reach                int64 // cells: a vertex this far from the centre or farther is untouched
	noise                field // edge wander
}

// mm converts meters to whole millimeters.
func mm(v float32) int64 { return int64(math.Round(float64(v) * 1000)) }

// meters converts millimeters to meters.
func mmMeters(v int64) float32 { return float32(float64(v) / 1000) }

// initTerrain compiles the terrain and resolves the features' default levels, each from
// the ground as the features before it left it.
func (g *Gen) initTerrain() {
	t := &g.W.Terrain
	g.relief = mm(t.Relief)
	g.reliefField = field{seed: mix64(g.W.Seed ^ saltRelief), scale: int32(max(t.ReliefScale, 1))}
	g.seaLevel = int64(NoWater)
	if t.Sea {
		g.seaLevel = mm(t.SeaLevel)
	}
	for i := range g.W.Features {
		f := &g.W.Features[i]
		h := fnv.New64a()
		h.Write([]byte(f.Name))
		ft := feature{Feature: f, height: mm(f.Height), depth: mm(f.Depth), r: int64(f.Radius), fall: int64(f.Falloff),
			rough: int64(math.Round(float64(f.Roughness) * 65536)),
			noise: field{seed: mix64(g.W.Seed ^ saltFeature ^ h.Sum64()), scale: int32(min(max(f.Radius, 4), 512))}}
		wander := (ft.r*ft.rough)>>17 + 1 // the radius moves by at most rough/2
		ft.reach = ft.r + wander + 1
		if f.IsWater() {
			ft.reach += rimCells + ft.fall
		}
		switch {
		case f.HasHeight:
			ft.level = ft.height
		case f.Kind == asset.FeatureSea:
			ft.level = 0
			if t.Sea {
				ft.level = g.seaLevel
			}
		default:
			ft.level, _ = g.natural(f.Cell[0], f.Cell[1], g.features)
		}
		g.features = append(g.features, ft)
	}
}

// isqrtQ8 returns sqrt(d2) in 1/256 cells for d2 in cells² (d2 < 2^46).
func isqrtQ8(d2 int64) int64 {
	n := d2 << 16
	r := int64(math.Sqrt(float64(n)))
	for r*r > n {
		r--
	}
	for (r+1)*(r+1) <= n {
		r++
	}
	return r
}

// weight is the Q16 weight of a vertex at distance d (Q8) from a centre: 1 within
// inner, 0 from outer on, smoothstep between.
func weight(d, inner, outer int64) int64 {
	inner = max(inner, 0)
	switch {
	case d <= inner:
		return 65536
	case d >= outer:
		return 0
	}
	return smooth((outer - d) << 16 / (outer - inner))
}

// natural returns the height of vertex (x, z) in millimeters and the level of the water
// over it (NoWater when none) from the relief and the given features, in order.
// Structures' pads are not included.
func (g *Gen) natural(x, z int32, feats []feature) (h, water int64) {
	if g.relief != 0 {
		h = (int64(g.reliefField.smoothValue(x, z)) - 1<<31) * g.relief >> 31
	}
	water = g.seaLevel
	for i := range feats {
		f := &feats[i]
		dx, dz := int64(x-f.Cell[0]), int64(z-f.Cell[1])
		if dx <= -f.reach || dx >= f.reach || dz <= -f.reach || dz >= f.reach {
			continue
		}
		d2 := dx*dx + dz*dz
		if d2 >= f.reach*f.reach {
			continue
		}
		d := isqrtQ8(d2)
		// The edge wanders by up to ±rough/2 of the radius with the feature's noise.
		r := f.r << 8
		r += r * f.rough >> 16 * (int64(f.noise.value(x, z)>>16) - 32768) >> 16
		switch f.Kind {
		case asset.FeatureHill:
			h += f.height * weight(d, r-f.fall<<8, r) >> 16
		case asset.FeaturePlain:
			h += (f.level - h) * weight(d, r-f.fall<<8, r) >> 16
		default: // lake, sea
			rim := r + rimCells<<8
			switch {
			case d < r:
				h = f.level + rimMM - (f.depth+rimMM)*smooth((r-d)<<16/r)>>16
				water = max(water, f.level)
			case d < rim:
				h = f.level + rimMM
			case d < rim+f.fall<<8:
				t := smooth((d - rim) << 16 / (f.fall << 8))
				h = f.level + rimMM + (h-f.level-rimMM)*t>>16
			}
		}
	}
	return h, water
}

// nearFeatures returns the features that can touch the vertices of rect (inclusive of
// its far edges).
func (g *Gen) nearFeatures(x0, z0, x1, z1 int32) []feature {
	var out []feature
	for i := range g.features {
		f := &g.features[i]
		r := int32(f.reach)
		if f.Cell[0]+r > x0 && f.Cell[0]-r < x1 && f.Cell[1]+r > z0 && f.Cell[1]-r < z1 {
			out = append(out, *f)
		}
	}
	return out
}

// Grid holds the vertex heights and water levels of a rectangle of vertices.
type Grid struct {
	X, Z   int32   // the first vertex
	W, D   int32   // vertices along x and z
	Height []int32 // millimeters, row-major
	Water  []int32 // millimeters, NoWater where dry
}

// At returns the height and the water level of vertex (x, z), which must be inside.
func (gr *Grid) At(x, z int32) (h, water int32) {
	i := (z-gr.Z)*gr.W + (x - gr.X)
	return gr.Height[i], gr.Water[i]
}

// Grid computes the vertices of the cells in rect grown by grow cells on every side:
// relief, features, then the pads of the structures near them, in key order.
func (g *Gen) Grid(rect Rect, grow int32) *Grid {
	gr := &Grid{X: rect.X - grow, Z: rect.Z - grow, W: rect.W + 1 + 2*grow, D: rect.D + 1 + 2*grow}
	n := int(gr.W * gr.D)
	gr.Height, gr.Water = make([]int32, n), make([]int32, n)
	x1, z1 := gr.X+gr.W-1, gr.Z+gr.D-1
	feats := g.nearFeatures(gr.X, gr.Z, x1, z1)
	for j := int32(0); j < gr.D; j++ {
		for i := int32(0); i < gr.W; i++ {
			h, w := g.natural(gr.X+i, gr.Z+j, feats)
			gr.Height[j*gr.W+i], gr.Water[j*gr.W+i] = clampMM(h), clampMM(w)
		}
	}
	for _, s := range g.padStructs(Rect{gr.X, gr.Z, gr.W, gr.D}) {
		g.pad(gr, &s)
	}
	return gr
}

func clampMM(v int64) int32 { return int32(max(min(v, math.MaxInt32), NoWater)) }

// padStructs returns the sites and places whose pad reaches the vertices of rect, by key.
func (g *Gen) padStructs(vr Rect) []Struct {
	area := Rect{vr.X - padBlend - 1, vr.Z - padBlend - 1, vr.W + 2*padBlend + 1, vr.D + 2*padBlend + 1}
	var out []Struct
	for i := range g.places {
		if g.places[i].Rect.Overlaps(area) {
			out = append(out, g.places[i])
		}
	}
	for r := range g.sites {
		g.eachSite(r, area, func(t *Struct) bool {
			if t.Rect.Overlaps(area) {
				out = append(out, *t)
			}
			return false
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// padLevel returns the height a structure's footprint is levelled to: the natural
// ground at the centre of its footprint.
func (g *Gen) padLevel(r Rect) int32 {
	h, _ := g.natural(r.X+r.W/2, r.Z+r.D/2, g.nearFeatures(r.X+r.W/2, r.Z+r.D/2, r.X+r.W/2, r.Z+r.D/2))
	return clampMM(h)
}

// pad levels the vertices of s's footprint to its ground and blends the ones within
// padBlend cells of it.
func (g *Gen) pad(gr *Grid, s *Struct) {
	level := int64(s.Ground)
	r := s.Rect
	for z := max(gr.Z, r.Z-padBlend); z <= min(gr.Z+gr.D-1, r.Z+r.D+padBlend); z++ {
		for x := max(gr.X, r.X-padBlend); x <= min(gr.X+gr.W-1, r.X+r.W+padBlend); x++ {
			gx := int64(max(r.X-x, x-(r.X+r.W), 0))
			gz := int64(max(r.Z-z, z-(r.Z+r.D), 0))
			i := (z-gr.Z)*gr.W + (x - gr.X)
			h := int64(gr.Height[i])
			if d2 := gx*gx + gz*gz; d2 == 0 {
				h = level
			} else if d2 < padBlend*padBlend {
				b := int64(padBlend) << 8
				h += (level - h) * smooth((b-isqrtQ8(d2))<<16/b) >> 16
			}
			gr.Height[i] = clampMM(h)
		}
	}
}

// wet reports whether a vertex of height h under water level w is under water.
func wet(h, w int32) bool { return w != NoWater && h < w }

// dryFootprint reports whether no vertex of r stands under water (pads not included).
func (g *Gen) dryFootprint(r Rect) bool {
	feats := g.nearFeatures(r.X, r.Z, r.X+r.W, r.Z+r.D)
	if len(feats) == 0 && g.seaLevel == NoWater {
		return true
	}
	for z := r.Z; z <= r.Z+r.D; z++ {
		for x := r.X; x <= r.X+r.W; x++ {
			if h, w := g.natural(x, z, feats); wet(clampMM(h), clampMM(w)) {
				return false
			}
		}
	}
	return true
}

// cellGround returns the height at the centre of cell (x, z) of gr, on the ground mesh's
// diagonal from its (x, z+1) corner to its (x+1, z) corner, and the highest water level
// of its corners.
func (gr *Grid) cellGround(x, z int32) (h, water int32) {
	h01, w01 := gr.At(x, z+1)
	h10, w10 := gr.At(x+1, z)
	_, w00 := gr.At(x, z)
	_, w11 := gr.At(x+1, z+1)
	return int32((int64(h01) + int64(h10)) / 2), max(w00, w01, w10, w11)
}

// CellWater reports whether cell (x, z) of gr is water: its centre lies below the
// highest water level of its corners.
func (gr *Grid) CellWater(x, z int32) bool {
	h, w := gr.cellGround(x, z)
	return wet(h, w)
}

// heightAt returns the ground height under (px, pz) meters on a grid of step cells
// whose quads start at vertex (ox, oz), each split along its diagonal from its (x0, z1)
// corner to its (x1, z0) corner, as the ground mesh is.
func (gr *Grid) heightAt(px, pz, cell float32, step, ox, oz int32) float32 {
	x, z := cellOf(px, cell), cellOf(pz, cell)
	x0, z0 := ox+floorDiv(x-ox, step)*step, oz+floorDiv(z-oz, step)*step
	s := float64(step)
	fx := (float64(px)/float64(cell) - float64(x0)) / s
	fz := (float64(pz)/float64(cell) - float64(z0)) / s
	m := func(x, z int32) float64 { h, _ := gr.At(x, z); return float64(h) / 1000 }
	h00, h10, h01, h11 := m(x0, z0), m(x0+step, z0), m(x0, z0+step), m(x0+step, z0+step)
	// Products rounded explicitly so arm64 cannot fuse them into the sums.
	if fx+fz <= 1 {
		return float32(h00 + float64((h10-h00)*fx) + float64((h01-h00)*fz))
	}
	return float32(h11 + float64((h01-h11)*(1-fx)) + float64((h10-h11)*(1-fz)))
}

// HeightAt returns the ground height under (px, pz) meters, on the triangles of the
// chunk's finest drawn level: a hero set to it stands exactly on the ground.
func (c *Chunk) HeightAt(px, pz, cell float32) float32 {
	return c.Grid.heightAt(px, pz, cell, c.Step, c.Rect.X, c.Rect.Z)
}

// WaterAt returns the water level over (px, pz) meters and whether the ground there is
// under it.
func (c *Chunk) WaterAt(px, pz, cell float32) (float32, bool) {
	x, z := cellOf(px, cell), cellOf(pz, cell)
	var w int32 = NoWater
	for _, v := range [4][2]int32{{x, z}, {x + 1, z}, {x, z + 1}, {x + 1, z + 1}} {
		_, cw := c.Grid.At(v[0], v[1])
		w = max(w, cw)
	}
	if w == NoWater {
		return 0, false
	}
	level := mmMeters(int64(w))
	return level, c.HeightAt(px, pz, cell) < level
}

// ChunkAt returns the chunk holding pos, generated.
func (g *Gen) ChunkAt(pos gmath.Vec3) *Chunk {
	return g.Chunk(g.ChunkOf(g.CellOf(pos)))
}

// HeightAt returns the ground height under pos (its x and z) in meters. It generates the
// chunk: a game asks its loaded World instead.
func (g *Gen) HeightAt(pos gmath.Vec3) float32 {
	return g.ChunkAt(pos).HeightAt(pos.X, pos.Z, g.W.Cell)
}

// WaterAt returns the water level over pos and whether the ground there is under it.
func (g *Gen) WaterAt(pos gmath.Vec3) (float32, bool) {
	return g.ChunkAt(pos).WaterAt(pos.X, pos.Z, g.W.Cell)
}
