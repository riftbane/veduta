package inspect

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gmath"
	"github.com/riftbane/veduta/internal/sheet"
	"github.com/riftbane/veduta/scene"
)

// ModelSheets lists the sheet kinds of Model in the order they are written. "summary" is
// the default; Options.Sheets may also hold "all" (every kind) or "none".
var ModelSheets = []string{"summary", "turntable", "silhouette", "normals", "wireframe", "uv_checker", "sections"}

// Thresholds of the model inspector. They are part of the report format (docs/model.md,
// "Inspection"): changing one changes which issues a model gets.
const (
	mdlWeldRel      = 1e-6 // positions closer than this × AABB diagonal are welded before topology checks
	mdlNeedleRel    = 1e-6 // 2·area ≤ this × longest edge² is a zero-area triangle
	mdlVolumeRel    = 1e-9 // closed part: signed volume below −this × part diagonal³ is inside out
	mdlOpposeCos    = -0.5 // mean vertex normal · face normal below this: normals oppose the winding
	mdlUVTol        = 1e-4 // UVs within [−tol, 1+tol] are in range
	mdlUVGrid       = 64   // UV overlap grid: cells along the longer UV extent of a part
	mdlUVOverlapMin = 0.01 // overlapping / covered UV cells above this is MESH_UV_OVERLAP
	mdlDensityCVMax = 0.5  // texel_density_cv above this is MESH_TEXEL_DENSITY_UNEVEN
	mdlPivotRel     = 1e-4 // pivot tolerance, × AABB diagonal
	mdlScaleMin     = 0.05 // meters: largest extent below this is MESH_SCALE_SUSPICIOUS
	mdlScaleMax     = 100  // meters: largest extent above this is MESH_SCALE_SUSPICIOUS
	mdlSymRel       = 1e-3 // a mirrored vertex within this × AABB diagonal of the surface matches
	mdlSymMin       = 0.98 // symmetry score below this is MESH_ASYMMETRIC
	mdlListMax      = 8    // items listed in an issue's "where"
	mdlLineMax      = 4096 // overlay lines (hole and non-manifold edges) drawn per tile
	mdlPad          = 2    // sheet padding in pixels
)

// Overlay colors of the sheets: boundary (hole) edges and non-manifold edges.
const (
	mdlHoleColor    = 0xffff3b30
	mdlNonManColor  = 0xffffd60a
	mdlSheetMaxW    = 640
	mdlSheetDefault = "summary"
)

// Model inspects model name of ir.Lib (spec §9.1) and returns its report, subject
// "model:<name>". The mesh is analysed part by part: positions are welded with an
// epsilon of 1e-6 × the AABB diagonal, then degenerate triangles, duplicate vertices,
// non-manifold edges, holes, inside-out parts, mixed winding, UV range and overlap,
// texel density, pivot, scale, symmetry and triangle budget are checked. Issue "where"
// maps name the source part ("part", its index in "parts", and "shape"); "triangles"
// are mesh triangle indices (triangle i is Mesh.Indices[3i:3i+3]).
//
// Sheets (ModelSheets) are written as PNG under opt.OutDir: by default one "summary";
// opt.Sheets selects others, "all" or "none". An unknown model or sheet kind is an
// error.
func Model(ir *Renderer, name string, opt Options) (*Report, error) {
	if ir == nil || ir.Lib == nil {
		return nil, errors.New("inspect model: no library")
	}
	for _, s := range opt.Sheets {
		if s != "all" && s != "none" && !mdlContains(ModelSheets, s) {
			return nil, fmt.Errorf("inspect model %s: unknown sheet %q (want one of %s, all or none)", name, s, strings.Join(ModelSheets, ", "))
		}
	}
	m := ir.Lib.Models[name]
	if m == nil {
		return nil, fmt.Errorf("inspect model: unknown model %q (have %s)", name, strings.Join(asset.Names(ir.Lib.Models), ", "))
	}
	a := mdlAnalyze(ir.Lib, m)
	rep := &Report{Subject: "model:" + name, Metrics: map[string]any{}}
	a.issues(rep)
	a.metrics(rep.Metrics)
	for _, kind := range ModelSheets {
		if !opt.wants(kind, kind == mdlSheetDefault) {
			continue
		}
		img, err := mdlSheet(ir, a, name, kind, opt)
		if err != nil {
			return nil, fmt.Errorf("inspect model %s: sheet %s: %w", name, kind, err)
		}
		path := opt.sheetPath(rep.Subject, kind)
		if err := mdlWritePNG(path, img); err != nil {
			return nil, fmt.Errorf("inspect model %s: %w", name, err)
		}
		rep.Sheets = append(rep.Sheets, path)
	}
	rep.Finish(opt.Focus)
	return rep, nil
}

func mdlContains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Geometry in float64. Each product that feeds an addition or subtraction is rounded
// explicitly so arm64 cannot fuse it into a multiply-add (see gmath.m32); the helpers are
// inlined, so rounding inside them also protects their callers.

type mdlVec [3]float64

func mdlV(v gmath.Vec3) mdlVec { return mdlVec{float64(v.X), float64(v.Y), float64(v.Z)} }

func (a mdlVec) add(b mdlVec) mdlVec { return mdlVec{a[0] + b[0], a[1] + b[1], a[2] + b[2]} }
func (a mdlVec) sub(b mdlVec) mdlVec { return mdlVec{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }
func (a mdlVec) scale(s float64) mdlVec {
	return mdlVec{float64(a[0] * s), float64(a[1] * s), float64(a[2] * s)}
}
func (a mdlVec) dot(b mdlVec) float64 {
	return float64(a[0]*b[0]) + float64(a[1]*b[1]) + float64(a[2]*b[2])
}
func (a mdlVec) len() float64        { return math.Sqrt(a.dot(a)) }
func (a mdlVec) mid(b mdlVec) mdlVec { return a.add(b).scale(0.5) }
func (a mdlVec) cross(b mdlVec) mdlVec {
	return mdlVec{
		float64(a[1]*b[2]) - float64(a[2]*b[1]),
		float64(a[2]*b[0]) - float64(a[0]*b[2]),
		float64(a[0]*b[1]) - float64(a[1]*b[0]),
	}
}

func (a mdlVec) finite() bool {
	for _, x := range a {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return false
		}
	}
	return true
}

// mdlR rounds to 4 decimals for reports; non-finite values become 0 (JSON has no NaN)
// and negative zero becomes zero.
func mdlR(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return 0
	}
	r := math.Round(x*1e4) / 1e4
	if r == 0 {
		return 0
	}
	return r
}

// mdlRV rounds a vector for reports ([x, y, z] in JSON).
func mdlRV(v mdlVec) gmath.Vec3 {
	return gmath.V3(float32(mdlR(v[0])), float32(mdlR(v[1])), float32(mdlR(v[2])))
}

// Analysis.

// mdlTri is one triangle of a part.
type mdlTri struct {
	id   int       // mesh triangle index: Mesh.Indices[3*id : 3*id+3]
	v    [3]uint32 // vertex indices
	w    [3]int32  // welded position ids
	bad  string    // degeneracy reason, "" for a valid triangle
	area float64   // world area
	n    mdlVec    // unit face normal (from the winding)
	uvA  float64   // UV area
}

// mdlLoop is one hole: a connected set of boundary edges.
type mdlLoop struct {
	edges  int
	center mdlVec
}

// mdlTex is what a part's material says about textures.
type mdlTex struct {
	material string // part material, "" when the entity's material decides
	texture  string // texture of the material, "" for none
	relevant bool   // the part can show a texture: no part material, or a textured one
	missing  bool   // the texture is named but not in the library
	clamp    bool   // the texture exists and does not tile
}

// mdlPart holds the measurements of one source part.
type mdlPart struct {
	info     asset.PartInfo
	root     int // index of the part that owns the source fields (mirrors follow "of")
	rootInfo asset.PartInfo
	count    int      // triangles in the part's index range
	tris     []mdlTri // valid triangles
	degen    []int
	why      map[string]int
	boundary [][2]int32 // directed boundary edges (welded ids)
	loops    []mdlLoop
	nonman   [][2]int32
	sameDir  int    // manifold edges traversed the same way by both triangles
	minor    []bool // per valid triangle: wound against its component's majority
	minority []int
	closed   bool
	area     float64
	volume   float64 // signed enclosed volume (majority orientation) when closed, else 0
	inward   []int
	inWhy    string
	tex      mdlTex
	uvMin    [2]float64
	uvMax    [2]float64
	uvOut    int
	uvVerts  int
	overlap  []int
	overlapR float64
}

// mdlAnalysis is everything Model measures on one compiled model.
type mdlAnalysis struct {
	lib       *asset.Library
	m         *asset.Model
	pos       []mdlVec
	ok        []bool
	weld      []int32
	wpos      []mdlVec
	bounds    gmath.AABB // finite vertices; empty when there are none
	bmin      mdlVec
	bmax      mdlVec
	diag      float64
	eps       float64
	triangles int
	parts     []*mdlPart
	area      float64
	cv        float64
	densMean  float64
	densOn    bool // some part can show a texture (the density issue applies)
	grid      *mdlTriGrid
	symDone   [3]bool // symmetry cache per axis
	symScore  [3]float64
	symBad    [3][]int32
	aggOut    mdlAgg // info-level MESH_UV_OUT_OF_RANGE of all parts
	aggOver   mdlAgg // info-level MESH_UV_OVERLAP of all parts
}

func mdlAnalyze(lib *asset.Library, m *asset.Model) *mdlAnalysis {
	a := &mdlAnalysis{lib: lib, m: m, triangles: len(m.Mesh.Indices) / 3}
	vs := m.Mesh.Vertices
	a.pos = make([]mdlVec, len(vs))
	a.ok = make([]bool, len(vs))
	a.bounds = gmath.EmptyAABB()
	for i, v := range vs {
		a.pos[i] = mdlV(v.Pos)
		if v.Pos.IsFinite() {
			a.ok[i] = true
			a.bounds = a.bounds.Extend(v.Pos)
		}
	}
	if !a.bounds.IsEmpty() {
		a.bmin, a.bmax = mdlV(a.bounds.Min), mdlV(a.bounds.Max)
		a.diag = a.bmax.sub(a.bmin).len()
	}
	a.eps = max(a.diag*mdlWeldRel, 1e-12)
	a.weld, a.wpos = mdlWeld(a.pos, a.ok, a.eps)
	infos := mdlPartInfos(m)
	for i := range infos {
		p := a.analyzePart(infos, i)
		a.area += p.area
		a.parts = append(a.parts, p)
	}
	a.texelDensity()
	return a
}

// mdlPartInfos returns the parts of m; meshes built in code without PartInfo get one
// part per mesh part (or one for all indices), shape "mesh".
func mdlPartInfos(m *asset.Model) []asset.PartInfo {
	if len(m.Parts) > 0 {
		return m.Parts
	}
	var out []asset.PartInfo
	for i, mp := range m.Mesh.Parts {
		mat := ""
		if mp.Material >= 0 && mp.Material < len(m.Materials) {
			mat = m.Materials[mp.Material]
		}
		out = append(out, asset.PartInfo{Index: i, Shape: "mesh", First: mp.First, Count: mp.Count, Material: mat, Of: -1})
	}
	if len(out) == 0 && len(m.Mesh.Indices) > 0 {
		out = append(out, asset.PartInfo{Index: 0, Shape: "mesh", Count: len(m.Mesh.Indices), Of: -1})
	}
	return out
}

// mdlWeld gives every finite position the id of the first earlier position within eps
// (smallest id wins), using a hash grid of cell size eps; non-finite positions get -1.
func mdlWeld(pos []mdlVec, ok []bool, eps float64) ([]int32, []mdlVec) {
	ids := make([]int32, len(pos))
	var reps []mdlVec
	cells := map[[3]int64][]int32{}
	cellOf := func(p mdlVec) [3]int64 {
		var c [3]int64
		for k := range 3 {
			c[k] = int64(max(-9e15, min(9e15, math.Floor(p[k]/eps))))
		}
		return c
	}
	for i, p := range pos {
		if !ok[i] {
			ids[i] = -1
			continue
		}
		c := cellOf(p)
		best := int32(-1)
		for dx := int64(-1); dx <= 1; dx++ {
			for dy := int64(-1); dy <= 1; dy++ {
				for dz := int64(-1); dz <= 1; dz++ {
					for _, id := range cells[[3]int64{c[0] + dx, c[1] + dy, c[2] + dz}] {
						if (best < 0 || id < best) && reps[id].sub(p).len() <= eps {
							best = id
						}
					}
				}
			}
		}
		if best < 0 {
			best = int32(len(reps))
			reps = append(reps, p)
			cells[c] = append(cells[c], best)
		}
		ids[i] = best
	}
	return ids, reps
}

// tri measures the triangle starting at index offset o.
func (a *mdlAnalysis) tri(o int) mdlTri {
	idx := a.m.Mesh.Indices
	t := mdlTri{id: o / 3}
	for k := range 3 {
		t.v[k] = idx[o+k]
		if int(t.v[k]) >= len(a.pos) {
			t.bad = "index_out_of_range"
			return t
		}
	}
	for k := range 3 {
		if !a.ok[t.v[k]] {
			t.bad = "non_finite"
			return t
		}
		t.w[k] = a.weld[t.v[k]]
	}
	switch {
	case t.v[0] == t.v[1] || t.v[1] == t.v[2] || t.v[0] == t.v[2]:
		t.bad = "repeated_index"
		return t
	case t.w[0] == t.w[1] || t.w[1] == t.w[2] || t.w[0] == t.w[2]:
		t.bad = "collapsed"
		return t
	}
	p0, p1, p2 := a.pos[t.v[0]], a.pos[t.v[1]], a.pos[t.v[2]]
	c := p1.sub(p0).cross(p2.sub(p0))
	l := c.len()
	e := max(p1.sub(p0).dot(p1.sub(p0)), p2.sub(p1).dot(p2.sub(p1)), p0.sub(p2).dot(p0.sub(p2)))
	if !(l > mdlNeedleRel*e) || math.IsInf(l, 0) {
		t.bad = "zero_area"
		return t
	}
	t.area = float64(l / 2)
	t.n = c.scale(1 / l)
	vs := a.m.Mesh.Vertices
	u0, u1, u2 := vs[t.v[0]].UV, vs[t.v[1]].UV, vs[t.v[2]].UV
	t.uvA = math.Abs(float64(float64(u1.X-u0.X)*float64(u2.Y-u0.Y))-float64(float64(u1.Y-u0.Y)*float64(u2.X-u0.X))) / 2
	if math.IsNaN(t.uvA) || math.IsInf(t.uvA, 0) {
		t.uvA = 0
	}
	return t
}

// mdlRootOf follows mirror parts to the part whose fields they copy.
func mdlRootOf(infos []asset.PartInfo, i int) int {
	for n := 0; n <= len(infos) && infos[i].Shape == "mirror" && infos[i].Of >= 0 && infos[i].Of < len(infos); n++ {
		i = infos[i].Of
	}
	return i
}

func (a *mdlAnalysis) analyzePart(infos []asset.PartInfo, i int) *mdlPart {
	info := infos[i]
	p := &mdlPart{info: info, root: mdlRootOf(infos, i), why: map[string]int{}}
	p.rootInfo = infos[p.root]
	idx := a.m.Mesh.Indices
	first := min(max(info.First, 0), len(idx))
	end := min(first+max(info.Count, 0), len(idx))
	for o := first; o+3 <= end; o += 3 {
		t := a.tri(o)
		p.count++
		if t.bad != "" {
			p.degen = append(p.degen, t.id)
			p.why[t.bad]++
			continue
		}
		p.tris = append(p.tris, t)
		p.area += t.area
	}
	p.tex = mdlTexOf(a.lib, info.Material)
	a.topology(p)
	a.orientation(p)
	a.uvs(p)
	return p
}

func mdlTexOf(lib *asset.Library, mat string) mdlTex {
	t := mdlTex{material: mat}
	if mat == "" {
		t.relevant = true
		return t
	}
	m := lib.Materials[mat]
	if m == nil || m.Texture == "" {
		return t
	}
	t.texture, t.relevant = m.Texture, true
	switch tx := lib.Textures[m.Texture]; {
	case tx == nil:
		t.missing = true
	case !tx.Tiling:
		t.clamp = true
	}
	return t
}

type mdlEdgeUse struct {
	a, b int32 // welded ids, a < b
	t    int32 // local triangle index
	fwd  bool  // the triangle runs a → b
}

type mdlAdj struct {
	t    int32
	same bool // both triangles run the shared edge the same way
}

// topology finds boundary, non-manifold and inconsistently wound edges of a part (on
// welded positions) and the triangles wound against their neighbours.
func (a *mdlAnalysis) topology(p *mdlPart) {
	uses := make([]mdlEdgeUse, 0, 3*len(p.tris))
	for li, t := range p.tris {
		for k := range 3 {
			wa, wb := t.w[k], t.w[(k+1)%3]
			if wa < wb {
				uses = append(uses, mdlEdgeUse{wa, wb, int32(li), true})
			} else {
				uses = append(uses, mdlEdgeUse{wb, wa, int32(li), false})
			}
		}
	}
	sort.Slice(uses, func(i, j int) bool {
		x, y := uses[i], uses[j]
		if x.a != y.a {
			return x.a < y.a
		}
		if x.b != y.b {
			return x.b < y.b
		}
		return x.t < y.t
	})
	adj := make([][]mdlAdj, len(p.tris))
	for i := 0; i < len(uses); {
		j := i + 1
		for j < len(uses) && uses[j].a == uses[i].a && uses[j].b == uses[i].b {
			j++
		}
		switch u := uses[i]; j - i {
		case 1:
			if u.fwd {
				p.boundary = append(p.boundary, [2]int32{u.a, u.b})
			} else {
				p.boundary = append(p.boundary, [2]int32{u.b, u.a})
			}
		case 2:
			v := uses[i+1]
			same := u.fwd == v.fwd
			if same {
				p.sameDir++
			}
			adj[u.t] = append(adj[u.t], mdlAdj{v.t, same})
			adj[v.t] = append(adj[v.t], mdlAdj{u.t, same})
		default:
			p.nonman = append(p.nonman, [2]int32{u.a, u.b})
		}
		i = j
	}
	p.closed = len(p.tris) > 0 && len(p.boundary) == 0 && len(p.nonman) == 0

	// Orientation: flood each edge-connected component; a triangle's parity flips across
	// an edge both triangles run the same way. The smaller parity class (ties: the one
	// without the component's first triangle) is the minority.
	p.minor = make([]bool, len(p.tris))
	parity := make([]int8, len(p.tris))
	for i := range parity {
		parity[i] = -1
	}
	var queue, comp []int32
	for s := range p.tris {
		if parity[s] >= 0 {
			continue
		}
		parity[s] = 0
		queue, comp = append(queue[:0], int32(s)), comp[:0]
		for len(queue) > 0 {
			x := queue[0]
			queue = queue[1:]
			comp = append(comp, x)
			for _, e := range adj[x] {
				if parity[e.t] < 0 {
					parity[e.t] = parity[x]
					if e.same {
						parity[e.t] ^= 1
					}
					queue = append(queue, e.t)
				}
			}
		}
		ones := 0
		for _, x := range comp {
			ones += int(parity[x])
		}
		minority := int8(1)
		if ones > len(comp)-ones {
			minority = 0
		}
		for _, x := range comp {
			if parity[x] == minority {
				p.minor[x] = true
			}
		}
	}
	for li, t := range p.tris {
		if p.minor[li] {
			p.minority = append(p.minority, t.id)
		}
	}

	// Holes: connected components of the boundary edges, in edge order.
	if len(p.boundary) > 0 {
		var ids []int32
		for _, e := range p.boundary {
			ids = append(ids, e[0], e[1])
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		ids = mdlUniq32(ids)
		find := func(id int32) int { return sort.Search(len(ids), func(i int) bool { return ids[i] >= id }) }
		parent := make([]int, len(ids))
		for i := range parent {
			parent[i] = i
		}
		root := func(x int) int {
			for parent[x] != x {
				parent[x] = parent[parent[x]]
				x = parent[x]
			}
			return x
		}
		for _, e := range p.boundary {
			ra, rb := root(find(e[0])), root(find(e[1]))
			if ra != rb {
				parent[max(ra, rb)] = min(ra, rb)
			}
		}
		loopOf := map[int]int{}
		for _, e := range p.boundary {
			r := root(find(e[0]))
			l, ok := loopOf[r]
			if !ok {
				l = len(p.loops)
				loopOf[r] = l
				p.loops = append(p.loops, mdlLoop{})
			}
			p.loops[l].edges++
		}
		sums := make([]mdlVec, len(p.loops))
		counts := make([]int, len(p.loops))
		for i, id := range ids {
			l := loopOf[root(i)]
			sums[l] = sums[l].add(a.wpos[id])
			counts[l]++
		}
		for l := range p.loops {
			p.loops[l].center = sums[l].scale(1 / float64(counts[l]))
		}
	}
}

func mdlUniq32(s []int32) []int32 {
	out := s[:0]
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}

// orientation computes the signed volume of a closed part (majority orientation) and the
// triangles whose normals point inward.
func (a *mdlAnalysis) orientation(p *mdlPart) {
	if len(p.tris) == 0 {
		return
	}
	lo, hi := mdlVec{math.Inf(1), math.Inf(1), math.Inf(1)}, mdlVec{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for _, t := range p.tris {
		for _, v := range t.v {
			for k := range 3 {
				lo[k], hi[k] = min(lo[k], a.pos[v][k]), max(hi[k], a.pos[v][k])
			}
		}
	}
	c, l := lo.mid(hi), hi.sub(lo).len()
	vol := 0.0
	for li, t := range p.tris {
		d := a.pos[t.v[0]].sub(c).dot(a.pos[t.v[1]].sub(c).cross(a.pos[t.v[2]].sub(c))) / 6
		if p.minor[li] {
			d = -d
		}
		vol += d
	}
	if p.closed {
		p.volume = vol
	}
	if p.closed && vol < -mdlVolumeRel*l*l*l {
		p.inWhy = "inside_out"
		for li, t := range p.tris {
			if !p.minor[li] {
				p.inward = append(p.inward, t.id)
			}
		}
		return
	}
	vs := a.m.Mesh.Vertices
	for li, t := range p.tris {
		if p.minor[li] {
			continue // reported as MESH_MIXED_WINDING
		}
		var n mdlVec
		for _, v := range t.v {
			n = n.add(mdlV(vs[v].Normal))
		}
		if ln := n.len(); ln > 0 && n.finite() && t.n.dot(n)/ln < mdlOpposeCos {
			p.inward = append(p.inward, t.id)
		}
	}
	if len(p.inward) > 0 {
		p.inWhy = "normals_oppose_winding"
	}
}

// uvs measures the UV range of a part and, when the part can show a texture, its UV
// overlap.
func (a *mdlAnalysis) uvs(p *mdlPart) {
	var verts []uint32
	for _, t := range p.tris {
		verts = append(verts, t.v[:]...)
	}
	sort.Slice(verts, func(i, j int) bool { return verts[i] < verts[j] })
	n := 0
	for i, v := range verts {
		if i == 0 || v != verts[i-1] {
			verts[n] = v
			n++
		}
	}
	verts = verts[:n]
	p.uvVerts = len(verts)
	p.uvMin = [2]float64{math.Inf(1), math.Inf(1)}
	p.uvMax = [2]float64{math.Inf(-1), math.Inf(-1)}
	vs := a.m.Mesh.Vertices
	for _, v := range verts {
		uv := vs[v].UV
		if !uv.IsFinite() {
			p.uvOut++
			continue
		}
		u, w := float64(uv.X), float64(uv.Y)
		p.uvMin = [2]float64{min(p.uvMin[0], u), min(p.uvMin[1], w)}
		p.uvMax = [2]float64{max(p.uvMax[0], u), max(p.uvMax[1], w)}
		if u < -mdlUVTol || u > 1+mdlUVTol || w < -mdlUVTol || w > 1+mdlUVTol {
			p.uvOut++
		}
	}
	if p.uvMin[0] > p.uvMax[0] {
		p.uvMin, p.uvMax = [2]float64{}, [2]float64{}
	}
	if p.tex.relevant {
		p.overlap, p.overlapR = mdlUVOverlap(p.tris, vs)
	}
}

// mdlUVOverlap rasterizes the UV triangles of a part on a grid of mdlUVGrid cells along
// its longer UV extent (cell centers, with a top-left rule so triangles sharing an edge
// never both cover a center on it) and returns the triangles covering a cell that
// another triangle also covers, and overlapping cells / covered cells.
func mdlUVOverlap(tris []mdlTri, vs []gfx.Vertex) ([]int, float64) {
	type uvTri struct {
		p  [3][2]float64
		id int
	}
	var uts []uvTri
	lo, hi := [2]float64{math.Inf(1), math.Inf(1)}, [2]float64{math.Inf(-1), math.Inf(-1)}
	for _, t := range tris {
		var ut uvTri
		ut.id = t.id
		ok := true
		for k := range 3 {
			uv := vs[t.v[k]].UV
			if !uv.IsFinite() {
				ok = false
				break
			}
			ut.p[k] = [2]float64{float64(uv.X), float64(uv.Y)}
		}
		if !ok {
			continue
		}
		ar := float64((ut.p[1][0]-ut.p[0][0])*(ut.p[2][1]-ut.p[0][1])) - float64((ut.p[1][1]-ut.p[0][1])*(ut.p[2][0]-ut.p[0][0]))
		if ar == 0 {
			continue
		}
		if ar < 0 {
			ut.p[1], ut.p[2] = ut.p[2], ut.p[1]
		}
		for k := range 3 {
			lo = [2]float64{min(lo[0], ut.p[k][0]), min(lo[1], ut.p[k][1])}
			hi = [2]float64{max(hi[0], ut.p[k][0]), max(hi[1], ut.p[k][1])}
		}
		uts = append(uts, ut)
	}
	if len(uts) < 2 {
		return nil, 0
	}
	ext := max(hi[0]-lo[0], hi[1]-lo[1])
	if !(ext > 0) || math.IsInf(ext, 0) {
		return nil, 0
	}
	cell := ext / mdlUVGrid
	nu := min(mdlUVGrid, max(1, int(math.Ceil((hi[0]-lo[0])/cell))))
	nv := min(mdlUVGrid, max(1, int(math.Ceil((hi[1]-lo[1])/cell))))
	counts := make([]uint16, nu*nv)
	visit := func(ut *uvTri, f func(i int)) {
		mnu := min(ut.p[0][0], ut.p[1][0], ut.p[2][0])
		mxu := max(ut.p[0][0], ut.p[1][0], ut.p[2][0])
		mnv := min(ut.p[0][1], ut.p[1][1], ut.p[2][1])
		mxv := max(ut.p[0][1], ut.p[1][1], ut.p[2][1])
		i0 := max(0, int(math.Ceil((mnu-lo[0])/cell-0.5)))
		i1 := min(nu-1, int(math.Floor((mxu-lo[0])/cell-0.5)))
		j0 := max(0, int(math.Ceil((mnv-lo[1])/cell-0.5)))
		j1 := min(nv-1, int(math.Floor((mxv-lo[1])/cell-0.5)))
		for j := j0; j <= j1; j++ {
			for i := i0; i <= i1; i++ {
				c := [2]float64{lo[0] + float64((float64(i)+0.5)*cell), lo[1] + float64((float64(j)+0.5)*cell)}
				if mdlUVInside(ut.p, c) {
					f(j*nu + i)
				}
			}
		}
	}
	for k := range uts {
		visit(&uts[k], func(i int) {
			if counts[i] < math.MaxUint16 {
				counts[i]++
			}
		})
	}
	covered, over := 0, 0
	for _, c := range counts {
		if c > 0 {
			covered++
		}
		if c > 1 {
			over++
		}
	}
	if over == 0 {
		return nil, 0
	}
	var out []int
	for k := range uts {
		hit := false
		visit(&uts[k], func(i int) {
			if counts[i] > 1 {
				hit = true
			}
		})
		if hit {
			out = append(out, uts[k].id)
		}
	}
	return out, float64(over) / float64(covered)
}

// mdlEdgeFn is the edge function of a → b at c, computed from the lexicographically
// smaller end so that the reversed edge gives exactly the negated value.
func mdlEdgeFn(a, b, c [2]float64) float64 {
	if a[0] < b[0] || (a[0] == b[0] && a[1] < b[1]) {
		return float64((b[0]-a[0])*(c[1]-a[1])) - float64((b[1]-a[1])*(c[0]-a[0]))
	}
	return -(float64((a[0]-b[0])*(c[1]-b[1])) - float64((a[1]-b[1])*(c[0]-b[0])))
}

// mdlUVInside reports whether c is inside the counter-clockwise triangle p; points on
// an edge count for exactly one of the two directions of that edge.
func mdlUVInside(p [3][2]float64, c [2]float64) bool {
	for k := range 3 {
		a, b := p[k], p[(k+1)%3]
		e := mdlEdgeFn(a, b, c)
		if e < 0 {
			return false
		}
		if e == 0 {
			d := [2]float64{b[0] - a[0], b[1] - a[1]}
			if !(d[1] < 0 || (d[1] == 0 && d[0] > 0)) {
				return false
			}
		}
	}
	return true
}

// texelDensity computes texel_density_cv: the area-weighted coefficient of variation of
// uv-area / world-area over the triangles of parts that can show a texture (all
// triangles when no part can).
func (a *mdlAnalysis) texelDensity() {
	for _, p := range a.parts {
		if p.tex.relevant && len(p.tris) > 0 {
			a.densOn = true
		}
	}
	use := func(p *mdlPart) bool { return !a.densOn || p.tex.relevant }
	var sa, su float64
	for _, p := range a.parts {
		if !use(p) {
			continue
		}
		for _, t := range p.tris {
			sa += t.area
			su += t.uvA
		}
	}
	if !(sa > 0) {
		return
	}
	a.densMean = su / sa
	if !(a.densMean > 0) {
		return
	}
	var sv float64
	for _, p := range a.parts {
		if !use(p) {
			continue
		}
		for _, t := range p.tris {
			d := t.uvA/t.area - a.densMean
			sv += float64(t.area * d * d)
		}
	}
	a.cv = math.Sqrt(sv/sa) / a.densMean
}

// Symmetry.

// mdlTriGrid is a uniform grid over the model bounds listing, per cell, the valid
// triangles whose bounds (grown by the match distance) touch the cell: cell i lists
// ids[start[i]:start[i+1]].
type mdlTriGrid struct {
	lo    mdlVec
	h     float64
	n     [3]int
	start []int32
	ids   []int32
	tris  [][3]mdlVec
	boxes [][2]mdlVec // triangle bounds grown by the match distance
}

// mdlGridEntries caps the grid: a coarser one is used when the triangles would be listed
// in more cells than this in total.
const mdlGridEntries = 1 << 24

func (a *mdlAnalysis) symGrid() *mdlTriGrid {
	if a.grid != nil {
		return a.grid
	}
	eps := max(a.diag*mdlSymRel, 1e-9)
	g := &mdlTriGrid{lo: a.bmin}
	for _, p := range a.parts {
		for _, t := range p.tris {
			tri := [3]mdlVec{a.pos[t.v[0]], a.pos[t.v[1]], a.pos[t.v[2]]}
			var box [2]mdlVec
			for k := range 3 {
				box[0][k] = min(tri[0][k], tri[1][k], tri[2][k]) - eps
				box[1][k] = max(tri[0][k], tri[1][k], tri[2][k]) + eps
			}
			g.tris = append(g.tris, tri)
			g.boxes = append(g.boxes, box)
		}
	}
	// Cells along the diagonal: twice the cube root of the triangle count, 8–128.
	// Counted in integers (c³ ≥ 8n) rather than with math.Cbrt, which is not exact.
	cells := 8
	for cells < 128 && cells*cells*cells < 8*len(g.tris) {
		cells++
	}
	for {
		g.h = max(a.diag/float64(cells), 2*eps)
		for k := range 3 {
			g.n[k] = min(128, max(1, int(math.Ceil((a.bmax[k]-a.bmin[k])/g.h))))
		}
		total := 0
		for _, b := range g.boxes {
			n := 1
			for k := range 3 {
				n *= g.cell(k, b[1][k]) - g.cell(k, b[0][k]) + 1
			}
			total += n
		}
		if total <= mdlGridEntries || cells <= 1 {
			g.start = make([]int32, g.n[0]*g.n[1]*g.n[2]+1)
			g.ids = make([]int32, total)
			break
		}
		cells /= 2
	}
	g.each(func(t, c int) { g.start[c+1]++ })
	for i := 1; i < len(g.start); i++ {
		g.start[i] += g.start[i-1]
	}
	fill := append([]int32(nil), g.start[:len(g.start)-1]...)
	g.each(func(t, c int) {
		g.ids[fill[c]] = int32(t)
		fill[c]++
	})
	a.grid = g
	return g
}

// each calls f for every (triangle, cell) pair of the grid, in triangle order.
func (g *mdlTriGrid) each(f func(t, c int)) {
	for t, b := range g.boxes {
		var c0, c1 [3]int
		for k := range 3 {
			c0[k], c1[k] = g.cell(k, b[0][k]), g.cell(k, b[1][k])
		}
		for z := c0[2]; z <= c1[2]; z++ {
			for y := c0[1]; y <= c1[1]; y++ {
				for x := c0[0]; x <= c1[0]; x++ {
					f(t, (z*g.n[1]+y)*g.n[0]+x)
				}
			}
		}
	}
}

func (g *mdlTriGrid) cell(k int, x float64) int {
	c := math.Floor((x - g.lo[k]) / g.h)
	if !(c >= 0) {
		return 0
	}
	return min(g.n[k]-1, int(min(c, 1e9)))
}

// near reports whether a triangle lies within eps of q.
func (g *mdlTriGrid) near(q mdlVec, eps float64) bool {
	i := (g.cell(2, q[2])*g.n[1]+g.cell(1, q[1]))*g.n[0] + g.cell(0, q[0])
	for _, id := range g.ids[g.start[i]:g.start[i+1]] {
		b := &g.boxes[id]
		if q[0] < b[0][0] || q[0] > b[1][0] || q[1] < b[0][1] || q[1] > b[1][1] || q[2] < b[0][2] || q[2] > b[1][2] {
			continue // outside the grown bounds: farther than eps
		}
		t := &g.tris[id]
		if mdlClosest(q, t[0], t[1], t[2]).sub(q).len() <= eps {
			return true
		}
	}
	return false
}

// mdlClosest returns the point of triangle abc closest to p (Ericson, Real-Time
// Collision Detection, 5.1.5).
func mdlClosest(p, a, b, c mdlVec) mdlVec {
	ab, ac, ap := b.sub(a), c.sub(a), p.sub(a)
	d1, d2 := ab.dot(ap), ac.dot(ap)
	if d1 <= 0 && d2 <= 0 {
		return a
	}
	bp := p.sub(b)
	d3, d4 := ab.dot(bp), ac.dot(bp)
	if d3 >= 0 && d4 <= d3 {
		return b
	}
	vc := float64(d1*d4) - float64(d3*d2)
	if vc <= 0 && d1 >= 0 && d3 <= 0 {
		return a.add(ab.scale(d1 / (d1 - d3)))
	}
	cp := p.sub(c)
	d5, d6 := ab.dot(cp), ac.dot(cp)
	if d6 >= 0 && d5 <= d6 {
		return c
	}
	vb := float64(d5*d2) - float64(d1*d6)
	if vb <= 0 && d2 >= 0 && d6 <= 0 {
		return a.add(ac.scale(d2 / (d2 - d6)))
	}
	va := float64(d3*d6) - float64(d5*d4)
	if va <= 0 && d4-d3 >= 0 && d5-d6 >= 0 {
		return b.add(c.sub(b).scale((d4 - d3) / ((d4 - d3) + (d5 - d6))))
	}
	den := va + vb + vc
	if den == 0 {
		return a
	}
	return a.add(ab.scale(vb / den)).add(ac.scale(vc / den))
}

// symPlane returns the model-space coordinate of the symmetry plane perpendicular to
// axis: the source-space origin plane (where "mirror" parts reflect), moved by the pivot.
func (a *mdlAnalysis) symPlane(axis int) float64 { return float64(a.m.PivotOffset.Get(axis)) }

// symmetry returns the fraction of the model's distinct vertex positions (of valid
// triangles) whose mirror image across the symmetry plane perpendicular to axis
// (symPlane) lies within mdlSymRel × diagonal of the surface, and the welded ids of the
// others. A model without valid triangles scores 1.
func (a *mdlAnalysis) symmetry(axis int) (float64, []int32) {
	if !a.symDone[axis] {
		a.symScore[axis], a.symBad[axis] = a.computeSymmetry(axis)
		a.symDone[axis] = true
	}
	return a.symScore[axis], a.symBad[axis]
}

// computeSymmetry measures what symmetry returns (uncached).
func (a *mdlAnalysis) computeSymmetry(axis int) (float64, []int32) {
	var ids []int32
	for _, p := range a.parts {
		for _, t := range p.tris {
			ids = append(ids, t.w[:]...)
		}
	}
	if len(ids) == 0 {
		return 1, nil
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	ids = mdlUniq32(ids)
	g := a.symGrid()
	eps := max(a.diag*mdlSymRel, 1e-9)
	c := a.symPlane(axis)
	var bad []int32
	for _, id := range ids {
		q := a.wpos[id]
		q[axis] = float64(2*c) - q[axis]
		inside := true
		for k := range 3 {
			if q[k] < a.bmin[k]-eps || q[k] > a.bmax[k]+eps {
				inside = false
			}
		}
		if !inside || !g.near(q, eps) {
			bad = append(bad, id)
		}
	}
	return float64(len(ids)-len(bad)) / float64(len(ids)), bad
}

// Duplicates.

type mdlDup struct {
	count int   // redundant vertices
	verts []int // redundant vertex indices (ascending)
	parts []int // parts referencing the duplicated vertices
}

// duplicates groups vertices with bit-identical position, normal and UV; groups spanning
// several parts are coincident geometry (cross), others unwelded vertices (within).
func (a *mdlAnalysis) duplicates() (within, cross mdlDup) {
	vs := a.m.Mesh.Vertices
	if len(vs) < 2 {
		return
	}
	vpart := make([]int, len(vs))
	for i := range vpart {
		vpart[i] = -1
	}
	idx := a.m.Mesh.Indices
	for _, p := range a.parts {
		first := min(max(p.info.First, 0), len(idx))
		end := min(first+max(p.info.Count, 0), len(idx))
		for _, v := range idx[first:end] {
			if int(v) < len(vs) && vpart[v] < 0 {
				vpart[v] = p.info.Index
			}
		}
	}
	key := func(v gfx.Vertex) [8]uint32 {
		return [8]uint32{math.Float32bits(v.Pos.X), math.Float32bits(v.Pos.Y), math.Float32bits(v.Pos.Z),
			math.Float32bits(v.Normal.X), math.Float32bits(v.Normal.Y), math.Float32bits(v.Normal.Z),
			math.Float32bits(v.UV.X), math.Float32bits(v.UV.Y)}
	}
	order := make([]int, len(vs))
	for i := range order {
		order[i] = i
	}
	keys := make([][8]uint32, len(vs))
	for i, v := range vs {
		keys[i] = key(v)
	}
	less := func(x, y [8]uint32) int {
		for k := range x {
			if x[k] != y[k] {
				if x[k] < y[k] {
					return -1
				}
				return 1
			}
		}
		return 0
	}
	sort.Slice(order, func(i, j int) bool {
		if c := less(keys[order[i]], keys[order[j]]); c != 0 {
			return c < 0
		}
		return order[i] < order[j]
	})
	wp, cp := map[int]bool{}, map[int]bool{}
	for i := 0; i < len(order); {
		j := i + 1
		for j < len(order) && keys[order[j]] == keys[order[i]] {
			j++
		}
		if j-i > 1 {
			grp := order[i:j]
			ps := map[int]bool{}
			for _, v := range grp {
				if vpart[v] >= 0 { // unreferenced vertices belong to no part
					ps[vpart[v]] = true
				}
			}
			d, set := &within, wp
			if len(ps) > 1 {
				d, set = &cross, cp
			}
			d.count += len(grp) - 1
			d.verts = append(d.verts, grp[1:]...)
			for q := range ps {
				if q >= 0 {
					set[q] = true
				}
			}
		}
		i = j
	}
	for _, x := range []struct {
		d   *mdlDup
		set map[int]bool
	}{{&within, wp}, {&cross, cp}} {
		sort.Ints(x.d.verts)
		for q := range x.set {
			x.d.parts = append(x.d.parts, q)
		}
		sort.Ints(x.d.parts)
	}
	return within, cross
}

// Report.

func mdlFirst(ids []int) []int {
	out := append([]int(nil), ids...)
	sort.Ints(out)
	if len(out) > mdlListMax {
		out = out[:mdlListMax]
	}
	return out
}

func (p *mdlPart) name() string {
	if p.info.Shape == "mirror" {
		return fmt.Sprintf("Part %d (mirror of part %d)", p.info.Index, p.info.Of)
	}
	return fmt.Sprintf("Part %d (%s)", p.info.Index, p.info.Shape)
}

// on names where the part's source fields live: "" for the part itself, " on part N"
// for a mirror (it copies them).
func (p *mdlPart) on() string {
	if p.root == p.info.Index || p.info.Shape != "mirror" {
		return ""
	}
	return fmt.Sprintf(" on part %d (the mirror copies it)", p.rootInfo.Index)
}

func (p *mdlPart) where() map[string]any {
	w := map[string]any{"part": p.info.Index, "shape": p.info.Shape}
	if p.info.Shape == "mirror" {
		w["of"] = p.info.Of
	}
	return w
}

// fields lists the source fields that size the part's geometry.
func (p *mdlPart) fields() string {
	switch p.rootInfo.Shape {
	case "box", "plane":
		return `"size" and "scale"`
	case "cylinder":
		return `"radius", "height" and "scale"`
	case "sphere":
		return `"radius", "segments", "rings" and "scale"`
	case "extrude":
		return `"profile", "depth" and "scale"`
	case "lathe":
		return `"profile", "segments" and "scale"`
	}
	return "vertices"
}

func mdlAxisName(axis string) (int, bool) {
	switch axis {
	case "x":
		return 0, true
	case "y":
		return 1, true
	case "z":
		return 2, true
	}
	return 0, false
}

func (a *mdlAnalysis) issues(r *Report) {
	for _, p := range a.parts {
		a.partIssues(r, p)
	}
	a.aggIssues(r)
	a.dupIssues(r)
	a.modelIssues(r)
}

func (a *mdlAnalysis) partIssues(r *Report, p *mdlPart) {
	if len(p.degen) > 0 {
		w := p.where()
		w["triangles"] = mdlFirst(p.degen)
		w["reasons"] = p.why
		var reasons []string
		for _, k := range asset.Names(p.why) {
			reasons = append(reasons, fmt.Sprintf("%d %s", p.why[k], k))
		}
		r.Add(Warning, "MESH_DEGENERATE_TRIANGLE", len(p.degen), w,
			fmt.Sprintf("%s has %d degenerate triangle(s) (%s). Check its %s%s for near-zero values or repeated points.",
				p.name(), len(p.degen), strings.Join(reasons, ", "), p.fields(), p.on()))
	}
	if len(p.nonman) > 0 {
		w := p.where()
		w["edges"] = len(p.nonman)
		var at []gmath.Vec3
		for _, e := range p.nonman[:min(len(p.nonman), mdlListMax)] {
			at = append(at, mdlRV(a.wpos[e[0]].mid(a.wpos[e[1]])))
		}
		w["at"] = at
		r.Add(Warning, "MESH_NONMANIFOLD_EDGE", len(p.nonman), w,
			fmt.Sprintf("%s has %d edge(s) shared by more than two triangles (first at %v): its surface folds onto itself. Check its %s%s for points that touch or repeat.",
				p.name(), len(p.nonman), mdlFmtVec(at[0]), p.fields(), p.on()))
	}
	if len(p.loops) > 0 {
		w := p.where()
		w["loops"] = len(p.loops)
		w["edges"] = len(p.boundary)
		var sizes []int
		var centers []gmath.Vec3
		for _, l := range p.loops[:min(len(p.loops), mdlListMax)] {
			sizes = append(sizes, l.edges)
			centers = append(centers, mdlRV(l.center))
		}
		w["loop_edges"] = sizes
		w["centers"] = centers
		sev, hint := Warning, fmt.Sprintf("%s should be closed but has %d hole loop(s) (%d boundary edges, first around %v). Check its %s%s for near-zero values.",
			p.name(), len(p.loops), len(p.boundary), mdlFmtVec(centers[0]), p.fields(), p.on())
		switch p.rootInfo.Shape {
		case "plane":
			sev = Info
			hint = fmt.Sprintf("%s is an open, single-sided surface (%d hole loop(s), %d boundary edges): expected for a plane. Use a \"box\" with a small \"size\" instead if it must be closed or seen from below.",
				p.name(), len(p.loops), len(p.boundary))
		case "lathe":
			sev = Info
			hint = fmt.Sprintf("%s is open (%d hole loop(s), %d boundary edges, first around %v): its \"profile\"%s does not start and end on the axis (r = 0) or end on its first point. Expected for tubes and cups; close the \"profile\" for a solid.",
				p.name(), len(p.loops), len(p.boundary), mdlFmtVec(centers[0]), p.on())
		}
		r.Add(sev, "MESH_OPEN_BOUNDARY", len(p.loops), w, hint)
	}
	if len(p.inward) > 0 {
		w := p.where()
		w["triangles"] = mdlFirst(p.inward)
		w["reason"] = p.inWhy
		if p.closed {
			w["signed_volume"] = mdlR(p.volume)
		}
		hint := fmt.Sprintf("%s has normals pointing inward. Set \"flip_normals\": %t%s or check winding.", p.name(), !p.rootInfo.FlipNormals, p.on())
		if p.inWhy == "normals_oppose_winding" {
			hint = fmt.Sprintf("%s has %d triangle(s) whose vertex normals point against their winding. Lower \"smooth_angle_deg\" (now %g) or check winding.",
				p.name(), len(p.inward), a.m.SmoothAngleDeg)
		}
		r.Add(Error, "MESH_FLIPPED_NORMALS", len(p.inward), w, hint)
	}
	if len(p.minority) > 0 {
		w := p.where()
		w["triangles"] = mdlFirst(p.minority)
		w["edges"] = p.sameDir
		r.Add(Error, "MESH_MIXED_WINDING", len(p.minority), w,
			fmt.Sprintf("%s has %d triangle(s) wound against their neighbours (%d shared edge(s) run the same way in both triangles); with back-face culling they render as holes. Check its %s, then \"flip_normals\"%s.",
				p.name(), len(p.minority), p.sameDir, p.fields(), p.on()))
	}
	if p.tex.relevant && p.uvOut > 0 {
		w := p.where()
		w["vertices"] = p.uvOut
		w["uv_min"] = [2]float64{mdlR(p.uvMin[0]), mdlR(p.uvMin[1])}
		w["uv_max"] = [2]float64{mdlR(p.uvMax[0]), mdlR(p.uvMax[1])}
		w["material"] = p.tex.material
		w["texture"] = p.tex.texture
		span := fmt.Sprintf("%s has %d of %d UVs outside [0, 1] (u %.2f..%.2f, v %.2f..%.2f; UVs are meters)",
			p.name(), p.uvOut, p.uvVerts, p.uvMin[0], p.uvMax[0], p.uvMin[1], p.uvMax[1])
		switch {
		case p.tex.clamp:
			r.Add(Warning, "MESH_UV_OUT_OF_RANGE", p.uvOut, w, span+fmt.Sprintf(" but texture %q does not tile, so its edge texels smear. Set \"tiling\": true in textures/%s.tex.json, or keep the part within 1 m (%s%s).",
				p.tex.texture, p.tex.texture, p.fields(), p.on()))
		case p.tex.missing:
			r.Add(Info, "MESH_UV_OUT_OF_RANGE", p.uvOut, w, span+fmt.Sprintf(" and texture %q of material %q is not in the library. Add textures/%s.tex.json (with \"tiling\": true to repeat).",
				p.tex.texture, p.tex.material, p.tex.texture))
		default: // tiling or entity-chosen texture: merged into one info issue
			g := &a.aggOut
			g.parts = append(g.parts, p.info.Index)
			g.count += p.uvOut
			if len(g.parts) == 1 {
				g.lo, g.hi = p.uvMin, p.uvMax
			}
			g.lo = [2]float64{min(g.lo[0], p.uvMin[0]), min(g.lo[1], p.uvMin[1])}
			g.hi = [2]float64{max(g.hi[0], p.uvMax[0]), max(g.hi[1], p.uvMax[1])}
		}
	}
	if p.tex.relevant && p.overlapR > mdlUVOverlapMin {
		if !p.tex.clamp { // merged into one info issue
			g := &a.aggOver
			g.parts = append(g.parts, p.info.Index)
			g.count += len(p.overlap)
			g.tris = append(g.tris, p.overlap...)
			g.ratio = max(g.ratio, p.overlapR)
			return
		}
		w := p.where()
		w["uv"] = p.info.UV
		w["triangles"] = mdlFirst(p.overlap)
		w["overlap_ratio"] = mdlR(p.overlapR)
		r.Add(Warning, "MESH_UV_OVERLAP", len(p.overlap), w,
			fmt.Sprintf("%s maps %d triangle(s) onto UV space other triangles of the part also use (%.0f%% of its covered UV cells, \"uv\": %q) and texture %q does not tile, so those faces show the same texels. Set \"tiling\": true in textures/%s.tex.json or choose another \"uv\" mode%s.",
				p.name(), len(p.overlap), 100*p.overlapR, p.info.UV, p.tex.texture, p.tex.texture, p.on()))
	}
}

// mdlAgg merges info-level UV findings of several parts into one issue.
type mdlAgg struct {
	parts  []int
	count  int
	lo, hi [2]float64
	tris   []int
	ratio  float64
}

// aggIssues adds the merged info-level UV issues.
func (a *mdlAnalysis) aggIssues(r *Report) {
	if g := a.aggOut; g.count > 0 {
		r.Add(Info, "MESH_UV_OUT_OF_RANGE", g.count,
			map[string]any{"parts": g.parts, "vertices": g.count,
				"uv_min": [2]float64{mdlR(g.lo[0]), mdlR(g.lo[1])}, "uv_max": [2]float64{mdlR(g.hi[0]), mdlR(g.hi[1])}},
			fmt.Sprintf("Parts %v have %d UVs outside [0, 1] (u %.2f..%.2f, v %.2f..%.2f; UVs are meters). Their texture tiles or is chosen by the entity's material, so it repeats every meter: nothing to do unless the repeat is unwanted (a texture without \"tiling\": true would smear its edges).",
				g.parts, g.count, g.lo[0], g.hi[0], g.lo[1], g.hi[1]))
	}
	if g := a.aggOver; g.count > 0 {
		r.Add(Info, "MESH_UV_OVERLAP", g.count,
			map[string]any{"parts": g.parts, "triangles": mdlFirst(g.tris), "overlap_ratio": mdlR(g.ratio)},
			fmt.Sprintf("Parts %v map %d triangles onto UV space other triangles of the same part also use (up to %.0f%% of a part's covered UV cells): expected for projected \"uv\" modes with a repeating texture. Nothing to do unless a part needs unique texels (choose another \"uv\" mode for it).",
				g.parts, g.count, 100*g.ratio))
	}
}

func (a *mdlAnalysis) dupIssues(r *Report) {
	within, cross := a.duplicates()
	if cross.count > 0 {
		r.Add(Warning, "MESH_DUPLICATE_VERTEX", cross.count,
			map[string]any{"vertices": mdlFirst(cross.verts), "parts": cross.parts},
			fmt.Sprintf("%d vertices repeat vertices of another part exactly (position, normal and UV) in parts %v: those parts coincide and will z-fight. Move one with \"position\" or remove the duplicated part.",
				cross.count, cross.parts))
	}
	if within.count > 0 {
		r.Add(Info, "MESH_DUPLICATE_VERTEX", within.count,
			map[string]any{"vertices": mdlFirst(within.verts), "parts": within.parts},
			fmt.Sprintf("%d vertices repeat another vertex of the same part exactly (position, normal and UV) in parts %v: the mesh is not welded. Re-cook the model from its source; the compiler shares such vertices.",
				within.count, within.parts))
	}
}

func (a *mdlAnalysis) modelIssues(r *Report) {
	m := a.m
	if a.densOn && a.cv > mdlDensityCVMax {
		sev := Info
		worst, dev := -1, -1.0
		var dens []float64
		for _, p := range a.parts {
			if !p.tex.relevant || len(p.tris) == 0 {
				dens = append(dens, 0)
				continue
			}
			if p.tex.texture != "" {
				sev = Warning
			}
			var sa, su float64
			for _, t := range p.tris {
				sa += t.area
				su += t.uvA
			}
			d := su / sa
			dens = append(dens, mdlR(d))
			// The part furthest from the mean in ratio, |log(d/mean)| ranked as
			// max(r, 1/r): math.Log is assembly on amd64 and portable Go on arm64.
			x := math.Inf(1)
			if d > 0 {
				r := d / a.densMean
				x = max(r, 1/r)
			}
			if x > dev {
				worst, dev = len(dens)-1, x
			}
		}
		wp := a.parts[worst]
		w := map[string]any{"cv": mdlR(a.cv), "mean": mdlR(a.densMean), "part_density": dens, "worst_part": wp.info.Index, "shape": wp.info.Shape}
		r.Add(sev, "MESH_TEXEL_DENSITY_UNEVEN", 1, w,
			fmt.Sprintf("Texel density is uneven (texel_density_cv %.2f > %.2g): part %d (%s) has %.2f× the model's mean UV area per m². UVs are meters, so the projection stretches it: set another \"uv\" mode on part %d (now %q)%s.",
				a.cv, mdlDensityCVMax, wp.info.Index, wp.info.Shape, dens[worst]/a.densMean, wp.info.Index, wp.info.UV, wp.on()))
	}

	if !a.bounds.IsEmpty() {
		tol := max(mdlPivotRel*a.diag, 1e-6)
		c := a.bmin.mid(a.bmax)
		switch m.Pivot {
		case "bottom-center", "center":
			pt := mdlVec{c[0], a.bmin[1], c[2]}
			if m.Pivot == "center" {
				pt = c
			}
			if math.Abs(pt[0]) > tol || math.Abs(pt[1]) > tol || math.Abs(pt[2]) > tol {
				r.Add(Warning, "MESH_PIVOT_OFF", 1, map[string]any{"pivot": m.Pivot, "pivot_point": mdlRV(pt)},
					fmt.Sprintf("The mesh does not honour \"pivot\": %q: its pivot point is at %v instead of the origin. Re-cook the model from its source (the compiler applies \"pivot\").",
						m.Pivot, mdlFmtVec(mdlRV(pt))))
			}
		default:
			var near mdlVec
			for k := range 3 {
				near[k] = min(max(0, a.bmin[k]), a.bmax[k])
			}
			if d := near.len(); d > tol {
				r.Add(Warning, "MESH_PIVOT_OFF", 1, map[string]any{"pivot": mdlPivotName(m.Pivot), "distance": mdlR(d), "nearest": mdlRV(near)},
					fmt.Sprintf("The model origin (\"pivot\": \"origin\") lies %.3g m outside the geometry (nearest point %v), so entities appear offset from their position. Set \"pivot\": \"bottom-center\" or \"center\", or move the parts' \"position\" so the geometry touches the origin.",
						d, mdlFmtVec(mdlRV(near))))
			}
		}
	}

	size := a.bmax.sub(a.bmin)
	if largest := max(size[0], size[1], size[2]); largest < mdlScaleMin || largest > mdlScaleMax {
		bound := fmt.Sprintf("< %g m", float64(mdlScaleMin))
		if largest > mdlScaleMax {
			bound = fmt.Sprintf("> %g m", float64(mdlScaleMax))
		}
		r.Add(Warning, "MESH_SCALE_SUSPICIOUS", 1, map[string]any{"size": mdlRV(size), "largest": mdlR(largest)},
			fmt.Sprintf("The largest extent is %.4g m (%s; expected 0.05–100 m). Units are meters: check the parts' \"size\", \"radius\", \"height\", \"profile\" and \"scale\".", largest, bound))
	}

	if axis, ok := mdlAxisName(m.Symmetry); ok {
		score, bad := a.symmetry(axis)
		if score < mdlSymMin {
			badSet := map[int32]bool{}
			for _, id := range bad {
				badSet[id] = true
			}
			var parts []int
			total := 0
			seen := map[int32]bool{}
			for _, p := range a.parts {
				hit := false
				for _, t := range p.tris {
					for _, id := range t.w {
						if badSet[id] {
							hit = true
						}
						if !seen[id] {
							seen[id] = true
							total++
						}
					}
				}
				if hit {
					parts = append(parts, p.info.Index)
				}
			}
			var at []gmath.Vec3
			for _, id := range bad[:min(len(bad), mdlListMax)] {
				at = append(at, mdlRV(a.wpos[id]))
			}
			r.Add(Warning, "MESH_ASYMMETRIC", len(bad),
				map[string]any{"axis": m.Symmetry, "plane": mdlR(a.symPlane(axis)), "score": mdlR(score), "parts": parts, "unmatched": at},
				fmt.Sprintf("The model is not mirror-symmetric across the source plane %s = 0 (symmetry_%s %.3f < %.2f): %d of %d vertices have no mirror image on the surface, in parts %v. Center those parts' \"position\" on the plane, build one side and add {\"shape\": \"mirror\", \"axis\": %q, \"of\": <part>}, or remove \"symmetry\".",
					m.Symmetry, m.Symmetry, score, mdlSymMin, len(bad), total, parts, m.Symmetry))
		}
	}

	if m.TriangleBudget > 0 && a.triangles > m.TriangleBudget {
		big := a.parts[0]
		for _, p := range a.parts {
			if p.count > big.count {
				big = p
			}
		}
		r.Add(Warning, "MESH_TRIANGLE_BUDGET", a.triangles,
			map[string]any{"triangles": a.triangles, "budget": m.TriangleBudget, "largest_part": big.info.Index, "largest_part_triangles": big.count},
			fmt.Sprintf("%d triangles exceed \"triangle_budget\" %d; %s alone has %d. Lower its \"segments\"/\"rings\" (or profile points)%s, or raise \"triangle_budget\".",
				a.triangles, m.TriangleBudget, big.name(), big.count, big.on()))
	}
}

func mdlPivotName(p string) string {
	if p == "" {
		return "origin"
	}
	return p
}

func mdlFmtVec(v gmath.Vec3) string {
	return fmt.Sprintf("[%g, %g, %g]", v.X, v.Y, v.Z)
}

// mdlPartStat is one entry of the "part_stats" metric.
type mdlPartStat struct {
	Part      int     `json:"part"`
	Shape     string  `json:"shape"`
	Material  string  `json:"material"`
	Triangles int     `json:"triangles"`
	Closed    bool    `json:"closed"`
	Area      float64 `json:"area"`
	Volume    float64 `json:"volume"`
}

func (a *mdlAnalysis) metrics(mt map[string]any) {
	m := a.m
	mt["triangles"] = a.triangles
	mt["vertices"] = len(m.Mesh.Vertices)
	mt["parts"] = len(a.parts)
	mt["materials"] = len(m.Materials)
	mt["aabb"] = [2]gmath.Vec3{mdlRV(a.bmin), mdlRV(a.bmax)}
	mt["size"] = mdlRV(a.bmax.sub(a.bmin))
	mt["pivot"] = mdlPivotName(m.Pivot)
	mt["pivot_offset"] = mdlRV(mdlV(m.PivotOffset))
	mt["surface_area"] = mdlR(a.area)
	vol := 0.0
	watertight := len(a.parts) > 0
	stats := []mdlPartStat{}
	for _, p := range a.parts {
		vol += math.Abs(p.volume)
		watertight = watertight && p.closed
		stats = append(stats, mdlPartStat{Part: p.info.Index, Shape: p.info.Shape, Material: p.info.Material,
			Triangles: p.count, Closed: p.closed, Area: mdlR(p.area), Volume: mdlR(p.volume)})
	}
	mt["volume"] = mdlR(vol)
	mt["watertight"] = watertight
	mt["part_stats"] = stats
	mt["texel_density_cv"] = mdlR(a.cv)
	sx, _ := a.symmetry(0)
	mt["symmetry_x"] = mdlR(sx)
	if axis, ok := mdlAxisName(m.Symmetry); ok && axis != 0 {
		s, _ := a.symmetry(axis)
		mt["symmetry_"+m.Symmetry] = mdlR(s)
	}
	mt["triangle_budget"] = m.TriangleBudget
}

// Sheets.

// mdlTile is one view of a sheet.
type mdlTile struct {
	label   string
	cam     scene.Camera
	mode    gfx.RenderMode
	overlay bool // draw hole and non-manifold edges on top
}

// mdlLayout returns the tile size and column count of sheet kind: by default the tiles
// fill a 640×360 sheet (sections: one row of square tiles); opt.Width/Height override
// the tile size, each clamped to [16, 1024]. A width given alone is clamped first and the
// height follows it at 3/4, rounded.
func mdlLayout(kind string, opt Options) (w, h, cols int) {
	rows := 2
	switch kind {
	case "summary":
		cols = 3
	case "turntable":
		cols = 5
	case "sections":
		cols, rows = 3, 1
	default:
		cols = 2
	}
	w = (mdlSheetMaxW - (cols+1)*mdlPad) / cols
	h = (360 - (rows+1)*mdlPad) / rows
	if rows == 1 {
		h = w
	}
	if opt.Width > 0 {
		w, h = min(max(opt.Width, 16), 1024), opt.Height
		if h <= 0 {
			h = (w*3 + 2) / 4
		}
	} else if opt.Height > 0 {
		h = opt.Height
	}
	return min(max(w, 16), 1024), min(max(h, 16), 1024), cols
}

// mdlTiles returns the views of sheet kind framing box b. Azimuths are measured from the
// model's front (its −Z side) towards its right (+X).
func mdlTiles(kind string, b gmath.AABB, aspect float32) []mdlTile {
	if b.IsEmpty() {
		b = gmath.AABB{Min: gmath.V3(-0.5, -0.5, -0.5), Max: gmath.V3(0.5, 0.5, 0.5)}
	}
	orbit := func(az, pitch float32) scene.Camera {
		return scene.FramePerspective(b, scene.OrbitDir(180-az, pitch), 30, aspect)
	}
	ortho := func(x, y, z float32) scene.Camera { return scene.FrameOrtho(b, gmath.V3(x, y, z), aspect) }
	front := func() scene.Camera { return ortho(0, 0, 1) }
	right := func() scene.Camera { return ortho(-1, 0, 0) }
	top := func() scene.Camera { return ortho(0, -1, 0) }
	iso := func() scene.Camera { return orbit(35, 25) }
	isoBack := func() scene.Camera { return orbit(215, 25) }
	four := func(mode gfx.RenderMode, overlay bool, l1, l2, l3, l4 string, c1, c2, c3, c4 scene.Camera) []mdlTile {
		return []mdlTile{{l1, c1, mode, overlay}, {l2, c2, mode, overlay}, {l3, c3, mode, overlay}, {l4, c4, mode, overlay}}
	}
	switch kind {
	case "summary":
		return []mdlTile{
			{"iso", iso(), gfx.ModeColor, true},
			{"front", front(), gfx.ModeColor, true},
			{"right", right(), gfx.ModeColor, true},
			{"normals", iso(), gfx.ModeNormals, false},
			{"wireframe", iso(), gfx.ModeWireframe, true},
			{"uv checker", iso(), gfx.ModeUVChecker, false},
		}
	case "turntable":
		names := map[int]string{0: "0 front", 90: "90 right", 180: "180 back", 270: "270 left"}
		var ts []mdlTile
		for az := 0; az < 360; az += 45 {
			l := names[az]
			if l == "" {
				l = fmt.Sprint(az)
			}
			ts = append(ts, mdlTile{l, orbit(float32(az), 20), gfx.ModeColor, true})
		}
		return append(ts, mdlTile{"top", top(), gfx.ModeColor, true}, mdlTile{"bottom", ortho(0, 1, 0), gfx.ModeColor, true})
	case "silhouette":
		return four(gfx.ModeSilhouette, false, "front", "right", "top", "iso", front(), right(), top(), iso())
	case "normals":
		return four(gfx.ModeNormals, false, "0 front", "90 right", "180 back", "270 left",
			orbit(0, 20), orbit(90, 20), orbit(180, 20), orbit(270, 20))
	case "wireframe":
		return four(gfx.ModeWireframe, true, "iso", "iso back", "front", "top", iso(), isoBack(), front(), top())
	case "uv_checker":
		return four(gfx.ModeUVChecker, false, "iso", "iso back", "front", "top", iso(), isoBack(), front(), top())
	case "sections":
		c := b.Center()
		cut := func(x, y, z float32) scene.Camera {
			cam := ortho(x, y, z)
			cam.Near = cam.Position.Sub(cam.Target).Len()
			return cam
		}
		return []mdlTile{
			{fmt.Sprintf("cut x=%.2f", c.X), cut(-1, 0, 0), gfx.ModeNormals, false},
			{fmt.Sprintf("cut y=%.2f", c.Y), cut(0, -1, 0), gfx.ModeNormals, false},
			{fmt.Sprintf("cut z=%.2f", c.Z), cut(0, 0, 1), gfx.ModeNormals, false},
		}
	}
	return nil
}

// overlay returns the debug-line callback drawing hole edges (red) and non-manifold
// edges (yellow), or nil when there are none.
func (a *mdlAnalysis) overlay() func(dl *gfx.DrawList, view int) {
	type line struct {
		a, b  gmath.Vec3
		color uint32
	}
	var lines []line
	for _, p := range a.parts {
		for _, e := range p.boundary {
			lines = append(lines, line{mdlVec32(a.wpos[e[0]]), mdlVec32(a.wpos[e[1]]), mdlHoleColor})
		}
	}
	for _, p := range a.parts {
		for _, e := range p.nonman {
			lines = append(lines, line{mdlVec32(a.wpos[e[0]]), mdlVec32(a.wpos[e[1]]), mdlNonManColor})
		}
	}
	if len(lines) == 0 {
		return nil
	}
	lines = lines[:min(len(lines), mdlLineMax)]
	return func(dl *gfx.DrawList, view int) {
		for _, l := range lines {
			dl.AddLine(gfx.DebugLine{A: l.a, B: l.b, Color: l.color, View: view})
		}
	}
}

func mdlVec32(v mdlVec) gmath.Vec3 { return gmath.V3(float32(v[0]), float32(v[1]), float32(v[2])) }

// mdlSheet renders sheet kind of model name.
func mdlSheet(ir *Renderer, a *mdlAnalysis, name, kind string, opt Options) (*gfx.Image, error) {
	if ir.r == nil || ir.res == nil {
		return nil, errors.New("renderer has no backend (use NewRenderer)")
	}
	if ir.res.Models[name] == nil {
		return nil, fmt.Errorf("model %q was not uploaded (add it to the library before NewRenderer)", name)
	}
	w, h, cols := mdlLayout(kind, opt)
	over := a.overlay()
	var imgs []*gfx.Image
	var labels []string
	for _, t := range mdlTiles(kind, a.bounds, float32(w)/float32(h)) {
		v := ModelView{Model: name, Camera: t.cam, Mode: t.mode, W: w, H: h}
		if t.overlay {
			v.Extra = over
		}
		fb, err := ir.RenderModel(v)
		if err != nil {
			return nil, err
		}
		imgs = append(imgs, fb.Image())
		labels = append(labels, t.label)
	}
	return sheet.Grid(imgs, labels, cols, mdlPad), nil
}

func mdlWritePNG(path string, img *gfx.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := img.EncodePNG(&buf); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
