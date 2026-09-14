package asset

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gmath"
)

// Binary codecs for the body chunks of .vda files. Layouts are specified field by field
// in docs/vda.md; every compiled field round-trips exactly (floats as raw bits). Empty
// lists decode as nil.

// MaxTextureSize bounds the width and height accepted by DecodeTexture.
const MaxTextureSize = 1 << 24

// wbuf appends little-endian primitives.
type wbuf struct{ b []byte }

func (w *wbuf) u8(v uint8)   { w.b = append(w.b, v) }
func (w *wbuf) u32(v uint32) { w.b = binary.LittleEndian.AppendUint32(w.b, v) }
func (w *wbuf) i64(v int)    { w.b = binary.LittleEndian.AppendUint64(w.b, uint64(int64(v))) }
func (w *wbuf) f32(v float32) {
	w.u32(math.Float32bits(v))
}
func (w *wbuf) bool(v bool) {
	if v {
		w.u8(1)
	} else {
		w.u8(0)
	}
}
func (w *wbuf) count(n int) { w.u32(uint32(n)) }
func (w *wbuf) str(s string) {
	w.count(len(s))
	w.b = append(w.b, s...)
}
func (w *wbuf) vec2(v gmath.Vec2) { w.f32(v.X); w.f32(v.Y) }
func (w *wbuf) vec3(v gmath.Vec3) { w.f32(v.X); w.f32(v.Y); w.f32(v.Z) }
func (w *wbuf) strs(list []string) {
	w.count(len(list))
	for _, s := range list {
		w.str(s)
	}
}

// rbuf reads little-endian primitives with a sticky error.
type rbuf struct {
	chunk string // chunk type, for messages
	field string // field being read, for messages
	b     []byte
	off   int
	err   error
}

func newReader(c Chunk, want string) (*rbuf, error) {
	if c.Type != want {
		return nil, fmt.Errorf("decode %s: chunk type is %q", want, c.Type)
	}
	return &rbuf{chunk: want, b: c.Data}, nil
}

func (r *rbuf) failf(format string, args ...any) {
	if r.err == nil {
		r.err = fmt.Errorf("decode %s: %s at offset %d: %s", r.chunk, r.field, r.off, fmt.Sprintf(format, args...))
	}
}

func (r *rbuf) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || len(r.b)-r.off < n {
		r.failf("truncated: need %d bytes, have %d", n, len(r.b)-r.off)
		return nil
	}
	p := r.b[r.off : r.off+n]
	r.off += n
	return p
}

func (r *rbuf) u8() uint8 {
	if p := r.take(1); p != nil {
		return p[0]
	}
	return 0
}

func (r *rbuf) u32() uint32 {
	if p := r.take(4); p != nil {
		return binary.LittleEndian.Uint32(p)
	}
	return 0
}

func (r *rbuf) i64() int {
	p := r.take(8)
	if p == nil {
		return 0
	}
	v := int64(binary.LittleEndian.Uint64(p))
	if int64(int(v)) != v {
		r.failf("value %d does not fit in an int on this platform", v)
		return 0
	}
	return int(v)
}

func (r *rbuf) f32() float32 { return math.Float32frombits(r.u32()) }

func (r *rbuf) bool() bool {
	switch v := r.u8(); v {
	case 0:
		return false
	case 1:
		return true
	default:
		r.failf("boolean byte is %d, want 0 or 1", v)
		return false
	}
}

// count reads a list length and checks that count elements of at least minSize bytes
// each fit in the rest of the payload.
func (r *rbuf) count(minSize int) int {
	n := uint64(r.u32())
	if r.err != nil {
		return 0
	}
	if n*uint64(minSize) > uint64(len(r.b)-r.off) {
		r.failf("truncated: %d elements of %d bytes do not fit in the %d bytes left", n, minSize, len(r.b)-r.off)
		return 0
	}
	return int(n)
}

func (r *rbuf) str() string {
	n := r.count(1)
	return string(r.take(n))
}

func (r *rbuf) vec2() gmath.Vec2 { return gmath.Vec2{X: r.f32(), Y: r.f32()} }
func (r *rbuf) vec3() gmath.Vec3 { return gmath.Vec3{X: r.f32(), Y: r.f32(), Z: r.f32()} }

func (r *rbuf) strs() []string {
	n := r.count(4)
	if n == 0 {
		return nil
	}
	out := make([]string, n)
	for i := range out {
		out[i] = r.str()
	}
	return out
}

// done reports the sticky error, or trailing bytes.
func (r *rbuf) done() error {
	if r.err == nil && r.off != len(r.b) {
		r.field = "end"
		r.failf("%d trailing bytes", len(r.b)-r.off)
	}
	return r.err
}

// EncodeModel returns the MESH chunk of m.
func EncodeModel(m *Model) Chunk {
	md := &m.Mesh
	w := wbuf{b: make([]byte, 0, 128+32*len(md.Vertices)+4*len(md.Indices)+64*len(m.Parts))}
	w.str(m.Name)
	w.str(m.Pivot)
	w.str(m.Symmetry)
	w.f32(m.SmoothAngleDeg)
	w.i64(m.TriangleBudget)
	w.vec3(m.PivotOffset)
	w.vec3(md.Bounds.Min)
	w.vec3(md.Bounds.Max)
	w.count(len(md.Vertices))
	for _, v := range md.Vertices {
		w.vec3(v.Pos)
		w.vec3(v.Normal)
		w.vec2(v.UV)
	}
	w.count(len(md.Indices))
	for _, i := range md.Indices {
		w.u32(i)
	}
	w.count(len(md.Parts))
	for _, p := range md.Parts {
		w.i64(p.First)
		w.i64(p.Count)
		w.i64(p.Material)
	}
	w.strs(m.Materials)
	w.count(len(m.Parts))
	for _, p := range m.Parts {
		w.i64(p.Index)
		w.str(p.Shape)
		w.i64(p.First)
		w.i64(p.Count)
		w.str(p.Material)
		w.str(p.UV)
		w.bool(p.FlipNormals)
		w.i64(p.Of)
	}
	return Chunk{Type: ChunkMesh, Data: w.b}
}

// DecodeModel parses a MESH chunk and checks that its indices and ranges are consistent
// (docs/vda.md).
func DecodeModel(c Chunk) (*Model, error) {
	r, err := newReader(c, ChunkMesh)
	if err != nil {
		return nil, err
	}
	m := &Model{}
	r.field = "header"
	m.Name = r.str()
	m.Pivot = r.str()
	m.Symmetry = r.str()
	m.SmoothAngleDeg = r.f32()
	m.TriangleBudget = r.i64()
	m.PivotOffset = r.vec3()
	m.Mesh.Bounds.Min = r.vec3()
	m.Mesh.Bounds.Max = r.vec3()
	r.field = "vertices"
	if n := r.count(32); n > 0 {
		m.Mesh.Vertices = make([]gfx.Vertex, n)
		for i := range m.Mesh.Vertices {
			m.Mesh.Vertices[i] = gfx.Vertex{Pos: r.vec3(), Normal: r.vec3(), UV: r.vec2()}
		}
	}
	r.field = "indices"
	if n := r.count(4); n > 0 {
		if n%3 != 0 {
			r.failf("index count %d is not a multiple of 3", n)
		}
		m.Mesh.Indices = make([]uint32, n)
		for i := range m.Mesh.Indices {
			v := r.u32()
			if r.err == nil && uint64(v) >= uint64(len(m.Mesh.Vertices)) {
				r.failf("index %d is %d, but there are %d vertices", i, v, len(m.Mesh.Vertices))
			}
			m.Mesh.Indices[i] = v
		}
	}
	r.field = "mesh parts"
	if n := r.count(24); n > 0 {
		m.Mesh.Parts = make([]gfx.MeshPart, n)
		for i := range m.Mesh.Parts {
			m.Mesh.Parts[i] = gfx.MeshPart{First: r.i64(), Count: r.i64(), Material: r.i64()}
		}
	}
	r.field = "materials"
	m.Materials = r.strs()
	r.field = "parts"
	if n := r.count(8*4 + 4*3 + 1); n > 0 {
		m.Parts = make([]PartInfo, n)
		for i := range m.Parts {
			p := &m.Parts[i]
			p.Index = r.i64()
			p.Shape = r.str()
			p.First = r.i64()
			p.Count = r.i64()
			p.Material = r.str()
			p.UV = r.str()
			p.FlipNormals = r.bool()
			p.Of = r.i64()
		}
	}
	if r.err == nil {
		r.field = "mesh parts"
		ni := len(m.Mesh.Indices)
		for i, p := range m.Mesh.Parts {
			switch {
			case !validRange(p.First, p.Count, ni):
				r.failf("part %d range [%d, +%d) is outside the %d indices or not whole triangles", i, p.First, p.Count, ni)
			case p.Material < 0 || p.Material >= max(1, len(m.Materials)):
				r.failf("part %d material index %d is outside the %d materials", i, p.Material, len(m.Materials))
			}
		}
		r.field = "parts"
		for i, p := range m.Parts {
			switch {
			case !validRange(p.First, p.Count, ni):
				r.failf("part %d range [%d, +%d) is outside the %d indices or not whole triangles", i, p.First, p.Count, ni)
			case p.Of < -1:
				r.failf("part %d mirrors part %d", i, p.Of)
			}
		}
	}
	if err := r.done(); err != nil {
		return nil, err
	}
	return m, nil
}

// validRange reports whether [first, first+count) lies within n indices and covers
// whole triangles.
func validRange(first, count, n int) bool {
	return first >= 0 && count >= 0 && first <= n && count <= n-first && count%3 == 0
}

// EncodeTexture returns the TEXR chunk of t. Each level is written with its own size;
// a level whose Pix is shorter than W×H is padded with zero pixels (DecodeTexture rejects
// levels that do not follow the mip chain rule).
func EncodeTexture(t *Texture) Chunk {
	size := 64 + len(t.Name)
	for _, l := range t.Data.Levels {
		if l != nil {
			size += 8 + 4*l.W*l.H
		}
	}
	w := wbuf{b: make([]byte, 0, size)}
	w.str(t.Name)
	w.bool(t.Tiling)
	w.i64(t.Layers)
	var bw, bh int
	if len(t.Data.Levels) > 0 && t.Data.Levels[0] != nil {
		bw, bh = t.Data.Levels[0].W, t.Data.Levels[0].H
	}
	w.u32(uint32(bw))
	w.u32(uint32(bh))
	w.count(len(t.Data.Levels))
	w.u8(uint8(t.Data.Wrap))
	for _, l := range t.Data.Levels {
		if l == nil {
			w.u32(0)
			w.u32(0)
			continue
		}
		w.u32(uint32(l.W))
		w.u32(uint32(l.H))
		for i := 0; i < l.W*l.H; i++ {
			var px uint32
			if i < len(l.Pix) {
				px = l.Pix[i]
			}
			w.u32(px)
		}
	}
	return Chunk{Type: ChunkTexture, Data: w.b}
}

// mipCount is the length of the full mip chain of a w×h image.
func mipCount(w, h int) int {
	n := 1
	for m := max(w, h); m > 1; m >>= 1 {
		n++
	}
	return n
}

// DecodeTexture parses a TEXR chunk and checks its mip chain (docs/vda.md).
func DecodeTexture(c Chunk) (*Texture, error) {
	r, err := newReader(c, ChunkTexture)
	if err != nil {
		return nil, err
	}
	t := &Texture{}
	r.field = "header"
	t.Name = r.str()
	t.Tiling = r.bool()
	t.Layers = r.i64()
	w, h, levels := r.u32(), r.u32(), r.u32()
	wrap := r.u8()
	if r.err == nil {
		switch {
		case wrap > uint8(gfx.WrapClamp):
			r.failf("wrap is %d, want 0 (repeat) or 1 (clamp)", wrap)
		case levels == 0 && (w != 0 || h != 0):
			r.failf("%dx%d texture without levels", w, h)
		case levels > 0 && (w == 0 || h == 0 || w > MaxTextureSize || h > MaxTextureSize):
			r.failf("size %dx%d out of range [1, %d]", w, h, MaxTextureSize)
		case levels > 0 && int(levels) > mipCount(int(w), int(h)):
			r.failf("%d levels exceed the %d of a full %dx%d mip chain", levels, mipCount(int(w), int(h)), w, h)
		}
	}
	t.Data.Wrap = gfx.Wrap(wrap)
	if r.err == nil && levels > 0 {
		t.Data.Levels = make([]*gfx.Image, levels)
		for i := range t.Data.Levels {
			r.field = fmt.Sprintf("level %d", i)
			lw, lh := int(r.u32()), int(r.u32())
			ww, wh := max(1, int(w)>>i), max(1, int(h)>>i)
			if r.err == nil && (lw != ww || lh != wh) {
				r.failf("size %dx%d, want %dx%d", lw, lh, ww, wh)
			}
			if r.err != nil {
				break
			}
			p := r.take(4 * lw * lh)
			if p == nil {
				break
			}
			img := &gfx.Image{W: lw, H: lh, Pix: make([]uint32, lw*lh)}
			for k := range img.Pix {
				img.Pix[k] = binary.LittleEndian.Uint32(p[4*k:])
			}
			t.Data.Levels[i] = img
		}
	}
	if err := r.done(); err != nil {
		return nil, err
	}
	return t, nil
}

// EncodeMaterial returns the MATL chunk of m.
func EncodeMaterial(m *Material) Chunk {
	w := wbuf{b: make([]byte, 0, 32+len(m.Name)+len(m.Texture)+len(m.Alpha))}
	w.str(m.Name)
	w.u32(m.Albedo)
	w.str(m.Texture)
	w.bool(m.Unlit)
	w.str(m.Alpha)
	w.f32(m.Cutoff)
	w.u8(uint8(m.Cull))
	w.u8(uint8(m.Filter))
	return Chunk{Type: ChunkMaterial, Data: w.b}
}

// DecodeMaterial parses a MATL chunk.
func DecodeMaterial(c Chunk) (*Material, error) {
	r, err := newReader(c, ChunkMaterial)
	if err != nil {
		return nil, err
	}
	r.field = "material"
	m := &Material{}
	m.Name = r.str()
	m.Albedo = r.u32()
	m.Texture = r.str()
	m.Unlit = r.bool()
	r.field = "alpha"
	if m.Alpha = r.str(); r.err == nil && indexOf(materialAlphas, m.Alpha) < 0 {
		r.failf("unknown alpha mode %q", m.Alpha)
	}
	r.field = "material"
	m.Cutoff = r.f32()
	cull, filter := r.u8(), r.u8()
	if r.err == nil && (cull > uint8(gfx.CullNone) || filter > uint8(gfx.FilterNearest)) {
		r.failf("cull %d or filter %d out of range", cull, filter)
	}
	m.Cull, m.Filter = gfx.CullMode(cull), gfx.Filter(filter)
	if err := r.done(); err != nil {
		return nil, err
	}
	return m, nil
}

// EncodeScene returns the SCEN chunk of s.
func EncodeScene(s *Scene) Chunk {
	w := wbuf{b: make([]byte, 0, 128+96*len(s.Entities))}
	w.str(s.Name)
	c := &s.Camera
	w.bool(c.Ortho)
	w.f32(c.FovDeg)
	w.f32(c.Size)
	w.f32(c.Near)
	w.f32(c.Far)
	w.vec3(c.Position)
	w.vec3(c.LookAt)
	w.vec3(s.Light.Dir)
	w.vec3(s.Light.Color)
	w.vec3(s.Light.Ambient)
	w.u32(s.Background)
	w.count(len(s.Entities))
	for i := range s.Entities {
		e := &s.Entities[i]
		w.str(e.Name)
		w.str(e.Kind)
		w.str(e.Model)
		w.str(e.Material)
		w.vec3(e.Position)
		w.vec3(e.RotationDeg)
		w.vec3(e.Scale)
		w.strs(e.Tags)
		w.str(e.Parent)
		w.bool(e.Visible)
		w.bool(e.Hitbox != nil)
		if e.Hitbox != nil {
			w.vec3(e.Hitbox.Min)
			w.vec3(e.Hitbox.Max)
		}
	}
	return Chunk{Type: ChunkScene, Data: w.b}
}

// DecodeScene parses a SCEN chunk.
func DecodeScene(c Chunk) (*Scene, error) {
	r, err := newReader(c, ChunkScene)
	if err != nil {
		return nil, err
	}
	s := &Scene{}
	r.field = "header"
	s.Name = r.str()
	r.field = "camera"
	cam := &s.Camera
	cam.Ortho = r.bool()
	cam.FovDeg = r.f32()
	cam.Size = r.f32()
	cam.Near = r.f32()
	cam.Far = r.f32()
	cam.Position = r.vec3()
	cam.LookAt = r.vec3()
	r.field = "light"
	s.Light.Dir = r.vec3()
	s.Light.Color = r.vec3()
	s.Light.Ambient = r.vec3()
	s.Background = r.u32()
	r.field = "entities"
	if n := r.count(4*4 + 36 + 4 + 4 + 1 + 1); n > 0 {
		s.Entities = make([]Entity, n)
		for i := range s.Entities {
			r.field = fmt.Sprintf("entity %d", i)
			e := &s.Entities[i]
			e.Name = r.str()
			e.Kind = r.str()
			e.Model = r.str()
			e.Material = r.str()
			e.Position = r.vec3()
			e.RotationDeg = r.vec3()
			e.Scale = r.vec3()
			e.Tags = r.strs()
			e.Parent = r.str()
			e.Visible = r.bool()
			if r.bool() {
				b := gmath.AABB{Min: r.vec3(), Max: r.vec3()}
				// Negated so a NaN component fails too.
				if r.err == nil && !(b.Min.X <= b.Max.X && b.Min.Y <= b.Max.Y && b.Min.Z <= b.Max.Z) {
					r.failf("hitbox min %v exceeds max %v", b.Min, b.Max)
				}
				e.Hitbox = &b
			}
		}
	}
	if err := r.done(); err != nil {
		return nil, err
	}
	return s, nil
}
