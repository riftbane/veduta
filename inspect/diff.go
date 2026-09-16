package inspect

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/internal/sheet"
)

// DiffReport compares two images (spec §9.4).
type DiffReport struct {
	Width         int     `json:"width"`
	Height        int     `json:"height"`
	ChangedPixels int     `json:"changed_pixels"`
	ChangedRatio  float64 `json:"changed_ratio"`
	BBox          *[4]int `json:"bbox"`       // bounds of changed pixels x0, y0, x1, y1 (exclusive), nil when identical
	MaxDelta      int     `json:"max_delta"`  // largest per-channel difference (0–255)
	MeanDelta     float64 `json:"mean_delta"` // mean of the per-pixel max channel difference over changed pixels
	Threshold     int     `json:"threshold"`  // channel differences at or below this are ignored
	SizeMismatch  bool    `json:"size_mismatch,omitempty"`
	Sheet         string  `json:"sheet,omitempty"`
}

// Diff compares a and b channel by channel. Differences of at most threshold are not
// counted. Images of different sizes are compared over their common area and reported
// with SizeMismatch. The returned sheet shows a | b | heat, fitted to 640 pixels wide:
// in the heat panel unchanged pixels are a darkened copy of a and changed pixels are red,
// brighter for larger differences.
func Diff(a, b *gfx.Image, threshold int) (DiffReport, *gfx.Image) {
	w, h := min(a.W, b.W), min(a.H, b.H)
	r := DiffReport{Width: w, Height: h, Threshold: threshold, SizeMismatch: a.W != b.W || a.H != b.H}
	heat := gfx.NewImage(w, h)
	x0, y0, x1, y1 := w, h, 0, 0
	var sum int
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			ca, cb := a.Pix[y*a.W+x], b.Pix[y*b.W+x]
			d := channelDelta(ca, cb)
			i := y*w + x
			if d <= threshold {
				heat.Pix[i] = 0xff000000 | (ca>>2)&0x3f3f3f
				continue
			}
			r.ChangedPixels++
			sum += d
			r.MaxDelta = max(r.MaxDelta, d)
			x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x+1), max(y1, y+1)
			v := uint32(96 + d*159/255)
			heat.Pix[i] = 0xff000000 | v<<16 | uint32(d/4)<<8
		}
	}
	if r.ChangedPixels > 0 {
		r.BBox = &[4]int{x0, y0, x1, y1}
		r.MeanDelta = round4(float64(sum) / float64(r.ChangedPixels))
	}
	if w*h > 0 {
		r.ChangedRatio = round4(float64(r.ChangedPixels) / float64(w*h))
	}
	panelW := (640 - 4*4) / 3
	pa, pb, ph := sheet.Fit(a, panelW, 360), sheet.Fit(b, panelW, 360), sheet.Fit(heat, panelW, 360)
	s := sheet.Grid([]*gfx.Image{pa, pb, ph}, []string{"a", "b", fmt.Sprintf("diff %d px", r.ChangedPixels)}, 3, 4)
	return r, s
}

func channelDelta(a, b uint32) int {
	d := 0
	for s := 0; s < 32; s += 8 {
		x, y := int(a>>s&0xff), int(b>>s&0xff)
		if x > y {
			d = max(d, x-y)
		} else {
			d = max(d, y-x)
		}
	}
	return d
}

// LoadImage reads a PNG or the color buffer of a .vframe bundle.
func LoadImage(path string) (*gfx.Image, error) {
	if strings.HasSuffix(path, ".vframe") {
		f, err := ReadFrame(path)
		if err != nil {
			return nil, err
		}
		return f.Image(), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	img, err := gfx.DecodePNG(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return img, nil
}
