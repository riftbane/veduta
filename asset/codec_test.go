package asset

import (
	"bytes"
	"math"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"

	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/gmath"
)

// gen produces random compiled assets from a fixed seed. With special set, floats are
// arbitrary bit patterns (NaN payloads, infinities, -0); otherwise they are ordinary
// numbers so values compare with reflect.DeepEqual.
type gen struct {
	r       *rand.Rand
	special bool
}

func newGen(seed uint64, special bool) *gen {
	return &gen{r: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)), special: special}
}

func (g *gen) n(max int) int { return g.r.IntN(max + 1) }
func (g *gen) b() bool       { return g.r.IntN(2) == 1 }
func (g *gen) i() int        { return int(g.r.Int64()) - int(g.r.Int64()) }

func (g *gen) f() float32 {
	if g.special {
		switch g.n(8) {
		case 0:
			return math.Float32frombits(0x7fc00000 | g.r.Uint32()&0x3fffff) // NaN with payload
		case 1:
			return float32(math.Copysign(0, -1))
		case 2:
			return float32(math.Inf(-1))
		}
		return math.Float32frombits(g.r.Uint32())
	}
	return float32(g.r.NormFloat64() * 100)
}

func (g *gen) v2() gmath.Vec2 { return gmath.V2(g.f(), g.f()) }
func (g *gen) v3() gmath.Vec3 { return gmath.V3(g.f(), g.f(), g.f()) }

// s returns a random string of arbitrary bytes (not necessarily UTF-8).
func (g *gen) s() string {
	b := make([]byte, g.n(12))
	for i := range b {
		b[i] = byte(g.r.Uint32())
	}
	return string(b)
}

func (g *gen) strs() []string {
	n := g.n(4)
	if n == 0 {
		return nil
	}
	out := make([]string, n)
	for i := range out {
		out[i] = g.s()
	}
	return out
}

// tri returns a random whole-triangle range within n indices.
func (g *gen) tri(n int) (first, count int) {
	t := n / 3
	a := g.n(t)
	c := g.n(t - a)
	return 3 * a, 3 * c
}

func (g *gen) model() *Model {
	m := &Model{Name: g.s(), Pivot: g.s(), Symmetry: g.s(), SmoothAngleDeg: g.f(), TriangleBudget: g.i(), PivotOffset: g.v3()}
	m.Mesh.Bounds = gmath.AABB{Min: g.v3(), Max: g.v3()}
	if nv := g.n(40); nv > 0 {
		m.Mesh.Vertices = make([]gfx.Vertex, nv)
		for i := range m.Mesh.Vertices {
			m.Mesh.Vertices[i] = gfx.Vertex{Pos: g.v3(), Normal: g.v3(), UV: g.v2()}
		}
		if ni := 3 * g.n(30); ni > 0 {
			m.Mesh.Indices = make([]uint32, ni)
			for i := range m.Mesh.Indices {
				m.Mesh.Indices[i] = uint32(g.r.IntN(nv))
			}
		}
	}
	m.Materials = g.strs()
	ni := len(m.Mesh.Indices)
	if np := g.n(5); np > 0 {
		m.Mesh.Parts = make([]gfx.MeshPart, np)
		m.Parts = make([]PartInfo, np)
		for i := range m.Parts {
			f, c := g.tri(ni)
			mat := g.r.IntN(max(1, len(m.Materials)))
			m.Mesh.Parts[i] = gfx.MeshPart{First: f, Count: c, Material: mat}
			m.Parts[i] = PartInfo{Index: g.i(), Shape: g.s(), First: f, Count: c, Material: g.s(), UV: g.s(),
				FlipNormals: g.b(), Of: g.r.IntN(10) - 1}
		}
	}
	m.DrawDistance = float32(g.n(100))
	var dist float32
	for range g.n(3) {
		dist += 1 + float32(g.n(50))
		l := LOD{Distance: dist}
		if g.b() {
			l.Model = "m" + g.s()
		} else {
			l.Mesh.Bounds = gmath.AABB{Min: g.v3(), Max: g.v3()}
			l.Mesh.Vertices = []gfx.Vertex{{Pos: g.v3(), Normal: g.v3(), UV: g.v2()}}
			l.Mesh.Indices = []uint32{0, 0, 0}
			for range m.Mesh.Parts {
				l.Mesh.Parts = append(l.Mesh.Parts, gfx.MeshPart{First: 0, Count: 3 * g.n(1), Material: g.r.IntN(max(1, len(m.Materials)))})
			}
		}
		m.LODs = append(m.LODs, l)
	}
	return m
}

func (g *gen) texture() *Texture {
	t := &Texture{Name: g.s(), Tiling: g.b(), Layers: g.i(), Data: gfx.TextureData{Wrap: gfx.Wrap(g.n(1))}}
	if g.n(10) == 0 {
		return t // no levels
	}
	w, h := 1+g.n(40), 1+g.n(40)
	levels := 1 + g.n(mipCount(w, h)-1)
	for i := 0; i < levels; i++ {
		img := gfx.NewImage(max(1, w>>i), max(1, h>>i))
		for k := range img.Pix {
			img.Pix[k] = g.r.Uint32()
		}
		t.Data.Levels = append(t.Data.Levels, img)
	}
	return t
}

func (g *gen) material() *Material {
	m := &Material{Name: g.s(), Albedo: g.r.Uint32(), Texture: g.s(), Unlit: g.b(),
		Alpha: materialAlphas[g.n(2)], Cutoff: g.f(), Cull: gfx.CullMode(g.n(1)), Filter: gfx.Filter(g.n(1))}
	if g.b() {
		m.Grid = [2]int{g.n(MaxGrid-1) + 1, g.n(MaxGrid-1) + 1}
	}
	return m
}

func (g *gen) scene() *Scene {
	s := &Scene{Name: g.s(), Background: g.r.Uint32(),
		Camera: Camera{Ortho: g.b(), FovDeg: g.f(), Size: g.f(), Near: g.f(), Far: g.f(), Position: g.v3(), LookAt: g.v3()},
		Light:  gfx.Light{Dir: g.v3(), Color: g.v3(), Ambient: g.v3()}}
	if n := g.n(6); n > 0 {
		s.Entities = make([]Entity, n)
		for i := range s.Entities {
			s.Entities[i] = Entity{Name: g.s(), Kind: g.s(), Model: g.s(), Material: g.s(), Position: g.v3(),
				RotationDeg: g.v3(), Scale: g.v3(), Tags: g.strs(), Parent: g.s(), Visible: g.b()}
			// Decoders reject hitboxes with min above max, and NaN is never ordered.
			if !g.special && g.b() {
				lo := g.v3()
				s.Entities[i].Hitbox = &gmath.AABB{Min: lo, Max: lo.Add(gmath.V3(1, 0, 2))}
			}
			s.Entities[i].Layer = g.n(MaxLayer-MinLayer) + MinLayer
			s.Entities[i].Frame = g.n(MaxFrame)
		}
	}
	return s
}

// codecCase adapts one codec to the generic round-trip checks.
type codecCase struct {
	name   string
	make   func(*gen) any
	encode func(any) Chunk
	decode func(Chunk) (any, error)
}

var codecCases = []codecCase{
	{"model", func(g *gen) any { return g.model() },
		func(v any) Chunk { return EncodeModel(v.(*Model)) },
		func(c Chunk) (any, error) { return DecodeModel(c) }},
	{"texture", func(g *gen) any { return g.texture() },
		func(v any) Chunk { return EncodeTexture(v.(*Texture)) },
		func(c Chunk) (any, error) { return DecodeTexture(c) }},
	{"material", func(g *gen) any { return g.material() },
		func(v any) Chunk { return EncodeMaterial(v.(*Material)) },
		func(c Chunk) (any, error) { return DecodeMaterial(c) }},
	{"scene", func(g *gen) any { return g.scene() },
		func(v any) Chunk { return EncodeScene(v.(*Scene)) },
		func(c Chunk) (any, error) { return DecodeScene(c) }},
}

func TestCodecRoundTrip(t *testing.T) {
	for _, cc := range codecCases {
		t.Run(cc.name, func(t *testing.T) {
			for seed := uint64(0); seed < 300; seed++ {
				special := seed%3 == 2
				v := cc.make(newGen(seed, special))
				c := cc.encode(v)
				if again := cc.encode(v); !bytes.Equal(c.Data, again.Data) || c.Type != again.Type {
					t.Fatalf("seed %d: encoding is not deterministic", seed)
				}
				back, err := cc.decode(c)
				if err != nil {
					t.Fatalf("seed %d: %v", seed, err)
				}
				// Re-encoding reproduces every byte, so float bits survive exactly.
				if re := cc.encode(back); !bytes.Equal(re.Data, c.Data) {
					t.Fatalf("seed %d: re-encoding differs", seed)
				}
				// Every field survives (NaN-free values compare with DeepEqual).
				if !special && !reflect.DeepEqual(back, v) {
					t.Fatalf("seed %d: round trip differs\n got  %+v\n want %+v", seed, back, v)
				}
			}
		})
	}
}

func TestCodecTruncationAndTrailing(t *testing.T) {
	for _, cc := range codecCases {
		t.Run(cc.name, func(t *testing.T) {
			// Use the largest payload among a few seeds (materials are always small, so a
			// fixed size target would never be reached).
			var c Chunk
			for seed := uint64(0); seed < 64; seed++ {
				if e := cc.encode(cc.make(newGen(seed, false))); len(e.Data) > len(c.Data) {
					c = e
				}
			}
			for n := 0; n < len(c.Data); n++ {
				if _, err := cc.decode(Chunk{Type: c.Type, Data: c.Data[:n]}); err == nil {
					t.Fatalf("payload truncated to %d of %d bytes accepted", n, len(c.Data))
				}
			}
			long := append(append([]byte(nil), c.Data...), 0)
			if _, err := cc.decode(Chunk{Type: c.Type, Data: long}); err == nil || !strings.Contains(err.Error(), "trailing") {
				t.Fatalf("trailing byte: %v", err)
			}
			if _, err := cc.decode(Chunk{Type: "XXXX", Data: c.Data}); err == nil {
				t.Fatal("wrong chunk type accepted")
			}
		})
	}
}

func TestDecodeModelValidation(t *testing.T) {
	base := func() *Model {
		return &Model{
			Mesh: gfx.MeshData{
				Vertices: make([]gfx.Vertex, 3),
				Indices:  []uint32{0, 1, 2},
				Parts:    []gfx.MeshPart{{First: 0, Count: 3, Material: 0}},
			},
			Materials: []string{"a"},
			Parts:     []PartInfo{{First: 0, Count: 3, Of: -1}},
		}
	}
	if _, err := DecodeModel(EncodeModel(base())); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Model){
		"index out of range":    func(m *Model) { m.Mesh.Indices[2] = 3 },
		"indices not triangles": func(m *Model) { m.Mesh.Indices = []uint32{0, 1} },
		"part past the end":     func(m *Model) { m.Mesh.Parts[0].First = 3 },
		"part partial triangle": func(m *Model) { m.Mesh.Parts[0].Count = 2 },
		"part negative":         func(m *Model) { m.Mesh.Parts[0].First = -3 },
		"part material":         func(m *Model) { m.Mesh.Parts[0].Material = 1 },
		"info range":            func(m *Model) { m.Parts[0].Count = 6 },
		"mirror of":             func(m *Model) { m.Parts[0].Of = -2 },
		"draw distance":         func(m *Model) { m.DrawDistance = -1 },
		"lod distance order": func(m *Model) {
			m.LODs = []LOD{{Distance: 10, Model: "a"}, {Distance: 10, Model: "b"}}
		},
		"lod parts": func(m *Model) { m.LODs = []LOD{{Distance: 10}} },
		"lod model with geometry": func(m *Model) {
			m.LODs = []LOD{{Distance: 10, Model: "far", Mesh: m.Mesh}}
		},
		"lod index": func(m *Model) {
			m.LODs = []LOD{{Distance: 10, Mesh: gfx.MeshData{Vertices: make([]gfx.Vertex, 1), Indices: []uint32{0, 1, 0},
				Parts: []gfx.MeshPart{{First: 0, Count: 3}}}}}
		},
	}
	for name, f := range cases {
		m := base()
		f(m)
		if _, err := DecodeModel(EncodeModel(m)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	m := base()
	m.Materials = nil // part material 0 with no materials means "entity/default"
	if _, err := DecodeModel(EncodeModel(m)); err != nil {
		t.Errorf("empty materials: %v", err)
	}
	c := EncodeModel(base())
	c.Data[len(c.Data)-17] = 2 // flip_normals byte of the last part, before draw_distance and lods
	if _, err := DecodeModel(c); err == nil || !strings.Contains(err.Error(), "boolean byte is 2") {
		t.Errorf("bad bool: %v", err)
	}
}

func TestDecodeTextureValidation(t *testing.T) {
	img := func(w, h int) *gfx.Image { return gfx.NewImage(w, h) }
	cases := map[string]*Texture{
		"level size":     {Data: gfx.TextureData{Levels: []*gfx.Image{img(8, 4), img(4, 4)}}},
		"too many":       {Data: gfx.TextureData{Levels: []*gfx.Image{img(2, 1), img(1, 1), img(1, 1)}}},
		"zero size":      {Data: gfx.TextureData{Levels: []*gfx.Image{img(0, 4)}}},
		"nil level":      {Data: gfx.TextureData{Levels: []*gfx.Image{img(2, 2), nil}}},
		"bad wrap":       {Data: gfx.TextureData{Levels: []*gfx.Image{img(1, 1)}, Wrap: 7}},
		"short pix pads": {Data: gfx.TextureData{Levels: []*gfx.Image{{W: 2, H: 2, Pix: []uint32{1}}}}},
	}
	for name, tex := range cases {
		_, err := DecodeTexture(EncodeTexture(tex))
		if name == "short pix pads" {
			if err != nil {
				t.Errorf("%s: %v", name, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// A full BuildMips chain round trips.
	base := gfx.NewImage(13, 5)
	for i := range base.Pix {
		base.Pix[i] = uint32(i) * 0x01020304
	}
	tex := &Texture{Name: "odd", Data: gfx.TextureData{Levels: gfx.BuildMips(base), Wrap: gfx.WrapClamp}, Layers: 2}
	back, err := DecodeTexture(EncodeTexture(tex))
	if err != nil || !reflect.DeepEqual(back, tex) {
		t.Fatalf("mip chain round trip: %v", err)
	}
	// Pixels are stored as B, G, R, A bytes.
	one := &Texture{Data: gfx.TextureData{Levels: []*gfx.Image{{W: 1, H: 1, Pix: []uint32{gfx.RGBA(0x11, 0x22, 0x33, 0x44)}}}}}
	c := EncodeTexture(one)
	if !bytes.Contains(c.Data, []byte{1, 0, 0, 0, 0x33, 0x22, 0x11, 0x44, 0, 0, 0, 0}) {
		t.Fatalf("pixel bytes % x", c.Data)
	}
	// Frames, clips and an edge round trip.
	sheet := &Texture{Name: "hero", Data: gfx.TextureData{Levels: []*gfx.Image{img(8, 4)}, Wrap: gfx.WrapClamp}, Layers: 1,
		Grid: [2]int{4, 2}, Play: "walk",
		Clips: []Clip{{Name: "hit", Frames: []int{5, 6}, FPS: 12, Next: "walk"}, {Name: "walk", Frames: []int{0, 1, 2, 3}, FPS: 8, Loop: true}},
		Edge:  &Edge{Priority: 20, Width: 1.5, Roughness: 0.25, Seed: -7}}
	back, err = DecodeTexture(EncodeTexture(sheet))
	if err != nil || !reflect.DeepEqual(back, sheet) {
		t.Fatalf("sheet round trip: %v\n%+v\n%+v", err, back, sheet)
	}
	bad := *sheet
	bad.Clips = []Clip{{Name: "walk", Frames: []int{8}, FPS: 8}}
	if _, err := DecodeTexture(EncodeTexture(&bad)); err == nil {
		t.Errorf("frame outside the grid accepted")
	}
}

func TestDecodeMaterialValidation(t *testing.T) {
	for name, m := range map[string]*Material{
		"alpha":  {Alpha: "glass"},
		"cull":   {Alpha: "opaque", Cull: 2},
		"filter": {Alpha: "opaque", Filter: 9},
		"grid":   {Alpha: "opaque", Grid: [2]int{3, 0}},
		"big":    {Alpha: "opaque", Grid: [2]int{MaxGrid + 1, 1}},
	} {
		if _, err := DecodeMaterial(EncodeMaterial(m)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// Scene entities round-trip their hitbox and layer; a box with min above max and a layer
// out of range are rejected.
func TestSceneCodecHitboxAndLayer(t *testing.T) {
	s := &Scene{Name: "twod", Camera: Camera{Ortho: true, Size: 12, Near: 0.1, Far: 200, Position: gmath.V3(0, 0, 100)},
		Entities: []Entity{
			{Name: "coin", Kind: "static", Model: "quad", Scale: gmath.One3, Visible: true, Layer: -7,
				Hitbox: &gmath.AABB{Min: gmath.V3(-0.25, -0.25, -0.5), Max: gmath.V3(0.25, 0.25, 0.5)}},
			{Name: "plain", Kind: "static", Scale: gmath.One3, Visible: true},
		}}
	c := EncodeScene(s)
	got, err := DecodeScene(c)
	if err != nil || !reflect.DeepEqual(got, s) {
		t.Fatalf("decoded %+v, %v", got, err)
	}
	if again := EncodeScene(got); !bytes.Equal(again.Data, c.Data) {
		t.Fatal("re-encoding changed the bytes")
	}
	s.Entities[1].Layer = MaxLayer + 1
	if _, err := DecodeScene(EncodeScene(s)); err == nil || !strings.Contains(err.Error(), "layer") {
		t.Fatalf("layer out of range: %v", err)
	}
	s.Entities[1].Layer = 0
	s.Entities[0].Hitbox.Min.Y = 1
	if _, err := DecodeScene(EncodeScene(s)); err == nil || !strings.Contains(err.Error(), "hitbox") {
		t.Fatalf("inverted hitbox: %v", err)
	}
}

// Sources compile, encode, pack, unpack and decode back to the same values.
func TestCompiledAssetsThroughVDA(t *testing.T) {
	mat, err := ParseMaterial("crate_wood.vmat", readTestdata(t, "materials/crate_wood.vmat"))
	if err != nil {
		t.Fatal(err)
	}
	scene, err := ParseScene("main.vscene", readTestdata(t, "scenes/main.vscene"))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range []struct {
		kind   Kind
		name   string
		body   Chunk
		decode func(Chunk) (any, error)
		want   any
	}{
		{KindMaterial, mat.Name, EncodeMaterial(mat), func(c Chunk) (any, error) { return DecodeMaterial(c) }, mat},
		{KindScene, scene.Name, EncodeScene(scene), func(c Chunk) (any, error) { return DecodeScene(c) }, scene},
	} {
		meta := Meta{Kind: a.kind, Name: a.name, Source: string(a.kind) + "s/" + a.name + a.kind.Ext(), SourceHash: testHash, Compiler: CompilerVersion}
		file, err := PackVDA(meta, a.body)
		if err != nil {
			t.Fatal(err)
		}
		gm, body, err := UnpackVDA(file)
		if err != nil || !reflect.DeepEqual(gm, meta) {
			t.Fatalf("%s: unpack %+v %v", a.kind, gm, err)
		}
		got, err := a.decode(body)
		if err != nil || !reflect.DeepEqual(got, a.want) {
			t.Fatalf("%s: decoded %+v, %v", a.kind, got, err)
		}
	}
}
