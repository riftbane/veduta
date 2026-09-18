package texture

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/riftbane/veduta/v2/asset"
)

// An image layer's rect takes that part of the PNG as if it were the whole file.
func TestImageRect(t *testing.T) {
	// rb.png is 2 × 1: a red pixel, then a blue one.
	right := compile(t, tex("[1, 1]", `, "mipmaps": false`, `{"type": "image", "path": "textures/src/rb.png", "rect": [1, 0, 1, 1]}`), Options{})
	left := compile(t, tex("[1, 1]", `, "mipmaps": false`, `{"type": "image", "path": "textures/src/rb.png", "rect": [0, 0, 1, 1]}`), Options{})
	if got := hex(right.Data.Levels[0].Pix[0]); got != "#0000ffff" {
		t.Errorf("right pixel %s, want blue", got)
	}
	if got := hex(left.Data.Levels[0].Pix[0]); got != "#ff0000ff" {
		t.Errorf("left pixel %s, want red", got)
	}
	for src, want := range map[string]string{
		`[1, 0, 2, 1]`: "is not inside the 2 × 1 image",
		`[0, 0, 0, 1]`: "is not inside",
		`[0, 0, 1]`:    "must be [x, y, width, height]",
	} {
		es := compileErrs(t, "r.vtex", tex("[1, 1]", "", `{"type": "image", "path": "textures/src/rb.png", "rect": `+src+`}`), Options{FS: os.DirFS(testdataDir(t))})
		if !strings.Contains(es.Error(), want) || !strings.Contains(es.Error(), "layers[0].rect") {
			t.Errorf("rect %s: %v, want %q", src, es, want)
		}
	}
	es := compileErrs(t, "r.vtex", tex("[1, 1]", "", `{"type": "solid", "color": "#ffffff", "rect": [0, 0, 1, 1]}`), Options{})
	if !strings.Contains(es.Error(), "rect: not used by layer type solid") {
		t.Errorf("rect on solid: %v", es)
	}
}

func TestSheet(t *testing.T) {
	src := `{"veduta": "texture/1", "size": [8, 4], "grid": [4, 2],
		"layers": [{"type": "checker", "cells": 4, "colors": ["#ff0000", "#0000ff"]}],
		"clips": {"walk": {"frames": [0, 1, 2, 3], "fps": 8}, "hit": {"frames": [5, 6], "fps": 12, "loop": false, "next": "walk"}},
		"play": "walk"}`
	tx := compile(t, src, Options{})
	if tx.Grid != [2]int{4, 2} || len(tx.Data.Levels) != 1 || tx.Play != "walk" {
		t.Fatalf("grid %v, %d levels (a sheet has none but the first), play %q", tx.Grid, len(tx.Data.Levels), tx.Play)
	}
	want := []asset.Clip{{Name: "hit", Frames: []int{5, 6}, FPS: 12, Next: "walk"}, {Name: "walk", Frames: []int{0, 1, 2, 3}, FPS: 8, Loop: true}}
	if !reflect.DeepEqual(tx.Clips, want) {
		t.Errorf("clips %+v, want %+v", tx.Clips, want)
	}
	if tx.Frames() != 8 || tx.Clip("hit") == nil || tx.Clip("run") != nil {
		t.Errorf("frames %d, clip lookup wrong", tx.Frames())
	}
	// A sheet may still ask for mipmaps.
	if tx := compile(t, strings.Replace(src, `"grid"`, `"mipmaps": true, "grid"`, 1), Options{}); len(tx.Data.Levels) != 4 {
		t.Errorf("%d levels with mipmaps asked", len(tx.Data.Levels))
	}
}

func TestFramesOneByOne(t *testing.T) {
	src := `{"veduta": "texture/1", "size": [2, 2],
		"layers": [{"type": "solid", "color": "#000000"}],
		"frames": [
			{"layers": [{"type": "rect", "xy": [0, 0], "size": [1, 2], "color": "#ff0000"}]},
			{"layers": [{"type": "rect", "xy": [1, 0], "size": [1, 2], "color": "#00ff00"}]},
			{"layers": [{"type": "solid", "color": "#0000ff", "opacity": 0.5}]}
		],
		"clips": {"flow": {"frames": [0, 1, 2], "fps": 4}}}`
	tx := compile(t, src, Options{})
	img := tx.Data.Levels[0]
	if img.W != 6 || img.H != 2 || tx.Grid != [2]int{3, 1} {
		t.Fatalf("sheet %d × %d grid %v, want 6 × 2 of 3 × 1 frames", img.W, img.H, tx.Grid)
	}
	var row []string
	for x := 0; x < 6; x++ {
		row = append(row, hex(img.Pix[img.W+x]))
	}
	want := []string{"#ff0000ff", "#000000ff", "#000000ff", "#00ff00ff", "#000080ff", "#000080ff"}
	if !reflect.DeepEqual(row, want) {
		t.Errorf("second row %v, want %v", row, want)
	}
	// Frames without layers of the texture's own are fine.
	compile(t, `{"veduta": "texture/1", "size": [1, 1], "frames": [{"layers": [{"type": "solid", "color": "#ffffff"}]}]}`, Options{})
}

func TestSheetErrors(t *testing.T) {
	cases := map[string][]string{
		`"size": [8, 4], "grid": [3, 2], "layers": [{"type": "solid", "color": "#ffffff"}]`: {
			`grid: 3 × 2 frames do not divide the 8 × 4 pixels evenly`},
		`"size": [8, 4], "grid": [0, 2], "layers": [{"type": "solid", "color": "#ffffff"}]`: {
			`grid[0]: 0 out of range [1, 256]`},
		`"size": [8, 4], "grid": [4, 2], "frames": [], "layers": [{"type": "solid", "color": "#ffffff"}]`: {
			`frames: not allowed with grid`},
		`"size": [48, 24], "autotile": true, "layers": [{"type": "solid", "color": "#ffffff"}], "edge": {"priority": 1}`: {
			`autotile: not with edge`},
		`"size": [30, 15], "autotile": true, "layers": [{"type": "solid", "color": "#ffffff"}]`: {
			`autotile: a frame must be 6 × 3 square tiles of an even size (the island's 3 × 3, then the lake's), got 30 × 15`},
		`"size": [96, 24], "grid": [1, 1], "autotile": true, "layers": [{"type": "solid", "color": "#ffffff"}]`: {
			`got 96 × 24`},
		`"size": [2, 2], "frames": []`: {
			`frames: 0 frames, want 1 to 256`},
		`"size": [2048, 2], "frames": [{"layers": [{"type": "solid", "color": "#ffffff"}]}, {"layers": [{"type": "solid", "color": "#ffffff"}]}, {"layers": [{"type": "solid", "color": "#ffffff"}]}]`: {
			`make a sheet of 6144 pixels, want at most 4096`},
		`"size": [2, 2], "frames": [{"layers": []}]`: {
			`frames[0].layers: at least one layer is required`},
		`"size": [2, 2], "frames": [{"layers": [{"type": "solid"}]}]`: {
			`frames[0].layers[0].color: is required`},
		`"size": [8, 4], "tiling": true, "grid": [4, 2], "layers": [{"type": "solid", "color": "#ffffff"}]`: {
			`tiling: a grid of frames cannot tile`},
		`"size": [8, 4], "layers": [{"type": "solid", "color": "#ffffff"}], "clips": {"walk": {"frames": [0], "fps": 1}}`: {
			`clips: need frames`},
		`"size": [8, 4], "grid": [4, 2], "layers": [{"type": "solid", "color": "#ffffff"}], "clips": {"Walk": {"frames": [8, -1], "fps": 0}, "hit": {"frames": [], "fps": 2000, "next": "nope"}}, "play": "run"`: {
			`clips.Walk: clip name "Walk" may only contain`,
			`clips.Walk.fps: must be a positive number, got 0`,
			`clips.Walk.frames[0]: frame 8 out of range [0, 7]`,
			`clips.Walk.frames[1]: frame -1 out of range [0, 7]`,
			`clips.hit.fps: 2000 out of range (0, 1000]`,
			`clips.hit.frames: at least one frame is required`,
			`clips.hit.next: only allowed when loop is false`,
			`play: no clip "run"`},
		`"size": [8, 4], "grid": [4, 2], "layers": [{"type": "solid", "color": "#ffffff"}], "clips": {"hit": {"frames": [0], "fps": 2, "loop": false, "next": "nope"}}`: {
			`clips.hit.next: no clip "nope"`},
		`"size": [8, 4], "grid": [4, 2], "layers": [{"type": "solid", "color": "#ffffff"}], "clips": {"hit": {"frames": [0]}}`: {
			`clips.hit.fps: is required`},
		`"size": [15, 16], "layers": [{"type": "solid", "color": "#ffffff"}], "edge": {"priority": 0, "width": 9, "roughness": 2}`: {
			`edge: a frame's width and height must be even to have an edge, got 15 × 16`,
			`edge.priority: 0 out of range [1, 1000]`,
			`edge.roughness: 2 out of range [0, 1]`,
			`edge.width: 9 out of range [0, 7.5]`},
		`"size": [16, 16], "layers": [{"type": "solid", "color": "#ffffff"}], "edge": {"width": 0}`: {
			`edge.priority: is required`,
			`edge.width: must be above 0`},
	}
	for body, wants := range cases {
		es := compileErrs(t, "s.vtex", `{"veduta": "texture/1", `+body+`}`, Options{})
		for _, w := range wants {
			if !strings.Contains(es.Error(), w) {
				t.Errorf("%s\nreports:\n%v\nwant %q", body, es, w)
			}
		}
	}
}

func TestEdge(t *testing.T) {
	tx := compile(t, `{"veduta": "texture/1", "size": [32, 16], "grid": [2, 1], "layers": [{"type": "solid", "color": "#ffffff"}], "edge": {"priority": 5, "seed": 3}}`, Options{})
	if want := (asset.Edge{Priority: 5, Width: 4, Roughness: 0.5, Seed: 3}); tx.Edge == nil || *tx.Edge != want {
		t.Errorf("edge %+v, want %+v (width a quarter of the 16 × 16 frame)", tx.Edge, want)
	}
}

func TestAutotile(t *testing.T) {
	tx := compile(t, `{"veduta": "texture/1", "size": [96, 24], "grid": [2, 1], "layers": [{"type": "solid", "color": "#ffffff"}], "autotile": true}`, Options{})
	if !tx.Autotile {
		t.Error("autotile not set")
	}
}

func TestClipFrame(t *testing.T) {
	walk := asset.Clip{Frames: []int{4, 5, 6}, FPS: 8, Loop: true}
	hit := asset.Clip{Frames: []int{1, 2}, FPS: 10}
	var got []int
	for tick := 0; tick < 10; tick++ {
		f, _ := walk.Frame(tick, 20)
		got = append(got, f)
	}
	// 8 fps at 20 ticks a second: steps at ticks 0, 3 (2.5 → ⌊⌋), 5, 8.
	if want := []int{4, 4, 4, 5, 5, 6, 6, 6, 4, 4}; !reflect.DeepEqual(got, want) {
		t.Errorf("walk frames %v, want %v", got, want)
	}
	for tick, want := range []struct {
		frame int
		ended bool
	}{{1, false}, {1, false}, {2, false}, {2, false}, {2, true}} {
		if f, ended := hit.Frame(tick, 20); f != want.frame || ended != want.ended {
			t.Errorf("hit at %d: %d %v, want %d %v", tick, f, ended, want.frame, want.ended)
		}
	}
}
