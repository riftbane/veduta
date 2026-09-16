package inspect

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/asset/texture"
	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/gmath"
	"github.com/riftbane/veduta/v2/internal/sheet"
	"github.com/riftbane/veduta/v2/scene"
)

// TexSource is the source of a texture, needed for the per-layer analysis: File is the
// source path (its base name must be "<name>.tex.json"), Data its bytes and FS the
// project's assets directory, from which image layers read their PNG files (nil when the
// texture has no image layer).
type TexSource struct {
	File string
	Data []byte
	FS   fs.FS
}

// Thresholds of the texture checks. Texel differences are the largest per-channel
// difference (R, G, B or A, 0–255) between two texels; luminance is Rec. 601
// (0.299 R + 0.587 G + 0.114 B) on the 0–255 scale.
const (
	// TEX_LOW_CONTRAST: luminance standard deviation below this…
	texLowContrastStd = 4.0
	// …unless the alpha channel carries the pattern (alpha_max − alpha_min at least this).
	texAlphaPattern = 16

	// TEX_SEAM: the mean difference across a wrap edge must be at least texSeamMin, above
	// texSeamTypical × the mean neighbour difference inside (same direction) +
	// texSeamOffset, and above texSeamLine × the strongest column (row) boundary inside.
	texSeamMin     = 8.0
	texSeamTypical = 2.0
	texSeamOffset  = 4.0
	texSeamLine    = 1.25
	// A layer is named as the cause of a seam when leaving it out removes at least this
	// share of the mean edge difference.
	texSeamCulpritShare = 0.5

	// TEX_MIP_ILLEGIBLE: luminance std at mip 2 / std at level 0 below this, checked when
	// there are at least 3 levels, level 2 is at least texMipMinSide texels on each side
	// and level 0 is not low-contrast.
	texMipRatio   = 0.35
	texMipMinSide = 4
	// A layer is named as the cause when leaving it out raises the ratio by at least this
	// (to 1 when the layer carried all the contrast).
	texMipCulpritGain = 0.2

	// TEX_LAYER_NO_EFFECT: leaving the layer out changes no level-0 channel by more than
	// this.
	texLayerMaxDelta = 1
	// Layer analysis renders the texture once per layer: it is skipped when
	// width × height × layers² exceeds this budget.
	texLayerBudget = 1 << 27

	texTileDefault = 256 // default tile size of texture sheets (square)
	texTileMax     = 320
	texPanel       = 150 // panel size of the standalone channels and mips sheets
	texModelW      = 320 // default on_model image size
	texModelH      = 240
)

// texSheetKinds are the sheets of Texture that take no argument, in the order they are
// written; on_model:<model> sheets follow.
var texSheetKinds = []string{"summary", "single", "tiled_2x2", "channels", "mips"}

// Texture inspects ir.Lib.Textures[name] (spec §9.2). The report subject is
// "texture:<name>". With src, every layer is also rendered left out once (the texture
// package's Skip option) to find layers with no effect and to name the layer behind a
// seam or illegible mips; with src == nil that analysis is skipped and an info issue
// TEX_LAYERS_NOT_CHECKED says so.
//
// Issues (texel difference = largest of |ΔR|, |ΔG|, |ΔB|, |ΔA|; luminance = Rec. 601,
// 0–255; everything measured on level 0 unless stated):
//
//   - TEX_NOT_POWER_OF_TWO: width or height is not a power of two. Warning when the
//     texture tiles or has mip levels, info otherwise. Count: non-power-of-two sides.
//   - TEX_SEAM (tiling only, warning, once per edge pair): mean difference between the
//     last and first column (row) ≥ 8, > 2 × the mean neighbour difference inside in the
//     same direction + 4, and > 1.25 × the strongest column (row) boundary inside, so a
//     repeated pattern edge that falls on the border is not a seam. Count: texel pairs
//     across the edge whose difference exceeds 2 × inside + 4.
//   - TEX_LOW_CONTRAST (warning): luminance std < 4 while the alpha range is < 16.
//   - TEX_ALPHA_UNUSED: warning when some texel has alpha < 255 but every material
//     using the texture is "opaque" (the transparency is ignored); info when a "blend"
//     material's texture × albedo alpha is 255 everywhere, or no texel of a "cutout"
//     material falls below its cutoff (nothing is transparent or cut out).
//   - TEX_MIP_ILLEGIBLE (warning): with ≥ 3 levels and a level 2 of at least 4×4, the
//     luminance std of level 2 is < 0.35 × that of level 0 (level 0 std ≥ 4).
//   - TEX_LAYER_NO_EFFECT (warning, one per layer): leaving layer i out changes no
//     level-0 channel by more than 1.
//
// Sheets: summary (default: single, tiled_2x2 when tiling, channels and mips in one
// grid), single, tiled_2x2, channels (R, G, B, A as gray), mips (every level upscaled
// with nearest neighbour to one size) and on_model:<model> (the texture on every part of
// the model, lit, iso view). Textures with transparency are shown over a gray checker.
func Texture(ir *Renderer, name string, src *TexSource, opt Options) (*Report, error) {
	if ir == nil || ir.Lib == nil {
		return nil, errors.New("inspect texture: no library")
	}
	tex := ir.Lib.Textures[name]
	if tex == nil {
		return nil, fmt.Errorf("inspect texture: unknown texture %q (have: %s)", name, strings.Join(asset.Names(ir.Lib.Textures), ", "))
	}
	if err := texCheckLevels(tex); err != nil {
		return nil, fmt.Errorf("inspect texture %s: %w", name, err)
	}
	fixed, models, err := texSheetRequest(ir.Lib, opt)
	if err != nil {
		return nil, fmt.Errorf("inspect texture %s: %w", name, err)
	}
	var layers *texLayers
	skipped := texSkip{reason: "no texture source was given", fix: "Inspect with the texture source (its .tex.json) to run them."}
	if src != nil {
		if layers, skipped, err = texLoadLayers(src, tex); err != nil {
			return nil, fmt.Errorf("inspect texture %s: layer analysis: %w", name, err)
		}
	}

	base := tex.Data.Levels[0]
	st := texImageStats(base)
	seam := texSeamStats(base)
	ratio, hasRatio := texMipContrast(tex)
	users := texUsers(ir.Lib, name)

	r := &Report{Subject: "texture:" + name, Metrics: map[string]any{}}
	wrap := "repeat"
	if tex.Data.Wrap == gfx.WrapClamp {
		wrap = "clamp"
	}
	m := r.Metrics
	m["size"] = [2]int{base.W, base.H}
	m["levels"] = len(tex.Data.Levels)
	m["tiling"] = tex.Tiling
	m["wrap"] = wrap
	m["layers"] = tex.Layers
	m["mean_color"] = gfx.FormatColor(st.mean)
	m["luminance_mean"] = round4(st.lumMean)
	m["luminance_std"] = round4(st.lumStd)
	m["luminance_range"] = st.lumRange
	m["alpha_min"] = st.alphaMin
	m["alpha_max"] = st.alphaMax
	m["nonopaque_texels"] = st.nonOpaque
	m["seam_delta_lr"] = round4(seam.lr)
	m["seam_delta_tb"] = round4(seam.tb)
	m["inside_delta"] = round4(seam.inside)
	m["inside_delta_x"] = round4(seam.insideX)
	m["inside_delta_y"] = round4(seam.insideY)
	if hasRatio {
		m["mip2_contrast_ratio"] = round4(ratio)
	} else {
		m["mip2_contrast_ratio"] = nil
	}
	m["materials"] = users

	texCheckPOT(r, tex)
	if tex.Tiling {
		texCheckSeam(r, base, seam, layers)
	}
	texCheckContrast(r, st)
	texCheckAlpha(r, ir.Lib, st, users)
	if hasRatio {
		texCheckMips(r, tex, st, ratio, layers)
	}
	if layers != nil {
		texCheckLayers(r, layers)
	} else {
		r.Add(Info, "TEX_LAYERS_NOT_CHECKED", tex.Layers, map[string]any{"reason": skipped.reason},
			fmt.Sprintf("Layer checks were skipped (%s): TEX_LAYER_NO_EFFECT is not reported and seams or illegible mips are not traced to a layer. %s", skipped.reason, skipped.fix))
	}

	for _, kind := range fixed {
		if err := texWriteSheet(r, opt, kind, texBuildSheet(tex, kind, texTile(opt))); err != nil {
			return nil, fmt.Errorf("inspect texture %s: %w", name, err)
		}
	}
	for _, model := range models {
		img, err := texOnModel(ir, name, model, users, opt)
		if err != nil {
			return nil, fmt.Errorf("inspect texture %s: sheet on_model:%s: %w", name, model, err)
		}
		if err := texWriteSheet(r, opt, "on_model:"+model, img); err != nil {
			return nil, fmt.Errorf("inspect texture %s: %w", name, err)
		}
	}
	r.Finish(opt.Focus)
	return r, nil
}

// texCheckLevels rejects textures whose images cannot be analysed.
func texCheckLevels(t *asset.Texture) error {
	if len(t.Data.Levels) == 0 {
		return errors.New("texture has no image")
	}
	for i, l := range t.Data.Levels {
		if l == nil || l.W <= 0 || l.H <= 0 || len(l.Pix) != l.W*l.H {
			return fmt.Errorf("level %d is empty or malformed", i)
		}
	}
	return nil
}

// texUsers returns the sorted names of the materials that use texture name.
func texUsers(lib *asset.Library, name string) []string {
	users := []string{}
	for _, n := range asset.Names(lib.Materials) {
		if m := lib.Materials[n]; m != nil && m.Texture == name {
			users = append(users, n)
		}
	}
	return users
}

// ---- statistics ----

// texStats are the level statistics used by the checks and metrics.
type texStats struct {
	lumMean, lumStd    float64 // luminance, 0–255
	lumRange           int     // 99th − 1st percentile of the rounded luminance
	mean               uint32  // mean RGB, opaque
	alphaMin, alphaMax int
	nonOpaque          int // texels with alpha < 255
}

// texLuma returns the Rec. 601 luminance of c times 1000 (0–255000).
func texLuma(c uint32) int64 {
	return 299*int64(c>>16&0xff) + 587*int64(c>>8&0xff) + 114*int64(c&0xff)
}

// texImageStats measures img with integer sums, so the result does not depend on
// evaluation order.
func texImageStats(img *gfx.Image) texStats {
	st := texStats{alphaMin: 255}
	n := int64(len(img.Pix))
	if n == 0 {
		st.alphaMin = 0
		return st
	}
	var sum, sq, sr, sg, sb int64
	var hist [256]int64
	for _, c := range img.Pix {
		l := texLuma(c)
		sum += l
		sq += l * l
		hist[(l+500)/1000]++
		sr += int64(c >> 16 & 0xff)
		sg += int64(c >> 8 & 0xff)
		sb += int64(c & 0xff)
		a := int(c >> 24)
		st.alphaMin = min(st.alphaMin, a)
		st.alphaMax = max(st.alphaMax, a)
		if a < 255 {
			st.nonOpaque++
		}
	}
	mean := float64(sum) / float64(n)
	st.lumMean = mean / 1000
	// mean*mean is rounded on its own so arm64 cannot fuse it (see gmath.m32).
	st.lumStd = math.Sqrt(max(float64(sq)/float64(n)-float64(mean*mean), 0)) / 1000
	st.mean = 0xff000000 | uint32((sr+n/2)/n)<<16 | uint32((sg+n/2)/n)<<8 | uint32((sb+n/2)/n)
	// Percentiles: the lowest (highest) value such that more than 1% of the texels are at
	// or below (above) it; with fewer than 100 texels these are the minimum and maximum.
	k := n / 100
	lo, hi := 0, 255
	for v, cum := 0, int64(0); v < 256; v++ {
		if cum += hist[v]; cum > k {
			lo = v
			break
		}
	}
	for v, cum := 255, int64(0); v >= 0; v-- {
		if cum += hist[v]; cum > k {
			hi = v
			break
		}
	}
	st.lumRange = max(hi-lo, 0)
	return st
}

// texSeam compares the wrap-around edges of an image with its inside.
type texSeam struct {
	lr, tb           float64 // mean texel difference last↔first column, last↔first row
	insideX, insideY float64 // mean difference of horizontally / vertically adjacent texels inside
	inside           float64 // both directions together
	lineX, lineY     float64 // strongest column / row boundary inside (mean over its pairs)
}

func texSeamStats(img *gfx.Image) texSeam {
	w, h := img.W, img.H
	col := make([]int64, w) // col[x]: sum of differences between column x and x+1 (mod w)
	row := make([]int64, h) // row[y]: sum of differences between row y and y+1 (mod h)
	for y := 0; y < h; y++ {
		line := img.Pix[y*w : (y+1)*w]
		next := img.Pix[((y+1)%h)*w : ((y+1)%h+1)*w]
		var rs int64
		for x, c := range line {
			col[x] += int64(channelDelta(c, line[(x+1)%w]))
			rs += int64(channelDelta(c, next[x]))
		}
		row[y] = rs
	}
	var s texSeam
	var sumX, sumY int64
	for x, c := range col {
		m := float64(c) / float64(h)
		if x == w-1 {
			s.lr = m
			continue
		}
		sumX += c
		s.lineX = max(s.lineX, m)
	}
	for y, c := range row {
		m := float64(c) / float64(w)
		if y == h-1 {
			s.tb = m
			continue
		}
		sumY += c
		s.lineY = max(s.lineY, m)
	}
	if w > 1 {
		s.insideX = float64(sumX) / float64((w-1)*h)
	}
	if h > 1 {
		s.insideY = float64(sumY) / float64((h-1)*w)
	}
	if pairs := (w-1)*h + (h-1)*w; pairs > 0 {
		s.inside = float64(sumX+sumY) / float64(pairs)
	}
	return s
}

// texMipContrast returns luminance std at level 2 / std at level 0, when there are at
// least 3 levels and level 0 is not uniform.
func texMipContrast(t *asset.Texture) (float64, bool) {
	lv := t.Data.Levels
	if len(lv) < 3 {
		return 0, false
	}
	s0 := texImageStats(lv[0]).lumStd
	if s0 == 0 {
		return 0, false
	}
	return texImageStats(lv[2]).lumStd / s0, true
}

// texMaxDelta returns the largest channel difference between a and b (255 when their
// sizes differ).
func texMaxDelta(a, b *gfx.Image) int {
	if a.W != b.W || a.H != b.H {
		return 255
	}
	d := 0
	for i, c := range a.Pix {
		d = max(d, channelDelta(c, b.Pix[i]))
	}
	return d
}

// ---- layer analysis ----

// texVariant is the texture rendered from its source, with or without one layer.
type texVariant struct {
	tex   *asset.Texture
	stats texStats
	seam  texSeam
	ratio float64 // mip-2 contrast ratio; 1 when level 0 has no contrast to lose
	mips  bool    // ratio is defined (≥ 3 levels)
	delta int     // largest channel difference of level 0 from the full rendering
}

// texLayers is the per-layer analysis: the source, the full rendering and one rendering
// per left-out layer.
type texLayers struct {
	src     asset.TextureSource
	full    texVariant
	without []texVariant // without[i]: layer i left out
}

// texNewVariant measures a rendering. Seam statistics are only needed (to name the layer
// behind a seam) when the texture tiles.
func texNewVariant(t *asset.Texture) texVariant {
	v := texVariant{tex: t, stats: texImageStats(t.Data.Levels[0])}
	if t.Tiling {
		v.seam = texSeamStats(t.Data.Levels[0])
	}
	if lv := t.Data.Levels; len(lv) >= 3 {
		v.mips, v.ratio = true, 1
		if v.stats.lumStd >= texLowContrastStd {
			v.ratio = texImageStats(lv[2]).lumStd / v.stats.lumStd
		}
	}
	return v
}

// texSkip says why the layer analysis did not run and what to do about it.
type texSkip struct{ reason, fix string }

// texLoadLayers renders src once in full and once per left-out layer. When the analysis
// cannot run (source out of date, over budget) it returns nil and the reason.
func texLoadLayers(src *TexSource, tex *asset.Texture) (*texLayers, texSkip, error) {
	l := &texLayers{}
	if _, err := asset.Decode(src.File, src.Data, asset.TypeTexture, &l.src); err != nil {
		return nil, texSkip{}, err
	}
	t0 := tex.Data.Levels[0]
	n := int64(len(l.src.Layers))
	if work := int64(t0.W) * int64(t0.H) * n * n; work > texLayerBudget {
		return nil, texSkip{
			reason: fmt.Sprintf("%dx%d with %d layers exceeds the analysis budget of %d texel-layer evaluations", t0.W, t0.H, n, texLayerBudget),
			fix:    "Inspect a copy of the source with a smaller \"size\" or fewer \"layers\" to run them.",
		}, nil
	}
	full, err := texture.Parse(src.File, src.Data, texture.Options{FS: src.FS})
	if err != nil {
		return nil, texSkip{}, err
	}
	if err := texCheckLevels(full); err != nil {
		return nil, texSkip{}, err
	}
	f0 := full.Data.Levels[0]
	if f0.W != t0.W || f0.H != t0.H {
		return nil, texSkip{
			reason: fmt.Sprintf("the source renders %dx%d but the texture is %dx%d; cook again", f0.W, f0.H, t0.W, t0.H),
			fix:    "Cook the project again so the compiled texture matches its source \"size\".",
		}, nil
	}
	l.full = texNewVariant(full)
	for i := range l.src.Layers {
		t, err := texture.Parse(src.File, src.Data, texture.Options{FS: src.FS, Skip: []int{i}})
		if err != nil {
			return nil, texSkip{}, err
		}
		if err := texCheckLevels(t); err != nil {
			return nil, texSkip{}, err
		}
		v := texNewVariant(t)
		v.delta = texMaxDelta(f0, t.Data.Levels[0])
		l.without = append(l.without, v)
	}
	return l, texSkip{}, nil
}

// typ returns the type of layer i ("?" when unknown).
func (l *texLayers) typ(i int) string {
	if i < 0 || i >= len(l.src.Layers) {
		return "?"
	}
	return l.src.Layers[i].Type
}

// ---- checks ----

func texIsPOT(n int) bool { return n > 0 && n&(n-1) == 0 }

// texNearestPOT returns the power of two nearest to n (ties round up), at most
// texture.MaxSize.
func texNearestPOT(n int) int {
	lo := 1
	for lo*2 <= n {
		lo *= 2
	}
	hi := lo * 2
	if hi-n <= n-lo && hi <= texture.MaxSize {
		return hi
	}
	return lo
}

func texCheckPOT(r *Report, tex *asset.Texture) {
	b := tex.Data.Levels[0]
	count := 0
	for _, n := range [2]int{b.W, b.H} {
		if !texIsPOT(n) {
			count++
		}
	}
	if count == 0 {
		return
	}
	want := [2]int{texNearestPOT(b.W), texNearestPOT(b.H)}
	where := map[string]any{"size": [2]int{b.W, b.H}, "suggested": want}
	mipmapped := len(tex.Data.Levels) > 1
	if tex.Tiling || mipmapped {
		why := "mip levels of odd sizes clamp their last row or column and drift from the base image"
		if tex.Tiling && !mipmapped {
			why = "a repeating texture of this size does not line up with power-of-two texel grids"
		}
		r.Add(Warning, "TEX_NOT_POWER_OF_TWO", count, where,
			fmt.Sprintf("Size %dx%d is not a power of two and the texture %s: %s. Set \"size\" to [%d, %d], the nearest powers of two.",
				b.W, b.H, texUse(tex.Tiling, mipmapped), why, want[0], want[1]))
		return
	}
	r.Add(Info, "TEX_NOT_POWER_OF_TWO", count, where,
		fmt.Sprintf("Size %dx%d is not a power of two; harmless for a clamped texture without mipmaps, but set \"size\" to [%d, %d] before turning on \"tiling\" or \"mipmaps\".",
			b.W, b.H, want[0], want[1]))
}

func texUse(tiling, mipmapped bool) string {
	switch {
	case tiling && mipmapped:
		return "tiles and has mipmaps"
	case tiling:
		return "tiles"
	}
	return "has mipmaps"
}

func texCheckSeam(r *Report, img *gfx.Image, s texSeam, layers *texLayers) {
	w, h := img.W, img.H
	for _, horizontal := range [2]bool{true, false} {
		edge, sides, mean, inside, line, n := "left-right", "left and right", s.lr, s.insideX, s.lineX, w
		if !horizontal {
			edge, sides, mean, inside, line, n = "top-bottom", "top and bottom", s.tb, s.insideY, s.lineY, h
		}
		limit := float64(texSeamTypical*inside) + texSeamOffset
		if n < 2 || mean < texSeamMin || mean <= limit || mean <= texSeamLine*line {
			continue
		}
		// Pairs across the edge that stand out from the inside.
		pairs, mismatched, worst, worstAt := h, 0, 0, 0
		if !horizontal {
			pairs = w
		}
		for k := 0; k < pairs; k++ {
			var d int
			if horizontal {
				d = channelDelta(img.Pix[k*w+w-1], img.Pix[k*w])
			} else {
				d = channelDelta(img.Pix[(h-1)*w+k], img.Pix[k])
			}
			if float64(d) > limit {
				mismatched++
			}
			if d > worst {
				worst, worstAt = d, k
			}
		}
		where := map[string]any{
			"edge":           edge,
			"mean_delta":     round4(mean),
			"inside_delta":   round4(inside),
			"line_delta_max": round4(line),
			"pairs":          pairs,
			"worst_delta":    worst,
		}
		if horizontal {
			where["worst_row"] = worstAt
		} else {
			where["worst_column"] = worstAt
		}
		hint := fmt.Sprintf("The %s edges differ by %.1f on average vs %.1f between neighbours inside, so the repeat shows a line.", sides, mean, inside)
		if i, without, ok := texSeamCulprit(layers, horizontal); ok {
			where["layer"], where["type"] = i, layers.typ(i)
			where["mean_delta_without_layer"] = round4(without)
			hint += fmt.Sprintf(" Layer %d (%s) causes most of it (%.1f without it): %s", i, layers.typ(i), without, texSeamFix(i, layers.typ(i)))
		} else {
			hint += " Make every layer wrap (gradients, images and shapes crossing the border do not), or set \"tiling\": false."
		}
		r.Add(Warning, "TEX_SEAM", mismatched, where, hint)
	}
}

// texSeamCulprit returns the layer whose removal lowers the edge difference most, when
// that removes at least texSeamCulpritShare of it.
func texSeamCulprit(l *texLayers, horizontal bool) (int, float64, bool) {
	if l == nil {
		return 0, 0, false
	}
	pick := func(s texSeam) float64 {
		if horizontal {
			return s.lr
		}
		return s.tb
	}
	base := pick(l.full.seam)
	best, bestDrop, without := -1, 0.0, 0.0
	for i, v := range l.without {
		if drop := base - pick(v.seam); drop > bestDrop {
			best, bestDrop, without = i, drop, pick(v.seam)
		}
	}
	if best < 0 || bestDrop < texSeamCulpritShare*base {
		return 0, 0, false
	}
	return best, without, true
}

func texSeamFix(i int, typ string) string {
	p := fmt.Sprintf("layers[%d]", i)
	switch typ {
	case "gradient":
		return fmt.Sprintf("a gradient does not wrap; remove %s or set \"tiling\": false.", p)
	case "image":
		return fmt.Sprintf("the PNG of %s.path does not wrap; make its opposite edges match, or set \"tiling\": false.", p)
	case "rect":
		return fmt.Sprintf("the rectangle is cut by the border; move %s.xy or shrink %s.size so it stays inside, or set \"tiling\": false.", p, p)
	case "circle":
		return fmt.Sprintf("the circle is cut by the border; move %s.center or shrink %s.radius so it stays inside, or set \"tiling\": false.", p, p)
	case "stripes":
		return fmt.Sprintf("the stripe period does not divide the texture; change %s.width or %s.angle_deg so whole periods fit \"size\".", p, p)
	case "checker":
		return fmt.Sprintf("set %s.cells so the cells divide \"size\" evenly.", p)
	}
	return fmt.Sprintf("change %s so it wraps, or set \"tiling\": false.", p)
}

func texCheckContrast(r *Report, st texStats) {
	if st.lumStd >= texLowContrastStd || st.alphaMax-st.alphaMin >= texAlphaPattern {
		return
	}
	r.Add(Warning, "TEX_LOW_CONTRAST", 1,
		map[string]any{"luminance_std": round4(st.lumStd), "luminance_range": st.lumRange, "luminance_mean": round4(st.lumMean)},
		fmt.Sprintf("Luminance std %.2f and range %d of 255: the texture reads as one flat color. Raise the contrast between the layers' colors (\"color\", \"colors\", \"from\"/\"to\") or their \"opacity\", or drop the texture and set the material \"albedo\".", st.lumStd, st.lumRange))
}

func texCheckAlpha(r *Report, lib *asset.Library, st texStats, users []string) {
	if len(users) == 0 {
		return
	}
	if st.alphaMin < 255 {
		for _, u := range users {
			if lib.Materials[u].Alpha != "opaque" {
				return
			}
		}
		files := make([]string, len(users))
		for i, u := range users {
			files[i] = "materials/" + u + ".mat.json"
		}
		r.Add(Warning, "TEX_ALPHA_UNUSED", st.nonOpaque,
			map[string]any{"materials": users, "nonopaque_texels": st.nonOpaque, "alpha_min": st.alphaMin},
			fmt.Sprintf("%d texels are not opaque (alpha down to %d) but every material using the texture is \"alpha\": \"opaque\", so the transparency is ignored. Set \"alpha\": \"cutout\" or \"blend\" in %s, or make the texture opaque (start its layers with an opaque \"solid\").",
				st.nonOpaque, st.alphaMin, strings.Join(files, ", ")))
		return
	}
	// The texture is opaque: flag blend/cutout materials whose albedo does not bring
	// transparency either.
	var names, what []string
	for _, u := range users {
		m := lib.Materials[u]
		minA := float64(st.alphaMin) * float64(m.Albedo>>24) / 255 // lowest effective alpha, 0–255
		switch {
		case m.Alpha == "blend" && minA >= 255:
			names = append(names, u)
			what = append(what, fmt.Sprintf("materials/%s.mat.json (\"blend\")", u))
		case m.Alpha == "cutout" && minA/255 >= float64(m.Cutoff):
			names = append(names, u)
			what = append(what, fmt.Sprintf("materials/%s.mat.json (\"cutout\", cutoff %g)", u, m.Cutoff))
		}
	}
	if len(names) == 0 {
		return
	}
	r.Add(Info, "TEX_ALPHA_UNUSED", len(names),
		map[string]any{"materials": names, "alpha_min": st.alphaMin},
		fmt.Sprintf("The texture is opaque (alpha_min %d) yet %s expect transparency: nothing is blended or cut out. Set \"alpha\": \"opaque\" there, or give the texture transparent texels (layer colors with alpha, e.g. \"#rrggbb80\").",
			st.alphaMin, strings.Join(what, ", ")))
}

func texCheckMips(r *Report, tex *asset.Texture, st texStats, ratio float64, layers *texLayers) {
	lv := tex.Data.Levels
	if min(lv[2].W, lv[2].H) < texMipMinSide || st.lumStd < texLowContrastStd || ratio >= texMipRatio {
		return
	}
	s2 := texImageStats(lv[2]).lumStd
	where := map[string]any{"level": 2, "contrast_level0": round4(st.lumStd), "contrast_level2": round4(s2), "ratio": round4(ratio)}
	hint := fmt.Sprintf("At mip 2 (1/4 resolution) the luminance contrast drops to %.0f%% of level 0 (std %.1f → %.1f): detail finer than about 4 texels averages away when the texture is seen from afar.", 100*ratio, st.lumStd, s2)
	if i, ok := texMipCulprit(layers); ok {
		where["layer"], where["type"] = i, layers.typ(i)
		hint += " " + texMipFix(i, layers, lv[0])
		hint += " For pixel art, set \"mipmaps\": false instead and use material \"filter\": \"nearest\"."
	} else {
		hint += " Make the pattern coarser (checker \"cells\", noise \"scale\"/\"octaves\", stripes \"width\"), or for pixel art set \"mipmaps\": false and material \"filter\": \"nearest\"."
	}
	r.Add(Warning, "TEX_MIP_ILLEGIBLE", 1, where, hint)
}

// texMipCulprit returns the layer whose removal raises the mip-2 contrast ratio most,
// when the gain is at least texMipCulpritGain.
func texMipCulprit(l *texLayers) (int, bool) {
	if l == nil || !l.full.mips {
		return 0, false
	}
	best, bestGain := -1, 0.0
	for i, v := range l.without {
		if !v.mips {
			continue
		}
		if gain := v.ratio - l.full.ratio; gain > bestGain {
			best, bestGain = i, gain
		}
	}
	return best, best >= 0 && bestGain >= texMipCulpritGain
}

func texMipFix(i int, l *texLayers, base *gfx.Image) string {
	src := l.src.Layers[i]
	p := fmt.Sprintf("layers[%d]", i)
	side := min(base.W, base.H)
	switch src.Type {
	case "checker":
		cells := max(src.Cells, 1)
		return fmt.Sprintf("Layer %d (checker) has %d cells per side (%.1f texels each): set %s.cells to %d or fewer.", i, cells, float64(side)/float64(cells), p, max(1, side/8))
	case "noise":
		scale := float32(texture.DefaultScale)
		if src.Scale != nil {
			scale = *src.Scale
		}
		return fmt.Sprintf("Layer %d (noise) has scale %g (%.1f texels per cell): lower %s.scale to %d or less, or reduce %s.octaves.", i, scale, float64(base.W)/float64(scale), p, max(1, base.W/8), p)
	case "stripes":
		width := float32(0)
		if src.Width != nil {
			width = *src.Width
		}
		return fmt.Sprintf("Layer %d (stripes) is %g texels wide: raise %s.width to 8 or more.", i, width, p)
	case "image":
		return fmt.Sprintf("Layer %d (image) has detail finer than 4 texels: use a smoother PNG for %s.path.", i, p)
	}
	return fmt.Sprintf("Layer %d (%s) carries the fine detail: make %s coarser.", i, src.Type, p)
}

func texCheckLayers(r *Report, l *texLayers) {
	for i, v := range l.without {
		if v.delta > texLayerMaxDelta {
			continue
		}
		r.Add(Warning, "TEX_LAYER_NO_EFFECT", 1,
			map[string]any{"layer": i, "type": l.typ(i), "max_delta": v.delta},
			texNoEffectHint(l.src.Layers, i, v.delta))
	}
}

// texColorAlpha returns the alpha of a source color and whether it parsed.
func texColorAlpha(s string) (uint32, bool) {
	c, err := gfx.ParseColor(s)
	if err != nil {
		return 0, false
	}
	return c >> 24, true
}

// texLayerColors returns the colors a layer paints with and the name of their field.
func texLayerColors(l asset.LayerSource) ([]string, string) {
	switch l.Type {
	case "solid", "noise", "rect", "circle":
		return []string{l.Color}, "color"
	case "stripes", "checker":
		return l.Colors, "colors"
	case "gradient":
		return []string{l.From, l.To}, "from/to"
	}
	return nil, ""
}

// texAllAlpha reports whether every color of the layer parses with alpha a.
func texAllAlpha(l asset.LayerSource, a uint32) bool {
	cs, _ := texLayerColors(l)
	if len(cs) == 0 {
		return false
	}
	for _, c := range cs {
		if ca, ok := texColorAlpha(c); !ok || ca != a {
			return false
		}
	}
	return true
}

// texCovers reports whether layer l paints every texel opaquely, hiding everything below.
func texCovers(l asset.LayerSource) bool {
	if l.Blend != "" && l.Blend != "normal" || l.Opacity != nil && *l.Opacity < 1 {
		return false
	}
	switch l.Type {
	case "solid", "stripes", "checker", "gradient":
		return texAllAlpha(l, 255)
	}
	return false
}

func texNoEffectHint(ls []asset.LayerSource, i, d int) string {
	l := ls[i]
	p := fmt.Sprintf("layers[%d]", i)
	head := fmt.Sprintf("Leaving out layer %d (%s) changes no texel by more than %d/255 (largest change %d)", i, l.Type, texLayerMaxDelta, d)
	if l.Opacity != nil && *l.Opacity == 0 {
		return head + fmt.Sprintf(": its opacity is 0. Raise %s.opacity or remove the layer.", p)
	}
	if texAllAlpha(l, 0) {
		_, field := texLayerColors(l)
		return head + fmt.Sprintf(": its colors are fully transparent. Give %s.%s some alpha or remove the layer.", p, field)
	}
	for j := i + 1; j < len(ls); j++ {
		if texCovers(ls[j]) {
			return head + fmt.Sprintf(": layer %d (%s) paints over it opaquely. Remove %s, move it after layers[%d], or lower layers[%d].opacity.", j, ls[j].Type, p, j, j)
		}
	}
	return head + fmt.Sprintf(": what it adds is hidden by later layers or blends to no change (e.g. \"multiply\" with white). Remove %s, or change its \"blend\", \"opacity\" or colors.", p)
}

// ---- sheets ----

// texSheetRequest validates opt.Sheets and returns the argument-free kinds to write (in
// texSheetKinds order) and the on_model models (in request order, without duplicates).
func texSheetRequest(lib *asset.Library, opt Options) ([]string, []string, error) {
	var models []string
	for _, s := range opt.Sheets {
		switch {
		case s == "none" || s == "all" || slices.Contains(texSheetKinds, s):
		case strings.HasPrefix(s, "on_model:"):
			m := strings.TrimPrefix(s, "on_model:")
			if lib.Models[m] == nil {
				return nil, nil, fmt.Errorf("sheet %q: unknown model %q (have: %s)", s, m, strings.Join(asset.Names(lib.Models), ", "))
			}
			if !slices.Contains(models, m) {
				models = append(models, m)
			}
		default:
			return nil, nil, fmt.Errorf("unknown sheet %q (valid: %s, on_model:<model>, all, none)", s, strings.Join(texSheetKinds, ", "))
		}
	}
	var fixed []string
	for _, k := range texSheetKinds {
		if opt.wants(k, k == "summary") {
			fixed = append(fixed, k)
		}
	}
	return fixed, models, nil
}

// texTile returns the square tile size of texture sheets: opt.Width clamped to
// [32, 320], default 256.
func texTile(opt Options) int {
	if opt.Width <= 0 {
		return texTileDefault
	}
	return min(max(opt.Width, 32), texTileMax)
}

func texWriteSheet(r *Report, opt Options, kind string, img *gfx.Image) error {
	path := opt.sheetPath(r.Subject, kind)
	var buf bytes.Buffer
	if err := img.EncodePNG(&buf); err != nil {
		return fmt.Errorf("sheet %s: %w", kind, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("sheet %s: %w", kind, err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("sheet %s: %w", kind, err)
	}
	r.Sheets = append(r.Sheets, path)
	return nil
}

func texBuildSheet(tex *asset.Texture, kind string, tile int) *gfx.Image {
	lv := tex.Data.Levels
	b := lv[0]
	switch kind {
	case "single":
		return sheet.Grid([]*gfx.Image{texBackdrop(texFit(b, tile))}, []string{fmt.Sprintf("%dx%d", b.W, b.H)}, 1, 4)
	case "tiled_2x2":
		return sheet.Grid([]*gfx.Image{texTiled(b, tile)}, []string{"tiled 2x2"}, 1, 4)
	case "channels":
		return sheet.Grid(texChannelPanels(b, texPanel), texChannelLabels(b, true), 4, 4)
	case "mips":
		return sheet.Grid(texMipPanels(lv, texPanel), texMipLabels(lv, true), min(len(lv), 4), 4)
	}
	// summary
	tiles := []*gfx.Image{texBackdrop(texFit(b, tile))}
	labels := []string{fmt.Sprintf("%dx%d", b.W, b.H)}
	if tex.Tiling {
		tiles = append(tiles, texTiled(b, tile))
		labels = append(labels, "tiled 2x2")
	}
	tiles = append(tiles, sheet.Grid(texChannelPanels(b, (tile-6)/2), texChannelLabels(b, false), 2, 2))
	labels = append(labels, "")
	cols := 1
	for cols*cols < len(lv) {
		cols++
	}
	tiles = append(tiles, sheet.Grid(texMipPanels(lv, max((tile-(cols+1)*2)/cols, 1)), texMipLabels(lv, false), cols, 2))
	labels = append(labels, "")
	return sheet.Grid(tiles, labels, 2, 4)
}

// texFitDims returns the size of a w×h image shown in a box×box square: an integer
// multiple when it fits (texels stay square and crisp), else scaled down to fit.
func texFitDims(w, h, box int) (int, int) {
	if w <= box && h <= box {
		k := box / max(w, h)
		return w * k, h * k
	}
	if w >= h {
		return box, max(1, h*box/w)
	}
	return max(1, w*box/h), box
}

// texFit scales img into a box×box square (nearest neighbour up, area average down).
func texFit(img *gfx.Image, box int) *gfx.Image {
	w, h := texFitDims(img.W, img.H, box)
	return sheet.Resize(img, w, h)
}

// texTiled fits img into half the box and repeats it 2×2 (fitting first keeps large
// textures cheap; for integer upscales the pixels equal fitting the 2×2 repeat).
func texTiled(img *gfx.Image, box int) *gfx.Image {
	f := texFit(img, max(box/2, 1))
	t := gfx.NewImage(2*f.W, 2*f.H)
	for _, o := range [4][2]int{{0, 0}, {f.W, 0}, {0, f.H}, {f.W, f.H}} {
		t.Blit(f, o[0], o[1])
	}
	return texBackdrop(t)
}

// texBackdrop composites img over an 8-pixel gray checker so transparency is visible;
// the result is opaque.
func texBackdrop(img *gfx.Image) *gfx.Image {
	out := gfx.NewImage(img.W, img.H)
	for y := 0; y < img.H; y++ {
		for x := 0; x < img.W; x++ {
			c := img.Pix[y*img.W+x]
			a := c >> 24
			if a == 255 {
				out.Pix[y*img.W+x] = c
				continue
			}
			bg := uint32(0x8c)
			if (x/8+y/8)&1 == 1 {
				bg = 0x64
			}
			var o uint32 = 0xff000000
			for s := 0; s < 24; s += 8 {
				v := ((c>>s&0xff)*a + bg*(255-a) + 127) / 255
				o |= v << s
			}
			out.Pix[y*img.W+x] = o
		}
	}
	return out
}

// texChannelPanels returns R, G, B and A of img as gray images fitted into box. The
// image is fitted first: Resize averages channels independently, so this is the same as
// fitting each channel.
func texChannelPanels(img *gfx.Image, box int) []*gfx.Image {
	f := texFit(img, box)
	var out []*gfx.Image
	for _, shift := range [4]uint{16, 8, 0, 24} {
		g := gfx.NewImage(f.W, f.H)
		for i, c := range f.Pix {
			v := c >> shift & 0xff
			g.Pix[i] = 0xff000000 | v<<16 | v<<8 | v
		}
		out = append(out, g)
	}
	return out
}

func texChannelLabels(img *gfx.Image, ranges bool) []string {
	names := [4]string{"R", "G", "B", "A"}
	if !ranges {
		return names[:]
	}
	out := make([]string, 4)
	for k, shift := range [4]uint{16, 8, 0, 24} {
		lo, hi := uint32(255), uint32(0)
		for _, c := range img.Pix {
			v := c >> shift & 0xff
			lo, hi = min(lo, v), max(hi, v)
		}
		out[k] = fmt.Sprintf("%s %d..%d", names[k], lo, hi)
	}
	return out
}

// texMipPanels upscales every level with nearest neighbour to the size level 0 takes in
// the box, so texel sizes compare directly.
func texMipPanels(levels []*gfx.Image, box int) []*gfx.Image {
	w, h := texFitDims(levels[0].W, levels[0].H, box)
	out := make([]*gfx.Image, len(levels))
	for i, l := range levels {
		out[i] = texBackdrop(sheet.Resize(l, w, h))
	}
	return out
}

func texMipLabels(levels []*gfx.Image, sizes bool) []string {
	out := make([]string, len(levels))
	for i, l := range levels {
		if sizes {
			out[i] = fmt.Sprintf("L%d %dx%d", i, l.W, l.H)
		} else {
			out[i] = fmt.Sprintf("L%d", i)
		}
	}
	return out
}

// texOnModel draws model with texture name on every part, lit by gfx.DefaultLight, from
// the iso direction (the "iso" camera preset), rendered at twice the size and averaged
// down. The pipeline state, filter and cutoff come from the first material (by name)
// that uses the texture, else asset.DefaultMaterial; the color is always white so the
// texture shows as it is.
func texOnModel(ir *Renderer, name, model string, users []string, opt Options) (*gfx.Image, error) {
	w, h := texModelW, texModelH
	if opt.Width > 0 {
		w = min(max(opt.Width, 32), texTileMax)
		h = opt.Height
		if h <= 0 {
			h = w * 3 / 4
		}
		h = min(max(h, 32), 2*texTileMax)
	}
	res := ir.Resources()
	mr := res.Models[model]
	if mr == nil || mr.Model == nil {
		return nil, fmt.Errorf("model %q is not uploaded", model)
	}
	tid, ok := res.Textures[name]
	if !ok || tid == 0 {
		return nil, fmt.Errorf("texture %q is not uploaded", name)
	}
	mat := &asset.DefaultMaterial
	if len(users) > 0 {
		mat = ir.Lib.Materials[users[0]]
	}
	b := mr.Model.Mesh.Bounds
	if b.IsEmpty() {
		b = gmath.AABB{Min: gmath.V3(-0.5, -0.5, -0.5), Max: gmath.V3(0.5, 0.5, 0.5)}
	}
	const ss = 2
	fw, fh := w*ss, h*ss
	cam := scene.FramePerspective(b, gmath.V3(-1, -1, -1), 40, float32(fw)/float32(fh))
	var dl gfx.DrawList
	dl.Clear = true
	dl.ClearColor = 0xff2a2f38
	dl.Mode = gfx.ModeColor
	dl.Light = gfx.DefaultLight
	view := dl.AddView(cam.GfxView(fw, fh))
	for _, p := range mr.Model.Mesh.Parts {
		if p.Count == 0 {
			continue
		}
		dl.Add(gfx.DrawCmd{View: view, Mesh: mr.Mesh, First: p.First, Count: p.Count, Model: gmath.Ident4(),
			Texture: tid, Color: gmath.Vec4{X: 1, Y: 1, Z: 1, W: 1}, State: mat.State(), Filter: mat.Filter,
			Unlit: mat.Unlit, Cutoff: mat.AlphaCutoff(), ID: 1})
	}
	fb := gfx.NewFramebuffer(fw, fh, false)
	be := ir.Backend()
	if err := be.Begin(fb); err != nil {
		return nil, err
	}
	if err := be.Draw(&dl); err != nil {
		return nil, err
	}
	if err := be.End(); err != nil {
		return nil, err
	}
	return sheet.Grid([]*gfx.Image{sheet.Resize(fb.Image(), w, h)}, []string{"on " + model}, 1, 4), nil
}
