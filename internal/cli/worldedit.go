package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/asset/cook"
	"github.com/riftbane/veduta/v2/inspect"
	"github.com/riftbane/veduta/v2/mcp"
	"github.com/riftbane/veduta/v2/world"
)

// MaxEditRadius bounds the region world_terrain and world_vegetation describe: a sea may
// reach far beyond it, the report covers the cells around its centre.
const MaxEditRadius = 96

// fnum writes a float32 as JSON, shortest form.
func fnum(v float32) string { return strconv.FormatFloat(float64(v), 'g', -1, 32) }

// WorldTerrainOptions are the flags of world terrain.
type WorldTerrainOptions struct {
	World     string
	Name      string
	Kind      string
	Cell      [2]int32
	Radius    int32
	Height    *float32
	Depth     *float32
	Falloff   *int
	Roughness *float32
	DryRun    bool
}

// GroundStats describes the ground of a region.
type GroundStats struct {
	Min        float64 `json:"min"`    // meters, lowest cell centre
	Max        float64 `json:"max"`    // meters, highest cell centre
	Centre     float64 `json:"centre"` // meters, the cell at the centre
	WaterCells int     `json:"water_cells"`
}

// FeatureReport is a terrain feature as the generator resolved it.
type FeatureReport struct {
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	Cell      [2]int32 `json:"cell"`
	Radius    int32    `json:"radius"`
	Level     float32  `json:"level"` // hill: height added; plain: level; lake, sea: water level (meters)
	Depth     float32  `json:"depth,omitempty"`
	Falloff   int32    `json:"falloff"`
	Roughness float32  `json:"roughness"`
}

// WorldTerrainReport is the report of world terrain.
type WorldTerrainReport struct {
	World         string        `json:"world"`
	Feature       FeatureReport `json:"feature"`
	Area          [4]int32      `json:"area"`   // x, z, width, depth of the cells described
	Chunks        [4]int32      `json:"chunks"` // cx0, cz0, cx1, cz1: chunks whose ground changes
	Before        GroundStats   `json:"before"`
	After         GroundStats   `json:"after"`
	PlacesInWater []string      `json:"places_in_water"` // places that stand in water now and did not before
	SitesRemoved  []string      `json:"sites_removed"`   // sites of the area that the water drops
	SitesAdded    []string      `json:"sites_added"`
	Written       bool          `json:"written"`
	File          string        `json:"file"`
	Sheet         string        `json:"sheet"`
}

// Human prints the change.
func (r *WorldTerrainReport) Human() string {
	var b strings.Builder
	f := r.Feature
	fmt.Fprintf(&b, "%s: %s %s at [%d, %d] radius %d", r.World, f.Kind, f.Name, f.Cell[0], f.Cell[1], f.Radius)
	if r.Written {
		fmt.Fprintf(&b, " — written to %s\n", r.File)
	} else {
		b.WriteString(" — not written (dry run)\n")
	}
	fmt.Fprintf(&b, "  ground %.2f..%.2f m (centre %.2f) → %.2f..%.2f m (centre %.2f); water cells %d → %d\n",
		r.Before.Min, r.Before.Max, r.Before.Centre, r.After.Min, r.After.Max, r.After.Centre, r.Before.WaterCells, r.After.WaterCells)
	if len(r.PlacesInWater) > 0 {
		fmt.Fprintf(&b, "  places now in water: %s\n", strings.Join(r.PlacesInWater, ", "))
	}
	if len(r.SitesRemoved)+len(r.SitesAdded) > 0 {
		fmt.Fprintf(&b, "  sites removed %v, added %v\n", r.SitesRemoved, r.SitesAdded)
	}
	fmt.Fprintf(&b, "sheet: %s\n", r.Sheet)
	return b.String()
}

// featureElem writes a feature as one line of JSON, optional fields only when given.
func featureElem(o *WorldTerrainOptions) string {
	var b strings.Builder
	fmt.Fprintf(&b, `{ "name": %q, "kind": %q, "cell": [%d, %d], "radius": %d`, o.Name, o.Kind, o.Cell[0], o.Cell[1], o.Radius)
	if o.Height != nil {
		fmt.Fprintf(&b, `, "height": %s`, fnum(*o.Height))
	}
	if o.Depth != nil {
		fmt.Fprintf(&b, `, "depth": %s`, fnum(*o.Depth))
	}
	if o.Falloff != nil {
		fmt.Fprintf(&b, `, "falloff": %d`, *o.Falloff)
	}
	if o.Roughness != nil {
		fmt.Fprintf(&b, `, "roughness": %s`, fnum(*o.Roughness))
	}
	b.WriteString(" }")
	return b.String()
}

// groundStats summarizes a region's cells.
func groundStats(reg *world.Region, centre [2]int32) GroundStats {
	st := GroundStats{Min: math.Inf(1), Max: math.Inf(-1)}
	for i, h := range reg.Heights {
		m := float64(h) / 1000
		st.Min, st.Max = min(st.Min, m), max(st.Max, m)
		if reg.Water[i] != world.NoWater {
			st.WaterCells++
		}
	}
	if len(reg.Heights) == 0 {
		st.Min, st.Max = 0, 0
	}
	if reg.Rect.Contains(centre[0], centre[1]) {
		st.Centre = float64(reg.Heights[(centre[1]-reg.Rect.Z)*reg.Rect.W+centre[0]-reg.Rect.X]) / 1000
	}
	return st
}

// WorldTerrain adds a hill, a plain, a lake or a sea to a world: the feature is appended
// to the world's features when the result compiles, and the report compares the ground
// before and after.
func (s *Session) WorldTerrain(o WorldTerrainOptions) (*WorldTerrainReport, error) {
	g0, lib, err := s.worldGen(o.World)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(asset.FeatureKinds, o.Kind) {
		return nil, usagef("world terrain: kind %q is not one of %v", o.Kind, asset.FeatureKinds)
	}
	w1, err := s.editWorld(o.World, "features", featureElem(&o), "", lib, true)
	if err != nil {
		return nil, err
	}
	g1 := world.New(w1, lib.Prefab)
	var f *asset.Feature
	for i := range w1.Features {
		if w1.Features[i].Name == o.Name {
			f = &w1.Features[i]
		}
	}
	level, _ := g1.FeatureLevel(o.Name)
	rep := &WorldTerrainReport{World: o.World, PlacesInWater: []string{}, SitesRemoved: []string{}, SitesAdded: []string{},
		Feature: FeatureReport{Name: f.Name, Kind: f.Kind, Cell: f.Cell, Radius: f.Radius, Level: level, Falloff: f.Falloff, Roughness: f.Roughness}}
	if f.IsWater() {
		rep.Feature.Depth = f.Depth
	}
	reach := g1.FeatureReach(o.Name)
	cx0, cz0 := g1.ChunkOf(f.Cell[0]-reach, f.Cell[1]-reach)
	cx1, cz1 := g1.ChunkOf(f.Cell[0]+reach, f.Cell[1]+reach)
	rep.Chunks = [4]int32{cx0, cz0, cx1, cz1}
	radius := min(reach, MaxEditRadius)
	before, after := g0.Region(f.Cell, radius), g1.Region(f.Cell, radius)
	rep.Area = [4]int32{after.Rect.X, after.Rect.Z, after.Rect.W, after.Rect.D}
	rep.Before, rep.After = groundStats(before, f.Cell), groundStats(after, f.Cell)
	wet := func(g *world.Gen, p asset.Place) bool {
		pf := lib.Prefabs[p.Prefab]
		if pf == nil {
			return false
		}
		for _, is := range g.Validate(pf, p.Cell, p.Rotation, p.Name) {
			if is.Code == world.CodePlaceWater {
				return true
			}
		}
		return false
	}
	for _, p := range w1.Places {
		if wet(g1, p) && !wet(g0, p) {
			rep.PlacesInWater = append(rep.PlacesInWater, p.Name)
		}
	}
	sites := func(r *world.Region) map[string]bool {
		out := map[string]bool{}
		for _, st := range r.Structs {
			if st.Place == "" {
				out[st.Key] = true
			}
		}
		return out
	}
	s0, s1 := sites(before), sites(after)
	for _, k := range asset.Names(s0) {
		if !s1[k] {
			rep.SitesRemoved = append(rep.SitesRemoved, k)
		}
	}
	for _, k := range asset.Names(s1) {
		if !s0[k] {
			rep.SitesAdded = append(rep.SitesAdded, k)
		}
	}
	rep.File = s.worldFile(o.World)
	if rep.Sheet, err = s.worldSheet(g1, o.World, f.Cell, radius); err != nil {
		return nil, err
	}
	if !o.DryRun {
		if _, err := s.editWorld(o.World, "features", featureElem(&o), "", lib, false); err != nil {
			return nil, err
		}
		rep.Written = true
	}
	return rep, nil
}

// worldFile returns the world's source path relative to the project.
func (s *Session) worldFile(name string) string {
	return cook.SourcePath(s.Root, s.Project, asset.KindWorld, name)
}

// worldSheet draws the map around centre into out/<world>.map.png.
func (s *Session) worldSheet(g *world.Gen, name string, centre [2]int32, radius int32) (string, error) {
	p := s.Out(name + ".map.png")
	if err := writePNGFile(p, inspect.MapImage(g, centre, max(radius, 8))); err != nil {
		return "", err
	}
	return s.Rel(p), nil
}

// WorldVegetationOptions are the flags of world vegetation.
type WorldVegetationOptions struct {
	World   string
	Name    string
	Prefab  string
	Model   string
	Density float32
	Biomes  []string
	Cell    *[2]int32
	Radius  int32
	Scale   []float32
	DryRun  bool
}

// WorldVegetationReport is the report of world vegetation.
type WorldVegetationReport struct {
	World     string   `json:"world"`
	Name      string   `json:"name"`
	Prefab    string   `json:"prefab,omitempty"`
	Model     string   `json:"model,omitempty"`
	Area      [4]int32 `json:"area"`   // x, z, width, depth of the cells described
	Plants    int      `json:"plants"` // plants of the rule in the area
	Triangles int      `json:"triangles"`
	// MaxChunkTriangles is the most triangles the rule adds to one chunk of the area at
	// full detail.
	MaxChunkTriangles int      `json:"max_chunk_triangles"`
	DrawDistance      float32  `json:"draw_distance,omitempty"` // the flora model's
	Warnings          []string `json:"warnings"`
	Written           bool     `json:"written"`
	File              string   `json:"file"`
	Sheet             string   `json:"sheet"`
}

// Human prints the rule's effect.
func (r *WorldVegetationReport) Human() string {
	var b strings.Builder
	what := r.Prefab
	if r.Model != "" {
		what = r.Model + " (flora)"
	}
	fmt.Fprintf(&b, "%s: vegetation %s of %s: %d plants, %d triangles in cells [%d, %d] %d×%d (at most %d in a chunk)",
		r.World, r.Name, what, r.Plants, r.Triangles, r.Area[0], r.Area[1], r.Area[2], r.Area[3], r.MaxChunkTriangles)
	if r.Written {
		fmt.Fprintf(&b, " — written to %s\n", r.File)
	} else {
		b.WriteString(" — not written (dry run)\n")
	}
	for _, w := range r.Warnings {
		fmt.Fprintf(&b, "  warning: %s\n", w)
	}
	fmt.Fprintf(&b, "sheet: %s\n", r.Sheet)
	return b.String()
}

// vegetationElem writes a vegetation rule as one line of JSON.
func vegetationElem(o *WorldVegetationOptions) string {
	var b strings.Builder
	fmt.Fprintf(&b, `{ "name": %q`, o.Name)
	if o.Prefab != "" {
		fmt.Fprintf(&b, `, "prefab": %q`, o.Prefab)
	}
	if o.Model != "" {
		fmt.Fprintf(&b, `, "model": %q`, o.Model)
	}
	fmt.Fprintf(&b, `, "density": %s`, fnum(o.Density))
	if len(o.Biomes) > 0 {
		q := make([]string, len(o.Biomes))
		for i, bi := range o.Biomes {
			q[i] = strconv.Quote(bi)
		}
		fmt.Fprintf(&b, `, "biomes": [%s]`, strings.Join(q, ", "))
	}
	if o.Cell != nil {
		fmt.Fprintf(&b, `, "cell": [%d, %d], "radius": %d`, o.Cell[0], o.Cell[1], o.Radius)
	} else if o.Radius != 0 {
		fmt.Fprintf(&b, `, "radius": %d`, o.Radius)
	}
	if len(o.Scale) > 0 {
		q := make([]string, len(o.Scale))
		for i, v := range o.Scale {
			q[i] = fnum(v)
		}
		fmt.Fprintf(&b, `, "scale": [%s]`, strings.Join(q, ", "))
	}
	b.WriteString(" }")
	return b.String()
}

// WorldVegetation adds a vegetation rule to a world: trees (a prefab) or flora (a model),
// everywhere or in a round area. The rule is appended when the result compiles; the
// report counts the plants it makes around its area and what they cost to draw.
func (s *Session) WorldVegetation(o WorldVegetationOptions) (*WorldVegetationReport, error) {
	_, lib, err := s.worldGen(o.World)
	if err != nil {
		return nil, err
	}
	switch {
	case o.Prefab != "" && lib.Prefabs[o.Prefab] == nil:
		return nil, usagef("world vegetation: unknown prefab %q (have: %s)", o.Prefab, strings.Join(asset.Names(lib.Prefabs), ", "))
	case o.Model != "" && lib.Models[o.Model] == nil:
		return nil, usagef("world vegetation: unknown model %q (have: %s)", o.Model, strings.Join(asset.Names(lib.Models), ", "))
	}
	w1, err := s.editWorld(o.World, "vegetation", vegetationElem(&o), "", lib, true)
	if err != nil {
		return nil, err
	}
	g1 := world.New(w1, lib.Prefab)
	idx := -1
	for i := range w1.Vegetation {
		if w1.Vegetation[i].Name == o.Name {
			idx = i
		}
	}
	v := w1.Vegetation[idx]
	rep := &WorldVegetationReport{World: o.World, Name: v.Name, Prefab: v.Prefab, Model: v.Model, Warnings: []string{}, File: s.worldFile(o.World)}
	centre, radius := [2]int32{0, 0}, int32(inspect.MapRadius)
	if v.Area {
		centre, radius = v.Cell, min(v.Radius+v.Radius/4+1, MaxEditRadius)
	}
	reg := g1.Region(centre, radius)
	rep.Area = [4]int32{reg.Rect.X, reg.Rect.Z, reg.Rect.W, reg.Rect.D}
	per := 0 // triangles per plant at full detail
	if v.Model != "" {
		m := lib.Models[v.Model]
		per = m.Triangles(0)
		rep.Plants = reg.Flora[idx]
		rep.DrawDistance = m.DrawDistance
		if m.DrawDistance == 0 {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("model %q has no draw_distance: every loaded chunk draws all its plants; give it one (and a lod level) in assets/models/%s.model.json", v.Model, v.Model))
		}
		if per > 16 {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("each plant of %q has %d triangles: flora should keep to a handful (a lathe cone of 3 or 4 segments)", v.Model, per))
		}
	} else {
		for _, e := range lib.Prefabs[v.Prefab].Entities {
			if m := lib.Models[e.Model]; m != nil {
				per += m.Triangles(0)
			}
		}
		for _, st := range reg.Scatter {
			if st.Vegetation == v.Name {
				rep.Plants++
			}
		}
	}
	rep.Triangles = rep.Plants * per
	chunks := map[[2]int32]int{}
	for z := reg.Rect.Z; z < reg.Rect.Z+reg.Rect.D; z += int32(w1.Chunk) {
		for x := reg.Rect.X; x < reg.Rect.X+reg.Rect.W; x += int32(w1.Chunk) {
			cx, cz := g1.ChunkOf(x, z)
			if _, done := chunks[[2]int32{cx, cz}]; done {
				continue
			}
			c := g1.Chunk(cx, cz)
			n := 0
			for _, f := range c.Flora {
				if f.Rule == idx {
					n++
				}
			}
			for _, st := range c.Scatter {
				if st.Vegetation == v.Name {
					n++
				}
			}
			chunks[[2]int32{cx, cz}] = n * per
			rep.MaxChunkTriangles = max(rep.MaxChunkTriangles, n*per)
		}
	}
	if rep.MaxChunkTriangles > world.Budget/2 {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf("a chunk gets %d triangles from this rule, more than half the console's budget of %d: lower density or use a simpler model", rep.MaxChunkTriangles, world.Budget))
	}
	if rep.Plants == 0 {
		rep.Warnings = append(rep.Warnings, "no plant in the area: check biomes, water and the structures' footprints")
	}
	if rep.Sheet, err = s.worldSheet(g1, o.World, centre, radius); err != nil {
		return nil, err
	}
	if !o.DryRun {
		if _, err := s.editWorld(o.World, "vegetation", vegetationElem(&o), "", lib, false); err != nil {
			return nil, err
		}
		rep.Written = true
	}
	return rep, nil
}

func parseFloatList(s string) ([]float32, error) {
	if s == "" {
		return nil, nil
	}
	var out []float32
	for _, part := range strings.Split(s, ",") {
		v, err := strconv.ParseFloat(strings.TrimSpace(part), 32)
		if err != nil {
			return nil, usagef("want numbers separated by commas, got %q", s)
		}
		out = append(out, float32(v))
	}
	return out, nil
}

func init() {
	prev := mcpExtraTools
	mcpExtraTools = func(m *mcpServer) []mcp.Tool {
		cell := func(desc string) map[string]any {
			return map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "minItems": 2, "maxItems": 2, "description": desc}
		}
		number := func(desc string) map[string]any { return map[string]any{"type": "number", "description": desc} }
		return append(prev(m),
			mcp.Tool{
				Name:        "world_terrain",
				Description: "Shape a world's ground: add a hill (height meters at the top, negative digs a hollow), a plain (levels the ground to height, default the ground at the centre: flatten before placing a town), a lake or a sea (water at height, default the ground at the centre for a lake or the sea level for a sea, depth meters deep, a dry rim and a shore). Features apply in file order around a centre vertex with a radius in cells; the edge wanders by roughness. The feature is appended to the world file's features when valid; the report gives the ground before and after (min, max, centre, water cells), the places now in water, the sites the water removes, and one map image (relief, water, outlines). dry_run only reports. Remove with world_remove.",
				InputSchema: schema(map[string]any{
					"world":     str("world name"),
					"name":      str("name of the feature (unique among places, features and vegetation; not starting with chunk_, site_ or place_)"),
					"kind":      enum("feature kind", asset.FeatureKinds...),
					"cell":      cell("centre vertex [x, z] (the min corner of cell [x, z])"),
					"radius":    num("cells from the centre to the edge (1 to 16384); for a lake or a sea, to the water's edge"),
					"height":    number("hill: meters added at the top (required); plain: level; lake, sea: water level (meters)"),
					"depth":     number("lake, sea: meters from the water to the bottom at the centre (default lake 2, sea 8)"),
					"falloff":   num("hill, plain: cells over which the edge blends (hill default radius, plain radius/3); lake, sea: shore width"),
					"roughness": number("0 to 1: how far the edge wanders from a circle (default hill 0.2, plain 0.1, lake and sea 0.3)"),
					"dry_run":   boolean("report without writing the file"),
				}, "world", "name", "kind", "cell", "radius"),
				Handler: m.sessionTool(func(ctx context.Context, s *Session, args json.RawMessage) (*mcp.Result, error) {
					var a struct {
						World     string   `json:"world"`
						Name      string   `json:"name"`
						Kind      string   `json:"kind"`
						Cell      [2]int32 `json:"cell"`
						Radius    int32    `json:"radius"`
						Height    *float32 `json:"height"`
						Depth     *float32 `json:"depth"`
						Falloff   *int     `json:"falloff"`
						Roughness *float32 `json:"roughness"`
						DryRun    bool     `json:"dry_run"`
					}
					if err := mcp.Strict(args, &a); err != nil {
						return nil, err
					}
					rep, err := s.WorldTerrain(WorldTerrainOptions{World: a.World, Name: a.Name, Kind: a.Kind, Cell: a.Cell, Radius: a.Radius,
						Height: a.Height, Depth: a.Depth, Falloff: a.Falloff, Roughness: a.Roughness, DryRun: a.DryRun})
					if err != nil {
						return nil, err
					}
					return textResult(rep, fitPNG(readPNG(s, rep.Sheet), mcpSheetMaxW, mcpSheetMaxH)), nil
				}),
			},
			mcp.Tool{
				Name:        "world_vegetation",
				Description: "Plant a world: trees and other one-cell prefabs (entities that collide) or flora models (grass, flowers: drawn with the ground, no entities, thinned with distance by the model's lod levels and cut at its draw_distance), at a density (share of cells), optionally on some biomes and inside a round area (cell, radius). The rule is appended to the world file's vegetation when valid; the report counts the plants in the area, their triangles (the most in one chunk) and warns about the console budget, with one map image. dry_run only reports. Remove with world_remove.",
				InputSchema: schema(map[string]any{
					"world":   str("world name"),
					"name":    str("name of the rule (unique among places, features and vegetation)"),
					"prefab":  str("one-cell prefab to plant (trees); give prefab or model"),
					"model":   str("flora model to plant (grass, flowers); give prefab or model"),
					"density": number("share of the cells that get a plant, more than 0 and at most 1"),
					"biomes":  strList("biomes to plant on (default any)"),
					"cell":    cell("centre [x, z] of the area (with radius; default the whole world)"),
					"radius":  num("cells from the centre (with cell)"),
					"scale":   map[string]any{"type": "array", "items": map[string]any{"type": "number"}, "minItems": 2, "maxItems": 2, "description": "model only: [min, max] scale of each plant (default [0.8, 1.2])"},
					"dry_run": boolean("report without writing the file"),
				}, "world", "name", "density"),
				Handler: m.sessionTool(func(ctx context.Context, s *Session, args json.RawMessage) (*mcp.Result, error) {
					var a struct {
						World   string    `json:"world"`
						Name    string    `json:"name"`
						Prefab  string    `json:"prefab"`
						Model   string    `json:"model"`
						Density float32   `json:"density"`
						Biomes  []string  `json:"biomes"`
						Cell    *[2]int32 `json:"cell"`
						Radius  int32     `json:"radius"`
						Scale   []float32 `json:"scale"`
						DryRun  bool      `json:"dry_run"`
					}
					if err := mcp.Strict(args, &a); err != nil {
						return nil, err
					}
					rep, err := s.WorldVegetation(WorldVegetationOptions{World: a.World, Name: a.Name, Prefab: a.Prefab, Model: a.Model, Density: a.Density,
						Biomes: a.Biomes, Cell: a.Cell, Radius: a.Radius, Scale: a.Scale, DryRun: a.DryRun})
					if err != nil {
						return nil, err
					}
					return textResult(rep, fitPNG(readPNG(s, rep.Sheet), mcpSheetMaxW, mcpSheetMaxH)), nil
				}),
			},
		)
	}
}
