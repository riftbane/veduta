package asset

import (
	"math"
	"sort"
	"strings"

	"github.com/riftbane/veduta/gmath"
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
