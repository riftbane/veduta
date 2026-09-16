package asset

import (
	"math"
	"sort"
	"strings"

	"github.com/riftbane/veduta/v2/gmath"
)

// World limits and defaults (docs/world.md).
const (
	MaxCell        = 64   // meters per cell
	MinChunk       = 4    // cells per chunk side
	MaxChunk       = 64   // cells per chunk side
	MaxExtent      = 4096 // chunks from the origin
	MaxView        = 4    // chunks loaded around the focus
	MaxWorldMeters = 8192 // extent × chunk × cell: how far the world reaches from the origin
	MinBiomeScale  = 4    // cells per biome noise period
	MaxBiomeScale  = 4096
	MaxWeight      = 1000 // biome weight
	MinSpacing     = 2    // cells between site regions
	MaxSpacing     = 4096

	MaxRelief          = 256   // terrain.relief, meters
	MinReliefScale     = 4     // terrain.relief_scale, cells
	MaxReliefScale     = 4096  //
	DefaultReliefScale = 48    //
	MaxLevel           = 1024  // sea level, plain and water levels, meters from 0
	MaxHill            = 256   // hill height, meters
	MaxDepth           = 256   // lake and sea depth, meters
	MaxRadius          = 16384 // feature and vegetation radius, cells
	MaxFeatures        = 4096
	MaxVegetation      = 256
	MaxFloraScale      = 16
)

// WorldDefaults holds the defaults of the optional world fields.
var WorldDefaults = World{Cell: 1, Chunk: 16, Extent: 512, View: 1, BiomeScale: 64}

// Rotations are the allowed place rotations, in degrees about +Y.
var Rotations = []int{0, 90, 180, 270}

// ReservedPrefixes start the names of entities a world generates; no place or persistent
// entity may use them, so generated names never collide with authored ones.
var ReservedPrefixes = []string{"chunk_", "site_", "place_"}

// Reserved returns the reserved prefix name starts with, or "".
func Reserved(name string) string {
	for _, p := range ReservedPrefixes {
		if strings.HasPrefix(name, p) {
			return p
		}
	}
	return ""
}

// ParseWorld decodes and compiles the world source file (for example
// "assets/worlds/overworld.world.json"). The world name is the file name without its
// ".world.json" suffix and must be a valid asset name. prefabs, when not nil, resolves
// prefab names so footprints can be checked against cells, spacing and the extent;
// unknown prefabs are not errors (inspection reports them).
func ParseWorld(file string, data []byte, prefabs func(string) *Prefab) (*World, error) {
	var src WorldSource
	loc, err := Decode(file, data, TypeWorld, &src)
	if err != nil {
		return nil, err
	}
	name, err := nameFromFile(KindWorld, file, loc)
	if err != nil {
		return nil, err
	}
	return CompileWorld(name, &src, loc, prefabs)
}

// CompileWorld validates src and returns the compiled world called name. Every problem is
// reported, located through loc (nil reports positions as unknown), in one Errors value.
// Asset references are checked for syntax only, except that prefabs resolved through
// prefabs (nil: none) must fit their cells, site regions and the world.
func CompileWorld(name string, src *WorldSource, loc *Locator, prefabs func(string) *Prefab) (*World, error) {
	c := NewChecker(ensureLoc(loc))
	if err := ValidName(name); err != nil {
		c.Errorf("", "world %v", err)
	}
	if prefabs == nil {
		prefabs = func(string) *Prefab { return nil }
	}
	d := WorldDefaults
	w := &World{
		Name:       name,
		Seed:       src.Seed,
		Cell:       c.Positive("cell", src.Cell, d.Cell),
		Chunk:      c.Int("chunk", src.Chunk, MinChunk, MaxChunk, d.Chunk),
		Extent:     c.Int("extent", src.Extent, 1, MaxExtent, d.Extent),
		View:       c.Int("view", src.View, 1, MaxView, d.View),
		BiomeScale: c.Int("biome_scale", src.BiomeScale, MinBiomeScale, MaxBiomeScale, d.BiomeScale),
		Camera:     compileCamera(c, &src.Camera),
		Light:      compileLight(c, src.Light),
		Background: c.Color("background", src.Background, defaultBackground),
	}
	if w.Cell > MaxCell {
		c.Errorf("cell", "%v out of range (0, %d]", w.Cell, MaxCell)
		w.Cell = d.Cell
	}
	if reach := float64(w.Extent) * float64(w.Chunk) * float64(w.Cell); reach > MaxWorldMeters {
		at := "extent"
		if src.Extent == 0 {
			at = "chunk"
			if src.Chunk == 0 {
				at = "cell"
			}
		}
		c.Errorf(at, "the world reaches %v m from the origin (extent %d × chunk %d × cell %v), more than %d m: float32 positions would lose precision",
			reach, w.Extent, w.Chunk, w.Cell, MaxWorldMeters)
	}
	biomes := map[string]int{} // lookup only
	if len(src.Biomes) == 0 {
		c.Errorf("biomes", "at least one biome is required ({name, ground, weight})")
	}
	w.Biomes = make([]Biome, 0, len(src.Biomes))
	for i, b := range src.Biomes {
		p := Path("biomes", i)
		bio := Biome{Name: b.Name, Ground: b.Ground, Weight: c.Int(Path(p, "weight"), b.Weight, 1, MaxWeight, 1)}
		if b.Name == "" {
			c.Errorf(Path(p, "name"), "is required")
		} else if c.Name(Path(p, "name"), b.Name) {
			if j, dup := biomes[b.Name]; dup {
				c.Errorf(Path(p, "name"), "duplicate biome %q (first used by biomes[%d])", b.Name, j)
			} else {
				biomes[b.Name] = i
			}
		}
		if b.Ground == "" {
			c.Errorf(Path(p, "ground"), "is required (name of a material whose texture tiles)")
		} else {
			c.Name(Path(p, "ground"), b.Ground)
		}
		w.Biomes = append(w.Biomes, bio)
	}
	biomeList := func(path string, list []string) []string {
		out := compileTags(c, path, list)
		for _, b := range out {
			if _, ok := biomes[b]; !ok && len(src.Biomes) > 0 {
				c.Errorf(Path(path, indexOf(list, b)), "unknown biome %q (want one of %v)", b, biomeNames(src.Biomes))
			}
		}
		return out
	}
	share := func(path string, v *float32, required bool, def float32) float32 {
		if v == nil {
			if required {
				c.Errorf(path, "is required (a share of the cells, more than 0 and at most 1)")
			}
			return def
		}
		if !gmath.IsFinite(*v) || *v <= 0 || *v > 1 {
			c.Errorf(path, "%v out of range (0, 1]", *v)
			return def
		}
		return *v
	}
	for i, s := range src.Scatter {
		p := Path("scatter", i)
		sc := Scatter{Prefab: s.Prefab, Density: share(Path(p, "density"), s.Density, true, 0.01)}
		if s.Prefab == "" {
			c.Errorf(Path(p, "prefab"), "is required")
		} else if c.Name(Path(p, "prefab"), s.Prefab) {
			if pf := prefabs(s.Prefab); pf != nil && (pf.Footprint.X > w.Cell || pf.Footprint.Y > w.Cell) {
				c.Errorf(Path(p, "prefab"), "prefab %q is %v×%v m, larger than one cell (%v m): scatter prefabs fit one cell; use sites for larger ones",
					s.Prefab, pf.Footprint.X, pf.Footprint.Y, w.Cell)
			}
		}
		sc.Biomes = biomeList(Path(p, "biomes"), s.Biomes)
		w.Scatter = append(w.Scatter, sc)
	}
	for i, s := range src.Sites {
		p := Path("sites", i)
		st := Site{Tag: s.Tag, Chance: share(Path(p, "chance"), s.Chance, false, 1)}
		if s.Tag == "" {
			c.Errorf(Path(p, "tag"), "is required (the tag the sites carry for min_distance rules)")
		} else {
			c.Name(Path(p, "tag"), s.Tag)
		}
		if len(s.Prefabs) == 0 {
			c.Errorf(Path(p, "prefabs"), "at least one prefab is required")
		}
		st.Prefabs = compileTags(c, Path(p, "prefabs"), s.Prefabs)
		st.Biomes = biomeList(Path(p, "biomes"), s.Biomes)
		switch {
		case s.Spacing == 0:
			c.Errorf(Path(p, "spacing"), "is required (cells between sites, %d to %d)", MinSpacing, MaxSpacing)
			st.Spacing = MinSpacing
		case s.Spacing < MinSpacing || s.Spacing > MaxSpacing:
			c.Errorf(Path(p, "spacing"), "%d out of range [%d, %d]", s.Spacing, MinSpacing, MaxSpacing)
			st.Spacing = MinSpacing
		default:
			st.Spacing = s.Spacing
			// Sites of one rule keep the largest min_distance any of its prefabs asks
			// for, so every prefab must leave that much room inside its region.
			var dist float32
			for _, name := range st.Prefabs {
				if pf := prefabs(name); pf != nil {
					dist = max(dist, pf.MaxDistance())
				}
			}
			for k, name := range st.Prefabs {
				pf := prefabs(name)
				if pf == nil {
					continue
				}
				fw, fd := w.Footprint(pf, 0)
				need := max(fw, fd) + w.Cells(dist)
				if need > st.Spacing {
					c.Errorf(Path(p, "spacing"), "%d cells is less than the %d×%d cell footprint of prefab %q (prefabs[%d]) plus the rule's largest min_distance (%v m): need at least %d",
						st.Spacing, fw, fd, name, k, dist, need)
				}
			}
		}
		w.Sites = append(w.Sites, st)
	}
	places := map[string]int{} // lookup only
	span := w.Span()
	for i, s := range src.Places {
		p := Path("places", i)
		pl := Place{Name: s.Name, Prefab: s.Prefab, Rotation: s.Rotation}
		if s.Name == "" {
			c.Errorf(Path(p, "name"), "is required")
		} else if c.Name(Path(p, "name"), s.Name) {
			if pre := Reserved(s.Name); pre != "" {
				c.Errorf(Path(p, "name"), "name %q starts with %q, which is reserved for generated entities", s.Name, pre)
			} else if j, dup := places[s.Name]; dup {
				c.Errorf(Path(p, "name"), "duplicate place %q (first used by places[%d])", s.Name, j)
			} else {
				places[s.Name] = i
			}
		}
		if s.Prefab == "" {
			c.Errorf(Path(p, "prefab"), "is required")
		} else {
			c.Name(Path(p, "prefab"), s.Prefab)
		}
		if indexOfInt(Rotations, s.Rotation) < 0 {
			c.Errorf(Path(p, "rotation"), "%d is not one of %v", s.Rotation, Rotations)
			pl.Rotation = 0
		}
		cellOK := false
		switch {
		case s.Cell == nil:
			c.Errorf(Path(p, "cell"), "is required ([x, z] in cells)")
		case len(s.Cell) != 2:
			c.Errorf(Path(p, "cell"), "want [x, z], got %d numbers", len(s.Cell))
		default:
			cellOK = true
			for k, v := range s.Cell {
				if v < -span || v >= span {
					c.Errorf(Path(Path(p, "cell"), k), "%d is outside the world (cells -%d to %d)", v, span, span-1)
					cellOK = false
				}
			}
		}
		if cellOK {
			pl.Cell = [2]int32{int32(s.Cell[0]), int32(s.Cell[1])}
			if pf := prefabs(s.Prefab); pf != nil {
				fw, fd := w.Footprint(pf, pl.Rotation)
				for k, end := range [2]int{s.Cell[0] + fw, s.Cell[1] + fd} {
					if end > span {
						c.Errorf(Path(Path(p, "cell"), k), "the %d×%d cell footprint of %q reaches cell %d, outside the world (last cell %d)", fw, fd, s.Prefab, end-1, span-1)
					}
				}
			}
		}
		w.Places = append(w.Places, pl)
	}
	w.Terrain = compileTerrain(c, src.Terrain, w)
	compileFeatures(c, src, w)
	compileVegetation(c, src, w, biomeList, share, prefabs)
	// Places, features and vegetation rules share one namespace (world_remove takes a name).
	named := map[string]string{} // lookup only: name → path of its first use
	claim := func(path, name string) {
		if name == "" || ValidName(name) != nil {
			return
		}
		if first, dup := named[name]; dup {
			c.Errorf(path, "name %q is already used by %s: places, features and vegetation rules need distinct names", name, first)
			return
		}
		named[name] = path
	}
	for i, pl := range src.Places {
		if places[pl.Name] == i {
			claim(Path(Path("places", i), "name"), pl.Name)
		}
	}
	for i, f := range src.Features {
		claim(Path(Path("features", i), "name"), f.Name)
	}
	for i, v := range src.Vegetation {
		claim(Path(Path("vegetation", i), "name"), v.Name)
	}
	w.Entities = compileEntities(c, src.Entities, "world")
	for i, e := range w.Entities {
		if pre := Reserved(e.Name); pre != "" {
			c.Errorf(Path(Path("entities", i), "name"), "name %q starts with %q, which is reserved for generated entities", e.Name, pre)
		}
	}
	if err := c.Err(); err != nil {
		return nil, err
	}
	return w, nil
}

// compileTerrain validates the optional terrain object.
func compileTerrain(c *Checker, src *TerrainSource, w *World) Terrain {
	t := Terrain{ReliefScale: DefaultReliefScale, LODDistance: float32(2 * float64(w.Chunk) * float64(w.Cell))}
	if src == nil {
		return t
	}
	t.Relief = c.Float("terrain.relief", src.Relief, 0, MaxRelief, 0)
	t.ReliefScale = c.Int("terrain.relief_scale", src.ReliefScale, MinReliefScale, MaxReliefScale, DefaultReliefScale)
	if src.SeaLevel != nil {
		t.Sea = true
		t.SeaLevel = c.Float("terrain.sea_level", src.SeaLevel, -MaxLevel, MaxLevel, 0)
	}
	if src.Water != "" && c.Name("terrain.water", src.Water) {
		t.Water = src.Water
	}
	if src.LODDistance != nil {
		if d := *src.LODDistance; !gmath.IsFinite(d) || d <= 0 || d > 100000 {
			c.Errorf("terrain.lod_distance", "%v out of range (0, 100000]", d)
		} else {
			t.LODDistance = d
		}
	}
	return t
}

// compileFeatures validates the terrain features and resolves their defaults
// (docs/world.md).
func compileFeatures(c *Checker, src *WorldSource, w *World) {
	if len(src.Features) > MaxFeatures {
		c.Errorf("features", "%d features, at most %d", len(src.Features), MaxFeatures)
	}
	span := w.Span()
	for i, fs := range src.Features {
		p := Path("features", i)
		f := Feature{Name: fs.Name, Kind: fs.Kind}
		nameField(c, Path(p, "name"), fs.Name)
		if fs.Kind == "" {
			c.Errorf(Path(p, "kind"), "is required (one of %v)", FeatureKinds)
		} else if indexOf(FeatureKinds, fs.Kind) < 0 {
			c.Errorf(Path(p, "kind"), "unknown value %q (want one of %v)", fs.Kind, FeatureKinds)
		}
		f.Cell = cellField(c, Path(p, "cell"), fs.Cell, span, true)
		switch {
		case fs.Radius == 0:
			c.Errorf(Path(p, "radius"), "is required (cells from the centre to the edge, 1 to %d)", MaxRadius)
			f.Radius = 1
		case fs.Radius < 1 || fs.Radius > MaxRadius:
			c.Errorf(Path(p, "radius"), "%d out of range [1, %d]", fs.Radius, MaxRadius)
			f.Radius = 1
		default:
			f.Radius = int32(fs.Radius)
		}
		r := f.Radius
		water := f.IsWater()
		if fs.Height != nil {
			f.HasHeight = true
			lim := float32(MaxLevel)
			if f.Kind == FeatureHill {
				lim = MaxHill
			}
			f.Height = c.Float(Path(p, "height"), fs.Height, -lim, lim, 0)
		} else if f.Kind == FeatureHill {
			c.Errorf(Path(p, "height"), "is required for a hill (meters added at the top, negative digs a hollow)")
		}
		if fs.Depth != nil && !water && f.Kind != "" {
			c.Errorf(Path(p, "depth"), "not used by kind %s (only lake and sea hold water)", f.Kind)
		}
		f.Depth = c.Float(Path(p, "depth"), fs.Depth, 0, MaxDepth, map[bool]float32{true: 8, false: 2}[f.Kind == FeatureSea])
		if fs.Depth != nil && *fs.Depth == 0 {
			c.Errorf(Path(p, "depth"), "0 out of range (0, %d]", MaxDepth)
		}
		switch f.Kind {
		case FeatureHill:
			f.Falloff, f.Roughness = r, 0.2
		case FeaturePlain:
			f.Falloff, f.Roughness = max(1, r/3), 0.1
		case FeatureLake:
			f.Falloff, f.Roughness = min(max(2, r/4), 16), 0.3
		case FeatureSea:
			f.Falloff, f.Roughness = min(max(4, r/8), 32), 0.3
		}
		if fs.Falloff != nil {
			lim := r
			if water {
				lim = MaxRadius
			}
			if v := *fs.Falloff; v < 0 || v > int(lim) {
				c.Errorf(Path(p, "falloff"), "%d out of range [0, %d]", v, lim)
			} else {
				f.Falloff = int32(v)
			}
		}
		f.Roughness = c.Float(Path(p, "roughness"), fs.Roughness, 0, 1, f.Roughness)
		w.Features = append(w.Features, f)
	}
}

// compileVegetation validates the vegetation rules.
func compileVegetation(c *Checker, src *WorldSource, w *World, biomeList func(string, []string) []string, share func(string, *float32, bool, float32) float32, prefabs func(string) *Prefab) {
	if len(src.Vegetation) > MaxVegetation {
		c.Errorf("vegetation", "%d rules, at most %d", len(src.Vegetation), MaxVegetation)
	}
	span := w.Span()
	for i, vs := range src.Vegetation {
		p := Path("vegetation", i)
		v := Vegetation{Name: vs.Name, Density: share(Path(p, "density"), vs.Density, true, 0.01)}
		nameField(c, Path(p, "name"), vs.Name)
		switch {
		case vs.Prefab == "" && vs.Model == "":
			c.Errorf(p, "needs a prefab (trees and other entities) or a model (grass, flowers: drawn with the ground)")
		case vs.Prefab != "" && vs.Model != "":
			c.Errorf(Path(p, "model"), "a rule has a prefab or a model, not both")
		case vs.Prefab != "":
			if c.Name(Path(p, "prefab"), vs.Prefab) {
				v.Prefab = vs.Prefab
				if pf := prefabs(vs.Prefab); pf != nil && (pf.Footprint.X > w.Cell || pf.Footprint.Y > w.Cell) {
					c.Errorf(Path(p, "prefab"), "prefab %q is %v×%v m, larger than one cell (%v m): vegetation prefabs fit one cell",
						vs.Prefab, pf.Footprint.X, pf.Footprint.Y, w.Cell)
				}
			}
		default:
			if c.Name(Path(p, "model"), vs.Model) {
				v.Model = vs.Model
			}
		}
		v.Biomes = biomeList(Path(p, "biomes"), vs.Biomes)
		switch {
		case vs.Cell == nil && vs.Radius == 0:
		case vs.Cell == nil:
			c.Errorf(Path(p, "cell"), "is required with radius (the centre [x, z] of the area)")
		case vs.Radius == 0:
			c.Errorf(Path(p, "radius"), "is required with cell (cells from the centre, 1 to %d)", MaxRadius)
		case vs.Radius < 1 || vs.Radius > MaxRadius:
			c.Errorf(Path(p, "radius"), "%d out of range [1, %d]", vs.Radius, MaxRadius)
		default:
			v.Area, v.Cell, v.Radius = true, cellField(c, Path(p, "cell"), vs.Cell, span, true), int32(vs.Radius)
		}
		v.Scale = [2]float32{0.8, 1.2}
		if vs.Scale != nil {
			switch {
			case vs.Prefab != "":
				c.Errorf(Path(p, "scale"), "only a model rule is scaled (a prefab keeps its entities' scale)")
			case len(vs.Scale) != 2:
				c.Errorf(Path(p, "scale"), "want [min, max], got %d numbers", len(vs.Scale))
			case !gmath.IsFinite(vs.Scale[0]) || !gmath.IsFinite(vs.Scale[1]) || vs.Scale[0] <= 0 || vs.Scale[0] > vs.Scale[1] || vs.Scale[1] > MaxFloraScale:
				c.Errorf(Path(p, "scale"), "[%v, %v] must satisfy 0 < min <= max <= %d", vs.Scale[0], vs.Scale[1], MaxFloraScale)
			default:
				v.Scale = [2]float32{vs.Scale[0], vs.Scale[1]}
			}
		}
		w.Vegetation = append(w.Vegetation, v)
	}
}

// nameField validates a required name that generated entity names may not shadow.
func nameField(c *Checker, path, name string) {
	switch {
	case name == "":
		c.Errorf(path, "is required")
	case !c.Name(path, name):
	case Reserved(name) != "":
		c.Errorf(path, "name %q starts with %q, which is reserved for generated entities", name, Reserved(name))
	}
}

// cellField validates a required [x, z] within the world's cells (-span to span-1), or
// its vertices (-span to span) when vertex is set.
func cellField(c *Checker, path string, v []int, span int, vertex bool) [2]int32 {
	hi := span - 1
	if vertex {
		hi = span
	}
	switch {
	case v == nil:
		c.Errorf(path, "is required ([x, z] in cells)")
	case len(v) != 2:
		c.Errorf(path, "want [x, z], got %d numbers", len(v))
	default:
		ok := true
		for k, x := range v {
			if x < -span || x > hi {
				c.Errorf(Path(path, k), "%d is outside the world (%d to %d)", x, -span, hi)
				ok = false
			}
		}
		if ok {
			return [2]int32{int32(v[0]), int32(v[1])}
		}
	}
	return [2]int32{}
}

// WorldDeps returns the prefab source files a world is compiled against (relative to the
// assets directory, sorted, unique), for cooking: a changed prefab recompiles the world.
func WorldDeps(src *WorldSource) []string {
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if name == "" || ValidName(name) != nil || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, KindPrefab.Dir()+"/"+name+KindPrefab.Ext())
	}
	for _, s := range src.Scatter {
		add(s.Prefab)
	}
	for _, s := range src.Sites {
		for _, p := range s.Prefabs {
			add(p)
		}
	}
	for _, p := range src.Places {
		add(p.Prefab)
	}
	for _, v := range src.Vegetation {
		add(v.Prefab)
	}
	sort.Strings(out)
	return out
}

func biomeNames(src []BiomeSource) []string {
	out := make([]string, 0, len(src))
	for _, b := range src {
		out = append(out, b.Name)
	}
	return out
}

// Span returns how many cells the world reaches from the origin on each axis: cells run
// from -Span to Span-1.
func (w *World) Span() int { return w.Extent * w.Chunk }

// Cells returns how many whole cells cover meters (at least 1 for a positive length,
// 0 for zero).
func (w *World) Cells(meters float32) int {
	if meters <= 0 {
		return 0
	}
	return max(1, int(math.Ceil(float64(meters)/float64(w.Cell)-1e-6)))
}

// Footprint returns the cells a prefab covers along x and z when turned by rotation
// degrees (90 and 270 swap width and depth).
func (w *World) Footprint(p *Prefab, rotation int) (x, z int) {
	x, z = w.Cells(p.Footprint.X), w.Cells(p.Footprint.Y)
	if rotation == 90 || rotation == 270 {
		x, z = z, x
	}
	return x, z
}

// Biome returns the index of the biome called name, or -1.
func (w *World) Biome(name string) int {
	for i := range w.Biomes {
		if w.Biomes[i].Name == name {
			return i
		}
	}
	return -1
}
