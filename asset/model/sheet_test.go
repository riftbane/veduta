package model

import (
	"fmt"
	"testing"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/gfx/soft"
	"github.com/riftbane/veduta/v2/gmath"
	"github.com/riftbane/veduta/v2/internal/golden"
	"github.com/riftbane/veduta/v2/sprite"
)

// Contact sheets of the sample models in testdata/models, compared with
// testdata/golden/model_<name>.png. Each sheet is 3×2 tiles of 256×192: front, right,
// top, iso (lit, one color per part), iso in uv_checker mode and iso wireframe. The axes
// at the model origin (after the pivot) are drawn in red (X), green (Y) and blue (Z).
func TestGoldenSheets(t *testing.T) {
	for _, name := range []string{"box", "cylinder", "sphere", "plane", "extrude", "lathe", "crate", "mirror", "flipped"} {
		t.Run(name, func(t *testing.T) {
			m, err := Parse(name+".model.json", readModel(t, name))
			if err != nil {
				t.Fatal(err)
			}
			golden.Image(t, "model_"+name, contactSheet(t, m))
		})
	}
}

const tileW, tileH = 256, 192

var partColors = []uint32{0xffd9a066, 0xff6fa8dc, 0xff93c47d, 0xffe06666, 0xffc27ba0, 0xffffd966, 0xff76a5af, 0xffb4a7d6}

func contactSheet(t *testing.T, m *asset.Model) *gfx.Image {
	t.Helper()
	r := soft.New(soft.Options{})
	defer r.Close()
	mesh, err := r.CreateMesh(&m.Mesh)
	if err != nil {
		t.Fatal(err)
	}
	iso := gmath.V3(1, 0.8, 1.3)
	views := []struct {
		label string
		dir   gmath.Vec3
		mode  gfx.RenderMode
	}{
		{"front", gmath.V3(0, 0, 1), gfx.ModeColor},
		{"right", gmath.V3(1, 0, 0), gfx.ModeColor},
		{"top", gmath.V3(0, 1, 0), gfx.ModeColor},
		{"iso", iso, gfx.ModeColor},
		{"uv", iso, gfx.ModeUVChecker},
		{"wire", iso, gfx.ModeWireframe},
	}
	b := m.Mesh.Bounds
	center := b.Center()
	radius := max(b.Size().Len()/2, 0.05)
	const fov = 35
	dist := float32(radius / gmath.Sin(gmath.Radians(fov/2)) * 1.05)
	sheet := gfx.NewImage(3*tileW, 2*tileH)
	for i, v := range views {
		eye := center.Add(v.dir.Normalize().Scale(dist))
		fb := gfx.NewFramebuffer(tileW, tileH, false)
		dl := gfx.DrawList{Clear: true, ClearColor: gfx.RGBA(0x20, 0x28, 0x30, 0xff), Mode: v.mode, Light: gfx.DefaultLight}
		near, far := max(dist-float32(2*radius), dist*0.01), dist+float32(2*radius)
		view := dl.AddView(gfx.View{
			View: gmath.LookAt(eye, center, gmath.Up),
			Proj: gmath.Perspective(gmath.Radians(fov), float32(tileW)/tileH, near, far),
			Eye:  eye, Near: near, Far: far,
		})
		for pi, p := range m.Mesh.Parts {
			color := gfx.ColorVec4(partColors[pi%len(partColors)])
			if v.mode == gfx.ModeUVChecker {
				color = gmath.V4(1, 1, 1, 1)
			}
			dl.Add(gfx.DrawCmd{View: view, Mesh: mesh, First: p.First, Count: p.Count, Model: gmath.Ident4(),
				Color: color, State: gfx.StateOpaque, ID: uint32(pi + 1)})
		}
		axis := radius * 0.3
		for k, c := range []uint32{0xffff4040, 0xff40ff40, 0xff4080ff} {
			dl.AddLine(gfx.DebugLine{B: gmath.Zero3.With(k, axis), Color: c, View: view})
		}
		if err := r.Begin(fb); err != nil {
			t.Fatal(err)
		}
		if err := r.Draw(&dl); err != nil {
			t.Fatal(err)
		}
		if err := r.End(); err != nil {
			t.Fatal(err)
		}
		x, y := i%3*tileW, i/3*tileH
		sheet.Blit(fb.Image(), x, y)
		text := v.label
		if i == 0 {
			text = fmt.Sprintf("%s  %s  %d tris", v.label, m.Name, len(m.Mesh.Indices)/3)
		}
		label(sheet, x+4, y+4, text)
	}
	for x := 0; x < sheet.W; x++ {
		sheet.Set(x, tileH, 0xff000000)
	}
	for y := 0; y < sheet.H; y++ {
		sheet.Set(tileW, y, 0xff000000)
		sheet.Set(2*tileW, y, 0xff000000)
	}
	return sheet
}

// label writes s in the built-in 8×8 font, white, at (x, y).
func label(img *gfx.Image, x, y int, s string) {
	f := sprite.DefaultFont()
	for i, r := range s {
		g := f.Glyph(r)
		for gy := g.Min.Y; gy < g.Max.Y; gy++ {
			for gx := g.Min.X; gx < g.Max.X; gx++ {
				if f.Atlas.At(gx, gy)>>24 >= 0x80 {
					img.Set(x+i*f.CellW+gx-g.Min.X, y+gy-g.Min.Y, 0xffffffff)
				}
			}
		}
	}
}
