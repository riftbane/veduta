package soft

import (
	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gmath"
)

// cubeMesh returns a unit cube centered at the origin: 24 vertices, 12 CCW triangles,
// each face mapped to the full [0,1]² UV square with v growing downwards on the face.
func cubeMesh() *gfx.MeshData {
	type face struct{ n, u, v gmath.Vec3 }
	faces := []face{
		{gmath.V3(1, 0, 0), gmath.V3(0, 0, -1), gmath.V3(0, 1, 0)},
		{gmath.V3(-1, 0, 0), gmath.V3(0, 0, 1), gmath.V3(0, 1, 0)},
		{gmath.V3(0, 1, 0), gmath.V3(1, 0, 0), gmath.V3(0, 0, -1)},
		{gmath.V3(0, -1, 0), gmath.V3(1, 0, 0), gmath.V3(0, 0, 1)},
		{gmath.V3(0, 0, 1), gmath.V3(1, 0, 0), gmath.V3(0, 1, 0)},
		{gmath.V3(0, 0, -1), gmath.V3(-1, 0, 0), gmath.V3(0, 1, 0)},
	}
	m := &gfx.MeshData{Bounds: gmath.AABB{Min: gmath.V3(-0.5, -0.5, -0.5), Max: gmath.V3(0.5, 0.5, 0.5)}}
	for _, f := range faces {
		c := f.n.Scale(0.5)
		u, v := f.u.Scale(0.5), f.v.Scale(0.5)
		base := uint32(len(m.Vertices))
		corners := []struct {
			p  gmath.Vec3
			uv gmath.Vec2
		}{
			{c.Sub(u).Sub(v), gmath.V2(0, 1)},
			{c.Add(u).Sub(v), gmath.V2(1, 1)},
			{c.Add(u).Add(v), gmath.V2(1, 0)},
			{c.Sub(u).Add(v), gmath.V2(0, 0)},
		}
		for _, k := range corners {
			m.Vertices = append(m.Vertices, gfx.Vertex{Pos: k.p, Normal: f.n, UV: k.uv})
		}
		m.Indices = append(m.Indices, base, base+1, base+2, base, base+2, base+3)
	}
	m.Parts = []gfx.MeshPart{{First: 0, Count: len(m.Indices)}}
	return m
}

// crateTexture is a 64×64 procedural texture: warm planks with a dark frame and a red
// marker in the top-left corner so orientation is visible.
func crateTexture() *gfx.TextureData {
	img := gfx.NewImage(64, 64)
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			c := gfx.RGBA(0xb0, 0x7a, 0x40, 0xff)
			if (y/8)%2 == 1 {
				c = gfx.RGBA(0x9a, 0x68, 0x34, 0xff)
			}
			if x%16 == 0 {
				c = gfx.RGBA(0x70, 0x48, 0x22, 0xff)
			}
			if x < 4 || y < 4 || x >= 60 || y >= 60 {
				c = gfx.RGBA(0x4a, 0x30, 0x18, 0xff)
			}
			if x >= 8 && x < 20 && y >= 8 && y < 20 {
				c = gfx.RGBA(0xd0, 0x30, 0x30, 0xff)
			}
			img.Set(x, y, c)
		}
	}
	return &gfx.TextureData{Levels: gfx.BuildMips(img), Wrap: gfx.WrapRepeat}
}

// quadMesh is a square in the XY plane spanning [-s, s]², facing +Z.
func quadMesh(s float32) *gfx.MeshData {
	n := gmath.V3(0, 0, 1)
	return &gfx.MeshData{
		Vertices: []gfx.Vertex{
			{Pos: gmath.V3(-s, -s, 0), Normal: n, UV: gmath.V2(0, 1)},
			{Pos: gmath.V3(s, -s, 0), Normal: n, UV: gmath.V2(1, 1)},
			{Pos: gmath.V3(s, s, 0), Normal: n, UV: gmath.V2(1, 0)},
			{Pos: gmath.V3(-s, s, 0), Normal: n, UV: gmath.V2(0, 0)},
		},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	}
}

// perspectiveView is a camera at eye looking at target.
func perspectiveView(eye, target gmath.Vec3, fovDeg float32, w, h int) gfx.View {
	return gfx.View{
		View: gmath.LookAt(eye, target, gmath.Up),
		Proj: gmath.Perspective(gmath.Radians(fovDeg), float32(w)/float32(h), 0.1, 100),
		Eye:  eye, Near: 0.1, Far: 100,
	}
}

// orthoPixelView maps pixel coordinates (origin top-left, y down) to the screen.
func orthoPixelView(w, h int) gfx.View {
	return gfx.View{View: gmath.Ident4(), Proj: gmath.Orthographic(0, float32(w), float32(h), 0, -1, 1), Near: -1, Far: 1}
}

// lcg is a tiny deterministic generator for test geometry.
type lcg uint64

func (l *lcg) float() float32 {
	*l = *l*6364136223846793005 + 1442695040888963407
	return float32(float32(uint32(*l>>40)) / float32(1<<24))
}

// white is an opaque white color multiplier.
var white = gmath.V4(1, 1, 1, 1)
