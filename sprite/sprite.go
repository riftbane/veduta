// Package sprite is the 2D sprite and HUD layer: it batches textured quads, 9-slice
// panels and bitmap text into a gfx.DrawList, on top of the same rasterizer as the 3D
// scene (spec §6.3).
//
// A Batch owns one overlay view with an orthographic camera in pixel coordinates (origin
// at the top-left corner, x right, y down) and draws with gfx.State2D (no depth test or
// write, alpha blending, no culling). Consecutive quads that share a texture, tint and
// filter are merged into a single DrawCmd. Overlay views are drawn only in
// gfx.ModeColor, so the HUD never pollutes the debug render modes.
//
//	b := sprite.Begin(dl, 1280, 720)
//	b.Rect(gmath.R(8, 8, 200, 24), 0xC0000000)
//	b.Text(sprite.DefaultFont(), fontTex, 12, 16, 2, "SCORE 12", 0xFFFFFFFF)
//	b.End()
package sprite

//go:generate go run ./internal/genfont

import (
	"image"

	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gmath"
)

// quadIndices are the two triangles of a quad whose corners are stored top-left,
// top-right, bottom-right, bottom-left: (TL, BL, BR) and (TL, BR, TR). The projection
// maps pixel y down to NDC y up, so both triangles are counter-clockwise on screen: front
// faces (gfx.CullMode), facing the viewer like the +Z normal. Sprites therefore survive
// back-face culling even though gfx.State2D disables it.
var quadIndices = [6]uint32{0, 3, 2, 0, 2, 1}

// spriteNormal is the normal of every sprite vertex (unused by unlit drawing, but kept
// meaningful for the normals buffer).
var spriteNormal = gmath.Vec3{X: 0, Y: 0, Z: 1}

// Batch accumulates 2D quads into a DrawList. Create one per frame with Begin and finish
// it with End. A Batch is not safe for concurrent use.
//
// Each run of consecutive quads with the same (texture, tint, filter) becomes one
// DrawCmd referencing transient geometry (Mesh 0) in the DrawList. The command is
// appended when the run starts and grown in place, so draw order is preserved even if
// the caller appends other commands or transient geometry to the same DrawList between
// sprite calls (that simply starts a new run).
type Batch struct {
	dl    *gfx.DrawList
	view  int
	ended bool

	// Current run: the command at dl.Cmds[cmd] when open.
	open   bool
	cmd    int
	tex    gfx.TextureID
	tint   uint32
	filter gfx.Filter

	quad [4]gfx.Vertex // scratch for AddTransient, avoids per-quad allocation
}

// Begin starts a sprite batch drawing into dl over a width×height pixel area. It appends
// an overlay view whose projection is gmath.Orthographic(0, width, height, 0, -1, 1):
// pixel (0, 0) is the top-left corner and y grows downward. width and height must be
// positive.
//
// Begin is small enough to be inlined, so a Batch that stays local to the calling
// function lives on its stack: a frame of sprites drawn into a reused DrawList does not
// allocate.
func Begin(dl *gfx.DrawList, width, height int) *Batch {
	b := &Batch{}
	b.begin(dl, width, height)
	return b
}

func (b *Batch) begin(dl *gfx.DrawList, width, height int) {
	if width <= 0 || height <= 0 {
		panic("sprite: Begin with a non-positive size")
	}
	b.dl = dl
	b.view = dl.AddView(gfx.View{
		View:    gmath.Ident4(),
		Proj:    gmath.Orthographic(0, float32(width), float32(height), 0, -1, 1),
		Near:    -1,
		Far:     1,
		Overlay: true,
	})
}

// View returns the index of the overlay view the batch draws with.
func (b *Batch) View() int { return b.view }

// Rect draws a solid, untextured rectangle. color is 0xAARRGGBB; its alpha blends.
func (b *Batch) Rect(dst gmath.Rect, color uint32) {
	b.quadUV(0, color, gfx.FilterNearest, dst, gmath.Vec2{}, gmath.Vec2{})
}

// Image draws the texel rectangle src of a texW×texH texture stretched over dst. The UVs
// are src divided by the texture size (v = 0 is the top row). tint multiplies the texels
// (0xFFFFFFFF leaves them unchanged). Nothing is drawn when texW or texH is not positive.
// A dst whose Min is greater than its Max mirrors the image along that axis.
func (b *Batch) Image(tex gfx.TextureID, texW, texH int, src image.Rectangle, dst gmath.Rect, tint uint32, filter gfx.Filter) {
	if texW <= 0 || texH <= 0 {
		return
	}
	iw, ih := 1/float32(texW), 1/float32(texH)
	uv0 := gmath.Vec2{X: float32(src.Min.X) * iw, Y: float32(src.Min.Y) * ih}
	uv1 := gmath.Vec2{X: float32(src.Max.X) * iw, Y: float32(src.Max.Y) * ih}
	b.quadUV(tex, tint, filter, dst, uv0, uv1)
}

// NineSlice draws the texel rectangle src of a texW×texH texture as a scalable panel.
// inset holds the left, top, right and bottom border widths in texels. The four corners
// keep their texel size (scale 1), the top and bottom edges stretch horizontally, the
// left and right edges stretch vertically, and the center stretches both ways.
//
// Insets are clamped to [0, src size] (left before right, top before bottom). When dst
// is narrower than left+right (or shorter than top+bottom) the corners shrink
// proportionally along that axis to fill dst exactly and the middle column (row)
// vanishes. Slices with no area, in the source or in dst, are skipped, so a regular panel
// emits 9 quads in row-major order from the top-left. Nothing is drawn when dst is empty
// or texW, texH are not positive.
func (b *Batch) NineSlice(tex gfx.TextureID, texW, texH int, src image.Rectangle, inset [4]int, dst gmath.Rect, tint uint32, filter gfx.Filter) {
	if texW <= 0 || texH <= 0 || src.Empty() || dst.IsEmpty() {
		return
	}
	sw, sh := src.Dx(), src.Dy()
	left := clampInt(inset[0], 0, sw)
	right := clampInt(inset[2], 0, sw-left)
	top := clampInt(inset[1], 0, sh)
	bottom := clampInt(inset[3], 0, sh-top)

	sx := [4]int{src.Min.X, src.Min.X + left, src.Max.X - right, src.Max.X}
	sy := [4]int{src.Min.Y, src.Min.Y + top, src.Max.Y - bottom, src.Max.Y}
	dx := splitAxis(dst.Min.X, dst.Max.X, left, right)
	dy := splitAxis(dst.Min.Y, dst.Max.Y, top, bottom)
	iw, ih := 1/float32(texW), 1/float32(texH)
	for j := 0; j < 3; j++ {
		if sy[j+1] <= sy[j] || dy[j+1] <= dy[j] {
			continue
		}
		for i := 0; i < 3; i++ {
			if sx[i+1] <= sx[i] || dx[i+1] <= dx[i] {
				continue
			}
			d := gmath.Rect{Min: gmath.Vec2{X: dx[i], Y: dy[j]}, Max: gmath.Vec2{X: dx[i+1], Y: dy[j+1]}}
			uv0 := gmath.Vec2{X: float32(sx[i]) * iw, Y: float32(sy[j]) * ih}
			uv1 := gmath.Vec2{X: float32(sx[i+1]) * iw, Y: float32(sy[j+1]) * ih}
			b.quadUV(tex, tint, filter, d, uv0, uv1)
		}
	}
}

// splitAxis returns the four slice boundaries of [lo, hi] for borders of a and b pixels,
// shrinking both borders proportionally when they do not fit.
func splitAxis(lo, hi float32, a, b int) [4]float32 {
	size := hi - lo
	fa, fb := float32(a), float32(b)
	if fa+fb > size {
		m := lo + size*fa/(fa+fb)
		return [4]float32{lo, m, m, hi}
	}
	return [4]float32{lo, lo + fa, hi - fb, hi}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// End finishes the batch. The DrawList then holds every command of the batch; the Batch
// must not be used afterwards (drawing after End panics).
func (b *Batch) End() {
	b.flush()
	b.ended = true
}

// flush closes the current run so the next quad starts a new DrawCmd.
func (b *Batch) flush() { b.open = false }

// quadUV appends one quad covering dst with UVs uv0 (top-left) to uv1 (bottom-right),
// merging it into the current run when possible.
func (b *Batch) quadUV(tex gfx.TextureID, tint uint32, filter gfx.Filter, dst gmath.Rect, uv0, uv1 gmath.Vec2) {
	if b.ended {
		panic("sprite: Batch used after End")
	}
	if tex == 0 {
		filter = gfx.FilterNearest // untextured: the filter is irrelevant, keep runs mergeable
	}
	dl := b.dl
	// The run continues only if its command is still the last one and its indices still
	// end the index buffer (checked in that order: the caller may have Reset the list).
	if b.open && (b.cmd != len(dl.Cmds)-1 || dl.Cmds[b.cmd].First+dl.Cmds[b.cmd].Count != len(dl.Indices) ||
		tex != b.tex || tint != b.tint || filter != b.filter) {
		b.flush()
	}
	b.quad[0] = gfx.Vertex{Pos: gmath.Vec3{X: dst.Min.X, Y: dst.Min.Y}, Normal: spriteNormal, UV: uv0}
	b.quad[1] = gfx.Vertex{Pos: gmath.Vec3{X: dst.Max.X, Y: dst.Min.Y}, Normal: spriteNormal, UV: gmath.Vec2{X: uv1.X, Y: uv0.Y}}
	b.quad[2] = gfx.Vertex{Pos: gmath.Vec3{X: dst.Max.X, Y: dst.Max.Y}, Normal: spriteNormal, UV: uv1}
	b.quad[3] = gfx.Vertex{Pos: gmath.Vec3{X: dst.Min.X, Y: dst.Max.Y}, Normal: spriteNormal, UV: gmath.Vec2{X: uv0.X, Y: uv1.Y}}
	first, count := dl.AddTransient(b.quad[:], quadIndices[:])
	if b.open {
		dl.Cmds[b.cmd].Count += count
		return
	}
	dl.Add(gfx.DrawCmd{
		View:    b.view,
		First:   first,
		Count:   count,
		Model:   gmath.Ident4(),
		Texture: tex,
		Color:   gfx.ColorVec4(tint),
		State:   gfx.State2D,
		Filter:  filter,
		Unlit:   true,
	})
	b.open, b.cmd, b.tex, b.tint, b.filter = true, len(dl.Cmds)-1, tex, tint, filter
}
