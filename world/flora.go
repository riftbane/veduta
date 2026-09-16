package world

import (
	"hash/fnv"
	"strconv"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gmath"
)

// Salts of the vegetation's hash streams.
const (
	saltVegetation = 0x76656765 // "vege"
	saltArea       = 0x61726561 // "area"
)

// areaWander is how far the edge of a vegetation area wanders from its circle, as a Q16
// share of the radius (0.3: ±15%).
const areaWander = 19660

// areaRule bounds a vegetation rule to a round area with a wandering edge.
type areaRule struct {
	cell  [2]int32
	r     int64 // cells
	noise field
}

// contains reports whether the centre of cell (x, z) lies inside the area.
func (a *areaRule) contains(x, z int32) bool {
	// Half cells, so the cell centre is an integer.
	dx, dz := 2*int64(x-a.cell[0])+1, 2*int64(z-a.cell[1])+1
	if reach := 2*a.r + a.r/2 + 2; dx > reach || dx < -reach || dz > reach || dz < -reach {
		return false
	}
	r := 2 * a.r << 8
	r += r * areaWander >> 16 * (int64(a.noise.value(x, z)>>16) - 32768) >> 16
	return (dx*dx+dz*dz)<<16 < r*r
}

// floraRule is a vegetation rule with a model: any number of rules may put an instance
// on a cell.
type floraRule struct {
	index  int // in the world's vegetation rules
	model  string
	salt   uint64
	cut    uint64
	biomes []bool
	area   *areaRule
	scale  [2]float32
}

// Flora is one instance of a flora model on a chunk: drawn with the chunk, never an
// entity.
type Flora struct {
	Rule   int      // index of its vegetation rule
	Cell   [2]int32 // the cell it grows in
	Pos    gmath.Vec3
	Turn   int     // quarter turns about +Y (0 to 3), after the mirror
	Mirror bool    // x is mirrored
	Scale  float32 // uniform
	Rank   uint32  // the flora mesh at level k keeps the instances with Rank < 2^32 >> k
}

func nameHash(name string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(name))
	return h.Sum64()
}

// initVegetation compiles the vegetation rules: prefabs join the scatter rules (after
// them, so a scatter rule wins a cell), models become flora rules.
func (g *Gen) initVegetation(lookup func(string) *asset.Prefab) {
	w := g.W
	for i := range w.Vegetation {
		v := &w.Vegetation[i]
		salt := saltVegetation ^ nameHash(v.Name)
		var area *areaRule
		if v.Area {
			area = &areaRule{cell: v.Cell, r: int64(v.Radius),
				noise: field{seed: mix64(w.Seed ^ saltArea ^ nameHash(v.Name)), scale: int32(min(max(v.Radius, 4), 512))}}
		}
		if v.Prefab != "" {
			p := lookup(v.Prefab)
			r := scatterRule{prefab: p, vegetation: v.Name, salt: salt, cut: share(v.Density), area: area}
			if p != nil {
				r.biomes = g.allowed(v.Biomes, p.Biomes)
			}
			g.scatter = append(g.scatter, r)
			continue
		}
		g.flora = append(g.flora, floraRule{index: i, model: v.Model, salt: salt, cut: share(v.Density),
			biomes: g.allowed(v.Biomes, nil), area: area, scale: v.Scale})
	}
}

// floraCell adds the flora instances of a dry cell outside every structure's footprint.
func (g *Gen) floraCell(c *Chunk, cell [2]int32, biome int, near []Struct) {
	if len(g.flora) == 0 {
		return
	}
	for i := range near {
		if near[i].Rect.Contains(cell[0], cell[1]) {
			return
		}
	}
	for i := range g.flora {
		rule := &g.flora[i]
		if !rule.biomes[biome] || rule.area != nil && !rule.area.contains(cell[0], cell[1]) {
			continue
		}
		h := hash(g.W.Seed, rule.salt, 0, int64(cell[0]), int64(cell[1]))
		if uint64(uint32(h)) >= rule.cut {
			continue
		}
		h = mix64(h)
		// Somewhere in the middle 60% of the cell, turned, mirrored and scaled by the hash.
		// The products are rounded explicitly so arm64 cannot fuse them into the sums.
		jx := 0.2 + float64(float64(h&0xffff)*(0.6/65536))
		jz := 0.2 + float64(float64(h>>16&0xffff)*(0.6/65536))
		t := float64(h>>32&0xffff) / 65535
		cellM := float64(g.W.Cell)
		px := float32(float64(float64(cell[0])+jx) * cellM)
		pz := float32(float64(float64(cell[1])+jz) * cellM)
		f := Flora{
			Rule: rule.index, Cell: cell, Turn: int(h >> 48 & 3), Mirror: h>>50&1 == 1,
			Scale: float32(float64(rule.scale[0]) + float64(float64(rule.scale[1]-rule.scale[0])*t)),
			Rank:  uint32(mix64(h)),
		}
		f.Pos = gmath.V3(px, c.HeightAt(px, pz, g.W.Cell), pz)
		c.Flora = append(c.Flora, f)
	}
}

// FloraName returns the model name of the flora of model on chunk (cx, cz).
func FloraName(model string, cx, cz int32) string {
	return "world:flora:" + model + ":" + strconv.Itoa(int(cx)) + ":" + strconv.Itoa(int(cz))
}

// FloraModels returns the flora models a chunk draws, one per model its instances use, in
// the order of the first rule using each.
func (g *Gen) FloraModels(c *Chunk) []string {
	var out []string
	seen := map[string]bool{} // lookup only
	for i := range g.flora {
		name := g.flora[i].model
		if seen[name] {
			continue
		}
		for _, f := range c.Flora {
			if f.Rule == g.flora[i].index {
				seen[name] = true
				out = append(out, name)
				break
			}
		}
	}
	return out
}

// Flora builds the model drawing every instance of flora model name on chunk c, with
// the source model's levels of detail and draw distance: level k draws the instances
// whose rank keeps them at that level (half of them at level 1, a quarter at level 2, …)
// with the source's level-k geometry. Vertices are local to the chunk's min corner.
// models resolves model names; it returns nil when the source model does not exist.
func (g *Gen) Flora(c *Chunk, name string, models func(string) *asset.Model) *asset.Model {
	src := models(name)
	if src == nil {
		return nil
	}
	m := &asset.Model{Name: FloraName(name, c.X, c.Z), Pivot: "origin", DrawDistance: src.DrawDistance}
	var insts []*Flora
	for i := range c.Flora {
		if f := &c.Flora[i]; g.W.Vegetation[f.Rule].Model == name {
			insts = append(insts, f)
		}
	}
	origin := g.Origin(c.Rect.X, c.Rect.Z)
	// geometry returns the mesh and materials of level k of the source.
	geometry := func(k int) (*gfx.MeshData, []string) {
		if k == 0 {
			return &src.Mesh, src.Materials
		}
		l := &src.LODs[k-1]
		if l.Model == "" {
			return &l.Mesh, src.Materials
		}
		if o := models(l.Model); o != nil {
			return &o.Mesh, o.Materials
		}
		return &src.Mesh, src.Materials
	}
	for k := 0; k <= len(src.LODs); k++ {
		_, mats := geometry(k)
		for _, mat := range mats {
			addMaterial(&m.Materials, mat)
		}
	}
	for k := 0; k <= len(src.LODs); k++ {
		md, mats := geometry(k)
		mesh := floraMesh(insts, uint64(1)<<32>>k, md, mats, m.Materials, origin)
		if k == 0 {
			m.Mesh = mesh
			continue
		}
		m.LODs = append(m.LODs, asset.LOD{Distance: src.LODs[k-1].Distance, Mesh: mesh})
	}
	bounds := m.Mesh.Bounds
	for _, l := range m.LODs {
		bounds = bounds.Union(l.Mesh.Bounds)
	}
	m.Mesh.Bounds = bounds
	for i, mat := range m.Materials {
		p := m.Mesh.Parts[i]
		m.Parts = append(m.Parts, asset.PartInfo{Index: i, Shape: "flora", First: p.First, Count: p.Count, Material: mat, UV: "box", Of: -1})
	}
	return m
}

// floraMesh places md once per instance whose rank is below keep, with a part per
// material of materials (md's parts use mats).
func floraMesh(insts []*Flora, keep uint64, md *gfx.MeshData, mats, materials []string, origin gmath.Vec3) gfx.MeshData {
	parts := make([][]uint32, len(materials))
	var mesh gfx.MeshData
	for _, f := range insts {
		if uint64(f.Rank) >= keep {
			continue
		}
		base := uint32(len(mesh.Vertices))
		ox, oy, oz := float64(f.Pos.X-origin.X), float64(f.Pos.Y), float64(f.Pos.Z-origin.Z)
		s := float64(f.Scale)
		for _, v := range md.Vertices {
			p, n := v.Pos, v.Normal
			if f.Mirror {
				p.X, n.X = -p.X, -n.X
			}
			for range f.Turn { // a quarter turn about +Y: (x, z) → (z, −x)
				p.X, p.Z = p.Z, -p.X
				n.X, n.Z = n.Z, -n.X
			}
			// Products rounded explicitly so arm64 cannot fuse them into the sums.
			v.Pos = gmath.V3(float32(float64(float64(p.X)*s)+ox), float32(float64(float64(p.Y)*s)+oy), float32(float64(float64(p.Z)*s)+oz))
			v.Normal = n
			mesh.Vertices = append(mesh.Vertices, v)
		}
		for _, part := range md.Parts {
			mat := ""
			if part.Material >= 0 && part.Material < len(mats) {
				mat = mats[part.Material]
			}
			k := addMaterial(&materials, mat)
			for i := part.First; i+2 < part.First+part.Count; i += 3 {
				a, b, c := md.Indices[i], md.Indices[i+1], md.Indices[i+2]
				if f.Mirror { // a mirror turns the winding around
					b, c = c, b
				}
				parts[k] = append(parts[k], base+a, base+b, base+c)
			}
		}
	}
	for k, p := range parts {
		mesh.Parts = append(mesh.Parts, gfx.MeshPart{First: len(mesh.Indices), Count: len(p), Material: k})
		mesh.Indices = append(mesh.Indices, p...)
	}
	mesh.Bounds = gmath.EmptyAABB()
	for _, v := range mesh.Vertices {
		mesh.Bounds = mesh.Bounds.Extend(v.Pos)
	}
	if mesh.Bounds.IsEmpty() {
		mesh.Bounds = gmath.AABB{}
	}
	return mesh
}

// FloraEntity returns the static entity drawing flora model name on chunk c: named
// chunk_<cx>_<cz>_flora_<model>, tagged flora, with an empty hitbox.
func (g *Gen) FloraEntity(c *Chunk, name string) asset.Entity {
	empty := gmath.EmptyAABB()
	return asset.Entity{
		Name: "chunk_" + coord(c.X) + "_" + coord(c.Z) + "_flora_" + name, Kind: "static",
		Model: FloraName(name, c.X, c.Z), Position: g.Origin(c.Rect.X, c.Rect.Z), Scale: gmath.One3,
		Tags: []string{"flora"}, Visible: true, Hitbox: &empty,
	}
}
