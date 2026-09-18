package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"

	formats "github.com/riftbane/veduta/v2/internal/schema"
	"github.com/riftbane/veduta/v2/script"
)

// editorDir is where a project keeps what editors read from the engine: the schemas of the
// source formats and, for a Lua game, the definitions of the API. It belongs to the engine
// version: init writes it and upgrade rewrites it.
const editorDir = ".veduta"

// editorFiles are the files that set an editor (VS Code, or any with a JSON and a Lua
// language server) up for a project: completion and checks in veduta.json and every asset
// and scenario, and in a Lua game's scripts. The files under editorDir are the engine's
// and are replaced; the settings files are the project's too, so only the keys named here
// are set in them. Paths are slash-separated.
func editorFiles(lua bool) (owned map[string][]byte, settings map[string]map[string]any) {
	owned = map[string][]byte{}
	type schemaRef struct {
		FileMatch []string `json:"fileMatch"`
		URL       string   `json:"url"`
	}
	var refs []schemaRef
	// The sources are JSON under suffixes of their own (crate.vmodel): the editor opens
	// them as JSON.
	assoc := map[string]string{}
	for _, f := range formats.Formats {
		b, _ := formats.FS.ReadFile(f.Schema)
		p := path.Join(editorDir, "schema", f.Schema)
		owned[p] = b
		refs = append(refs, schemaRef{FileMatch: f.Match, URL: "./" + p})
		for _, m := range f.Match {
			if path.Ext(m) != ".json" {
				assoc[m] = "json"
			}
		}
	}
	settings = map[string]map[string]any{
		".vscode/settings.json":   {"json.schemas": refs, "files.associations": assoc},
		".vscode/extensions.json": {"recommendations": []string{"golang.go"}},
	}
	if lua {
		owned[path.Join(editorDir, "lua", "veduta.d.lua")] = script.Types
		settings[".luarc.json"] = map[string]any{
			"runtime.version": "Lua 5.4",
			// What the engine's Lua leaves out (lua docs topic).
			"runtime.builtin":   map[string]string{"io": "disable", "os": "disable", "debug": "disable", "package": "disable"},
			"workspace.library": []string{editorDir + "/lua"},
		}
		settings[".vscode/extensions.json"] = map[string]any{"recommendations": []string{"sumneko.lua"}}
	}
	return owned, settings
}

// writeEditorFiles writes the editor files into a project and returns the ones it changed,
// sorted. A settings file that is there but is not a JSON object (VS Code allows comments)
// is left alone and named in skipped.
func writeEditorFiles(root string, lua bool) (changed, skipped []string, err error) {
	owned, settings := editorFiles(lua)
	files := map[string][]byte{}
	for p, b := range owned {
		files[p] = b
	}
	for p, keys := range settings {
		old, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		obj := map[string]any{}
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			return nil, nil, err
		case json.Unmarshal(old, &obj) != nil || obj == nil:
			skipped = append(skipped, p)
			continue
		}
		before := map[string]string{} // the engine's keys as the file has them
		for k := range keys {
			b, _ := json.Marshal(obj[k])
			before[k] = string(b)
		}
		same := err == nil
		for k, v := range keys {
			if list, ok := v.([]string); ok && k == "recommendations" {
				// Keep the project's own recommendations beside the engine's.
				if have, ok := obj[k].([]any); ok {
					for _, h := range have {
						if s, ok := h.(string); ok && !slices.Contains(list, s) {
							list = append(list, s)
						}
					}
				}
				v = list
			}
			if m, ok := v.(map[string]string); ok {
				// Keep the project's own associations beside the engine's.
				merged := map[string]any{}
				if have, ok := obj[k].(map[string]any); ok {
					for h, x := range have {
						merged[h] = x
					}
				}
				for h, x := range m {
					merged[h] = x
				}
				v = merged
			}
			obj[k] = v
			if b, _ := json.Marshal(v); string(b) != before[k] {
				same = false
			}
		}
		if same { // left as the project wrote it, down to its layout
			continue
		}
		files[p] = editorJSON(obj)
	}
	for _, p := range sortedKeys(files) {
		full := filepath.Join(root, filepath.FromSlash(p))
		if old, err := os.ReadFile(full); err == nil && bytes.Equal(old, files[p]) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return changed, skipped, err
		}
		if err := os.WriteFile(full, files[p], 0o644); err != nil {
			return changed, skipped, err
		}
		changed = append(changed, p)
	}
	slices.Sort(skipped)
	return changed, skipped, nil
}

// refreshEditorFiles brings the editor files of a project set up for editors (it has
// editorDir) to this tool's version, as upgrade does: a tool that learned a format or a
// field is then what VS Code checks sources against. Failures are not the build's.
func refreshEditorFiles(root string, lua bool) {
	if _, err := os.Stat(filepath.Join(root, editorDir)); err == nil {
		writeEditorFiles(root, lua)
	}
}

func editorJSON(v any) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(v)
	return b.Bytes()
}
