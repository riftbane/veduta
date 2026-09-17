package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/asset/cook"
	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/inspect"
	"github.com/riftbane/veduta/v2/mcp"
	"github.com/riftbane/veduta/v2/world"
)

// MaxMapRadius bounds world_map: a 481×481 cell region fits one pixel per cell.
const MaxMapRadius = 240

// worldGen loads the library and prepares the generator of world name.
func (s *Session) worldGen(name string) (*world.Gen, *asset.Library, error) {
	if err := asset.ValidName(name); err != nil {
		return nil, nil, usagef("world: %v", err)
	}
	lib, errs, err := s.PartialLibrary()
	if err != nil {
		return nil, nil, err
	}
	src := cook.SourcePath(s.Root, s.Project, asset.KindWorld, name)
	var own asset.Errors
	for _, e := range errs {
		if strings.HasSuffix(e.File, src) {
			own = append(own, e)
		}
	}
	if len(own) > 0 {
		return nil, nil, fmt.Errorf("world %s does not compile: %w", name, own)
	}
	w := lib.Worlds[name]
	if w == nil {
		return nil, nil, fmt.Errorf("unknown world %q (have: %s)", name, strings.Join(asset.Names(lib.Worlds), ", "))
	}
	return world.New(w, lib.Prefab), lib, nil
}

// StructReport describes a placed structure for reports.
type StructReport struct {
	Key       string   `json:"key"`
	Prefab    string   `json:"prefab"`
	Cell      [2]int32 `json:"cell"`      // min corner of the footprint
	Footprint [2]int32 `json:"footprint"` // cells along x and z
	Rotation  int      `json:"rotation"`
	Tags      []string `json:"tags"`
	Place     string   `json:"place,omitempty"` // the place's name
	Site      string   `json:"site,omitempty"`  // the site rule's tag
}

func structReport(g *world.Gen, s *world.Struct) StructReport {
	r := StructReport{Key: s.Key, Prefab: s.Prefab.Name, Cell: [2]int32{s.Rect.X, s.Rect.Z}, Footprint: [2]int32{s.Rect.W, s.Rect.D}, Rotation: s.Rotation, Tags: s.Tags, Place: s.Place}
	if r.Tags == nil {
		r.Tags = []string{}
	}
	if s.Site >= 0 && s.Site < len(g.W.Sites) {
		r.Site = g.W.Sites[s.Site].Tag
	}
	return r
}

// WorldMapReport is the report of world map.
type WorldMapReport struct {
	World   string         `json:"world"`
	Center  [2]int32       `json:"center"`
	Radius  int32          `json:"radius"`
	Rect    [4]int32       `json:"rect"` // x, z, width, depth in cells
	Cells   int            `json:"cells"`
	Biomes  []BiomeShare   `json:"biomes"`
	Places  []StructReport `json:"places"`
	Sites   []StructReport `json:"sites"`
	Scatter int            `json:"scatter"`
	Missing []string       `json:"missing_prefabs"`
	Sheet   string         `json:"sheet"`
	// Ground is the region's lowest and highest cell centre and its water cells.
	Ground     GroundStats         `json:"ground"`
	Features   []FeatureReport     `json:"features"`   // features whose area touches the region
	Vegetation []VegetationSummary `json:"vegetation"` // every rule, with its plants in the region
}

// VegetationSummary is a vegetation rule and its plants in a region.
type VegetationSummary struct {
	Name   string    `json:"name"`
	Prefab string    `json:"prefab,omitempty"`
	Model  string    `json:"model,omitempty"`
	Area   *[3]int32 `json:"area,omitempty"` // x, z, radius
	Plants int       `json:"plants"`
}

// BiomeShare is a biome's share of a region.
type BiomeShare struct {
	Name   string  `json:"name"`
	Ground string  `json:"ground"`
	Share  float64 `json:"share"`
}

// Human prints the region's biomes and structures.
func (r *WorldMapReport) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s around [%d, %d] ± %d cells (%d cells)\n", r.World, r.Center[0], r.Center[1], r.Radius, r.Cells)
	for _, bs := range r.Biomes {
		fmt.Fprintf(&b, "  biome %-12s %-12s %3.0f%%\n", bs.Name, bs.Ground, 100*bs.Share)
	}
	for _, p := range r.Places {
		fmt.Fprintf(&b, "  place %-12s %-12s at [%d, %d] %d×%d rot %d\n", p.Place, p.Prefab, p.Cell[0], p.Cell[1], p.Footprint[0], p.Footprint[1], p.Rotation)
	}
	for _, p := range r.Sites {
		fmt.Fprintf(&b, "  site  %-12s %-12s at [%d, %d] %d×%d\n", p.Key, p.Prefab, p.Cell[0], p.Cell[1], p.Footprint[0], p.Footprint[1])
	}
	fmt.Fprintf(&b, "  ground %.2f..%.2f m, %d water cells\n", r.Ground.Min, r.Ground.Max, r.Ground.WaterCells)
	for _, f := range r.Features {
		fmt.Fprintf(&b, "  feature %-12s %-6s at [%d, %d] radius %d level %.2f\n", f.Name, f.Kind, f.Cell[0], f.Cell[1], f.Radius, f.Level)
	}
	for _, v := range r.Vegetation {
		fmt.Fprintf(&b, "  vegetation %-12s %s%s: %d plants\n", v.Name, v.Prefab, v.Model, v.Plants)
	}
	fmt.Fprintf(&b, "  scatter: %d\nsheet: %s\n", r.Scatter, r.Sheet)
	return b.String()
}

// WorldMap describes the cells within radius of center and draws them into
// out/<world>.map.png.
func (s *Session) WorldMap(name string, center [2]int32, radius int32) (*WorldMapReport, error) {
	if radius <= 0 {
		radius = inspect.MapRadius
	}
	if radius > MaxMapRadius {
		return nil, usagef("world map: radius %d is more than %d cells", radius, MaxMapRadius)
	}
	g, _, err := s.worldGen(name)
	if err != nil {
		return nil, err
	}
	reg := g.Region(center, radius)
	rep := &WorldMapReport{World: name, Center: center, Radius: radius, Rect: [4]int32{reg.Rect.X, reg.Rect.Z, reg.Rect.W, reg.Rect.D},
		Cells: int(reg.Rect.W * reg.Rect.D), Places: []StructReport{}, Sites: []StructReport{}, Scatter: len(reg.Scatter), Missing: g.Missing}
	if rep.Missing == nil {
		rep.Missing = []string{}
	}
	for i, b := range g.W.Biomes {
		rep.Biomes = append(rep.Biomes, BiomeShare{b.Name, b.Ground, reg.Shares[i]})
	}
	for i := range reg.Structs {
		r := structReport(g, &reg.Structs[i])
		if r.Place != "" {
			rep.Places = append(rep.Places, r)
		} else {
			rep.Sites = append(rep.Sites, r)
		}
	}
	rep.Ground = groundStats(reg, center)
	rep.Features, rep.Vegetation = []FeatureReport{}, []VegetationSummary{}
	for _, f := range g.W.Features {
		reach := g.FeatureReach(f.Name)
		if !reg.Rect.Overlaps(world.Rect{X: f.Cell[0] - reach, Z: f.Cell[1] - reach, W: 2 * reach, D: 2 * reach}) {
			continue
		}
		level, _ := g.FeatureLevel(f.Name)
		fr := FeatureReport{Name: f.Name, Kind: f.Kind, Cell: f.Cell, Radius: f.Radius, Level: level, Falloff: f.Falloff, Roughness: f.Roughness}
		if f.IsWater() {
			fr.Depth = f.Depth
		}
		rep.Features = append(rep.Features, fr)
	}
	for i, v := range g.W.Vegetation {
		vs := VegetationSummary{Name: v.Name, Prefab: v.Prefab, Model: v.Model, Plants: reg.Flora[i]}
		if v.Area {
			vs.Area = &[3]int32{v.Cell[0], v.Cell[1], v.Radius}
		}
		for _, st := range reg.Scatter {
			if st.Vegetation == v.Name {
				vs.Plants++
			}
		}
		rep.Vegetation = append(rep.Vegetation, vs)
	}
	img := inspect.MapImage(g, center, radius)
	p := s.Out(name + ".map.png")
	if err := writePNGFile(p, img); err != nil {
		return nil, err
	}
	rep.Sheet = s.Rel(p)
	return rep, nil
}

// WorldQueryReport is the report of world query.
type WorldQueryReport struct {
	World    string          `json:"world"`
	Cell     [2]int32        `json:"cell"`
	Chunk    [2]int32        `json:"chunk"`
	Biome    string          `json:"biome"`
	Ground   string          `json:"ground"`
	Occupant *StructReport   `json:"occupant,omitempty"`
	Nearest  []world.Nearest `json:"nearest"`
	// Height is the ground at the cell's centre in meters; Water the level of the water
	// over it when the centre lies under water.
	Height     float64  `json:"height"`
	Water      *float64 `json:"water,omitempty"`
	Features   []string `json:"features"`   // features that shape the cell's corner, in file order
	Vegetation []string `json:"vegetation"` // vegetation rules with a plant on the cell
}

// WorldQuery describes one cell.
func (s *Session) WorldQuery(name string, cell [2]int32) (*WorldQueryReport, error) {
	g, _, err := s.worldGen(name)
	if err != nil {
		return nil, err
	}
	if !g.Bounds().Contains(cell[0], cell[1]) {
		span := g.W.Span()
		return nil, usagef("world query: cell [%d, %d] is outside %s (cells -%d to %d)", cell[0], cell[1], name, span, span-1)
	}
	info := g.Query(cell[0], cell[1], 4)
	rep := &WorldQueryReport{World: name, Cell: info.Cell, Chunk: info.Chunk, Biome: info.Biome, Ground: info.Ground, Nearest: info.Nearest,
		Features: g.FeaturesAt(cell[0], cell[1]), Vegetation: []string{}}
	if rep.Features == nil {
		rep.Features = []string{}
	}
	c := g.Chunk(info.Chunk[0], info.Chunk[1])
	centre := g.Center(cell[0], cell[1])
	rep.Height = math.Round(float64(c.HeightAt(centre.X, centre.Z, g.W.Cell))*1000) / 1000
	if level, wet := c.WaterAt(centre.X, centre.Z, g.W.Cell); wet {
		l := math.Round(float64(level)*1000) / 1000
		rep.Water = &l
	}
	for _, f := range c.Flora {
		if f.Cell == cell && !slices.Contains(rep.Vegetation, g.W.Vegetation[f.Rule].Name) {
			rep.Vegetation = append(rep.Vegetation, g.W.Vegetation[f.Rule].Name)
		}
	}
	for _, st := range c.Scatter {
		if st.Cell == cell && st.Vegetation != "" {
			rep.Vegetation = append(rep.Vegetation, st.Vegetation)
		}
	}
	if info.Occupant != nil {
		r := structReport(g, info.Occupant)
		rep.Occupant = &r
	}
	return rep, nil
}

// WorldPlaceOptions are the flags of world place.
type WorldPlaceOptions struct {
	World    string
	Prefab   string
	Name     string
	Cell     *[2]int32 // the cell to validate; nil: search from Near
	Near     [2]int32
	Within   int32 // search radius in cells (default 64)
	Rotation int
	DryRun   bool
}

// WorldPlaceReport is the report of world place.
type WorldPlaceReport struct {
	World     string         `json:"world"`
	Name      string         `json:"name"`
	Prefab    string         `json:"prefab"`
	Cell      [2]int32       `json:"cell"`
	Rotation  int            `json:"rotation"`
	Footprint [2]int32       `json:"footprint"`
	Chunk     [2]int32       `json:"chunk"`
	Valid     bool           `json:"valid"`
	Issues    []world.Issue  `json:"issues"`
	Searched  int            `json:"searched,omitempty"` // candidates tried by the search
	Rejected  map[string]int `json:"rejected,omitempty"` // candidates rejected per code
	Displaces []string       `json:"displaces"`          // sites the place removes
	Written   bool           `json:"written"`
	File      string         `json:"file"`
}

// ExitCode is 1 when the place is not valid.
func (r *WorldPlaceReport) ExitCode() int {
	if !r.Valid {
		return 1
	}
	return 0
}

// Human prints the outcome.
func (r *WorldPlaceReport) Human() string {
	var b strings.Builder
	if r.Valid {
		fmt.Fprintf(&b, "%s: %s (%s) at [%d, %d] rot %d, %d×%d cells, chunk [%d, %d]", r.World, r.Name, r.Prefab, r.Cell[0], r.Cell[1], r.Rotation, r.Footprint[0], r.Footprint[1], r.Chunk[0], r.Chunk[1])
		if r.Written {
			fmt.Fprintf(&b, " — written to %s", r.File)
		} else {
			b.WriteString(" — not written (dry run)")
		}
		b.WriteByte('\n')
		if len(r.Displaces) > 0 {
			fmt.Fprintf(&b, "  displaces %s\n", strings.Join(r.Displaces, ", "))
		}
		return b.String()
	}
	fmt.Fprintf(&b, "%s: %s (%s) cannot go at [%d, %d]:\n", r.World, r.Name, r.Prefab, r.Cell[0], r.Cell[1])
	for _, is := range r.Issues {
		fmt.Fprintf(&b, "  %-7s %s %s\n", is.Severity, is.Code, is.Msg)
	}
	if r.Searched > 0 {
		fmt.Fprintf(&b, "  searched %d cells, rejected %v\n", r.Searched, r.Rejected)
	}
	return b.String()
}

// WorldPlace validates (o.Cell) or finds (from o.Near) a cell for a place and, when it
// is valid and not a dry run, appends the place to the world's file.
func (s *Session) WorldPlace(o WorldPlaceOptions) (*WorldPlaceReport, error) {
	g, lib, err := s.worldGen(o.World)
	if err != nil {
		return nil, err
	}
	if err := asset.ValidName(o.Name); err != nil {
		return nil, usagef("world place: %v", err)
	}
	if pre := asset.Reserved(o.Name); pre != "" {
		return nil, usagef("world place: name %q starts with %q, which is reserved for generated entities", o.Name, pre)
	}
	for _, p := range g.W.Places {
		if p.Name == o.Name {
			return nil, usagef("world place: %s already has a place called %q (remove it first)", o.World, o.Name)
		}
	}
	p := lib.Prefabs[o.Prefab]
	if p == nil {
		return nil, usagef("world place: unknown prefab %q (have: %s)", o.Prefab, strings.Join(asset.Names(lib.Prefabs), ", "))
	}
	rot := o.Rotation
	valid := false
	for _, r := range asset.Rotations {
		valid = valid || r == rot
	}
	if !valid {
		return nil, usagef("world place: rotation %d is not one of %v", rot, asset.Rotations)
	}
	fw, fd := g.W.Footprint(p, rot)
	rep := &WorldPlaceReport{World: o.World, Name: o.Name, Prefab: o.Prefab, Rotation: rot, Footprint: [2]int32{int32(fw), int32(fd)}, Issues: []world.Issue{}, Displaces: []string{}}
	rep.File = cook.SourcePath(s.Root, s.Project, asset.KindWorld, o.World)
	if o.Cell != nil {
		rep.Cell = *o.Cell
		rep.Issues = g.Validate(p, rep.Cell, rot, "")
		if rep.Issues == nil {
			rep.Issues = []world.Issue{}
		}
		rep.Valid = len(rep.Issues) == 0
	} else {
		within := o.Within
		if within <= 0 {
			within = inspect.MapRadius
		}
		cell, ok, rejected := g.Solve(p, o.Near, within, rot)
		rep.Cell, rep.Valid, rep.Rejected = cell, ok, rejected
		for _, n := range rejected {
			rep.Searched += n
		}
		if ok {
			rep.Searched++
		} else {
			rep.Cell = o.Near
			rep.Issues = append(rep.Issues, world.Issue{Code: "WORLD_PLACE_NOWHERE", Severity: "error",
				Msg: fmt.Sprintf("no valid cell within %d cells of [%d, %d]: widen the search or move it", within, o.Near[0], o.Near[1])})
		}
	}
	if !rep.Valid {
		return rep, nil
	}
	cx, cz := g.ChunkOf(rep.Cell[0], rep.Cell[1])
	rep.Chunk = [2]int32{cx, cz}
	if d := g.Displaced(p, rep.Cell, rot); d != nil {
		rep.Displaces = d
	}
	if o.DryRun {
		return rep, nil
	}
	place := asset.PlaceSource{Name: o.Name, Prefab: o.Prefab, Cell: []int{int(rep.Cell[0]), int(rep.Cell[1])}, Rotation: rot}
	if err := s.editWorldPlaces(o.World, &place, "", lib); err != nil {
		return nil, err
	}
	rep.Written = true
	return rep, nil
}

// WorldRemoveReport is the report of world remove.
type WorldRemoveReport struct {
	World   string `json:"world"`
	Name    string `json:"name"`
	Kind    string `json:"kind"` // place, feature or vegetation
	Removed bool   `json:"removed"`
	File    string `json:"file"`
}

// WorldRemove deletes a place, a terrain feature or a vegetation rule from the world's
// file.
func (s *Session) WorldRemove(name, what string) (*WorldRemoveReport, error) {
	g, lib, err := s.worldGen(name)
	if err != nil {
		return nil, err
	}
	rep := &WorldRemoveReport{World: name, Name: what, File: s.worldFile(name)}
	for _, p := range g.W.Places {
		if p.Name == what {
			rep.Kind = "place"
		}
	}
	for _, f := range g.W.Features {
		if f.Name == what {
			rep.Kind = "feature"
		}
	}
	for _, v := range g.W.Vegetation {
		if v.Name == what {
			rep.Kind = "vegetation"
		}
	}
	switch rep.Kind {
	case "":
		return nil, usagef("world remove: %s has no place, feature or vegetation rule called %q", name, what)
	case "place":
		err = s.editWorldPlaces(name, nil, what, lib)
	case "feature":
		_, err = s.editWorld(name, "features", "", what, lib, false)
	default:
		_, err = s.editWorld(name, "vegetation", "", what, lib, false)
	}
	if err != nil {
		return nil, err
	}
	rep.Removed = true
	return rep, nil
}

// editWorld rewrites the world's source file with an element appended to (add, JSON
// text) or removed from (the element named remove) its array field, touching no other
// byte, and checks that the result compiles. The compiled world is returned; nothing is
// written when dryRun is set.
func (s *Session) editWorld(name, field, add, remove string, lib *asset.Library, dryRun bool) (*asset.World, error) {
	file := filepath.Join(s.Root, filepath.FromSlash(cook.SourcePath(s.Root, s.Project, asset.KindWorld, name)))
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	out, err := editNamed(data, field, add, remove)
	if err != nil {
		return nil, fmt.Errorf("edit %s: %w", s.Rel(file), err)
	}
	w, err := asset.ParseWorld(s.Rel(file), out, lib.Prefab)
	if err != nil {
		return nil, usagef("%s: %w", name, err)
	}
	if !dryRun {
		if err := writeFileAtomic(file, out); err != nil {
			return nil, err
		}
	}
	return w, nil
}

// editWorldPlaces rewrites the world's source file with a place appended or removed,
// touching no other byte, and checks that the result compiles to what was asked.
func (s *Session) editWorldPlaces(name string, add *asset.PlaceSource, remove string, lib *asset.Library) error {
	elem := placeElem(add)
	w, err := s.editWorld(name, "places", elem, remove, lib, true)
	if err != nil {
		return err
	}
	for _, p := range w.Places {
		if add != nil && p.Name == add.Name && p.Prefab == add.Prefab && int(p.Cell[0]) == add.Cell[0] && int(p.Cell[1]) == add.Cell[1] && p.Rotation == add.Rotation {
			add = nil
		}
		if remove != "" && p.Name == remove {
			return fmt.Errorf("edit %s: place %q is still there", name, remove)
		}
	}
	if add != nil {
		return fmt.Errorf("edit %s: the place did not land in the file", name)
	}
	_, err = s.editWorld(name, "places", elem, remove, lib, false)
	return err
}

// placeElem writes a place as one line of JSON ("" for nil).
func placeElem(p *asset.PlaceSource) string {
	if p == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, `{ "name": %q, "prefab": %q, "cell": [%d, %d]`, p.Name, p.Prefab, p.Cell[0], p.Cell[1])
	if p.Rotation != 0 {
		fmt.Fprintf(&b, `, "rotation": %d`, p.Rotation)
	}
	b.WriteString(" }")
	return b.String()
}

// editPlaces edits the "places" array of a world source in place (see editNamed).
func editPlaces(data []byte, add *asset.PlaceSource, remove string) ([]byte, error) {
	return editNamed(data, "places", placeElem(add), remove)
}

// editNamed edits an array field of a world source in place: add (the JSON text of one
// element) is appended, or the element whose "name" is remove is deleted. Formatting, key
// order and every other byte of the file stay as they are; a missing field is inserted
// after the nearest field that precedes it in the documented key order.
func editNamed(data []byte, field, add, remove string) ([]byte, error) {
	fields, err := manifestFields(data)
	if err != nil {
		return nil, err
	}
	unit := "  " // indentation unit, from the first key
	if l := lineLead(fields[0].lead); strings.TrimLeft(l, "\r\n") != "" {
		unit = strings.TrimLeft(l, "\r\n")
	}
	i := findField(fields, field)
	if i < 0 {
		if add == "" {
			return nil, fmt.Errorf("no %s field", field)
		}
		// Insert after the last field that documents before this one.
		order := worldKeyOrder()
		at := indexOf(order, field)
		after := -1
		for k, f := range fields {
			if idx := indexOf(order, strings.ToLower(f.key)); idx >= 0 && idx < at {
				after = k
			}
		}
		if after < 0 {
			after = len(fields) - 1
		}
		f := fields[after]
		lead := lineLead(f.lead)
		inner := lead + unit
		if !strings.ContainsAny(lead, "\r\n") {
			lead, inner = " ", " "
		}
		colon := f.colon
		if strings.ContainsAny(colon, "\r\n") {
			colon = ": "
		}
		text := "," + lead + `"` + field + `"` + colon + "[" + inner + add + lead + "]"
		out := append([]byte{}, data[:f.valEnd]...)
		out = append(out, text...)
		return append(out, data[f.valEnd:]...), nil
	}
	f := fields[i]
	val := data[f.valStart:f.valEnd]
	if len(val) == 0 || val[0] != '[' {
		return nil, fmt.Errorf("%s is not an array", field)
	}
	elems, err := arrayElems(val)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", field, err)
	}
	fieldLead := lineLead(f.lead)
	inner := fieldLead + unit
	if !strings.ContainsAny(fieldLead, "\r\n") { // an inline array stays on its line
		fieldLead, inner = " ", " "
	}
	var out []byte
	switch {
	case add != "" && len(elems) == 0:
		out = append(append([]byte{}, data[:f.valStart]...), "["+inner+add+fieldLead+"]"...)
		out = append(out, data[f.valEnd:]...)
	case add != "":
		last := elems[len(elems)-1]
		// The new element copies the indentation of the last one.
		prevEnd := 1
		if len(elems) > 1 {
			prevEnd = elems[len(elems)-2][1]
		}
		lead := lineLead(string(val[prevEnd:last[0]]))
		if !strings.ContainsAny(lead, "\r\n") {
			lead = " "
		}
		pos := f.valStart + last[1]
		out = append(append([]byte{}, data[:pos]...), ","+lead+add...)
		out = append(out, data[pos:]...)
	default:
		k := -1
		for j, e := range elems {
			var p struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(val[e[0]:e[1]], &p) == nil && p.Name == remove {
				k = j
			}
		}
		if k < 0 {
			return nil, fmt.Errorf("no element of %s named %q", field, remove)
		}
		start, end := elems[k][0], elems[k][1]
		switch {
		case len(elems) == 1:
			start, end = 1, len(val)-1 // leave "[]"
		case k > 0:
			start = elems[k-1][1] // from the end of the previous element (its comma and white space go too)
		default:
			end = elems[1][0] // up to the next element
		}
		out = append(append([]byte{}, data[:f.valStart+start]...), data[f.valStart+end:]...)
	}
	return out, nil
}

// arrayElems returns the byte spans of the elements of a JSON array text.
func arrayElems(val []byte) ([][2]int, error) {
	dec := json.NewDecoder(bytes.NewReader(val))
	if t, err := dec.Token(); err != nil || t != json.Delim('[') {
		return nil, fmt.Errorf("not an array")
	}
	var out [][2]int
	for dec.More() {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		end := int(dec.InputOffset())
		start := end - len(raw)
		if start < 0 || !bytes.Equal(val[start:end], raw) {
			return nil, fmt.Errorf("cannot locate element %d", len(out))
		}
		out = append(out, [2]int{start, end})
	}
	return out, nil
}

// worldKeyOrder is the order in which docs/world.md documents the fields.
func worldKeyOrder() []string {
	t := reflect.TypeOf(asset.WorldSource{})
	keys := make([]string, 0, t.NumField())
	for i := range t.NumField() {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		keys = append(keys, name)
	}
	return keys
}

// sessionTool wraps a tool handler that needs the project session.
func (m *mcpServer) sessionTool(f func(ctx context.Context, s *Session, args json.RawMessage) (*mcp.Result, error)) func(context.Context, json.RawMessage) (*mcp.Result, error) {
	return func(ctx context.Context, args json.RawMessage) (*mcp.Result, error) {
		s, err := m.session()
		if err != nil {
			return errResult(err), nil
		}
		res, err := f(ctx, s, args)
		if err != nil {
			return errResult(err), nil
		}
		return res, nil
	}
}

func writePNGFile(p string, img *gfx.Image) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer f.Close()
	return img.EncodePNG(f)
}

func writeFileAtomic(p string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*.json")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), p)
}

func parseCellFlag(s string) (*[2]int32, error) {
	if s == "" {
		return nil, nil
	}
	var x, z int32
	if _, err := fmt.Sscanf(strings.ReplaceAll(s, " ", ""), "%d,%d", &x, &z); err != nil {
		return nil, usagef("want x,z (whole cells), got %q", s)
	}
	return &[2]int32{x, z}, nil
}

func init() {
	register(command{
		name:    "world",
		usage:   "world map|query|place|terrain|vegetation|remove NAME [flags]",
		summary: "describe and edit a world: its map (--center x,z --radius N), one cell (query --cell x,z), a landmark placed where the rules allow (place --prefab P --name N [--cell x,z | --near x,z --within N] [--rotation R] [--dry-run]), a hill, plain, lake or sea (terrain --kind K --name N --cell x,z --radius R [--height H] [--depth D] [--falloff F] [--roughness X] [--dry-run]), trees or flora (vegetation --name N --prefab P|--model M --density D [--biomes a,b] [--cell x,z --radius R] [--scale min,max] [--dry-run]), or any of them removed (remove --name N)",
		project: true,
		run: func(env *Env, s *Session, args []string) (any, error) {
			if len(args) < 2 {
				return nil, usagef("world: want map|query|place|terrain|vegetation|remove NAME")
			}
			sub, name, rest := args[0], args[1], args[2:]
			fs := newFlags("world "+sub, env.Stderr)
			switch sub {
			case "map":
				center := fs.String("center", "0,0", "centre cell x,z")
				radius := fs.Int("radius", inspect.MapRadius, "half-size in cells")
				if err := parseFlags(fs, rest); err != nil {
					return nil, err
				}
				c, err := parseCellFlag(*center)
				if err != nil {
					return nil, err
				}
				return s.WorldMap(name, *c, int32(*radius))
			case "query":
				cell := fs.String("cell", "", "cell x,z")
				if err := parseFlags(fs, rest); err != nil {
					return nil, err
				}
				c, err := parseCellFlag(*cell)
				if err != nil || c == nil {
					return nil, usagef("world query: --cell x,z is required")
				}
				return s.WorldQuery(name, *c)
			case "place":
				var o WorldPlaceOptions
				var cell, near string
				o.World = name
				fs.StringVar(&o.Prefab, "prefab", "", "prefab to place")
				fs.StringVar(&o.Name, "name", "", "name of the place")
				fs.StringVar(&cell, "cell", "", "cell x,z to validate (default: search from --near)")
				fs.StringVar(&near, "near", "0,0", "cell x,z the search starts from")
				within := fs.Int("within", inspect.MapRadius, "search radius in cells")
				fs.IntVar(&o.Rotation, "rotation", 0, "0, 90, 180 or 270")
				fs.BoolVar(&o.DryRun, "dry-run", false, "validate or search without writing")
				if err := parseFlags(fs, rest); err != nil {
					return nil, err
				}
				if o.Prefab == "" || o.Name == "" {
					return nil, usagef("world place: --prefab and --name are required")
				}
				var err error
				if o.Cell, err = parseCellFlag(cell); err != nil {
					return nil, err
				}
				n, err := parseCellFlag(near)
				if err != nil {
					return nil, err
				}
				o.Near, o.Within = *n, int32(*within)
				return s.WorldPlace(o)
			case "terrain":
				o := WorldTerrainOptions{World: name}
				var cell, height, depth, roughness string
				falloff := fs.Int("falloff", -1, "cells of the edge's blend (hill, plain) or of the shore (lake, sea)")
				fs.StringVar(&o.Kind, "kind", "", "hill, plain, lake or sea")
				fs.StringVar(&o.Name, "name", "", "name of the feature")
				fs.StringVar(&cell, "cell", "", "centre vertex x,z")
				radius := fs.Int("radius", 0, "cells from the centre to the edge")
				fs.StringVar(&height, "height", "", "hill: meters at the top; plain: level; lake, sea: water level")
				fs.StringVar(&depth, "depth", "", "lake, sea: meters deep at the centre")
				fs.StringVar(&roughness, "roughness", "", "0 to 1: how far the edge wanders")
				fs.BoolVar(&o.DryRun, "dry-run", false, "report without writing")
				if err := parseFlags(fs, rest); err != nil {
					return nil, err
				}
				c, err := parseCellFlag(cell)
				if err != nil || c == nil || o.Kind == "" || o.Name == "" || *radius == 0 {
					return nil, usagef("world terrain: --kind, --name, --cell x,z and --radius are required")
				}
				o.Cell, o.Radius = *c, int32(*radius)
				for _, f := range []struct {
					flag string
					dst  **float32
				}{{height, &o.Height}, {depth, &o.Depth}, {roughness, &o.Roughness}} {
					if f.flag != "" {
						v, err := strconv.ParseFloat(f.flag, 32)
						if err != nil {
							return nil, usagef("world terrain: %q is not a number", f.flag)
						}
						x := float32(v)
						*f.dst = &x
					}
				}
				if *falloff >= 0 {
					o.Falloff = falloff
				}
				return s.WorldTerrain(o)
			case "vegetation":
				o := WorldVegetationOptions{World: name}
				var cell, biomes, scale string
				fs.StringVar(&o.Name, "name", "", "name of the rule")
				fs.StringVar(&o.Prefab, "prefab", "", "one-cell prefab to plant (trees)")
				fs.StringVar(&o.Model, "model", "", "flora model to plant (grass, flowers)")
				density := fs.Float64("density", 0, "share of the cells that get a plant")
				fs.StringVar(&biomes, "biomes", "", "biomes to plant on, comma-separated")
				fs.StringVar(&cell, "cell", "", "centre x,z of the area")
				radius := fs.Int("radius", 0, "cells from the centre")
				fs.StringVar(&scale, "scale", "", "model: min,max scale")
				fs.BoolVar(&o.DryRun, "dry-run", false, "report without writing")
				if err := parseFlags(fs, rest); err != nil {
					return nil, err
				}
				if o.Name == "" || *density == 0 {
					return nil, usagef("world vegetation: --name, --density and --prefab or --model are required")
				}
				var err error
				if o.Cell, err = parseCellFlag(cell); err != nil {
					return nil, err
				}
				if o.Scale, err = parseFloatList(scale); err != nil {
					return nil, err
				}
				if biomes != "" {
					o.Biomes = strings.Split(biomes, ",")
				}
				o.Density, o.Radius = float32(*density), int32(*radius)
				return s.WorldVegetation(o)
			case "remove":
				place := fs.String("name", "", "name of the place, feature or vegetation rule to remove")
				if err := parseFlags(fs, rest); err != nil {
					return nil, err
				}
				if *place == "" {
					return nil, usagef("world remove: --name is required")
				}
				return s.WorldRemove(name, *place)
			}
			return nil, usagef("world: unknown subcommand %q (want map, query, place, terrain, vegetation or remove)", sub)
		},
	})
	prev := mcpExtraTools
	mcpExtraTools = func(m *mcpServer) []mcp.Tool {
		cell := func(desc string) map[string]any {
			return map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "minItems": 2, "maxItems": 2, "description": desc}
		}
		return append(prev(m),
			mcp.Tool{
				Name:        "world_map",
				Description: "Describe a world around a cell: biome shares, the ground's lowest and highest point and its water cells, the features (hills, plains, lakes, seas) touching the region with their resolved levels, every vegetation rule with its plants in the region, the places and sites with their cell rectangles, the number of scattered prefabs, and one image of the map (biomes shaded by the relief, water in blue, features and vegetation areas as labelled circles, sites in orange, places in red, north up). Cells are integers; the world's cell size is in its file.",
				InputSchema: schema(map[string]any{
					"world":  str("world name"),
					"center": cell("centre cell [x, z] (default [0, 0])"),
					"radius": num(fmt.Sprintf("half-size in cells (default %d, max %d)", inspect.MapRadius, MaxMapRadius)),
				}, "world"),
				Handler: m.sessionTool(func(ctx context.Context, s *Session, args json.RawMessage) (*mcp.Result, error) {
					var a struct {
						World  string   `json:"world"`
						Center [2]int32 `json:"center"`
						Radius int      `json:"radius"`
					}
					if err := mcp.Strict(args, &a); err != nil {
						return nil, err
					}
					rep, err := s.WorldMap(a.World, a.Center, int32(a.Radius))
					if err != nil {
						return nil, err
					}
					return textResult(rep, fitPNG(readPNG(s, rep.Sheet), mcpSheetMaxW, mcpSheetMaxH)), nil
				}),
			},
			mcp.Tool{
				Name:        "world_query",
				Description: "What is at a cell of a world: its biome and ground material, its chunk, the ground's height at its centre and the water level when it is under water, the features shaping it, the vegetation rules with a plant on it, the place, site or scattered prefab occupying it (with its cell rectangle), and the nearest place of each name and site of each tag with the free cells between. Exact and cheap: ask before and after placing.",
				InputSchema: schema(map[string]any{"world": str("world name"), "cell": cell("cell [x, z]")}, "world", "cell"),
				Handler: m.sessionTool(func(ctx context.Context, s *Session, args json.RawMessage) (*mcp.Result, error) {
					var a struct {
						World string   `json:"world"`
						Cell  [2]int32 `json:"cell"`
					}
					if err := mcp.Strict(args, &a); err != nil {
						return nil, err
					}
					rep, err := s.WorldQuery(a.World, a.Cell)
					if err != nil {
						return nil, err
					}
					return textResult(rep), nil
				}),
			},
			mcp.Tool{
				Name:        "world_place",
				Description: "Put a landmark (a prefab) in a world. With cell: validate that cell (inside the world, allowed biome, no overlap with other places, min_distance rules kept). Without: search outward from near (rings up to within cells, east first then clockwise) for the first valid cell. A valid place is appended to the world file's places and returned with its footprint, chunk and the sites it displaces; an invalid one is refused with every reason and nothing is written. dry_run only reports.",
				InputSchema: schema(map[string]any{
					"world":    str("world name"),
					"prefab":   str("prefab name"),
					"name":     str("name of the place (valid name, not starting with chunk_, site_ or place_)"),
					"cell":     cell("cell [x, z] of the footprint's min corner to validate"),
					"near":     cell("search start [x, z] when cell is not given (default [0, 0])"),
					"within":   num(fmt.Sprintf("search radius in cells (default %d)", inspect.MapRadius)),
					"rotation": num("0, 90, 180 or 270 degrees"),
					"dry_run":  boolean("validate or search without writing the file"),
				}, "world", "prefab", "name"),
				Handler: m.sessionTool(func(ctx context.Context, s *Session, args json.RawMessage) (*mcp.Result, error) {
					var a struct {
						World    string    `json:"world"`
						Prefab   string    `json:"prefab"`
						Name     string    `json:"name"`
						Cell     *[2]int32 `json:"cell"`
						Near     [2]int32  `json:"near"`
						Within   int       `json:"within"`
						Rotation int       `json:"rotation"`
						DryRun   bool      `json:"dry_run"`
					}
					if err := mcp.Strict(args, &a); err != nil {
						return nil, err
					}
					rep, err := s.WorldPlace(WorldPlaceOptions{World: a.World, Prefab: a.Prefab, Name: a.Name, Cell: a.Cell, Near: a.Near, Within: int32(a.Within), Rotation: a.Rotation, DryRun: a.DryRun})
					if err != nil {
						return nil, err
					}
					res := textResult(rep)
					res.IsError = !rep.Valid
					return res, nil
				}),
			},
			mcp.Tool{
				Name:        "world_remove",
				Description: "Remove a place, a terrain feature or a vegetation rule from a world's file by name (the sites a place displaced come back).",
				InputSchema: schema(map[string]any{"world": str("world name"), "name": str("name of the place, feature or vegetation rule")}, "world", "name"),
				Handler: m.sessionTool(func(ctx context.Context, s *Session, args json.RawMessage) (*mcp.Result, error) {
					var a struct {
						World string `json:"world"`
						Name  string `json:"name"`
					}
					if err := mcp.Strict(args, &a); err != nil {
						return nil, err
					}
					rep, err := s.WorldRemove(a.World, a.Name)
					if err != nil {
						return nil, err
					}
					return textResult(rep), nil
				}),
			},
		)
	}
}
