package world

import (
	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gmath"
)

// ground builds the ground mesh of a chunk from the biome of each of its cells: on
// every row, runs of cells sharing a ground material become one quad with tiled UVs
// (one texture repeat per cell), so plain terrain costs a few triangles. Vertices are
// local to the chunk's min corner; the mesh has one part per material, in biome order.
func (g *Gen) ground(rect Rect, biomes []int) *asset.Model {
	n := rect.W
	cx, cz := g.ChunkOf(rect.X, rect.Z)
	m := &asset.Model{Name: GroundName(cx, cz), Pivot: "origin"}
	matOf := make([]int, len(g.W.Biomes)) // biome → material index in m.Materials
	for i, b := range g.W.Biomes {
		k := -1
		for j, name := range m.Materials {
			if name == b.Ground {
				k = j
				break
			}
		}
		if k < 0 {
			k = len(m.Materials)
			m.Materials = append(m.Materials, b.Ground)
		}
		matOf[i] = k
	}
	type quad struct{ x0, x1, z int32 }
	runs := make([][]quad, len(m.Materials))
	for z := int32(0); z < n; z++ {
		x := int32(0)
		for x < n {
			k := matOf[biomes[z*n+x]]
			x1 := x + 1
			for x1 < n && matOf[biomes[z*n+x1]] == k {
				x1++
			}
			runs[k] = append(runs[k], quad{x, x1, z})
			x = x1
		}
	}
	mesh := &m.Mesh
	up := gmath.V3(0, 1, 0)
	for k, qs := range runs {
		first := len(mesh.Indices)
		for _, q := range qs {
			x0, x1 := meters(q.x0, g.W.Cell), meters(q.x1, g.W.Cell)
			z0, z1 := meters(q.z, g.W.Cell), meters(q.z+1, g.W.Cell)
			base := uint32(len(mesh.Vertices))
			mesh.Vertices = append(mesh.Vertices,
				gfx.Vertex{Pos: gmath.V3(x0, 0, z0), Normal: up, UV: gmath.V2(float32(q.x0), float32(q.z))},
				gfx.Vertex{Pos: gmath.V3(x0, 0, z1), Normal: up, UV: gmath.V2(float32(q.x0), float32(q.z+1))},
				gfx.Vertex{Pos: gmath.V3(x1, 0, z0), Normal: up, UV: gmath.V2(float32(q.x1), float32(q.z))},
				gfx.Vertex{Pos: gmath.V3(x1, 0, z1), Normal: up, UV: gmath.V2(float32(q.x1), float32(q.z+1))},
			)
			// Counter-clockwise seen from above (+Y).
			mesh.Indices = append(mesh.Indices, base, base+1, base+2, base+2, base+1, base+3)
		}
		if count := len(mesh.Indices) - first; count > 0 {
			mesh.Parts = append(mesh.Parts, gfx.MeshPart{First: first, Count: count, Material: k})
			m.Parts = append(m.Parts, asset.PartInfo{Index: len(m.Parts), Shape: "plane", First: first, Count: count, Material: m.Materials[k], UV: "planar", Of: -1})
		}
	}
	side := meters(n, g.W.Cell)
	mesh.Bounds = gmath.AABB{Min: gmath.Zero3, Max: gmath.V3(side, 0, side)}
	return m
}
