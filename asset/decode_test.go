package asset

import (
	"errors"
	"strings"
	"testing"

	"github.com/riftbane/veduta/v2/gmath"
)

const modelSrc = `{
  "veduta": "model/1",
  "name": "crate",
  "parts": [
    { "shape": "box", "size": [1, 1, 1] },
    { "shape": "cylinder", "radius": 0.1, "height": 1.2 }
  ]
}`

func TestDecodeOK(t *testing.T) {
	var m ModelSource
	loc, err := Decode("crate.model.json", []byte(modelSrc), TypeModel, &m)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "crate" || len(m.Parts) != 2 || *m.Parts[1].Radius != 0.1 {
		t.Fatalf("decoded %+v", m)
	}
	cases := map[string][2]int{
		"":                 {1, 1},
		"veduta":           {2, 13},
		"parts":            {4, 12},
		"parts[0]":         {5, 5},
		"parts[1].radius":  {6, 38},
		"parts[0].size[2]": {5, 38},
		"parts[1].missing": {6, 5}, // falls back to the parent object
	}
	for path, want := range cases {
		if l, c := loc.Pos(path); l != want[0] || c != want[1] {
			t.Errorf("Pos(%q) = %d:%d, want %d:%d", path, l, c, want[0], want[1])
		}
	}
	e := loc.Errorf("parts[1].radius", "too small")
	if e.Error() != "crate.model.json:6:38: parts[1].radius: too small" {
		t.Errorf("Errorf = %q", e.Error())
	}
}

func decodeErr(t *testing.T, src string) *SourceError {
	t.Helper()
	var m ModelSource
	_, err := Decode("m.model.json", []byte(src), TypeModel, &m)
	if err == nil {
		t.Fatalf("expected an error for %s", src)
	}
	var se *SourceError
	if !errors.As(err, &se) {
		t.Fatalf("error %v is not a SourceError", err)
	}
	return se
}

func TestDecodeErrors(t *testing.T) {
	cases := []struct {
		name, src string
		line, col int
		msg       string
	}{
		{"unknown field", "{\n \"veduta\": \"model/1\",\n \"parts\": [ {\"shape\": \"box\", \"sizee\": [1]} ]\n}", 3, 30, "parts[0].sizee: unknown field"},
		{"wrong type", "{\n \"veduta\": \"model/1\",\n \"parts\": [ {\"shape\": 3} ]\n}", 3, 23, "parts[0].shape: cannot use JSON number"},
		{"syntax", "{\n \"veduta\": \"model/1\",\n \"parts\": [ {\"shape\" \"box\"} ]\n}", 3, 22, "invalid JSON"},
		{"duplicate", "{\"veduta\": \"model/1\", \"parts\": [], \"parts\": []}", 1, 36, "duplicate key \"parts\""},
		{"missing header", "{\"parts\": []}", 1, 1, "missing \"veduta\" header"},
		{"wrong header", "{\"veduta\": \"texture/1\"}", 1, 12, "want \"model/1\""},
		{"trailing", "{\"veduta\": \"model/1\"} {}", 1, 23, "unexpected data"},
		{"not object", "[1, 2]", 1, 1, "must be a JSON object"},
		{"truncated", "{\"veduta\": \"model/1\", \"parts\": [", 1, 33, "unexpected end"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := decodeErr(t, c.src)
			if e.Line != c.line || e.Col != c.col || !strings.Contains(e.Msg, c.msg) {
				t.Errorf("got %d:%d %q, want %d:%d containing %q", e.Line, e.Col, e.Msg, c.line, c.col, c.msg)
			}
		})
	}
}

func TestCheckerHelpers(t *testing.T) {
	var m ModelSource
	loc, err := Decode("m.model.json", []byte(modelSrc), TypeModel, &m)
	if err != nil {
		t.Fatal(err)
	}
	c := NewChecker(loc)
	if v := c.Vec3("parts[0].size", m.Parts[0].Size, gmath.Zero3); v.X != 1 || !c.OK() {
		t.Fatal("valid vec3 rejected")
	}
	c.Vec3("parts[0].size", []float32{1, 2}, gmath.Zero3)
	c.Color("parts[0].color", "#12345", 0)
	c.Enum("parts[0].shape", "cube", []string{"box"}, "box")
	c.Positive("parts[1].radius", ptr(float32(-1)), 1)
	c.Forbid("parts[0]", "shape box", map[string]bool{"radius": true, "height": false})
	if len(c.Errs) != 5 {
		t.Fatalf("got %d errors: %v", len(c.Errs), c.Errs)
	}
	if !strings.Contains(c.Err().Error(), "parts[0].radius: not used by shape box") {
		t.Fatalf("errors: %v", c.Err())
	}
}

func TestValidName(t *testing.T) {
	for _, ok := range []string{"crate", "gem_1", "a-b", "9lives"} {
		if ValidName(ok) != nil {
			t.Errorf("%q rejected", ok)
		}
	}
	for _, bad := range []string{"", "Crate", "_x", "a b", "a/b", strings.Repeat("a", 65)} {
		if ValidName(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestKindFiles(t *testing.T) {
	if n, ok := KindTexture.NameFromFile("wood.tex.json"); !ok || n != "wood" {
		t.Errorf("texture name = %q %v", n, ok)
	}
	if _, ok := KindModel.NameFromFile(".model.json"); ok {
		t.Error("empty name accepted")
	}
	if KindScenario.Dir() != "tests/scenarios" || KindMaterial.Ext() != ".mat.json" {
		t.Error("kind layout changed")
	}
}

func ptr[T any](v T) *T { return &v }
