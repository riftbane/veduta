package inspect

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/tilemap"
)

// Issue codes of Map.
const (
	MapMissingAsset   = "MAP_MISSING_ASSET"   // a terrain's texture or material is not in the library
	MapUnusedTerrain  = "MAP_UNUSED_TERRAIN"  // a terrain paints no cell
	MapObjectsOverlap = "MAP_OBJECTS_OVERLAP" // two objects cover a cell together
)

// MapCodes are the issue codes of Map, in the order they are documented.
var MapCodes = []string{MapMissingAsset, MapUnusedTerrain, MapObjectsOverlap}

// MapSheets are the sheets Map can write.
var MapSheets = []string{"summary"}

// UseMap draws the tile map called name with the scenes the renderer draws from now on:
// its textures, materials and chunk models are uploaded.
func (ir *Renderer) UseMap(name string) (*tilemap.Map, error) {
	src := ir.Lib.Maps[name]
	if src == nil {
		return nil, fmt.Errorf("unknown map %q (have: %s)", name, strings.Join(asset.Names(ir.Lib.Maps), ", "))
	}
	m := tilemap.New(src, ir.Lib)
	tex := m.Textures()
	for _, n := range asset.Names(tex) {
		if err := ir.res.AddTexture(ir.r, n, tex[n]); err != nil {
			return nil, err
		}
	}
	models := m.Models()
	for _, n := range asset.Names(models) {
		if err := ir.res.AddModel(ir.r, n, models[n]); err != nil {
			return nil, err
		}
	}
	mats := m.Materials()
	for _, n := range asset.Names(mats) {
		t := ir.Lib.Textures[mats[n].Texture]
		if t == nil {
			t = tex[mats[n].Texture]
		}
		ir.res.AddMaterial(n, mats[n], t)
	}
	ir.statics = m.Statics()
	return m, nil
}

// Map inspects a tile map: the assets its terrains need, how much each terrain and layer
// paints, and a picture of the whole map with its objects outlined.
func Map(ir *Renderer, name string, opt Options) (*Report, error) {
	if ir == nil || ir.Lib == nil {
		return nil, errors.New("inspect map: no library")
	}
	for _, s := range opt.Sheets {
		if s != "all" && s != "none" && !slices.Contains(MapSheets, s) {
			return nil, fmt.Errorf("inspect map %s: unknown sheet %q (valid: %s, all, none)", name, s, strings.Join(MapSheets, ", "))
		}
	}
	if opt.Focus != "" && !slices.Contains(MapCodes, opt.Focus) {
		return nil, fmt.Errorf("inspect map %s: unknown focus %q (valid: %s)", name, opt.Focus, strings.Join(MapCodes, ", "))
	}
	src := ir.Lib.Maps[name]
	if src == nil {
		return nil, fmt.Errorf("inspect map: unknown map %q (have: %s)", name, strings.Join(asset.Names(ir.Lib.Maps), ", "))
	}
	rep := &Report{Subject: "map:" + name, Metrics: map[string]any{}}
	counts := make([]int, len(src.Terrains))
	var layers []map[string]any
	for _, l := range src.Layers {
		n := 0
		for _, v := range l.Cells {
			if v > 0 {
				counts[v-1]++
				n++
			}
		}
		layers = append(layers, map[string]any{"name": l.Name, "cells": n, "z": l.Z, "layer": l.Layer})
	}
	var terrains []map[string]any
	for i, t := range src.Terrains {
		tm := map[string]any{"name": t.Name, "cells": counts[i]}
		tex := t.Texture
		switch {
		case t.Material != "" && ir.Lib.Materials[t.Material] == nil:
			rep.Add(Error, MapMissingAsset, 1, map[string]any{"terrain": t.Name, "material": t.Material},
				fmt.Sprintf("Terrain %s is drawn with material %q, which the project does not have: add assets/materials/%s.vmat or name another.", t.Name, t.Material, t.Material))
		case t.Material != "":
			tex = ir.Lib.Materials[t.Material].Texture
		case ir.Lib.Textures[t.Texture] == nil:
			rep.Add(Error, MapMissingAsset, 1, map[string]any{"terrain": t.Name, "texture": t.Texture},
				fmt.Sprintf("Terrain %s is drawn with texture %q, which the project does not have: add assets/textures/%s.vtex or name another.", t.Name, t.Texture, t.Texture))
		}
		if tx := ir.Lib.Textures[tex]; tx != nil && tx.Edge != nil {
			tm["edge_priority"] = tx.Edge.Priority
		} else if tx != nil && tx.Autotile {
			tm["autotile"] = true
		}
		if counts[i] == 0 {
			rep.Add(Info, MapUnusedTerrain, 1, map[string]any{"terrain": t.Name},
				fmt.Sprintf("Terrain %s paints no cell of the file: fine when the game paints with it (map.set), else remove it.", t.Name))
		}
		terrains = append(terrains, tm)
	}
	for i := range src.Objects {
		for j := i + 1; j < len(src.Objects); j++ {
			a, b := &src.Objects[i], &src.Objects[j]
			if a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H {
				rep.Add(Warning, MapObjectsOverlap, 1, map[string]any{"a": a.Name, "b": b.Name},
					fmt.Sprintf("Objects %s and %s cover a cell together: a game looking for the object at a cell finds either; move one unless they are meant to overlap.", a.Name, b.Name))
			}
		}
	}
	m := tilemap.New(src, ir.Lib)
	tris := 0
	models := m.Models()
	for _, n := range asset.Names(models) {
		tris += len(models[n].Mesh.Indices) / 3
	}
	rep.Metrics["size"] = [2]int{src.W, src.H}
	rep.Metrics["tile"] = src.Tile
	rep.Metrics["layers"] = layers
	rep.Metrics["terrains"] = terrains
	rep.Metrics["objects"] = len(src.Objects)
	rep.Metrics["chunks"] = len(models)
	rep.Metrics["triangles"] = tris
	if opt.wants("summary", true) {
		img, err := mapPicture(ir, src)
		if err != nil {
			return nil, fmt.Errorf("inspect map %s: sheet summary: %w", name, err)
		}
		p := opt.sheetPath(rep.Subject, "summary")
		if err := scnWritePNG(p, img); err != nil {
			return nil, fmt.Errorf("inspect map %s: sheet summary: %w", name, err)
		}
		rep.Sheets = append(rep.Sheets, p)
	}
	rep.Finish(opt.Focus)
	return rep, nil
}

// mapPicture is the map's Picture (tick 0) scaled to at most 1024 × 768 pixels (up by a
// whole factor, at most 4, when it is smaller), with every object outlined.
func mapPicture(ir *Renderer, src *asset.Map) (*gfx.Image, error) {
	m := tilemap.New(src, ir.Lib)
	pic := m.Picture(0, 0)
	w, h := pic.W, pic.H
	if k := min(1024/w, 768/h, 4); k >= 1 {
		w, h = w*k, h*k
	} else if w*768 > h*1024 {
		w, h = 1024, max(1, h*1024/w)
	} else {
		w, h = max(1, w*768/h), 768
	}
	img := gfx.NewImage(w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := pic.Pix[y*pic.H/h*pic.W+x*pic.W/w]
			if c>>24 == 0 {
				c = 0xff101018
			}
			img.Pix[y*w+x] = c
		}
	}
	cw, ch := m.CellSize()
	px := func(cells, cell, size, out int) int { return min(cells*cell*out/size, out-1) }
	for _, o := range src.Objects {
		x0, y0 := px(o.X, cw, pic.W, w), px(o.Y, ch, pic.H, h)
		x1, y1 := max(x0, px(o.X+o.W, cw, pic.W, w)-1), max(y0, px(o.Y+o.H, ch, pic.H, h)-1)
		for x := x0; x <= x1; x++ {
			img.Pix[y0*w+x], img.Pix[y1*w+x] = 0xffffe040, 0xffffe040
		}
		for y := y0; y <= y1; y++ {
			img.Pix[y*w+x0], img.Pix[y*w+x1] = 0xffffe040, 0xffffe040
		}
	}
	return img, nil
}
