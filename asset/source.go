package asset

// Source structs mirror the JSON source formats one to one. They are decoded strictly
// (unknown fields are errors). Vectors are plain number slices and colors are strings so
// that validation can report the exact JSON path — and therefore line and column — of a
// bad value. Optional scalars whose zero value is meaningful are pointers.

// ModelSource is assets/models/<name>.model.json.
type ModelSource struct {
	Veduta         string       `json:"veduta"`
	Name           string       `json:"name,omitempty"`             // must equal the file name when present
	Units          string       `json:"units,omitempty"`            // "m" (the only unit in v0.1.0)
	Pivot          string       `json:"pivot,omitempty"`            // origin (default), center, bottom-center
	SmoothAngleDeg *float32     `json:"smooth_angle_deg,omitempty"` // default 30
	Symmetry       string       `json:"symmetry,omitempty"`         // "", x, y, z: check MESH_ASYMMETRIC
	TriangleBudget int          `json:"triangle_budget,omitempty"`  // 0: default budget
	Parts          []PartSource `json:"parts"`
}

// PartSource is one primitive of a model. Which fields are allowed depends on Shape.
type PartSource struct {
	Shape       string      `json:"shape"` // box, cylinder, sphere, plane, extrude, lathe, mirror
	Size        []float32   `json:"size,omitempty"`
	Radius      *float32    `json:"radius,omitempty"`
	Height      *float32    `json:"height,omitempty"`
	Segments    int         `json:"segments,omitempty"`
	Rings       int         `json:"rings,omitempty"`
	Profile     [][]float32 `json:"profile,omitempty"`
	Depth       *float32    `json:"depth,omitempty"`
	Axis        string      `json:"axis,omitempty"`
	Of          *int        `json:"of,omitempty"`
	Position    []float32   `json:"position,omitempty"`
	RotationDeg []float32   `json:"rotation_deg,omitempty"`
	Scale       []float32   `json:"scale,omitempty"`
	Material    string      `json:"material,omitempty"`
	UV          string      `json:"uv,omitempty"`
	FlipNormals bool        `json:"flip_normals,omitempty"`
}

// TextureSource is assets/textures/<name>.tex.json.
type TextureSource struct {
	Veduta  string        `json:"veduta"`
	Size    []int         `json:"size"`
	Tiling  bool          `json:"tiling,omitempty"`
	Mipmaps *bool         `json:"mipmaps,omitempty"` // default true
	Layers  []LayerSource `json:"layers"`
}

// LayerSource is one step of a texture's layer program. Which fields are allowed depends
// on Type.
type LayerSource struct {
	Type     string    `json:"type"` // solid, noise, stripes, rect, circle, gradient, checker, image
	Color    string    `json:"color,omitempty"`
	Colors   []string  `json:"colors,omitempty"`
	Opacity  *float32  `json:"opacity,omitempty"` // default 1
	Blend    string    `json:"blend,omitempty"`   // normal (default), multiply, screen, add
	Seed     *int64    `json:"seed,omitempty"`
	Scale    *float32  `json:"scale,omitempty"`
	Octaves  int       `json:"octaves,omitempty"`
	Width    *float32  `json:"width,omitempty"`
	AngleDeg *float32  `json:"angle_deg,omitempty"`
	XY       []float32 `json:"xy,omitempty"`
	Size     []float32 `json:"size,omitempty"`
	Corner   *float32  `json:"corner,omitempty"`
	Outline  *float32  `json:"outline,omitempty"`
	Center   []float32 `json:"center,omitempty"`
	Radius   *float32  `json:"radius,omitempty"`
	From     string    `json:"from,omitempty"`
	To       string    `json:"to,omitempty"`
	Cells    int       `json:"cells,omitempty"`
	Path     string    `json:"path,omitempty"`
	Fit      string    `json:"fit,omitempty"` // contain (default), cover, stretch
}

// MaterialSource is assets/materials/<name>.mat.json.
type MaterialSource struct {
	Veduta  string   `json:"veduta"`
	Albedo  string   `json:"albedo,omitempty"`  // default #ffffff
	Texture string   `json:"texture,omitempty"` // texture name
	Unlit   bool     `json:"unlit,omitempty"`
	Alpha   string   `json:"alpha,omitempty"`  // opaque (default), blend, cutout
	Cutoff  *float32 `json:"cutoff,omitempty"` // cutout threshold, default 0.5
	Cull    string   `json:"cull,omitempty"`   // back (default), none
	Filter  string   `json:"filter,omitempty"` // bilinear (default), nearest
}

// SceneSource is assets/scenes/<name>.scene.json.
type SceneSource struct {
	Veduta     string         `json:"veduta"`
	Camera     CameraSource   `json:"camera"`
	Light      *LightSource   `json:"light,omitempty"`
	Background string         `json:"background,omitempty"`
	Entities   []EntitySource `json:"entities"`
}

// CameraSource is the scene camera.
type CameraSource struct {
	Type     string    `json:"type,omitempty"`    // perspective (default), orthographic
	FovDeg   *float32  `json:"fov_deg,omitempty"` // perspective, default 60
	Size     *float32  `json:"size,omitempty"`    // orthographic visible height in meters
	Near     *float32  `json:"near,omitempty"`    // default 0.1
	Far      *float32  `json:"far,omitempty"`     // default 200
	Position []float32 `json:"position"`
	LookAt   []float32 `json:"look_at"`
}

// LightSource is the scene's directional light.
type LightSource struct {
	Direction []float32 `json:"direction"`
	Color     string    `json:"color,omitempty"`   // default #ffffff
	Ambient   string    `json:"ambient,omitempty"` // default #404040
}

// EntitySource is one entity of a scene.
type EntitySource struct {
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	Model       string    `json:"model,omitempty"`
	Material    string    `json:"material,omitempty"`
	Position    []float32 `json:"position,omitempty"`
	RotationDeg []float32 `json:"rotation_deg,omitempty"`
	Scale       []float32 `json:"scale,omitempty"`
	Tags        []string  `json:"tags,omitempty"`
	Parent      string    `json:"parent,omitempty"`
	Visible     *bool     `json:"visible,omitempty"` // default true
}

// ScenarioSource is tests/scenarios/<name>.scenario.json.
type ScenarioSource struct {
	Veduta      string         `json:"veduta"`
	Scene       string         `json:"scene"`
	Seed        uint64         `json:"seed"`
	Ticks       int            `json:"ticks"`
	Inputs      []InputSource  `json:"inputs,omitempty"`
	Expect      []ExpectSource `json:"expect,omitempty"`
	Invariants  []string       `json:"invariants,omitempty"`
	Screenshots []int          `json:"screenshots,omitempty"`
}

// InputSource is one input event of a scenario or input script. Keys use W3C
// KeyboardEvent.code names (KeyW, Space, ArrowLeft, …); buttons are left, right, middle.
type InputSource struct {
	Tick    int          `json:"tick"`
	Press   []string     `json:"press,omitempty"`
	Release []string     `json:"release,omitempty"`
	Mouse   *MouseSource `json:"mouse,omitempty"`
	Buttons []string     `json:"buttons,omitempty"`
	Text    string       `json:"text,omitempty"`
}

// MouseSource is a mouse position in frame pixels.
type MouseSource struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
}

// ExpectSource is one expectation of a scenario: either an entity path comparison or a
// trace event count.
type ExpectSource struct {
	Tick     int    `json:"tick"`
	Entity   string `json:"entity,omitempty"`
	Path     string `json:"path,omitempty"`
	Op       string `json:"op,omitempty"`
	Value    any    `json:"value,omitempty"`
	Trace    string `json:"trace,omitempty"`
	CountMin *int   `json:"count_min,omitempty"`
	CountMax *int   `json:"count_max,omitempty"`
}

// ProjectSource is the project manifest veduta.json.
type ProjectSource struct {
	Veduta            string      `json:"veduta"`
	Name              string      `json:"name"`
	Title             string      `json:"title"`
	Icon              string      `json:"icon"`
	Engine            string      `json:"engine"`
	Entry             string      `json:"entry"`
	Resolution        []int       `json:"resolution"`
	InspectResolution []int       `json:"inspect_resolution"`
	TickRate          int         `json:"tick_rate"`
	DefaultScene      string      `json:"default_scene"`
	DefaultSeed       uint64      `json:"default_seed"`
	Assets            string      `json:"assets"`
	Cooked            string      `json:"cooked"`
	Invariants        []string    `json:"invariants"`
	Bounds            [][]float32 `json:"bounds"`
}
