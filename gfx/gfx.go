// Package gfx defines the rendering interface shared by every backend: meshes, textures,
// pipeline state, the per-frame DrawList and the Framebuffer it renders into.
//
// The only backend in v0.1.0 is the software rasterizer in gfx/soft. The types are
// handle-based (MeshID, TextureID) so that a GPU backend can implement Backend later
// without changing games.
package gfx

import (
	"fmt"

	"github.com/riftbane/veduta/gmath"
)

// TextureID is a backend texture handle; 0 means "no texture".
type TextureID uint32

// MeshID is a backend mesh handle; 0 means "transient geometry in the DrawList".
type MeshID uint32

// Filter selects texture sampling.
type Filter uint8

// Texture filters.
const (
	FilterBilinear Filter = iota
	FilterNearest
)

var filterNames = [...]string{"bilinear", "nearest"}

func (f Filter) String() string { return enumString(filterNames[:], int(f)) }

// ParseFilter parses "bilinear" or "nearest".
func ParseFilter(s string) (Filter, error) {
	i, err := enumParse("filter", filterNames[:], s)
	return Filter(i), err
}

// Wrap selects how texture coordinates outside [0, 1) are handled.
type Wrap uint8

// Wrap modes.
const (
	WrapRepeat Wrap = iota
	WrapClamp
)

// BlendMode selects how fragments combine with the framebuffer.
type BlendMode uint8

// Blend modes.
const (
	BlendOpaque BlendMode = iota
	BlendAlpha
	BlendAdd
)

var blendNames = [...]string{"opaque", "alpha", "add"}

func (b BlendMode) String() string { return enumString(blendNames[:], int(b)) }

// ParseBlend parses "opaque", "alpha" or "add".
func ParseBlend(s string) (BlendMode, error) {
	i, err := enumParse("blend", blendNames[:], s)
	return BlendMode(i), err
}

// CullMode selects face culling.
type CullMode uint8

// Cull modes. Front faces are counter-clockwise on screen.
const (
	CullBack CullMode = iota
	CullNone
)

var cullNames = [...]string{"back", "none"}

func (c CullMode) String() string { return enumString(cullNames[:], int(c)) }

// ParseCull parses "back" or "none".
func ParseCull(s string) (CullMode, error) {
	i, err := enumParse("cull", cullNames[:], s)
	return CullMode(i), err
}

// RenderMode selects what the backend writes into the color buffer. Every mode except
// ModeColor is a debug visualization for inspection.
type RenderMode uint8

// Render modes (spec §6.2).
const (
	ModeColor      RenderMode = iota // lit, textured, blended: the final image
	ModeWireframe                    // hidden-line wireframe over a dark fill
	ModeNormals                      // world-space normal as RGB = n*0.5+0.5
	ModeDepth                        // linear eye depth, near white to far black
	ModeIDs                          // entity id false color
	ModeSilhouette                   // white coverage on black
	ModeOverdraw                     // fragments per pixel as a heat map (no depth test)
	ModeUVChecker                    // UV checkerboard instead of the material texture
	ModeCollision                    // dimmed color render; debug lines (AABBs) on top
)

var modeNames = [...]string{"color", "wireframe", "normals", "depth", "ids", "silhouette", "overdraw", "uv_checker", "collision"}

// RenderModes lists every render mode in declaration order.
var RenderModes = []RenderMode{ModeColor, ModeWireframe, ModeNormals, ModeDepth, ModeIDs, ModeSilhouette, ModeOverdraw, ModeUVChecker, ModeCollision}

func (m RenderMode) String() string { return enumString(modeNames[:], int(m)) }

// ParseRenderMode parses a mode name such as "wireframe" or "uv_checker".
func ParseRenderMode(s string) (RenderMode, error) {
	i, err := enumParse("render mode", modeNames[:], s)
	return RenderMode(i), err
}

func enumString(names []string, i int) string {
	if i >= 0 && i < len(names) {
		return names[i]
	}
	return fmt.Sprintf("invalid(%d)", i)
}

func enumParse(what string, names []string, s string) (int, error) {
	for i, n := range names {
		if n == s {
			return i, nil
		}
	}
	return 0, fmt.Errorf("unknown %s %q (want one of %v)", what, s, names)
}

// PipelineState configures the fixed-function stages for a draw command.
type PipelineState struct {
	DepthTest  bool
	DepthWrite bool
	Blend      BlendMode
	Cull       CullMode
}

// Common pipeline states.
var (
	// StateOpaque is the default for lit 3D geometry.
	StateOpaque = PipelineState{DepthTest: true, DepthWrite: true, Blend: BlendOpaque, Cull: CullBack}
	// StateTransparent is used by alpha-blended 3D materials: depth tested, not written.
	StateTransparent = PipelineState{DepthTest: true, DepthWrite: false, Blend: BlendAlpha, Cull: CullBack}
	// State2D is used by the sprite/HUD layer: no depth, alpha blended, no culling.
	State2D = PipelineState{DepthTest: false, DepthWrite: false, Blend: BlendAlpha, Cull: CullNone}
)

// Vertex is the single vertex format of v0.1.0.
type Vertex struct {
	Pos    gmath.Vec3
	Normal gmath.Vec3
	UV     gmath.Vec2
}

// MeshPart is a contiguous index range drawn with one material.
type MeshPart struct {
	First    int // first index
	Count    int // number of indices, a multiple of 3
	Material int // index into the model's material list
}

// MeshData is indexed triangle geometry. Triangles are counter-clockwise when viewed
// from the front.
type MeshData struct {
	Vertices []Vertex
	Indices  []uint32
	Parts    []MeshPart
	Bounds   gmath.AABB
}

// TextureData is a texture with its mip chain. Levels[0] is full resolution; every next
// level halves each dimension (never below 1).
type TextureData struct {
	Levels []*Image
	Wrap   Wrap
}

// Light is one directional light plus ambient. Colors are linear RGB in [0, 1].
type Light struct {
	Dir     gmath.Vec3 // direction the light travels, world space (need not be normalized)
	Color   gmath.Vec3
	Ambient gmath.Vec3
}

// DefaultLight is a white light from above-front with a dim ambient term.
var DefaultLight = Light{Dir: gmath.V3(-0.4, -1, -0.3), Color: gmath.One3, Ambient: gmath.V3(0.25, 0.25, 0.25)}

// View is a camera: the transforms applied to every command that references it.
type View struct {
	View    gmath.Mat4 // world → eye
	Proj    gmath.Mat4 // eye → clip (OpenGL convention, z in [-w, w])
	Eye     gmath.Vec3 // eye position in world space
	Near    float32    // near plane distance (used by ModeDepth)
	Far     float32    // far plane distance (used by ModeDepth)
	Overlay bool       // 2D HUD layer: rendered only in ModeColor
}

// DrawCmd draws a range of triangles with one transform, material and pipeline state.
type DrawCmd struct {
	View    int        // index into DrawList.Views
	Mesh    MeshID     // 0 selects transient geometry (DrawList.Verts / Indices)
	First   int        // first index
	Count   int        // index count; 0 with a non-zero Mesh draws the whole mesh
	Model   gmath.Mat4 // object → world
	Texture TextureID  // 0: untextured
	Color   gmath.Vec4 // RGBA multiplier (albedo × tint), components in [0, 1]
	State   PipelineState
	Filter  Filter
	Unlit   bool    // ignore the light: color = texel × Color
	Cutoff  float32 // alpha test: discard fragments with alpha < Cutoff (0 disables)
	ID      uint32  // entity id written to the ID buffer (0 = none)
}

// DebugLine is a 3D line drawn after all triangles, one pixel wide.
type DebugLine struct {
	A, B      gmath.Vec3
	Color     uint32
	View      int
	DepthTest bool
}

// DrawList is everything needed to render one frame. It is reused across frames with
// Reset so steady-state rendering does not allocate.
type DrawList struct {
	Clear      bool
	ClearColor uint32
	Mode       RenderMode
	Light      Light
	Views      []View
	Cmds       []DrawCmd
	Verts      []Vertex // transient geometry, referenced by commands with Mesh == 0
	Indices    []uint32
	Lines      []DebugLine
}

// Reset empties the list, keeping allocated capacity.
func (d *DrawList) Reset() {
	d.Clear = false
	d.ClearColor = 0
	d.Mode = ModeColor
	d.Light = Light{}
	d.Views = d.Views[:0]
	d.Cmds = d.Cmds[:0]
	d.Verts = d.Verts[:0]
	d.Indices = d.Indices[:0]
	d.Lines = d.Lines[:0]
}

// AddView appends a view and returns its index.
func (d *DrawList) AddView(v View) int {
	d.Views = append(d.Views, v)
	return len(d.Views) - 1
}

// Add appends a draw command.
func (d *DrawList) Add(c DrawCmd) { d.Cmds = append(d.Cmds, c) }

// AddLine appends a debug line.
func (d *DrawList) AddLine(l DebugLine) { d.Lines = append(d.Lines, l) }

// AddTransient appends transient vertices and indices (relative to verts) and returns
// the index range to put in a DrawCmd with Mesh == 0.
func (d *DrawList) AddTransient(verts []Vertex, indices []uint32) (first, count int) {
	base := uint32(len(d.Verts))
	d.Verts = append(d.Verts, verts...)
	first = len(d.Indices)
	for _, i := range indices {
		d.Indices = append(d.Indices, base+i)
	}
	return first, len(indices)
}

// Framebuffer is a render target. Color is BGRA8 (0xAARRGGBB), Depth holds window-space
// depth in [0, 1] (0 = near), ID holds the entity id of the visible fragment (0 = none).
// Normal is optional: when non-nil it receives the world-space normal of the visible
// fragment packed as 0xFFRRGGBB with each channel = (n*0.5+0.5)*255, or 0 for none.
type Framebuffer struct {
	W, H   int
	Color  []uint32
	Depth  []float32
	ID     []uint32
	Normal []uint32
}

// NewFramebuffer allocates a w×h framebuffer; normals adds the optional normal buffer.
func NewFramebuffer(w, h int, normals bool) *Framebuffer {
	n := w * h
	fb := &Framebuffer{W: w, H: h, Color: make([]uint32, n), Depth: make([]float32, n), ID: make([]uint32, n)}
	if normals {
		fb.Normal = make([]uint32, n)
	}
	return fb
}

// Image returns the color buffer as an Image sharing its memory.
func (f *Framebuffer) Image() *Image { return &Image{W: f.W, H: f.H, Pix: f.Color} }

// FrameStats counts work done by the last Draw.
type FrameStats struct {
	Commands  int // draw commands processed
	Triangles int // triangles submitted
	Culled    int // rejected by backface or zero-area tests
	Clipped   int // triangles that needed clipping (fully outside ones are counted in Culled)
	Drawn     int // triangles rasterized after clipping
	Fragments int // fragments that passed the depth/alpha tests
}

// Backend renders DrawLists into framebuffers.
type Backend interface {
	CreateTexture(t *TextureData) (TextureID, error)
	CreateMesh(m *MeshData) (MeshID, error)
	// UpdateMesh replaces the geometry behind an existing mesh handle (a streamed world
	// reuses the handles of unloaded chunks).
	UpdateMesh(id MeshID, m *MeshData) error
	Begin(target *Framebuffer) error
	Draw(dl *DrawList) error
	End() error
}
