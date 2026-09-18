// Package tilemap runs the tile maps of scenes (docs/map.md): the cells a game reads and
// paints, and the meshes that draw them, rebuilt a chunk at a time when cells change.
//
// A map lies in the XY plane, seen by a camera looking down −Z: cell (x, y) is x columns
// right and y rows down from the top-left cell, and covers Tile × Tile meters from the
// origin (the map's top-left corner) + (x·Tile, −y·Tile). Each layer is drawn at the
// origin's z plus its own. Everything here is deterministic and allocation order never
// depends on map iteration.
package tilemap

import (
	"fmt"
	"sort"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/gmath"
	"github.com/riftbane/veduta/v2/scene"
)

// ChunkSize is the number of cells along each side of the square chunks a layer is drawn
// in: a chunk out of view is not drawn, and changing a cell rebuilds only the chunks whose
// look it changes.
const ChunkSize = 16

// edgeLift is how much nearer the camera each rank of borders is drawn than the cells.
const edgeLift = 0.001

// Map is a tile map being played: the compiled map, its cells as the game changed them,
// and the models, materials and textures that draw it.
type Map struct {
	src   *asset.Map
	lib   *asset.Library
	cells [][]uint8 // per layer, as asset.MapLayer.Cells
	// OnEvent, when set, receives the map_set event of every change.
	OnEvent func(name string, fields map[string]any)

	cw, ch    int      // chunks across and down
	dirty     [][]bool // per layer, per chunk
	models    map[string]*asset.Model
	materials map[string]*asset.Material
	textures  map[string]*asset.Texture
	draws     []drawInfo // per terrain
	statics   []scene.Static
}

// drawInfo is how a terrain is drawn.
type drawInfo struct {
	material string         // of its cells
	edge     *asset.Edge    // nil: no border
	edgeMat  string         // of its border
	frame    [2]int         // its frames' size in pixels, for the border's coordinates
	rank     int            // borders of a higher rank are drawn nearer
	priority int            // edge priority, 0 without an edge
	tex      *asset.Texture // its texture, nil when unknown
}

// New returns a map playing src, drawn with the textures and materials of lib.
func New(src *asset.Map, lib *asset.Library) *Map {
	m := &Map{src: src, lib: lib, models: map[string]*asset.Model{}, materials: map[string]*asset.Material{},
		textures: map[string]*asset.Texture{}}
	m.cw, m.ch = (src.W+ChunkSize-1)/ChunkSize, (src.H+ChunkSize-1)/ChunkSize
	for _, l := range src.Layers {
		m.cells = append(m.cells, append([]uint8(nil), l.Cells...))
		d := make([]bool, m.cw*m.ch)
		for i := range d {
			d[i] = true
		}
		m.dirty = append(m.dirty, d)
	}
	m.prepare()
	return m
}

// prepare works out how every terrain is drawn: the material of its cells, and for a
// texture with an edge the atlas and material of its border.
func (m *Map) prepare() {
	m.draws = make([]drawInfo, len(m.src.Terrains))
	var withEdge []int
	for i, t := range m.src.Terrains {
		d := &m.draws[i]
		var base *asset.Material
		if t.Material != "" {
			d.material = t.Material
			base = m.lib.Materials[t.Material]
			if base != nil {
				d.tex = m.lib.Textures[base.Texture]
			}
		} else {
			d.tex = m.lib.Textures[t.Texture]
			d.material = "map:tile:" + t.Texture
			mat := &asset.Material{Name: d.material, Albedo: 0xffffffff, Texture: t.Texture, Unlit: true, Alpha: "cutout",
				Cutoff: 0.5, Cull: gfx.CullNone, Filter: gfx.FilterNearest}
			if d.tex != nil {
				mat.Grid = d.tex.Grid
			}
			m.materials[d.material] = mat
			base = mat
		}
		tx := d.tex
		if tx == nil || tx.Edge == nil || base == nil || len(tx.Data.Levels) == 0 {
			continue
		}
		d.edge = tx.Edge
		d.priority = tx.Edge.Priority
		d.frame = [2]int{tx.Data.Levels[0].W / max(tx.Grid[0], 1), tx.Data.Levels[0].H / max(tx.Grid[1], 1)}
		atlas := "map:edge:" + tx.Name
		if m.textures[atlas] == nil {
			m.textures[atlas] = EdgeAtlas(atlas, tx)
		}
		mat := *base
		mat.Name = "map:edge:" + d.material
		mat.Texture = atlas
		mat.Grid = m.textures[atlas].Grid
		mat.Cull = gfx.CullNone
		if mat.Alpha == "opaque" {
			mat.Alpha, mat.Cutoff = "cutout", 0.5
		}
		d.edgeMat = mat.Name
		m.materials[mat.Name] = &mat
		withEdge = append(withEdge, i)
	}
	sort.SliceStable(withEdge, func(a, b int) bool { return m.draws[withEdge[a]].priority < m.draws[withEdge[b]].priority })
	for r, i := range withEdge {
		m.draws[i].rank = r + 1
	}
}

// Name returns the map's name.
func (m *Map) Name() string { return m.src.Name }

// Source returns the compiled map as its file describes it.
func (m *Map) Source() *asset.Map { return m.src }

// Size returns the map's columns and rows.
func (m *Map) Size() (w, h int) { return m.src.W, m.src.H }

// Inside reports whether cell (x, y) is on the map.
func (m *Map) Inside(x, y int) bool { return x >= 0 && y >= 0 && x < m.src.W && y < m.src.H }

// CellAt returns the cell holding world point (x, y); it may be outside the map.
func (m *Map) CellAt(x, y float32) (cx, cy int) {
	t := m.src.Tile
	return int(gmath.Floor((x - m.src.Origin.X) / t)), int(gmath.Floor((m.src.Origin.Y - y) / t))
}

// Center returns the world point at the center of cell (x, y).
func (m *Map) Center(x, y int) (float32, float32) {
	t := m.src.Tile
	return m.src.Origin.X + float32((float32(x)+0.5)*t), m.src.Origin.Y - float32((float32(y)+0.5)*t)
}

// Layer returns the index of the layer called name, or -1.
func (m *Map) Layer(name string) int { return m.src.Layer(name) }

// Get returns the terrain of cell (x, y) of layer l, or nil for an empty cell or a cell off
// the map.
func (m *Map) Get(l, x, y int) *asset.MapTerrain {
	if l < 0 || l >= len(m.cells) || !m.Inside(x, y) {
		return nil
	}
	if v := m.cells[l][y*m.src.W+x]; v > 0 {
		return &m.src.Terrains[v-1]
	}
	return nil
}

// Set paints cell (x, y) of layer l with the terrain called terrain, or empties it for "".
func (m *Map) Set(l, x, y int, terrain string) error {
	if l < 0 || l >= len(m.cells) {
		return fmt.Errorf("map %s: no layer %d", m.src.Name, l)
	}
	if !m.Inside(x, y) {
		return fmt.Errorf("map %s: cell (%d, %d) is off the %d × %d map", m.src.Name, x, y, m.src.W, m.src.H)
	}
	v := uint8(0)
	if terrain != "" {
		i := m.src.Terrain(terrain)
		if i < 0 {
			return fmt.Errorf("map %s: no terrain %q (it has %s)", m.src.Name, terrain, m.terrainNames())
		}
		v = uint8(i + 1)
	}
	if m.cells[l][y*m.src.W+x] == v {
		return nil
	}
	m.cells[l][y*m.src.W+x] = v
	// The cell and its borders on the cells around it.
	for cy := max(y-1, 0) / ChunkSize; cy <= min(y+1, m.src.H-1)/ChunkSize; cy++ {
		for cx := max(x-1, 0) / ChunkSize; cx <= min(x+1, m.src.W-1)/ChunkSize; cx++ {
			m.dirty[l][cy*m.cw+cx] = true
		}
	}
	if m.OnEvent != nil {
		m.OnEvent("map_set", map[string]any{"layer": m.src.Layers[l].Name, "x": x, "y": y, "terrain": terrain})
	}
	return nil
}

func (m *Map) terrainNames() string {
	s := ""
	for i, t := range m.src.Terrains {
		if i > 0 {
			s += ", "
		}
		s += t.Name
	}
	return s
}

// Has reports whether a terrain with tag paints cell (x, y) on layer l, or on any layer
// when l is -1.
func (m *Map) Has(l, x, y int, tag string) bool {
	for i := range m.cells {
		if l >= 0 && i != l {
			continue
		}
		if t := m.Get(i, x, y); t != nil {
			for _, g := range t.Tags {
				if g == tag {
					return true
				}
			}
		}
	}
	return false
}

// Cells returns the cells of layer l as the game left them (index + 1 of a terrain, 0 for
// none), for snapshots. The slice is the map's own.
func (m *Map) Cells(l int) []uint8 { return m.cells[l] }

// SetCells replaces the cells of every layer, as Cells returned them.
func (m *Map) SetCells(cells [][]uint8) error {
	if len(cells) != len(m.cells) {
		return fmt.Errorf("map %s: %d layers of cells, the map has %d", m.src.Name, len(cells), len(m.cells))
	}
	for l, c := range cells {
		if len(c) != len(m.cells[l]) {
			return fmt.Errorf("map %s: layer %d has %d cells, want %d", m.src.Name, l, len(c), len(m.cells[l]))
		}
		for _, v := range c {
			if int(v) > len(m.src.Terrains) {
				return fmt.Errorf("map %s: cell value %d names no terrain", m.src.Name, v)
			}
		}
		copy(m.cells[l], c)
		for i := range m.dirty[l] {
			m.dirty[l][i] = true
		}
	}
	return nil
}

// Materials returns the materials the map makes (its terrains' drawn with a texture, and
// their borders'), by name.
func (m *Map) Materials() map[string]*asset.Material { return m.materials }

// Textures returns the textures the map makes (the edge atlases), by name.
func (m *Map) Textures() map[string]*asset.Texture { return m.textures }

// Models rebuilds the chunks whose cells changed and returns the models that draw the map,
// by name. A model of a chunk that did not change is the same value as before.
func (m *Map) Models() map[string]*asset.Model {
	for l := range m.dirty {
		for cy := 0; cy < m.ch; cy++ {
			for cx := 0; cx < m.cw; cx++ {
				if !m.dirty[l][cy*m.cw+cx] {
					continue
				}
				m.dirty[l][cy*m.cw+cx] = false
				name := m.chunkName(l, cx, cy)
				if md := m.build(l, cx, cy, name); md != nil {
					m.models[name] = md
				} else {
					delete(m.models, name)
				}
			}
		}
	}
	m.statics = m.statics[:0]
	for l := range m.cells {
		for cy := 0; cy < m.ch; cy++ {
			for cx := 0; cx < m.cw; cx++ {
				if name := m.chunkName(l, cx, cy); m.models[name] != nil {
					m.statics = append(m.statics, scene.Static{Model: name, World: gmath.Ident4(), Layer: m.src.Layers[l].Layer})
				}
			}
		}
	}
	return m.models
}

// Statics returns what draws the map as of the last Models: one static per chunk that has
// cells, layer by layer.
func (m *Map) Statics() []scene.Static { return m.statics }

func (m *Map) chunkName(l, cx, cy int) string {
	return fmt.Sprintf("map:%s:%d:%d:%d", m.src.Name, l, cx, cy)
}

// quad appends a quad facing +Z from world (x0, y0) (top left) to (x1, y1) (bottom right)
// with texture coordinates (u0, v0) to (u1, v1).
func quad(md *gfx.MeshData, x0, y0, x1, y1, z, u0, v0, u1, v1 float32) {
	n := uint32(len(md.Vertices))
	up := gmath.V3(0, 0, 1)
	md.Vertices = append(md.Vertices,
		gfx.Vertex{Pos: gmath.V3(x0, y0, z), Normal: up, UV: gmath.V2(u0, v0)},
		gfx.Vertex{Pos: gmath.V3(x0, y1, z), Normal: up, UV: gmath.V2(u0, v1)},
		gfx.Vertex{Pos: gmath.V3(x1, y1, z), Normal: up, UV: gmath.V2(u1, v1)},
		gfx.Vertex{Pos: gmath.V3(x1, y0, z), Normal: up, UV: gmath.V2(u1, v0)})
	md.Indices = append(md.Indices, n, n+1, n+2, n, n+2, n+3)
}

// build returns the model of chunk (cx, cy) of layer l, or nil when it has no cell: a part
// per terrain for its cells, in terrain order, then a part per terrain for its borders, by
// rank.
func (m *Map) build(l, cx, cy int, name string) *asset.Model {
	src := m.src
	nt := len(src.Terrains)
	cellQuads := make([][][4]int, nt) // per terrain: cells
	type edgeQuad struct{ x, y, shape, quarter int }
	edgeQuads := make([][]edgeQuad, nt)
	x0, y0 := cx*ChunkSize, cy*ChunkSize
	for y := y0; y < min(y0+ChunkSize, src.H); y++ {
		for x := x0; x < min(x0+ChunkSize, src.W); x++ {
			if v := m.at(l, x, y); v > 0 {
				cellQuads[v-1] = append(cellQuads[v-1], [4]int{x, y})
			}
			m.cellEdges(l, x, y, func(u, shape, quarter int) {
				edgeQuads[u-1] = append(edgeQuads[u-1], edgeQuad{x, y, shape, quarter})
			})
		}
	}
	md := gfx.MeshData{}
	var mats []string
	part := func(mat string, first int) {
		if n := len(md.Indices) - first; n > 0 {
			mats = append(mats, mat)
			md.Parts = append(md.Parts, gfx.MeshPart{First: first, Count: n, Material: len(mats) - 1})
		}
	}
	t := src.Tile
	ox, oy, oz := src.Origin.X, src.Origin.Y, src.Origin.Z+src.Layers[l].Z
	wx := func(x int) float32 { return ox + float32(float32(x)*t) }
	wy := func(y int) float32 { return oy - float32(float32(y)*t) }
	for i := 0; i < nt; i++ {
		first := len(md.Indices)
		for _, c := range cellQuads[i] {
			quad(&md, wx(c[0]), wy(c[1]), wx(c[0]+1), wy(c[1]+1), oz, 0, 0, 1, 1)
		}
		part(m.draws[i].material, first)
	}
	order := make([]int, 0, nt)
	for i := range edgeQuads {
		if len(edgeQuads[i]) > 0 {
			order = append(order, i)
		}
	}
	sort.SliceStable(order, func(a, b int) bool { return m.draws[order[a]].rank < m.draws[order[b]].rank })
	half := float32(t / 2)
	for _, i := range order {
		d := &m.draws[i]
		first := len(md.Indices)
		z := oz + float32(float32(d.rank)*edgeLift)
		for _, e := range edgeQuads[i] {
			qx := wx(e.x) + float32(float32(e.quarter%2)*half)
			qy := wy(e.y) - float32(float32(e.quarter/2)*half)
			u0, v0, u1, v1 := edgeUV(e.shape, e.quarter, d.frame[0], d.frame[1])
			quad(&md, qx, qy, qx+half, qy-half, z, u0, v0, u1, v1)
		}
		part(d.edgeMat, first)
	}
	if len(md.Parts) == 0 {
		return nil
	}
	lo, hi := md.Vertices[0].Pos, md.Vertices[0].Pos
	for _, v := range md.Vertices[1:] {
		lo, hi = lo.Min(v.Pos), hi.Max(v.Pos)
	}
	md.Bounds = gmath.AABB{Min: lo, Max: hi}
	return &asset.Model{Name: name, Mesh: md, Materials: mats}
}

// at returns the terrain index + 1 of cell (x, y) of layer l, 0 for an empty cell or one off
// the map.
func (m *Map) at(l, x, y int) int {
	if !m.Inside(x, y) {
		return 0
	}
	return int(m.cells[l][y*m.src.W+x])
}

// priority returns the edge priority of terrain index + 1 v: -1 for an empty cell, 0 for a
// terrain without an edge.
func (m *Map) priority(v int) int {
	if v == 0 {
		return -1
	}
	return m.draws[v-1].priority
}

// cellEdges calls f for every border drawn in cell (x, y) of layer l, in the order of the
// neighbours (N, NE, E, SE, S, SW, W, NW, the first of each terrain) then of the quarters
// (NW, NE, SW, SE): u is the terrain index + 1 whose border it is, shape and quarter say
// which image of its edge atlas.
func (m *Map) cellEdges(l, x, y int, f func(u, shape, quarter int)) {
	p := m.priority(m.at(l, x, y))
	near := [8]int{m.at(l, x, y-1), m.at(l, x+1, y-1), m.at(l, x+1, y), m.at(l, x+1, y+1),
		m.at(l, x, y+1), m.at(l, x-1, y+1), m.at(l, x-1, y), m.at(l, x-1, y-1)}
	for k, u := range near {
		if u == 0 || m.draws[u-1].edge == nil || m.priority(u) <= p || slicesIndex(near[:k], u) >= 0 {
			continue
		}
		// Per quarter: the side above or below, the side left or right, the diagonal.
		for q, sides := range [4][3]int{quarterNW: {0, 6, 7}, quarterNE: {0, 2, 1}, quarterSW: {4, 6, 5}, quarterSE: {4, 2, 3}} {
			hz, vt, dg := near[sides[0]] == u, near[sides[1]] == u, near[sides[2]] == u
			switch {
			case hz && vt:
				f(u, shapeL, q)
			case hz:
				f(u, shapeH, q)
			case vt:
				f(u, shapeV, q)
			case dg:
				f(u, shapeCorner, q)
			}
		}
	}
}

func slicesIndex(s []int, v int) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}
