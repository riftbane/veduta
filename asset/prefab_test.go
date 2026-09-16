package asset

import (
	"reflect"
	"testing"

	"github.com/riftbane/veduta/v2/gmath"
)

func TestParsePrefabFull(t *testing.T) {
	src := `{
  "veduta": "prefab/1",
  "footprint": [3, 2],
  "tags": ["house", "building"],
  "rules": { "biomes": ["plain", "forest"], "min_distance": { "house": 1, "city": 6.5 } },
  "entities": [
    { "name": "walls", "kind": "static", "model": "house", "material": "wood", "position": [1.5, 0, 1] },
    { "name": "roof", "kind": "static", "model": "roof", "parent": "walls", "position": [0, 2, 0] },
    { "name": "door", "kind": "static", "position": [1.5, 0, 2], "hitbox": [[-0.5, 0, -0.1], [0.5, 2, 0.1]], "tags": ["door"] }
  ]
}`
	p, err := ParsePrefab("house.prefab.json", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	want := &Prefab{
		Name:      "house",
		Footprint: gmath.V2(3, 2),
		Tags:      []string{"house", "building"},
		Biomes:    []string{"plain", "forest"},
		Distances: []Distance{{"city", 6.5}, {"house", 1}},
	}
	got := *p
	got.Entities = nil
	if !reflect.DeepEqual(&got, want) {
		t.Fatalf("got %+v\nwant %+v", &got, want)
	}
	if len(p.Entities) != 3 || p.Entities[1].Parent != "walls" || p.Entities[2].Hitbox == nil || p.Entities[0].Scale != gmath.One3 {
		t.Fatalf("entities %+v", p.Entities)
	}
	if p.Distance("city") != 6.5 || p.Distance("tree") != 0 || p.MaxDistance() != 6.5 || !p.HasTag("house") || p.HasTag("tree") {
		t.Fatal("rule lookups")
	}
}

func TestParsePrefabMinimal(t *testing.T) {
	p, err := ParsePrefab("clearing.prefab.json", []byte(`{"veduta": "prefab/1", "footprint": [4, 4], "entities": []}`))
	if err != nil {
		t.Fatal(err)
	}
	if p.Tags != nil || p.Biomes != nil || p.Distances != nil || p.Entities != nil {
		t.Fatalf("%+v", p)
	}
}

func TestParsePrefabErrors(t *testing.T) {
	fp := `"footprint": [2, 2]`
	cases := []struct {
		name, src string
		wants     []wantErr
	}{
		{"missing footprint", `{"veduta": "prefab/1", "entities": []}`,
			[]wantErr{{`footprint: is required`, ""}}},
		{"footprint length", `{"veduta": "prefab/1", "footprint": [2], "entities": []}`,
			[]wantErr{{`footprint: want 2 numbers, got 1`, `[2]`}}},
		{"footprint zero", `{"veduta": "prefab/1", "footprint": [0, 2], "entities": []}`,
			[]wantErr{{`footprint[0]: 0 out of range (0, 1024]`, `0,`}}},
		{"footprint huge", `{"veduta": "prefab/1", "footprint": [2, 2000], "entities": []}`,
			[]wantErr{{`footprint[1]: 2000 out of range (0, 1024]`, `2000`}}},
		{"duplicate tag", `{"veduta": "prefab/1", ` + fp + `, "tags": ["a", "a"], "entities": []}`,
			[]wantErr{{`tags[1]: duplicate "a"`, `"a"]`}}},
		{"bad biome name", `{"veduta": "prefab/1", ` + fp + `, "rules": {"biomes": ["Plain"]}, "entities": []}`,
			[]wantErr{{`rules.biomes[0]: name "Plain"`, `"Plain"`}}},
		{"negative distance", `{"veduta": "prefab/1", ` + fp + `, "rules": {"min_distance": {"city": -1}}, "entities": []}`,
			[]wantErr{{`rules.min_distance.city: -1 out of range [0, 4096]`, `-1`}}},
		{"bad distance tag", `{"veduta": "prefab/1", ` + fp + `, "rules": {"min_distance": {"City": 1}}, "entities": []}`,
			[]wantErr{{`rules.min_distance.City: name "City"`, `1}`}}},
		{"camera entity", `{"veduta": "prefab/1", ` + fp + `, "entities": [{"name": "cam", "kind": "camera"}]}`,
			[]wantErr{{`entities[0].kind: a prefab cannot hold a camera`, `"camera"`}}},
		{"unknown parent", `{"veduta": "prefab/1", ` + fp + `, "entities": [{"name": "a", "kind": "static", "parent": "b"}]}`,
			[]wantErr{{`entities[0].parent: no entity named "b" in this prefab`, `"b"`}}},
		{"unknown field", `{"veduta": "prefab/1", ` + fp + `, "cells": 2, "entities": []}`,
			[]wantErr{{`cells: unknown field`, `"cells"`}}},
		{"wrong header", `{"veduta": "scene/1", ` + fp + `, "entities": []}`,
			[]wantErr{{`header is "scene/1", want "prefab/1"`, `"scene/1"`}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParsePrefab("p.prefab.json", []byte(c.src))
			checkErrs(t, c.src, err, c.wants...)
		})
	}
}

func TestPrefabDocExamples(t *testing.T) {
	for i, ex := range docJSON(t, "prefab.md") {
		if _, err := ParsePrefab("example.prefab.json", []byte(ex)); err != nil {
			t.Errorf("docs/prefab.md example %d: %v", i, err)
		}
	}
}
