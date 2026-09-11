// Package asset defines Veduta's declarative source formats (model, texture, material,
// scene, scenario, project manifest), compiles them into runtime data, stores compiled
// assets in the chunked .vda container, and cooks a project's assets incrementally.
//
// Every source file is JSON with a "veduta": "<type>/<version>" header. Decoding is strict:
// unknown fields, wrong types and invalid values are errors that carry the file, line and
// column of the offending value (SourceError). Paths inside sources are relative to the
// project's assets directory. Colors are "#RRGGBB" or "#RRGGBBAA"; angles are degrees in
// files and radians in code.
//
// Nothing in this package reads the clock or the environment. Filesystem access happens
// only through explicit fs.FS arguments, never at tick time.
package asset

import (
	"fmt"
	"strings"

	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gmath"
)

// CompilerVersion is stored in every .vda META chunk; changing it invalidates cooked
// assets.
const CompilerVersion = "veduta-asset/0.1.0"

// Source format headers (the value of the "veduta" field).
const (
	TypeModel    = "model/1"
	TypeTexture  = "texture/1"
	TypeMaterial = "material/1"
	TypeScene    = "scene/1"
	TypeScenario = "scenario/1"
	TypeProject  = "project/1"
)

// Kind names an asset kind; it is also the directory name under assets/ (plural) and
// the file suffix (see Ext).
type Kind string

// Asset kinds.
const (
	KindModel    Kind = "model"
	KindTexture  Kind = "texture"
	KindMaterial Kind = "material"
	KindScene    Kind = "scene"
	KindScenario Kind = "scenario"
)

// CookedKinds are the kinds compiled into .vda files by Cook, in cooking order.
var CookedKinds = []Kind{KindTexture, KindMaterial, KindModel, KindScene}

// Dir returns the directory holding sources of kind k, relative to assets/
// (scenarios live in tests/scenarios, relative to the project root).
func (k Kind) Dir() string {
	switch k {
	case KindModel:
		return "models"
	case KindTexture:
		return "textures"
	case KindMaterial:
		return "materials"
	case KindScene:
		return "scenes"
	case KindScenario:
		return "tests/scenarios"
	}
	return string(k)
}

// Ext returns the source file suffix of kind k, for example ".model.json".
func (k Kind) Ext() string {
	switch k {
	case KindTexture:
		return ".tex.json"
	case KindMaterial:
		return ".mat.json"
	}
	return "." + string(k) + ".json"
}

// Header returns the "veduta" header value of kind k.
func (k Kind) Header() string {
	switch k {
	case KindModel:
		return TypeModel
	case KindTexture:
		return TypeTexture
	case KindMaterial:
		return TypeMaterial
	case KindScene:
		return TypeScene
	case KindScenario:
		return TypeScenario
	}
	return ""
}

// NameFromFile returns the asset name of a source file name ("crate.model.json" →
// "crate") and whether the suffix matched kind k.
func (k Kind) NameFromFile(base string) (string, bool) {
	ext := k.Ext()
	if !strings.HasSuffix(base, ext) || len(base) == len(ext) {
		return "", false
	}
	return strings.TrimSuffix(base, ext), true
}

// SourceError is a problem located in a source file. Line and Col are 1-based; 0 means
// unknown. It is the structured error `{file,line,col,msg}` returned by tools.
type SourceError struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Col  int    `json:"col"`
	Msg  string `json:"msg"`
}

func (e *SourceError) Error() string {
	switch {
	case e.Line > 0:
		return fmt.Sprintf("%s:%d:%d: %s", e.File, e.Line, e.Col, e.Msg)
	case e.File != "":
		return fmt.Sprintf("%s: %s", e.File, e.Msg)
	}
	return e.Msg
}

// Errors is a list of source errors reported together; it implements error.
type Errors []*SourceError

func (es Errors) Error() string {
	var b strings.Builder
	for i, e := range es {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(e.Error())
	}
	return b.String()
}

// Err returns nil for an empty list and the list itself otherwise.
func (es Errors) Err() error {
	if len(es) == 0 {
		return nil
	}
	return es
}

// Model is a compiled model: one mesh whose parts map to materials by name.
type Model struct {
	Name      string
	Mesh      gfx.MeshData // Mesh.Parts[i] is source part i; Parts[i].Material indexes Materials
	Materials []string     // distinct material names in first-use order; "" = entity/default
	Parts     []PartInfo   // one per source part, same order as Mesh.Parts
	// Settings that inspection needs.
	SmoothAngleDeg float32
	Symmetry       string // "", "x", "y" or "z": the symmetry the author asked to check
	TriangleBudget int
	Pivot          string
	PivotOffset    gmath.Vec3 // translation applied to every vertex to honour Pivot
}

// PartInfo describes one source part of a compiled model.
type PartInfo struct {
	Index       int    // source part index
	Shape       string // box, cylinder, sphere, plane, extrude, lathe, mirror
	First       int    // first index in Mesh.Indices
	Count       int    // number of indices
	Material    string // material name, "" when the part has none
	UV          string // box, planar, cylindrical or spherical
	FlipNormals bool
	Of          int // mirror: source part index that was mirrored, else -1
}

// Texture is a compiled texture with its mip chain.
type Texture struct {
	Name   string
	Data   gfx.TextureData // Wrap is WrapRepeat when Tiling, else WrapClamp
	Tiling bool
	Layers int // number of source layers
}

// Material is a compiled material.
type Material struct {
	Name    string
	Albedo  uint32 // BGRA8
	Texture string // texture name, "" for none
	Unlit   bool
	Alpha   string // opaque, blend or cutout
	Cutoff  float32
	Cull    gfx.CullMode
	Filter  gfx.Filter
}

// DefaultMaterial is used when neither the model part nor the entity names a material.
var DefaultMaterial = Material{Name: "", Albedo: 0xffffffff, Alpha: "opaque", Cutoff: 0.5, Cull: gfx.CullBack, Filter: gfx.FilterBilinear}

// State returns the pipeline state for drawing with m.
func (m *Material) State() gfx.PipelineState {
	st := gfx.StateOpaque
	if m.Alpha == "blend" {
		st = gfx.StateTransparent
	}
	st.Cull = m.Cull
	return st
}

// AlphaCutoff returns the DrawCmd cutoff for m: Cutoff for cutout materials, else 0.
func (m *Material) AlphaCutoff() float32 {
	if m.Alpha == "cutout" {
		return m.Cutoff
	}
	return 0
}

// Scene is a compiled scene description (validated; references are not resolved).
type Scene struct {
	Name       string
	Camera     Camera
	Light      gfx.Light
	Background uint32
	Entities   []Entity // scene-file order; entity ids are assigned in this order
}

// Camera is the scene camera.
type Camera struct {
	Ortho    bool
	FovDeg   float32 // perspective vertical field of view
	Size     float32 // orthographic visible height in meters
	Near     float32
	Far      float32
	Position gmath.Vec3
	LookAt   gmath.Vec3
}

// Entity is one entity of a compiled scene.
type Entity struct {
	Name        string
	Kind        string
	Model       string
	Material    string
	Position    gmath.Vec3
	RotationDeg gmath.Vec3
	Scale       gmath.Vec3
	Tags        []string
	Parent      string
	Visible     bool
}
