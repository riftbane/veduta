package world

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/gmath"
)

// Rect is a rectangle of cells: x in [X, X+W), z in [Z, Z+D).
type Rect struct{ X, Z, W, D int32 }

// Overlaps reports whether the rectangles share a cell.
func (r Rect) Overlaps(o Rect) bool {
	return r.X < o.X+o.W && o.X < r.X+r.W && r.Z < o.Z+o.D && o.Z < r.Z+r.D
}

// Contains reports whether cell (x, z) is inside r.
func (r Rect) Contains(x, z int32) bool {
	return x >= r.X && x < r.X+r.W && z >= r.Z && z < r.Z+r.D
}

// Grow returns r extended by n cells on every side.
func (r Rect) Grow(n int32) Rect { return Rect{r.X - n, r.Z - n, r.W + 2*n, r.D + 2*n} }

// Inside reports whether r lies within o.
func (r Rect) Inside(o Rect) bool {
	return r.X >= o.X && r.Z >= o.Z && r.X+r.W <= o.X+o.W && r.Z+r.D <= o.Z+o.D
}

// Center returns the cell at the middle of r.
func (r Rect) Center() (x, z int32) { return r.X + r.W/2, r.Z + r.D/2 }

// Gap returns the number of free cells between r and o along the axis where they are
// farthest apart (0 when they touch or overlap).
func (r Rect) Gap(o Rect) int32 {
	gx := max(o.X-(r.X+r.W), r.X-(o.X+o.W), 0)
	gz := max(o.Z-(r.Z+r.D), r.Z-(o.Z+o.D), 0)
	return max(gx, gz)
}

// Struct is one placed structure: a prefab on a rectangle of cells. Key is unique in
// the world and prefixes the names of the structure's entities.
type Struct struct {
	Key      string
	Prefab   *asset.Prefab
	Rect     Rect
	Rotation int      // degrees about +Y: 0, 90, 180 or 270
	Tags     []string // the prefab's tags, plus the site rule's tag for a site
	Place    string   // the place's name, "" for a site or a scatter item
	// Vegetation is the vegetation rule's name for a prefab it scattered.
	Vegetation string
	Site       int      // the site rule's index, -1 for a place or a scatter item
	Cell       [2]int32 // scatter: the cell; site: the region; place: the cell
	Ground     int32    // millimeters: the height the structure stands on
}

// Chunk is the generated content of one chunk: its cells' biomes, its vertices and the
// scatter items. The ground model comes from Gen.Ground, the sites and places touching
// the chunk from Gen.Structures.
type Chunk struct {
	X, Z    int32
	Rect    Rect
	Biomes  []int    // per cell, row-major
	Grid    *Grid    // heights and water, from one cell before the chunk to one after it
	Step    int32    // grid step of the finest drawn level (Gen.BaseStep)
	Scatter []Struct // in cell order (row-major)
	Flora   []Flora  // in cell order, then rule order
}

// scatterRule is a scatter rule or a vegetation rule with a prefab: one prefab per cell.
type scatterRule struct {
	prefab     *asset.Prefab // nil when missing
	vegetation string        // the vegetation rule's name, "" for a scatter rule
	salt       uint64        // hash stream: a scatter rule's index, a vegetation rule's name
	index      int64
	cut        uint64
	biomes     []bool    // allowed biome indices
	area       *areaRule // nil: everywhere
}

type siteRule struct {
	asset.Site
	prefabs []*asset.Prefab // the known ones, in rule order
	cut     uint64
	dist    int32 // cells: the largest min_distance of the rule's prefabs
}

// Gen generates the content of one world. Create it with New; it is not safe for
// concurrent use (it memoizes sites).
type Gen struct {
	W       *asset.World
	Missing []string // prefabs the world names but the lookup did not find, sorted

	prefabs map[string]*asset.Prefab // lookup only, never iterated
	noise   field
	cum     []uint64 // biome i covers noise values below cum[i]
	scatter []scatterRule
	sites   []siteRule
	places  []Struct
	memo    map[[3]int32]*Struct // site memo: rule, rx, rz → the kept site or nil

	relief      int64 // millimeters
	reliefField field
	seaLevel    int64 // millimeters, NoWater without a sea
	features    []feature
	flora       []floraRule
}

// New prepares a generator for w. prefabs resolves prefab names (nil: none known);
// rules naming unknown prefabs are skipped and the names listed in Missing.
func New(w *asset.World, prefabs func(string) *asset.Prefab) *Gen {
	if prefabs == nil {
		prefabs = func(string) *asset.Prefab { return nil }
	}
	g := &Gen{W: w, prefabs: map[string]*asset.Prefab{}, memo: map[[3]int32]*Struct{}}
	g.noise = field{seed: w.Seed, scale: int32(w.BiomeScale)}
	missing := map[string]bool{}
	lookup := func(name string) *asset.Prefab {
		if p, ok := g.prefabs[name]; ok {
			return p
		}
		p := prefabs(name)
		g.prefabs[name] = p
		if p == nil {
			missing[name] = true
		}
		return p
	}
	g.cum = g.thresholds()
	g.initTerrain()
	for i, s := range w.Scatter {
		p := lookup(s.Prefab)
		r := scatterRule{prefab: p, salt: saltScatter, index: int64(i), cut: share(s.Density)}
		if p != nil {
			r.biomes = g.allowed(s.Biomes, p.Biomes)
		}
		g.scatter = append(g.scatter, r)
	}
	g.initVegetation(lookup)
	for _, s := range w.Sites {
		r := siteRule{Site: s, cut: share(s.Chance)}
		var dist float32
		for _, name := range s.Prefabs {
			if p := lookup(name); p != nil {
				r.prefabs = append(r.prefabs, p)
				dist = max(dist, p.MaxDistance())
			}
		}
		r.dist = int32(w.Cells(dist))
		g.sites = append(g.sites, r)
	}
	for _, pl := range w.Places {
		p := lookup(pl.Prefab)
		if p == nil {
			continue
		}
		fw, fd := w.Footprint(p, pl.Rotation)
		rect := Rect{pl.Cell[0], pl.Cell[1], int32(fw), int32(fd)}
		g.places = append(g.places, Struct{
			Key: "place_" + pl.Name, Prefab: p, Rect: rect,
			Rotation: pl.Rotation, Tags: p.Tags, Place: pl.Name, Site: -1, Cell: pl.Cell, Ground: g.padLevel(rect),
		})
	}
	for name := range missing {
		g.Missing = append(g.Missing, name)
	}
	sort.Strings(g.Missing)
	return g
}

// thresholds splits the noise among the biomes in proportion to their weights. Value
// noise is not uniform (it crowds around the middle of its range), so the cuts are
// quantiles of a fixed sample of the field: 96×96 cells, three per lattice step, at
// seeded offsets, sorted. The last cut is the top of the range.
func (g *Gen) thresholds() []uint64 {
	w := g.W
	const n = 96
	stride := max(1, g.noise.scale/3)
	h := hash(w.Seed, saltBiome, -1, -1, -1)
	ox, oz := int32(h%uint64(g.noise.scale)), int32((h>>32)%uint64(g.noise.scale))
	samples := make([]uint32, 0, n*n)
	for j := int32(0); j < n; j++ {
		for i := int32(0); i < n; i++ {
			samples = append(samples, g.noise.value(ox+i*stride, oz+j*stride))
		}
	}
	sort.Slice(samples, func(a, b int) bool { return samples[a] < samples[b] })
	var total, acc uint64
	for _, b := range w.Biomes {
		total += uint64(b.Weight)
	}
	cum := make([]uint64, 0, len(w.Biomes))
	for _, b := range w.Biomes {
		acc += uint64(b.Weight)
		k := int(acc * uint64(len(samples)) / total)
		if k >= len(samples) {
			cum = append(cum, 1<<32)
		} else {
			cum = append(cum, uint64(samples[k]))
		}
	}
	if len(cum) > 0 {
		cum[len(cum)-1] = 1 << 32
	}
	return cum
}

// allowed returns the biome indices a rule with its own biome list and a prefab's rule
// list both allow (an empty list allows every biome).
func (g *Gen) allowed(rule, prefab []string) []bool {
	out := make([]bool, len(g.W.Biomes))
	for i, b := range g.W.Biomes {
		out[i] = (len(rule) == 0 || contains(rule, b.Name)) && (len(prefab) == 0 || contains(prefab, b.Name))
	}
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Bounds returns the rectangle of every cell of the world.
func (g *Gen) Bounds() Rect {
	span := int32(g.W.Span())
	return Rect{-span, -span, 2 * span, 2 * span}
}

// ChunkRect returns the cells of chunk (cx, cz).
func (g *Gen) ChunkRect(cx, cz int32) Rect {
	n := int32(g.W.Chunk)
	return Rect{cx * n, cz * n, n, n}
}

// ChunkOf returns the chunk holding cell (x, z).
func (g *Gen) ChunkOf(x, z int32) (cx, cz int32) {
	n := int32(g.W.Chunk)
	return floorDiv(x, n), floorDiv(z, n)
}

// CellOf returns the cell holding a world position (its x and z).
func (g *Gen) CellOf(pos gmath.Vec3) (x, z int32) {
	return cellOf(pos.X, g.W.Cell), cellOf(pos.Z, g.W.Cell)
}

func cellOf(v, cell float32) int32 {
	q := float64(v) / float64(cell)
	i := int32(q)
	if float64(i) > q {
		i--
	}
	return i
}

// Origin returns the world position of the min corner of cell (x, z), on the ground.
func (g *Gen) Origin(x, z int32) gmath.Vec3 {
	return gmath.V3(meters(x, g.W.Cell), 0, meters(z, g.W.Cell))
}

// Center returns the world position of the middle of cell (x, z), on the ground.
func (g *Gen) Center(x, z int32) gmath.Vec3 {
	// Products and halves rounded explicitly, so arm64 cannot fuse them into the sum.
	c := float64(g.W.Cell)
	half := float64(c / 2)
	return gmath.V3(float32(float64(float64(x)*c)+half), 0, float32(float64(float64(z)*c)+half))
}

// meters converts a cell coordinate to meters (one product, no fusion possible).
func meters(cells int32, cell float32) float32 {
	return float32(float64(cells) * float64(cell))
}

// Biome returns the index of the biome of cell (x, z).
func (g *Gen) Biome(x, z int32) int {
	v := uint64(g.noise.value(x, z))
	for i, c := range g.cum {
		if v < c {
			return i
		}
	}
	return len(g.cum) - 1
}

// Cells converts meters to whole cells (rounding up).
func (g *Gen) Cells(m float32) int32 { return int32(g.W.Cells(m)) }

// need returns the free cells two structures must keep between them: the larger of
// what either asks for against the other's tags.
func (g *Gen) need(a, b *Struct) int32 {
	var m float32
	for _, t := range b.Tags {
		m = max(m, a.Prefab.Distance(t))
	}
	for _, t := range a.Tags {
		m = max(m, b.Prefab.Distance(t))
	}
	return g.Cells(m)
}

// conflicts reports whether a and b are closer than their rules allow.
func (g *Gen) conflicts(a, b *Struct) bool {
	return a.Rect.Grow(g.need(a, b)).Overlaps(b.Rect)
}

// Places returns the explicit places whose prefab exists, in file order.
func (g *Gen) Places() []Struct { return g.places }

// siteTags returns a site's tags: the prefab's plus the rule's.
func siteTags(p *asset.Prefab, tag string) []string {
	if contains(p.Tags, tag) {
		return p.Tags
	}
	return append(append([]string(nil), p.Tags...), tag)
}

// site returns the site of rule r in region (rx, rz), or nil: none by chance, a
// wrong biome, outside the world, on water, or yielding to a place or a site of an
// earlier rule.
func (g *Gen) site(r int, rx, rz int32) *Struct {
	key := [3]int32{int32(r), rx, rz}
	if s, ok := g.memo[key]; ok {
		return s
	}
	s := g.siteCandidate(r, rx, rz)
	if s != nil && g.siteYields(s, r) {
		s = nil
	}
	if len(g.memo) >= 1<<16 {
		clear(g.memo) // bounded memory on a long walk; results never change
	}
	g.memo[key] = s
	return s
}

func (g *Gen) siteCandidate(r int, rx, rz int32) *Struct {
	rule := &g.sites[r]
	if len(rule.prefabs) == 0 {
		return nil
	}
	h := hash(g.W.Seed, saltSite, int64(r), int64(rx), int64(rz))
	if uint64(uint32(h)) >= rule.cut {
		return nil
	}
	h = mix64(h)
	p := rule.prefabs[int(h%uint64(len(rule.prefabs)))]
	fw, fd := g.W.Footprint(p, 0)
	s := int32(rule.Spacing)
	rangeX, rangeZ := s-int32(fw)-rule.dist, s-int32(fd)-rule.dist
	if rangeX < 0 || rangeZ < 0 {
		return nil // a prefab unknown at cook time that does not fit its region
	}
	h = mix64(h)
	ox, oz := int32(h%uint64(rangeX+1)), int32((h>>32)%uint64(rangeZ+1))
	rect := Rect{rx*s + ox, rz*s + oz, int32(fw), int32(fd)}
	if !rect.Inside(g.Bounds()) {
		return nil
	}
	cx, cz := rect.Center()
	if b := g.W.Biomes[g.Biome(cx, cz)].Name; !(len(rule.Biomes) == 0 || contains(rule.Biomes, b)) || !(len(p.Biomes) == 0 || contains(p.Biomes, b)) {
		return nil
	}
	if !g.dryFootprint(rect) {
		return nil
	}
	return &Struct{
		Key: fmt.Sprintf("site_%d_%s_%s", r, coord(rx), coord(rz)), Prefab: p, Rect: rect,
		Tags: siteTags(p, rule.Tag), Site: r, Cell: [2]int32{rx, rz}, Ground: g.padLevel(rect),
	}
}

// siteYields reports whether candidate s of rule r conflicts with a place or with a
// site of an earlier rule.
func (g *Gen) siteYields(s *Struct, r int) bool {
	for i := range g.places {
		if g.conflicts(s, &g.places[i]) {
			return true
		}
	}
	for r2 := 0; r2 < r; r2++ {
		if g.anySite(r2, s, func(t *Struct) bool { return g.conflicts(s, t) }) {
			return true
		}
	}
	return false
}

// anySite calls f for every site of rule r that could lie within reach of s (its
// rectangle grown by the largest distance either side could ask for) and reports
// whether f returned true for one of them.
func (g *Gen) anySite(r int, s *Struct, f func(*Struct) bool) bool {
	rule := &g.sites[r]
	var reach int32
	for _, p := range rule.prefabs {
		reach = max(reach, g.need(s, &Struct{Prefab: p, Tags: siteTags(p, rule.Tag)}))
	}
	return g.eachSite(r, s.Rect.Grow(reach), f)
}

// eachSite calls f for every site of rule r whose region overlaps rect, in region
// order, until f returns true.
func (g *Gen) eachSite(r int, rect Rect, f func(*Struct) bool) bool {
	s := int32(g.sites[r].Spacing)
	x0, x1 := floorDiv(rect.X, s), floorDiv(rect.X+rect.W-1, s)
	z0, z1 := floorDiv(rect.Z, s), floorDiv(rect.Z+rect.D-1, s)
	for rz := z0; rz <= z1; rz++ {
		for rx := x0; rx <= x1; rx++ {
			if t := g.site(r, rx, rz); t != nil && f(t) {
				return true
			}
		}
	}
	return false
}

// Structures returns the sites and places whose footprint touches chunk (cx, cz),
// sorted by key. A structure spanning several chunks is returned for each of them.
func (g *Gen) Structures(cx, cz int32) []Struct {
	rect := g.ChunkRect(cx, cz)
	var out []Struct
	for i := range g.places {
		if g.places[i].Rect.Overlaps(rect) {
			out = append(out, g.places[i])
		}
	}
	for r := range g.sites {
		g.eachSite(r, rect, func(t *Struct) bool {
			if t.Rect.Overlaps(rect) {
				out = append(out, *t)
			}
			return false
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Chunk generates chunk (cx, cz): its biomes, vertices and scatter items. Scatter
// skips cells under water.
func (g *Gen) Chunk(cx, cz int32) *Chunk {
	rect := g.ChunkRect(cx, cz)
	c := &Chunk{X: cx, Z: cz, Rect: rect, Grid: g.Grid(rect, 1)}
	n := int32(g.W.Chunk)
	biomes := make([]int, n*n)
	for z := int32(0); z < n; z++ {
		for x := int32(0); x < n; x++ {
			biomes[z*n+x] = g.Biome(rect.X+x, rect.Z+z)
		}
	}
	c.Biomes = biomes
	c.Step = g.BaseStep(c)
	if len(g.scatter) == 0 && len(g.flora) == 0 {
		return c
	}
	// Structures that keep scatter away: those within reach of the chunk.
	var reach int32
	for i := range g.scatter {
		if p := g.scatter[i].prefab; p != nil {
			reach = max(reach, g.Cells(p.MaxDistance()))
		}
	}
	var near []Struct
	for i := range g.places {
		near = append(near, g.places[i])
	}
	for r := range g.sites {
		rr := reach
		for _, p := range g.sites[r].prefabs {
			rr = max(rr, g.Cells(p.MaxDistance()))
		}
		g.eachSite(r, rect.Grow(rr), func(t *Struct) bool { near = append(near, *t); return false })
	}
	prefix := "chunk_" + coord(cx) + "_" + coord(cz) + "_c"
	for z := int32(0); z < n; z++ {
		for x := int32(0); x < n; x++ {
			cell := [2]int32{rect.X + x, rect.Z + z}
			b := biomes[z*n+x]
			ground, water := c.Grid.cellGround(cell[0], cell[1])
			if wet(ground, water) {
				continue
			}
			g.floraCell(c, cell, b, near)
			for r := range g.scatter {
				rule := &g.scatter[r]
				if rule.prefab == nil || !rule.biomes[b] || rule.area != nil && !rule.area.contains(cell[0], cell[1]) {
					continue
				}
				h := hash(g.W.Seed, rule.salt, rule.index, int64(cell[0]), int64(cell[1]))
				if uint64(uint32(h)) >= rule.cut {
					continue
				}
				centre := g.Center(cell[0], cell[1])
				s := Struct{Key: prefix + strconv.Itoa(int(z*n+x)), Prefab: rule.prefab, Rect: Rect{cell[0], cell[1], 1, 1}, Tags: rule.prefab.Tags, Site: -1, Cell: cell,
					Ground: int32(mm(c.HeightAt(centre.X, centre.Z, g.W.Cell))), Vegetation: rule.vegetation}
				free := true
				for i := range near {
					if g.conflicts(&s, &near[i]) {
						free = false
						break
					}
				}
				if free {
					c.Scatter = append(c.Scatter, s)
				}
				break // the first rule that fires owns the cell
			}
		}
	}
	return c
}

// GroundName returns the model name of chunk (cx, cz)'s ground: it contains ':' so it
// can never collide with an asset name.
func GroundName(cx, cz int32) string {
	return "world:ground:" + strconv.Itoa(int(cx)) + ":" + strconv.Itoa(int(cz))
}

// GroundEntity returns the static entity drawing the chunk's ground. It has an empty
// hitbox, so it never takes part in collisions.
func (g *Gen) GroundEntity(c *Chunk) asset.Entity {
	empty := gmath.EmptyAABB()
	return asset.Entity{
		Name: "chunk_" + coord(c.X) + "_" + coord(c.Z) + "_ground", Kind: "static",
		Model: GroundName(c.X, c.Z), Position: g.Origin(c.Rect.X, c.Rect.Z), Scale: gmath.One3,
		Tags: []string{"ground"}, Visible: true, Hitbox: &empty,
	}
}

// Instantiate returns the entities of a structure in world space, in prefab order,
// named "<key>_<entity>"; parents refer to those names. Root entities stand on the
// structure's ground.
func (g *Gen) Instantiate(s *Struct) []asset.Entity {
	ox, oz := float64(meters(s.Rect.X, g.W.Cell)), float64(meters(s.Rect.Z, g.W.Cell))
	oy := float64(s.Ground) / 1000
	return Place(s.Prefab, s.Key, ox, oy, oz, s.Rotation)
}

// Place returns the entities of prefab p placed with the min corner of its footprint at
// (ox, oy, oz), turned by rotation (0, 90, 180 or 270 degrees about +Y) about the
// footprint's centre, in prefab order and named "<key>_<entity>"; parents refer to those
// names, and children keep their positions relative to them.
func Place(p *asset.Prefab, key string, ox, oy, oz float64, rotation int) []asset.Entity {
	out := make([]asset.Entity, len(p.Entities))
	fw, fd := float64(p.Footprint.X), float64(p.Footprint.Y)
	for i := range p.Entities {
		e := p.Entities[i]
		e.Name = key + "_" + e.Name
		if e.Parent != "" {
			e.Parent = key + "_" + e.Parent
			out[i] = e
			continue
		}
		// Turn about the footprint's centre, then anchor the rotated footprint's min
		// corner at the cell: exact for right angles, no trigonometry.
		// Halves are rounded explicitly: the compiler turns /2 into *0.5, which arm64
		// would otherwise fuse into the subtraction.
		hw, hd := float64(fw/2), float64(fd/2)
		x, z := float64(e.Position.X)-hw, float64(e.Position.Z)-hd
		cx, cz := hw, hd
		switch rotation {
		case 90:
			x, z = z, -x
			cx, cz = hd, hw
		case 180:
			x, z = -x, -z
		case 270:
			x, z = -z, x
			cx, cz = hd, hw
		}
		e.Position = gmath.V3(float32(ox+cx+x), float32(oy+float64(e.Position.Y)), float32(oz+cz+z))
		e.RotationDeg.Y += float32(rotation)
		out[i] = e
	}
	return out
}

// coord writes a cell coordinate for an entity name: "n" stands for minus.
func coord(v int32) string {
	if v < 0 {
		return "n" + strconv.Itoa(int(-v))
	}
	return strconv.Itoa(int(v))
}

// ParseCoord reads a coordinate written by coord.
func ParseCoord(s string) (int32, error) {
	neg := strings.HasPrefix(s, "n")
	v, err := strconv.ParseInt(strings.TrimPrefix(s, "n"), 10, 32)
	if err != nil {
		return 0, err
	}
	if neg {
		v = -v
	}
	return int32(v), nil
}
