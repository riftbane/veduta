package world

import (
	"math"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/gmath"
)

// WaterMaterial names the built-in material of water surfaces, used when the world's
// terrain names none; the engine registers DefaultWater under it.
const WaterMaterial = "world:water"

// DefaultWater is the built-in water: opaque, a lake blue, lit like the ground.
var DefaultWater = asset.Material{Name: WaterMaterial, Albedo: 0xff3b76b0, Alpha: "opaque", Cutoff: 0.5, Cull: gfx.CullBack, Filter: gfx.FilterBilinear}

// groundTolerance is how far, in millimeters, a chunk's finest drawn level may pass from
// its vertices: gentle ground is drawn with a coarser grid even up close. 6 cm is well
// under a pixel of a 240-line frame for ground more than a few meters away.
const groundTolerance = 60

// groundSteps returns the grid steps a chunk of n cells can be drawn with: 1, 2, 4, …
// while they divide the chunk, at most 16.
func groundSteps(n int32) []int32 {
	steps := []int32{1}
	for s := int32(2); s <= min(n, 16) && n%s == 0; s *= 2 {
		steps = append(steps, s)
	}
	return steps
}

// BaseStep returns the grid step of chunk c's finest drawn level: the coarsest step whose
// triangles pass within 6 cm of every vertex and whose quads each cover cells of one
// ground material, except that on uneven ground a 2-cell quad may cover several (it takes
// the material of its middle cell: four times fewer triangles are worth a blockier biome
// edge). Flat ground and gentle relief are drawn with fewer triangles even up close;
// HeightAt follows the same triangles.
func (g *Gen) BaseStep(c *Chunk) int32 {
	n := c.Rect.W
	matOf := func(x, z int32) string { return g.W.Biomes[c.Biomes[z*n+x]].Ground }
	uneven := false
	h0, _ := c.Grid.At(c.Rect.X, c.Rect.Z)
	for z := c.Rect.Z; z <= c.Rect.Z+n && !uneven; z++ {
		for x := c.Rect.X; x <= c.Rect.X+n; x++ {
			if h, _ := c.Grid.At(x, z); h != h0 {
				uneven = true
				break
			}
		}
	}
	best := int32(1)
	for _, s := range groundSteps(n)[1:] {
		for qz := int32(0); qz < n; qz += s {
			for qx := int32(0); qx < n; qx += s {
				if !g.quadFits(c, qx, qz, s, matOf, uneven && s == 2) {
					return best
				}
			}
		}
		best = s
	}
	return best
}

// quadFits reports whether the quad of step s at chunk-local (qx, qz) covers one ground
// material (unless mixed) and interpolates every vertex it covers within
// groundTolerance.
func (g *Gen) quadFits(c *Chunk, qx, qz, s int32, matOf func(x, z int32) string, mixed bool) bool {
	m := matOf(qx, qz)
	h := func(x, z int32) int64 { v, _ := c.Grid.At(c.Rect.X+x, c.Rect.Z+z); return int64(v) }
	h00, h10, h01, h11 := h(qx, qz), h(qx+s, qz), h(qx, qz+s), h(qx+s, qz+s)
	for dz := int32(0); dz <= s; dz++ {
		for dx := int32(0); dx <= s; dx++ {
			if !mixed && dx < s && dz < s && matOf(qx+dx, qz+dz) != m {
				return false
			}
			// The interpolated height times s, on the triangle holding the vertex.
			var at int64
			if dx+dz <= s {
				at = h00*int64(s) + (h10-h00)*int64(dx) + (h01-h00)*int64(dz)
			} else {
				at = h11*int64(s) + (h01-h11)*int64(s-dx) + (h10-h11)*int64(s-dz)
			}
			if d := h(qx+dx, qz+dz)*int64(s) - at; d > groundTolerance*int64(s) || d < -groundTolerance*int64(s) {
				return false
			}
		}
	}
	return true
}

// Ground builds the ground model of chunk c: one mesh per level of detail, each with a
// part per ground material and a part for the water. The finest level samples every
// c.Step-th vertex (BaseStep), each next level every other vertex of the one before;
// flat runs of quads at one height become one quad; every level hangs skirts
// below the chunk's edges that are not straight, deep enough to close the cracks between
// neighbouring chunks drawn at different levels. The water is the same at every level:
// one quad per run of wet cells at one level. Vertices are local to the chunk's min
// corner; one texture repeat per cell. The k-th coarser level is drawn from the terrain's
// lod_distance × 2^(k-1) on; a level with no fewer triangles than the one before is left
// out.
func (g *Gen) Ground(c *Chunk) *asset.Model {
	cx, cz := c.X, c.Z
	m := &asset.Model{Name: GroundName(cx, cz), Pivot: "origin"}
	matOf := make([]int, len(g.W.Biomes)) // biome → material index
	for i, b := range g.W.Biomes {
		matOf[i] = addMaterial(&m.Materials, b.Ground)
	}
	water := -1
	n := c.Rect.W
	for z := int32(0); z < n && water < 0; z++ {
		for x := int32(0); x < n; x++ {
			if g.waterQuad(c, x, z) != NoWater {
				name := g.W.Terrain.Water
				if name == "" {
					name = WaterMaterial
				}
				water = addMaterial(&m.Materials, name)
				break
			}
		}
	}
	b := groundBuilder{g: g, c: c, matOf: matOf, water: water, materials: len(m.Materials)}
	normals := b.normals()
	var prev, k int
	for _, s := range groundSteps(n) {
		if s < c.Step {
			continue
		}
		mesh := b.level(s, normals)
		tris := len(mesh.Indices) / 3
		switch {
		case k == 0:
			m.Mesh = mesh
		case tris < prev && len(m.LODs) < 4:
			m.LODs = append(m.LODs, asset.LOD{Distance: float32(float64(g.W.Terrain.LODDistance) * float64(int(1)<<(k-1))), Mesh: mesh})
		default:
			k++
			continue
		}
		prev = tris
		k++
	}
	bounds := m.Mesh.Bounds
	for _, l := range m.LODs {
		bounds = bounds.Union(l.Mesh.Bounds)
	}
	m.Mesh.Bounds = bounds // culling must keep every level's skirts
	for i := range m.Materials {
		first, count := 0, 0
		if i < len(m.Mesh.Parts) {
			first, count = m.Mesh.Parts[i].First, m.Mesh.Parts[i].Count
		}
		m.Parts = append(m.Parts, asset.PartInfo{Index: i, Shape: "plane", First: first, Count: count, Material: m.Materials[i], UV: "planar", Of: -1})
	}
	return m
}

func addMaterial(list *[]string, name string) int {
	for j, n := range *list {
		if n == name {
			return j
		}
	}
	*list = append(*list, name)
	return len(*list) - 1
}

// waterQuad returns the water level of cell (x, z) of chunk c (chunk-local) when its
// surface must be drawn: some corner is under water. NoWater otherwise.
func (g *Gen) waterQuad(c *Chunk, x, z int32) int32 {
	gr := c.Grid
	x, z = c.Rect.X+x, c.Rect.Z+z
	var level int32 = NoWater
	low := int32(math.MaxInt32)
	for _, v := range [4][2]int32{{x, z}, {x + 1, z}, {x, z + 1}, {x + 1, z + 1}} {
		h, w := gr.At(v[0], v[1])
		level, low = max(level, w), min(low, h)
	}
	if level == NoWater || low >= level {
		return NoWater
	}
	return level
}

type groundBuilder struct {
	g         *Gen
	c         *Chunk
	matOf     []int
	water     int // material index of the water, -1 when the chunk has none
	materials int
}

// height returns the height of chunk-local vertex (x, z) in millimeters.
func (b *groundBuilder) height(x, z int32) int32 {
	h, _ := b.c.Grid.At(b.c.Rect.X+x, b.c.Rect.Z+z)
	return h
}

// normals returns the normal of every vertex of the chunk (row-major, n+1 per row) from
// central differences of the heights around it, so neighbouring chunks light their
// shared edges alike.
func (b *groundBuilder) normals() []gmath.Vec3 {
	n := b.c.Rect.W
	out := make([]gmath.Vec3, (n+1)*(n+1))
	span := 2000 * float64(b.g.W.Cell) // two cells, in millimeters
	for z := int32(0); z <= n; z++ {
		for x := int32(0); x <= n; x++ {
			sx := float64(b.height(x+1, z)-b.height(x-1, z)) / span
			sz := float64(b.height(x, z+1)-b.height(x, z-1)) / span
			if sx == 0 && sz == 0 {
				out[z*(n+1)+x] = gmath.V3(0, 1, 0)
				continue
			}
			l := math.Sqrt(float64(sx*sx) + float64(sz*sz) + 1)
			out[z*(n+1)+x] = gmath.V3(float32(-sx/l), float32(1/l), float32(-sz/l))
		}
	}
	return out
}

// level builds the mesh of the level sampling every s-th vertex.
func (b *groundBuilder) level(s int32, normals []gmath.Vec3) gfx.MeshData {
	g, c := b.g, b.c
	n := c.Rect.W
	cell := g.W.Cell
	var mesh gfx.MeshData
	idx := make([]int32, (n+1)*(n+1))
	for i := range idx {
		idx[i] = -1
	}
	vertex := func(x, z int32) uint32 {
		k := z*(n+1) + x
		if idx[k] < 0 {
			idx[k] = int32(len(mesh.Vertices))
			mesh.Vertices = append(mesh.Vertices, gfx.Vertex{
				Pos:    gmath.V3(meters(x, cell), mmMeters(int64(b.height(x, z))), meters(z, cell)),
				Normal: normals[k], UV: gmath.V2(float32(x), float32(z)),
			})
		}
		return uint32(idx[k])
	}
	parts := make([][]uint32, b.materials)
	// quadMat is the material of the quad whose min corner is (x, z): the biome of its
	// middle cell.
	quadMat := func(x, z int32) int {
		mx, mz := min(x+s/2, n-1), min(z+s/2, n-1)
		return b.matOf[c.Biomes[mz*n+mx]]
	}
	flat := func(x, z int32) bool { // the quad and its corners' normals are level
		h := b.height(x, z)
		for _, v := range [4][2]int32{{x, z}, {x + s, z}, {x, z + s}, {x + s, z + s}} {
			if b.height(v[0], v[1]) != h || normals[v[1]*(n+1)+v[0]].Y != 1 {
				return false
			}
		}
		return true
	}
	for z := int32(0); z < n; z += s {
		for x := int32(0); x < n; {
			k := quadMat(x, z)
			x1 := x + s
			if flat(x, z) {
				h := b.height(x, z)
				for x1 < n && quadMat(x1, z) == k && flat(x1, z) && b.height(x1, z) == h {
					x1 += s
				}
			}
			// Counter-clockwise seen from above (+Y).
			v00, v01, v10, v11 := vertex(x, z), vertex(x, z+s), vertex(x1, z), vertex(x1, z+s)
			parts[k] = append(parts[k], v00, v01, v10, v10, v01, v11)
			x = x1
		}
	}
	b.skirts(s, parts, vertex, quadMat, &mesh)
	if b.water >= 0 {
		b.waterQuads(parts, &mesh)
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

// skirts hangs a strip below every edge of the chunk that is not straight, as deep as
// the edge's height range: wherever a neighbour drawn at another level leaves a
// crack, the higher side's strip closes it.
func (b *groundBuilder) skirts(s int32, parts [][]uint32, vertex func(x, z int32) uint32, quadMat func(x, z int32) int, mesh *gfx.MeshData) {
	n := b.c.Rect.W
	// Each edge runs so that along × down points out of the chunk: north +X, east +Z,
	// south -X, west -Z.
	edges := [4]struct{ x, z, dx, dz int32 }{{0, 0, 1, 0}, {n, 0, 0, 1}, {n, n, -1, 0}, {0, n, 0, -1}}
	for _, e := range edges {
		lo, hi := int32(math.MaxInt32), int32(math.MinInt32)
		straight := true // every level interpolates a straight edge exactly: no crack
		for i := int32(0); i <= n; i++ {
			h := b.height(e.x+i*e.dx, e.z+i*e.dz)
			lo, hi = min(lo, h), max(hi, h)
			if i >= 2 && int64(b.height(e.x+(i-2)*e.dx, e.z+(i-2)*e.dz))+int64(h) != 2*int64(b.height(e.x+(i-1)*e.dx, e.z+(i-1)*e.dz)) {
				straight = false
			}
		}
		if straight {
			continue
		}
		depth := int64(hi) - int64(lo)
		below := map[int32]uint32{} // edge position → bottom vertex; lookup only
		bottom := func(i int32, top uint32) uint32 {
			if v, ok := below[i]; ok {
				return v
			}
			v := mesh.Vertices[top]
			v.Pos.Y = mmMeters(int64(b.height(e.x+i*e.dx, e.z+i*e.dz)) - depth)
			mesh.Vertices = append(mesh.Vertices, v)
			below[i] = uint32(len(mesh.Vertices) - 1)
			return below[i]
		}
		for i := int32(0); i < n; i += s {
			ax, az := e.x+i*e.dx, e.z+i*e.dz
			bx, bz := ax+s*e.dx, az+s*e.dz
			pa, pb := vertex(ax, az), vertex(bx, bz)
			qa, qb := bottom(i, pa), bottom(i+s, pb)
			k := quadMat(min(ax, bx, n-s), min(az, bz, n-s))
			parts[k] = append(parts[k], pa, pb, qa, pb, qb, qa)
		}
	}
}

// waterQuads adds one quad per run of cells, along each row, whose water is drawn at the
// same level.
func (b *groundBuilder) waterQuads(parts [][]uint32, mesh *gfx.MeshData) {
	g, c := b.g, b.c
	n := c.Rect.W
	cell := g.W.Cell
	up := gmath.V3(0, 1, 0)
	for z := int32(0); z < n; z++ {
		for x := int32(0); x < n; {
			level := g.waterQuad(c, x, z)
			x1 := x + 1
			for x1 < n && g.waterQuad(c, x1, z) == level {
				x1++
			}
			if level != NoWater {
				y := mmMeters(int64(level))
				base := uint32(len(mesh.Vertices))
				x0m, x1m, z0m, z1m := meters(x, cell), meters(x1, cell), meters(z, cell), meters(z+1, cell)
				mesh.Vertices = append(mesh.Vertices,
					gfx.Vertex{Pos: gmath.V3(x0m, y, z0m), Normal: up, UV: gmath.V2(float32(x), float32(z))},
					gfx.Vertex{Pos: gmath.V3(x0m, y, z1m), Normal: up, UV: gmath.V2(float32(x), float32(z+1))},
					gfx.Vertex{Pos: gmath.V3(x1m, y, z0m), Normal: up, UV: gmath.V2(float32(x1), float32(z))},
					gfx.Vertex{Pos: gmath.V3(x1m, y, z1m), Normal: up, UV: gmath.V2(float32(x1), float32(z+1))},
				)
				parts[b.water] = append(parts[b.water], base, base+1, base+2, base+2, base+1, base+3)
			}
			x = x1
		}
	}
}
