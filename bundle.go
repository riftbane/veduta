package veduta

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/inspect"
	"github.com/riftbane/veduta/scene"
)

// writeBundle writes the frame bundle of f: color, depth, ids, normals, camera, and for
// every entity with a model its projected coverage (pixels and bounds when drawn alone),
// which query --coverage turns into occlusion ratios.
func writeBundle(path string, e *engine, f *frame, camera string) error {
	fb := f.FB
	hdr := inspect.FrameHeader{
		Width: fb.W, Height: fb.H, Scene: e.ctx.Scene.Name, Tick: e.tick, Seed: e.seed,
		Mode: f.Mode.String(), View: f.View.View, Proj: f.View.Proj,
		Camera: inspect.FrameCamera{Name: camera, Ortho: f.Camera.Ortho, FovDeg: f.Camera.FovDeg, Size: f.Camera.Size,
			Near: f.Camera.Near, Far: f.Camera.Far, Position: f.Camera.Position, Target: f.Camera.Target},
		Entities: []inspect.FrameEntity{},
	}
	proj, err := e.projected(f)
	if err != nil {
		return err
	}
	for _, ent := range e.ctx.Scene.Entities() {
		if !ent.Alive() || ent.Model == "" {
			continue
		}
		p := proj[ent.ID]
		hdr.Entities = append(hdr.Entities, inspect.FrameEntity{ID: ent.ID, Name: ent.Name, Kind: ent.Kind, Projected: p.pixels, BBox: p.bbox})
	}
	return inspect.WriteFrame(path, &inspect.Frame{FrameHeader: hdr, Color: fb.Color, Depth: fb.Depth, ID: fb.ID, Normal: fb.Normal})
}

func sceneDrawOptions(f *frame, mode gfx.RenderMode) scene.DrawOptions {
	return scene.DrawOptions{Camera: f.Camera, Width: f.FB.W, Height: f.FB.H, Mode: mode}
}

type projection struct {
	pixels int
	bbox   [4]int
}

// projected renders each entity alone in silhouette mode with the frame's camera and
// counts the pixels it covers.
func (e *engine) projected(f *frame) (map[uint32]projection, error) {
	out := map[uint32]projection{}
	w, h := f.FB.W, f.FB.H
	fb := gfx.NewFramebuffer(w, h, false)
	var all gfx.DrawList
	e.ctx.Scene.Draw(&all, e.res, sceneDrawOptions(f, gfx.ModeSilhouette))
	var dl gfx.DrawList
	for _, ent := range e.ctx.Scene.Entities() {
		if !ent.Alive() || ent.Model == "" {
			continue
		}
		dl.Reset()
		dl.Clear, dl.Mode, dl.Light = true, gfx.ModeSilhouette, all.Light
		dl.Views = append(dl.Views, all.Views...)
		for _, c := range all.Cmds {
			if c.ID == ent.ID {
				c.State.DepthTest = true
				dl.Add(c)
			}
		}
		if len(dl.Cmds) == 0 {
			continue
		}
		if err := e.renderer.Begin(fb); err != nil {
			return nil, err
		}
		if err := e.renderer.Draw(&dl); err != nil {
			return nil, err
		}
		e.renderer.End()
		p := projection{bbox: [4]int{w, h, 0, 0}}
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				if fb.ID[y*w+x] == ent.ID {
					p.pixels++
					p.bbox = [4]int{min(p.bbox[0], x), min(p.bbox[1], y), max(p.bbox[2], x+1), max(p.bbox[3], y+1)}
				}
			}
		}
		if p.pixels == 0 {
			p.bbox = [4]int{}
		}
		out[ent.ID] = p
	}
	return out, nil
}

// query answers ID-buffer questions about a frame bundle (no simulation needed).
func (h *headless) query(args []string) error {
	fs := h.flags("query")
	framePath := fs.String("frame", "", "frame bundle (.vframe) written by render --bundle")
	at := fs.String("at", "", "pixel x,y to inspect")
	coverage := fs.Bool("coverage", false, "per-entity pixel counts, bounds and occlusion ratios")
	if err := parse(fs, args); err != nil {
		return err
	}
	if *framePath == "" || (*at == "") == !*coverage {
		return usageError{errors.New("query: need --frame and exactly one of --at x,y or --coverage")}
	}
	f, err := inspect.ReadFrame(*framePath)
	if err != nil {
		return err
	}
	if *coverage {
		writeJSON(h.stdout, map[string]any{"ok": true, "frame": *framePath, "coverage": f.Coverage()})
		return nil
	}
	x, y, err := parseXY(*at)
	if err != nil {
		return usageError{err}
	}
	p, err := f.At(x, y)
	if err != nil {
		return err
	}
	writeJSON(h.stdout, map[string]any{"ok": true, "frame": *framePath, "pixel": p})
	return nil
}

func parseXY(s string) (int, int, error) {
	a, b, ok := strings.Cut(s, ",")
	x, err1 := strconv.Atoi(strings.TrimSpace(a))
	y, err2 := strconv.Atoi(strings.TrimSpace(b))
	if !ok || err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("query: --at wants x,y, got %q", s)
	}
	return x, y, nil
}
