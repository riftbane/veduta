package inspect

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/internal/sheet"
	"github.com/riftbane/veduta/world"
)

// WorldCodes are the issue codes of World, in the order they are documented.
var WorldCodes = []string{
	world.CodeMissingPrefab, world.CodeMissingAsset, world.CodeNotTiling, world.CodePlaceOverlap,
	world.CodePlaceOutside, world.CodePlaceBiome, world.CodePlaceWater, world.CodePlaceTooClose, world.CodeBudget, world.CodeViewShort,
}

// WorldSheets are the sheets World can write.
var WorldSheets = []string{"map"}

// MapRadius is the default half-size, in cells, of the map sheet and of world_map.
const MapRadius = 64

// World inspects a world: its generator is checked against the library (world.Check) and
// the map around the origin is drawn.
func World(ir *Renderer, name string, opt Options) (*Report, error) {
	if ir == nil || ir.Lib == nil {
		return nil, errors.New("inspect world: no library")
	}
	for _, s := range opt.Sheets {
		if s != "all" && s != "none" && !slices.Contains(WorldSheets, s) {
			return nil, fmt.Errorf("inspect world %s: unknown sheet %q (valid: %s, all, none)", name, s, strings.Join(WorldSheets, ", "))
		}
	}
	if opt.Focus != "" && !slices.Contains(WorldCodes, opt.Focus) {
		return nil, fmt.Errorf("inspect world %s: unknown focus %q (valid: %s)", name, opt.Focus, strings.Join(WorldCodes, ", "))
	}
	src := ir.Lib.Worlds[name]
	if src == nil {
		return nil, fmt.Errorf("inspect world: unknown world %q (have: %s)", name, strings.Join(asset.Names(ir.Lib.Worlds), ", "))
	}
	g := world.New(src, ir.Lib.Prefab)
	chk := g.Check(ir.Lib)
	rep := &Report{Subject: "world:" + name, Metrics: chk.Metrics}
	for _, is := range chk.Issues {
		rep.Add(is.Severity, is.Code, 1, is.Where, is.Msg)
	}
	rep.Metrics["extent_m"] = float64(src.Extent) * float64(src.Chunk) * float64(src.Cell)
	rep.Metrics["cells"] = uint64(2*src.Span()) * uint64(2*src.Span())
	rep.Metrics["places"] = len(src.Places)
	rep.Metrics["missing_prefabs"] = g.Missing
	if opt.wants("map", true) {
		img := MapImage(g, [2]int32{0, 0}, MapRadius)
		p := opt.sheetPath(rep.Subject, "map")
		if err := scnWritePNG(p, img); err != nil {
			return nil, fmt.Errorf("inspect world %s: sheet map: %w", name, err)
		}
		rep.Sheets = append(rep.Sheets, p)
	}
	rep.Finish(opt.Focus)
	return rep, nil
}

// Biome colours of the map, by biome index (wrapping).
var mapPalette = []uint32{
	0xff6da75a, 0xff2f6b3a, 0xffd8c88a, 0xff4a7fb5, 0xff8a8a8a, 0xff8c6a4a, 0xff9a6bb5, 0xff4ab5a8,
}

// MapImage draws the world around center: one cell per pixel (scaled up so the image
// is at least 480 pixels), biomes in their palette colour, scatter as dark dots, sites
// outlined in orange, places filled in red and labelled with their name, and a legend.
func MapImage(g *world.Gen, center [2]int32, radius int32) *gfx.Image {
	r := g.Region(center, radius)
	n := int(max(r.Rect.W, r.Rect.D, 1))
	ppc := max(1, 480/n)
	w, h := int(r.Rect.W)*ppc, int(r.Rect.D)*ppc
	legendH := 12*(len(g.W.Biomes)+1) + 6
	img := gfx.NewImage(max(w, 1), max(h, 1)+legendH)
	img.Fill(sheet.Background)
	for z := 0; z < int(r.Rect.D); z++ {
		for x := 0; x < int(r.Rect.W); x++ {
			c := mapPalette[r.Biomes[z*int(r.Rect.W)+x]%len(mapPalette)]
			sheet.FillRect(img, x*ppc, z*ppc, ppc, ppc, c)
		}
	}
	px := func(cell int32, origin int32) int { return int(cell-origin) * ppc }
	for _, s := range r.Scatter {
		sheet.FillRect(img, px(s.Rect.X, r.Rect.X), px(s.Rect.Z, r.Rect.Z), ppc, ppc, 0xa0203018)
	}
	for _, s := range r.Structs {
		x, z := px(s.Rect.X, r.Rect.X), px(s.Rect.Z, r.Rect.Z)
		bw, bd := int(s.Rect.W)*ppc, int(s.Rect.D)*ppc
		fill, line := uint32(0x60ff8c1a), uint32(0xffff8c1a)
		if s.Place != "" {
			fill, line = 0x80e03030, 0xffff4040
		}
		sheet.FillRect(img, x, z, bw, bd, fill)
		sheet.FillRect(img, x, z, bw, 1, line)
		sheet.FillRect(img, x, z+bd-1, bw, 1, line)
		sheet.FillRect(img, x, z, 1, bd, line)
		sheet.FillRect(img, x+bw-1, z, 1, bd, line)
		if s.Place != "" {
			sheet.Label(img, x+2, z+2, 1, s.Place)
		}
	}
	// Axes through the origin, when inside.
	if r.Rect.Contains(0, r.Rect.Z) {
		sheet.FillRect(img, px(0, r.Rect.X), 0, 1, h, 0x60ffffff)
	}
	if r.Rect.Contains(r.Rect.X, 0) {
		sheet.FillRect(img, 0, px(0, r.Rect.Z), w, 1, 0x60ffffff)
	}
	y := h + 3
	sheet.Text(img, 3, y, 1, fmt.Sprintf("cells [%d,%d]..[%d,%d]  north up  %d px/cell", r.Rect.X, r.Rect.Z, r.Rect.X+r.Rect.W-1, r.Rect.Z+r.Rect.D-1, ppc), sheet.LabelFG)
	for i, b := range g.W.Biomes {
		y += 12
		sheet.FillRect(img, 3, y, 8, 8, mapPalette[i%len(mapPalette)])
		sheet.Text(img, 15, y, 1, fmt.Sprintf("%s (%s) %.0f%%", b.Name, b.Ground, 100*r.Shares[i]), sheet.LabelFG)
	}
	return img
}
