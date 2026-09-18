package schema

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/riftbane/veduta/v2/asset"
)

var update = flag.Bool("update", false, "rewrite the schemas from asset/source.go")

// roots are the formats and the struct each one decodes into.
var roots = map[string]struct{ root, header string }{
	"project.schema.json":  {"ProjectSource", asset.TypeProject},
	"model.schema.json":    {"ModelSource", asset.TypeModel},
	"texture.schema.json":  {"TextureSource", asset.TypeTexture},
	"material.schema.json": {"MaterialSource", asset.TypeMaterial},
	"scene.schema.json":    {"SceneSource", asset.TypeScene},
	"scenario.schema.json": {"ScenarioSource", asset.TypeScenario},
	"prefab.schema.json":   {"PrefabSource", asset.TypePrefab},
	"world.schema.json":    {"WorldSource", asset.TypeWorld},
	"map.schema.json":      {"MapSource", asset.TypeMap},
}

// enums are the values fields allow, by struct and JSON name, as the compilers check them
// (asset/model, asset/texture, asset/material.go, asset/scene.go, asset/world.go).
var enums = map[string][]any{
	"ModelSource.units":     {"m"},
	"ModelSource.pivot":     {"origin", "center", "bottom-center"},
	"ModelSource.symmetry":  {"x", "y", "z"},
	"PartSource.shape":      {"box", "cylinder", "sphere", "plane", "extrude", "lathe", "mirror"},
	"PartSource.axis":       {"x", "y", "z"},
	"PartSource.uv":         {"box", "planar", "cylindrical", "spherical"},
	"LayerSource.type":      {"solid", "noise", "stripes", "rect", "circle", "gradient", "checker", "image"},
	"LayerSource.blend":     {"normal", "multiply", "screen", "add"},
	"LayerSource.fit":       {"contain", "cover", "stretch"},
	"MaterialSource.alpha":  {"opaque", "blend", "cutout"},
	"MaterialSource.cull":   {"back", "none"},
	"MaterialSource.filter": {"bilinear", "nearest"},
	"CameraSource.type":     {"perspective", "orthographic"},
	"ExpectSource.op":       anys(asset.ExpectOps),
	"FeatureSource.kind":    anys(asset.FeatureKinds),
	"InputSource.press[]":   anys(asset.ButtonNames),
	"InputSource.release[]": anys(asset.ButtonNames),
	"PlaceSource.rotation":  {0, 90, 180, 270},
}

// required overrides the rule (a field without omitempty that is not a pointer is required)
// where the checks say otherwise.
var required = map[string][]string{
	"ProjectSource":    {"veduta", "name", "engine"},
	"SceneSource":      {"veduta", "camera"},
	"WorldSource":      {"veduta", "camera", "biomes"},
	"TextureSource":    {"veduta", "size"},
	"ClipSource":       {"frames", "fps"},
	"EdgeSource":       {"priority"},
	"MapTerrainSource": {"key", "name"},
}

func anys(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

// generate builds every schema from the source structs.
func generate(t *testing.T) map[string][]byte {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join("..", "..", "asset", "source.go"), nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	structs := map[string]*ast.TypeSpec{}
	docs := map[string]string{}
	for _, d := range file.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, s := range g.Specs {
			if ts, ok := s.(*ast.TypeSpec); ok {
				if _, ok := ts.Type.(*ast.StructType); ok {
					structs[ts.Name.Name] = ts
					docs[ts.Name.Name] = text(g.Doc)
				}
			}
		}
	}
	out := map[string][]byte{}
	for name, r := range roots {
		defs := map[string]any{}
		var def func(string) map[string]any
		var typ func(owner, field string, e ast.Expr) map[string]any
		typ = func(owner, field string, e ast.Expr) map[string]any {
			switch x := e.(type) {
			case *ast.StarExpr:
				return typ(owner, field, x.X)
			case *ast.ArrayType:
				return map[string]any{"type": "array", "items": typ(owner, field+"[]", x.Elt)}
			case *ast.MapType:
				return map[string]any{"type": "object", "additionalProperties": typ(owner, field+"{}", x.Value)}
			case *ast.InterfaceType:
				return map[string]any{}
			case *ast.Ident:
				s := map[string]any{}
				switch x.Name {
				case "string":
					s["type"] = "string"
				case "bool":
					s["type"] = "boolean"
				case "int", "int64":
					s["type"] = "integer"
				case "uint64":
					s["type"], s["minimum"] = "integer", 0
				case "float32", "float64":
					s["type"] = "number"
				case "any":
				default:
					if _, ok := structs[x.Name]; !ok {
						t.Fatalf("%s.%s: type %s", owner, field, x.Name)
					}
					if _, done := defs[x.Name]; !done {
						defs[x.Name] = nil
						defs[x.Name] = def(x.Name)
					}
					return map[string]any{"$ref": "#/definitions/" + x.Name}
				}
				if values, ok := enums[owner+"."+field]; ok && values != nil {
					s["enum"] = values
				}
				return s
			}
			t.Fatalf("%s.%s: unhandled type %T", owner, field, e)
			return nil
		}
		def = func(sname string) map[string]any {
			st := structs[sname].Type.(*ast.StructType)
			props := map[string]any{}
			var req []string
			for _, f := range st.Fields.List {
				tag, _ := strconv.Unquote(f.Tag.Value)
				jsonTag := reflect.StructTag(tag).Get("json")
				jname, opts, _ := strings.Cut(jsonTag, ",")
				p := typ(sname, jname, f.Type)
				if jname == "veduta" && sname == r.root {
					p = map[string]any{"const": r.header}
				}
				if doc := strings.TrimSpace(text(f.Doc) + " " + text(f.Comment)); doc != "" {
					p["description"] = doc
				}
				props[jname] = p
				_, pointer := f.Type.(*ast.StarExpr)
				if !strings.Contains(opts, "omitempty") && !pointer {
					req = append(req, jname)
				}
			}
			if r, ok := required[sname]; ok {
				req = r
			}
			s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
			if len(req) > 0 {
				sort.Strings(req)
				s["required"] = req
			}
			if docs[sname] != "" {
				s["description"] = docs[sname]
			}
			return s
		}
		schema := def(r.root)
		schema["$schema"] = "http://json-schema.org/draft-07/schema#"
		schema["title"] = strings.TrimSuffix(name, ".schema.json")
		if len(defs) > 0 {
			schema["definitions"] = defs
		}
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(schema); err != nil {
			t.Fatal(err)
		}
		out[name] = b.Bytes()
	}
	return out
}

// text is a comment group as one line.
func text(g *ast.CommentGroup) string {
	if g == nil {
		return ""
	}
	return strings.Join(strings.Fields(g.Text()), " ")
}

// TestSchemasCurrent: the schemas are what asset/source.go gives now, one per format.
func TestSchemasCurrent(t *testing.T) {
	want := generate(t)
	if len(want) != len(Formats) {
		t.Fatalf("%d schemas for %d formats", len(want), len(Formats))
	}
	for _, f := range Formats {
		b, ok := want[f.Schema]
		if !ok {
			t.Fatalf("no schema generated for %s", f.Schema)
		}
		if *update {
			if err := os.WriteFile(f.Schema, b, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := FS.ReadFile(f.Schema)
		if err != nil || !bytes.Equal(got, b) {
			t.Errorf("%s is out of date: go test ./internal/schema -update", f.Schema)
		}
	}
}

// TestSourcesValidate checks every source file of the repository against its schema, so a
// schema that refuses a file the engine accepts is noticed.
func TestSourcesValidate(t *testing.T) {
	root := filepath.Join("..", "..")
	checked := 0
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); p != root && (strings.HasPrefix(n, ".") || n == "out") {
				return filepath.SkipDir
			}
			return nil
		}
		for _, f := range Formats {
			for _, m := range f.Match {
				if ok, _ := path.Match(m, d.Name()); !ok {
					continue
				}
				data, err := os.ReadFile(p)
				if err != nil {
					return err
				}
				var v any
				if json.Unmarshal(data, &v) != nil {
					continue // broken on purpose: a test fixture of the engine's own errors
				}
				if m, ok := v.(map[string]any); ok && m["veduta"] != roots[f.Schema].header {
					continue // a fixture of a wrong header
				}
				raw, _ := FS.ReadFile(f.Schema)
				var s map[string]any
				if err := json.Unmarshal(raw, &s); err != nil {
					t.Fatalf("%s: %v", f.Schema, err)
				}
				if problems := validate(s, s, v, ""); len(problems) > 0 && !strings.Contains(p, "invalid") && !strings.Contains(p, "bad") {
					if ok, _ := engineAccepts(p, data); ok {
						t.Errorf("%s: the engine accepts it, the schema does not: %s", p, strings.Join(problems, "; "))
					}
				}
				checked++
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 50 {
		t.Errorf("only %d source files checked", checked)
	}
}

// engineAccepts reports whether the engine's strict decoder takes the file: fixtures of
// errors are not the schema's business.
func engineAccepts(p string, data []byte) (bool, error) {
	var header struct {
		Veduta string `json:"veduta"`
	}
	json.Unmarshal(data, &header)
	var v any
	switch header.Veduta {
	case asset.TypeProject:
		v = &asset.ProjectSource{}
	case asset.TypeModel:
		v = &asset.ModelSource{}
	case asset.TypeTexture:
		v = &asset.TextureSource{}
	case asset.TypeMaterial:
		v = &asset.MaterialSource{}
	case asset.TypeScene:
		v = &asset.SceneSource{}
	case asset.TypeScenario:
		v = &asset.ScenarioSource{}
	case asset.TypePrefab:
		v = &asset.PrefabSource{}
	case asset.TypeWorld:
		v = &asset.WorldSource{}
	default:
		return false, nil
	}
	_, err := asset.Decode(p, data, header.Veduta, v)
	return err == nil, err
}

// validate checks a value against the part of JSON Schema the generated schemas use.
func validate(root, s map[string]any, v any, at string) []string {
	if ref, ok := s["$ref"].(string); ok {
		name := strings.TrimPrefix(ref, "#/definitions/")
		return validate(root, root["definitions"].(map[string]any)[name].(map[string]any), v, at)
	}
	var problems []string
	fail := func(format string, args ...any) { problems = append(problems, at+": "+fmt.Sprintf(format, args...)) }
	if c, ok := s["const"]; ok && !reflect.DeepEqual(c, v) {
		fail("want %v", c)
	}
	if e, ok := s["enum"].([]any); ok {
		found := false
		for _, x := range e {
			if reflect.DeepEqual(x, v) {
				found = true
			}
		}
		if !found {
			fail("%v is not one of %v", v, e)
		}
	}
	switch s["type"] {
	case "string":
		if _, ok := v.(string); !ok {
			fail("want a string")
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			fail("want a boolean")
		}
	case "number":
		if _, ok := v.(float64); !ok {
			fail("want a number")
		}
	case "integer":
		if f, ok := v.(float64); !ok || f != math.Trunc(f) {
			fail("want an integer")
		}
	case "array":
		a, ok := v.([]any)
		if !ok {
			fail("want an array")
			break
		}
		for i, x := range a {
			problems = append(problems, validate(root, s["items"].(map[string]any), x, fmt.Sprintf("%s[%d]", at, i))...)
		}
	case "object":
		o, ok := v.(map[string]any)
		if !ok {
			fail("want an object")
			break
		}
		props, _ := s["properties"].(map[string]any)
		for k, x := range o {
			if ps, ok := props[k].(map[string]any); ok {
				problems = append(problems, validate(root, ps, x, at+"."+k)...)
			} else if extra, ok := s["additionalProperties"].(map[string]any); ok {
				problems = append(problems, validate(root, extra, x, at+"."+k)...)
			} else if s["additionalProperties"] == false {
				fail("unknown field %s", k)
			}
		}
		if req, ok := s["required"].([]any); ok {
			for _, r := range req {
				if _, ok := o[r.(string)]; !ok {
					fail("missing %s", r)
				}
			}
		}
	}
	return problems
}

// TestValidateCatches: the validator this package's test relies on finds what it should.
func TestValidateCatches(t *testing.T) {
	raw, _ := FS.ReadFile("material.schema.json")
	var s map[string]any
	json.Unmarshal(raw, &s)
	for doc, want := range map[string]bool{
		`{"veduta": "material/1"}`:                   true,
		`{"veduta": "material/1", "alpha": "blend"}`: true,
		`{"veduta": "material/1", "alpha": "glass"}`: false,
		`{"veduta": "material/1", "shiny": true}`:    false,
		`{"veduta": "model/1"}`:                      false,
		`{"veduta": "material/1", "unlit": "yes"}`:   false,
	} {
		var v any
		json.Unmarshal([]byte(doc), &v)
		if got := len(validate(s, s, v, "")) == 0; got != want {
			t.Errorf("%s: valid %v, want %v", doc, got, want)
		}
	}
}
