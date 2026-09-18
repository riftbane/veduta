package asset

// Source structs mirror the JSON source formats one to one. They are decoded strictly
// (unknown fields are errors). Vectors are plain number slices and colors are strings so
// that validation can report the exact JSON path — and therefore line and column — of a
// bad value. Optional scalars whose zero value is meaningful are pointers.

// ModelSource is assets/models/<name>.vmodel.
type ModelSource struct {
	Veduta         string       `json:"veduta"`
	Name           string       `json:"name,omitempty"`             // must equal the file name when present
	Units          string       `json:"units,omitempty"`            // "m" (the only unit in v0.1.0)
	Pivot          string       `json:"pivot,omitempty"`            // origin (default), center, bottom-center
	SmoothAngleDeg *float32     `json:"smooth_angle_deg,omitempty"` // default 30
	Symmetry       string       `json:"symmetry,omitempty"`         // "", x, y, z: check MESH_ASYMMETRIC
	TriangleBudget int          `json:"triangle_budget,omitempty"`  // 0: default budget
	LOD            []LODSource  `json:"lod,omitempty"`              // levels of detail, nearest first
	DrawDistance   *float32     `json:"draw_distance,omitempty"`    // not drawn beyond (default: always)
	Parts          []PartSource `json:"parts"`
}

// LODSource is one level of detail of a model.
type LODSource struct {
	Distance *float32 `json:"distance"`
	Model    string   `json:"model,omitempty"` // another model drawn instead (default: fewer segments)
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

// TextureSource is assets/textures/<name>.vtex.
type TextureSource struct {
	Veduta  string        `json:"veduta"`
	Size    []int         `json:"size"`
	Tiling  bool          `json:"tiling,omitempty"`
	Mipmaps *bool         `json:"mipmaps,omitempty"` // default true, false for a sheet of frames
	Layers  []LayerSource `json:"layers,omitempty"`
	// Grid cuts the image into frames: [columns, rows].
	Grid []int `json:"grid,omitempty"`
	// Frames draws the frames one by one, each size wide, over Layers.
	Frames []FrameSource          `json:"frames,omitempty"`
	Clips  map[string]*ClipSource `json:"clips,omitempty"` // named sequences of frames
	Play   string                 `json:"play,omitempty"`  // the clip shown when nothing picks a frame
	Edge   *EdgeSource            `json:"edge,omitempty"`  // how the texture spills over lower terrains in a map
	// Autotile: every frame is 6 × 3 tiles, an island and a lake, picked by a map's cells.
	Autotile bool `json:"autotile,omitempty"`
}

// FrameSource is one frame of a texture drawn frame by frame.
type FrameSource struct {
	Layers []LayerSource `json:"layers"`
}

// ClipSource is a named animation of a texture's frames.
type ClipSource struct {
	Frames []int    `json:"frames"`
	FPS    *float32 `json:"fps"`
	Loop   *bool    `json:"loop,omitempty"` // default true
	Next   string   `json:"next,omitempty"` // the clip that follows a clip that does not loop
}

// EdgeSource shapes the border a terrain draws over the cells of lower terrains around it.
type EdgeSource struct {
	Priority  *int     `json:"priority"`
	Width     *float32 `json:"width,omitempty"`     // pixels, default a quarter of a frame's smaller side
	Roughness *float32 `json:"roughness,omitempty"` // 0 to 1, default 0.5
	Seed      int64    `json:"seed,omitempty"`
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
	Fit      string    `json:"fit,omitempty"`  // contain (default), cover, stretch
	Rect     []int     `json:"rect,omitempty"` // image: [x, y, width, height] of the PNG to use (default all)
}

// MaterialSource is assets/materials/<name>.vmat.
type MaterialSource struct {
	Veduta  string   `json:"veduta"`
	Albedo  string   `json:"albedo,omitempty"`  // default #ffffff
	Texture string   `json:"texture,omitempty"` // texture name
	Unlit   bool     `json:"unlit,omitempty"`
	Alpha   string   `json:"alpha,omitempty"`  // opaque (default), blend, cutout
	Cutoff  *float32 `json:"cutoff,omitempty"` // cutout threshold, default 0.5
	Cull    string   `json:"cull,omitempty"`   // back (default), none
	Filter  string   `json:"filter,omitempty"` // bilinear (default), nearest
	Grid    []int    `json:"grid,omitempty"`   // [columns, rows]: the texture is a sheet of frames
}

// SceneSource is assets/scenes/<name>.vscene.
type SceneSource struct {
	Veduta     string         `json:"veduta"`
	Camera     CameraSource   `json:"camera"`
	Light      *LightSource   `json:"light,omitempty"`
	Background string         `json:"background,omitempty"`
	Entities   []EntitySource `json:"entities"`
	Map        string         `json:"map,omitempty"` // a tile map drawn under the entities
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
	// Hitbox is [[minx, miny, minz], [maxx, maxy, maxz]] in the entity's local space.
	Hitbox [][]float32 `json:"hitbox,omitempty"`
	Layer  int         `json:"layer,omitempty"` // draw order, -1000..1000, default 0
	Frame  int         `json:"frame,omitempty"` // the frame of a material's grid it shows, from 0
	Anim   string      `json:"anim,omitempty"`  // the clip of its texture it plays
}

// ScenarioSource is tests/scenarios/<name>.vscenario.
type ScenarioSource struct {
	Veduta      string         `json:"veduta"`
	Scene       string         `json:"scene,omitempty"`
	Seed        uint64         `json:"seed"`
	Ticks       int            `json:"ticks"`
	Inputs      []InputSource  `json:"inputs,omitempty"`
	Expect      []ExpectSource `json:"expect,omitempty"`
	Invariants  []string       `json:"invariants,omitempty"`
	Screenshots []int          `json:"screenshots,omitempty"`
	World       string         `json:"world,omitempty"` // instead of scene
	At          []int          `json:"at,omitempty"`    // start cell of a world, default [0, 0]
	Saves       map[string]any `json:"saves,omitempty"` // the game's saves when the run starts, by name
}

// InputSource is one input event of a scenario or input script: the buttons (ButtonNames)
// that go down and up at Tick.
type InputSource struct {
	Tick    int      `json:"tick"`
	Press   []string `json:"press,omitempty"`
	Release []string `json:"release,omitempty"`
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
	Name              string      `json:"name"`               // a-z, 0-9, _ and -, up to 64: archive names
	Title             string      `json:"title"`              // what a player sees (default: name)
	Icon              string      `json:"icon"`               // a PNG beside the title on the console
	Engine            string      `json:"engine"`             // the engine version, as v2.0.0
	Entry             string      `json:"entry"`              // a Go game's package (default ./cmd/game)
	Script            string      `json:"script"`             // a Lua game's main script, as main.lua
	API               int         `json:"api"`                // the Lua API level the game needs (default 1)
	Resolution        []int       `json:"resolution"`         // [width, height] of the frame (default [320, 240])
	InspectResolution []int       `json:"inspect_resolution"` // [width, height] of inspection images (default [320, 240])
	TickRate          int         `json:"tick_rate"`          // ticks per second (default 20)
	DefaultScene      string      `json:"default_scene"`      // default main
	DefaultWorld      string      `json:"default_world"`      // a world to start in instead of the scene
	DefaultSeed       uint64      `json:"default_seed"`       // default 1
	Assets            string      `json:"assets"`             // default assets
	Cooked            string      `json:"cooked"`             // default assets/.cooked
	Invariants        []string    `json:"invariants"`         // checked in every run: finite_positions, within_bounds, entity_count_max:N, no_overlap:a,b or a game's own
	Bounds            [][]float32 `json:"bounds"`             // [[min x, y, z], [max x, y, z]] in meters (default [[-100, -50, -100], [100, 100, 100]])
}

// PrefabSource is assets/prefabs/<name>.vprefab.
type PrefabSource struct {
	Veduta    string         `json:"veduta"`
	Footprint []float32      `json:"footprint"` // [width, depth] in meters
	Tags      []string       `json:"tags,omitempty"`
	Rules     *RulesSource   `json:"rules,omitempty"`
	Entities  []EntitySource `json:"entities"`
}

// RulesSource are the placement rules of a prefab.
type RulesSource struct {
	Biomes      []string           `json:"biomes,omitempty"`       // biomes the prefab may stand in (default: any)
	MinDistance map[string]float32 `json:"min_distance,omitempty"` // tag → meters of free ground
}

// WorldSource is assets/worlds/<name>.vworld.
type WorldSource struct {
	Veduta     string             `json:"veduta"`
	Seed       uint64             `json:"seed,omitempty"`
	Cell       *float32           `json:"cell,omitempty"`        // default 1
	Chunk      int                `json:"chunk,omitempty"`       // default 16
	Extent     int                `json:"extent,omitempty"`      // default 512
	View       int                `json:"view,omitempty"`        // default 1
	BiomeScale int                `json:"biome_scale,omitempty"` // default 64
	Camera     CameraSource       `json:"camera"`
	Light      *LightSource       `json:"light,omitempty"`
	Background string             `json:"background,omitempty"`
	Biomes     []BiomeSource      `json:"biomes"`
	Terrain    *TerrainSource     `json:"terrain,omitempty"`
	Features   []FeatureSource    `json:"features,omitempty"`
	Vegetation []VegetationSource `json:"vegetation,omitempty"`
	Scatter    []ScatterSource    `json:"scatter,omitempty"`
	Sites      []SiteSource       `json:"sites,omitempty"`
	Places     []PlaceSource      `json:"places,omitempty"`
	Entities   []EntitySource     `json:"entities,omitempty"`
}

// BiomeSource is one biome of a world.
type BiomeSource struct {
	Name   string `json:"name"`
	Ground string `json:"ground"`           // material name
	Weight int    `json:"weight,omitempty"` // default 1
}

// TerrainSource shapes the ground of a world: generated relief, the sea level, the water
// material and the ground's levels of detail.
type TerrainSource struct {
	Relief      *float32 `json:"relief,omitempty"`       // meters above and below 0, default 0 (flat)
	ReliefScale int      `json:"relief_scale,omitempty"` // cells per period, default 48
	SeaLevel    *float32 `json:"sea_level,omitempty"`    // ground below it is sea (default: no sea)
	Water       string   `json:"water,omitempty"`        // material of water surfaces (default: built-in)
	LODDistance *float32 `json:"lod_distance,omitempty"` // meters to the first coarser level, default 2 chunks
}

// FeatureSource is one terrain feature of a world: a hill, a plain, a lake or a sea.
type FeatureSource struct {
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	Cell      []int    `json:"cell"`   // centre [x, z]
	Radius    int      `json:"radius"` // cells
	Height    *float32 `json:"height,omitempty"`
	Depth     *float32 `json:"depth,omitempty"`
	Falloff   *int     `json:"falloff,omitempty"`
	Roughness *float32 `json:"roughness,omitempty"`
}

// VegetationSource is one vegetation rule of a world: trees (a prefab) or flora (a
// model), everywhere or in a round area.
type VegetationSource struct {
	Name    string    `json:"name"`
	Prefab  string    `json:"prefab,omitempty"`
	Model   string    `json:"model,omitempty"`
	Density *float32  `json:"density"`
	Biomes  []string  `json:"biomes,omitempty"`
	Cell    []int     `json:"cell,omitempty"`
	Radius  int       `json:"radius,omitempty"`
	Scale   []float32 `json:"scale,omitempty"` // model: [min, max], default [0.8, 1.2]
}

// ScatterSource is one scatter rule of a world.
type ScatterSource struct {
	Prefab  string   `json:"prefab"`
	Biomes  []string `json:"biomes,omitempty"`
	Density *float32 `json:"density"`
}

// SiteSource is one site rule of a world.
type SiteSource struct {
	Tag     string   `json:"tag"`
	Prefabs []string `json:"prefabs"`
	Biomes  []string `json:"biomes,omitempty"`
	Spacing int      `json:"spacing"`
	Chance  *float32 `json:"chance,omitempty"` // default 1
}

// PlaceSource is one explicit landmark of a world.
type PlaceSource struct {
	Name     string `json:"name"`
	Prefab   string `json:"prefab"`
	Cell     []int  `json:"cell"` // [x, z] in cells
	Rotation int    `json:"rotation,omitempty"`
}

// MapSource is assets/maps/<name>.vmap.
type MapSource struct {
	Veduta   string             `json:"veduta"`
	Size     []int              `json:"size"`             // [columns, rows]
	Tile     *float32           `json:"tile,omitempty"`   // meters per cell, default 1
	Origin   []float32          `json:"origin,omitempty"` // world position of the top-left corner, default [0, 0, 0]
	Terrains []MapTerrainSource `json:"terrains"`
	Layers   []MapLayerSource   `json:"layers"`
	Objects  []MapObjectSource  `json:"objects,omitempty"`
}

// MapTerrainSource is what a map paints cells with.
type MapTerrainSource struct {
	Key      string   `json:"key"` // the character standing for it in rows
	Name     string   `json:"name"`
	Texture  string   `json:"texture,omitempty"`
	Material string   `json:"material,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

// MapLayerSource is one layer of a map's cells.
type MapLayerSource struct {
	Name  string   `json:"name"`
	Z     *float32 `json:"z,omitempty"`     // default 0.1 × its index
	Layer int      `json:"layer,omitempty"` // draw order
	Rows  []string `json:"rows"`            // one string per row, one character per cell, a space for none
}

// MapObjectSource is a named rectangle of cells a game reads.
type MapObjectSource struct {
	Name  string         `json:"name"`
	At    []int          `json:"at"`             // [column, row] of its top-left cell
	Size  []int          `json:"size,omitempty"` // [columns, rows], default [1, 1]
	Tags  []string       `json:"tags,omitempty"`
	Props map[string]any `json:"props,omitempty"` // strings, numbers and booleans
}
