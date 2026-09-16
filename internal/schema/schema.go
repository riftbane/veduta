// Package schema holds JSON Schemas of the engine's source formats for editors: completion,
// hover text and checks while typing. They are generated from the source structs of package
// asset (go test ./internal/schema -update writes them) and say what a schema can say: the
// fields, their types, the values a field allows, the fields a file must have. The engine's
// own checks (cook, build) stay the authority.
package schema

import "embed"

// FS holds the schemas, one <format>.schema.json per format.
//
//go:embed *.schema.json
var FS embed.FS

// Format is a source format: its schema and the file names it applies to.
type Format struct {
	Schema string
	Match  []string
}

// Formats are every source format, the manifest first.
var Formats = []Format{
	{"project.schema.json", []string{"veduta.json"}},
	{"model.schema.json", []string{"*.model.json"}},
	{"texture.schema.json", []string{"*.tex.json"}},
	{"material.schema.json", []string{"*.mat.json"}},
	{"scene.schema.json", []string{"*.scene.json"}},
	{"scenario.schema.json", []string{"*.scenario.json"}},
	{"prefab.schema.json", []string{"*.prefab.json"}},
	{"world.schema.json", []string{"*.world.json"}},
}
