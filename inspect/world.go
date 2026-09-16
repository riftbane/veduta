package inspect

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/internal/sheet"
	"github.com/riftbane/veduta/v2/world"
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

// Outline colours of features by kind and of vegetation areas.
var (
	mapHill       = uint32(0xfff2dfa8)
	mapPlain      = uint32(0xffe0e0e0)
	mapWaterLine  = uint32(0xffa8dcff)
	mapVegetation = uint32(0xffa6f07c)
)

// MapImage draws the world around center: one cell per pixel (scaled up so the image
// is at least 480 pixels), biomes in their palette colour shaded by the relief (lit from
// the north-west), water in blue (darker where deeper), scatter as dark dots, sites
// outlined in orange, places filled in red and labelled with their name, features and
// vegetation areas as labelled circles, and a legend.
func MapImage(g *world.Gen, center [2]int32, radius int32) *gfx.Image {
	r := g.Region(center, radius)
	n := int(max(r.Rect.W, r.Rect.D, 1))
	ppc := max(1, 480/n)
	w, h := int(r.Rect.W)*ppc, int(r.Rect.D)*ppc
	legendH := 12*(len(g.W.Biomes)+2) + 6
	img := gfx.NewImage(max(w, 1), max(h, 1)+legendH)
	img.Fill(sheet.Background)
	rw, rd := int(r.Rect.W), int(r.Rect.D)
	height := func(x, z int) float64 {
		x, z = min(max(x, 0), rw-1), min(max(z, 0), rd-1)
		return float64(r.Heights[z*rw+x]) / 1000
	}
	cell := float64(g.W.Cell)
	lo, hi := math.Inf(1), math.Inf(-1)
	water := 0
	for z := 0; z < rd; z++ {
		for x := 0; x < rw; x++ {
			i := z*rw + x
			hm := height(x, z)
			lo, hi = min(lo, hm), max(hi, hm)
			var c uint32
			if r.Water[i] != world.NoWater {
				water++
				depth := min(max(float64(r.Water[i])/1000-hm, 0), 8)
				c = mapShade(0xff3b76b0, 1.1-float64(depth*0.08))
			} else {
				// Lambert on the slope, the light from the north-west and above.
				sx := (height(x+1, z) - height(x-1, z)) / (2 * cell)
				sz := (height(x, z+1) - height(x, z-1)) / (2 * cell)
				l := math.Sqrt(float64(sx*sx) + float64(sz*sz) + 1)
				lit := (float64(sx*0.5) + 0.8 + float64(sz*0.3)) / l
				c = mapShade(mapPalette[r.Biomes[i]%len(mapPalette)], 0.35+float64(0.75*min(max(lit, 0), 1.2)))
			}
			sheet.FillRect(img, x*ppc, z*ppc, ppc, ppc, c)
		}
	}
	px := func(cell int32, origin int32) int { return int(cell-origin) * ppc }
	for _, s := range r.Scatter {
		sheet.FillRect(img, px(s.Rect.X, r.Rect.X), px(s.Rect.Z, r.Rect.Z), ppc, ppc, 0xa0203018)
	}
	circle := func(c [2]int32, radius int32, color uint32, label string) {
		// Pixels whose cell centre lies within half a cell of the circle.
		in, out := int64(2*radius-1), int64(2*radius+1)
		for z := 0; z < rd; z++ {
			for x := 0; x < rw; x++ {
				dx, dz := 2*(int64(r.Rect.X)+int64(x)-int64(c[0]))+1, 2*(int64(r.Rect.Z)+int64(z)-int64(c[1]))+1
				if d2 := dx*dx + dz*dz; d2 >= max(in, 0)*max(in, 0) && d2 < out*out {
					sheet.FillRect(img, x*ppc, z*ppc, ppc, ppc, color)
				}
			}
		}
		lx, lz := px(c[0], r.Rect.X), px(c[1], r.Rect.Z)
		if lx >= 0 && lz >= 0 && lx < w && lz < h {
			sheet.Label(img, lx+2, lz+2, 1, label)
		}
	}
	for _, f := range g.W.Features {
		color := mapHill
		switch {
		case f.IsWater():
			color = mapWaterLine
		case f.Kind == asset.FeaturePlain:
			color = mapPlain
		}
		circle(f.Cell, f.Radius, color, f.Name)
	}
	for _, v := range g.W.Vegetation {
		if v.Area {
			circle(v.Cell, v.Radius, mapVegetation, v.Name)
		}
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
	if rw*rd > 0 {
		y += 12
		sheet.Text(img, 3, y, 1, fmt.Sprintf("ground %.1f..%.1f m  water %.0f%%", lo, hi, 100*float64(water)/float64(rw*rd)), sheet.LabelFG)
	}
	for i, b := range g.W.Biomes {
		y += 12
		sheet.FillRect(img, 3, y, 8, 8, mapPalette[i%len(mapPalette)])
		sheet.Text(img, 15, y, 1, fmt.Sprintf("%s (%s) %.0f%%", b.Name, b.Ground, 100*r.Shares[i]), sheet.LabelFG)
	}
	return img
}

// mapShade scales the colour's RGB by k (clamped), keeping its alpha.
func mapShade(c uint32, k float64) uint32 {
	ch := func(shift uint) uint32 {
		v := float64(c>>shift&0xff) * k
		return uint32(min(max(v, 0), 255)) << shift
	}
	return c&0xff000000 | ch(16) | ch(8) | ch(0)
}
