package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// workflowStep returns the shell script of the step called name in a GitHub Actions
// workflow: the `run: |` block scalar that follows `- name: <name>`, with the block's
// indentation removed, as the runner hands it to the shell.
func workflowStep(t *testing.T, workflow, name string) string {
	t.Helper()
	lines := strings.Split(workflow, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) != "- name: "+name {
			continue
		}
		if i+1 >= len(lines) || strings.TrimSpace(lines[i+1]) != "run: |" {
			t.Fatalf("step %q has no run block", name)
		}
		indent := ""
		var script []string
		for _, b := range lines[i+2:] {
			if strings.TrimSpace(b) == "" {
				script = append(script, "")
				continue
			}
			if indent == "" {
				indent = b[:len(b)-len(strings.TrimLeft(b, " "))]
			}
			if !strings.HasPrefix(b, indent) {
				break
			}
			script = append(script, strings.TrimPrefix(b, indent))
		}
		return strings.Join(script, "\n")
	}
	t.Fatalf("workflow has no step %q:\n%s", name, workflow)
	return ""
}

// flatObject decodes a JSON object of string fields, refusing duplicate keys, nesting
// and trailing data: what the console's card/1 reader and the engine's strict decoders
// accept.
func flatObject(data []byte) (map[string]string, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, fmt.Errorf("not an object (%v, %v)", tok, err)
	}
	m := map[string]string{}
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return nil, err
		}
		v, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := k.(string)
		val, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("field %q is %v, not a string", key, v)
		}
		if _, dup := m[key]; dup {
			return nil, fmt.Errorf("duplicate key %q", key)
		}
		m[key] = val
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing data after the object")
	}
	return m, nil
}

// TestReleaseWorkflowCard runs the archive step of the release workflow veduta init
// writes, with a stand-in for `go build`, on cards in every layout an author may give
// them, and checks what each archive holds: one card/1 object whose version is the tag,
// and the manifest's icon as icon.png named by the card. A card that is not a card/1
// object, or an icon that does not exist, stops the job before anything is built.
func TestReleaseWorkflowCard(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the release workflow runs on ubuntu-latest")
	}
	for _, tool := range []string{"sh", "jq", "tar", "sha256sum"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not available: %v", tool, err)
		}
	}
	dir := filepath.Join(t.TempDir(), "demo")
	env := &Env{Version: "dev", Stdout: io.Discard, Stderr: io.Discard}
	if _, err := Init(env, InitOptions{Dir: dir, NoTidy: true}); err != nil {
		t.Fatal(err)
	}
	wf, err := os.ReadFile(filepath.Join(dir, ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	// Game binaries are built by the toolchain the engine's CI checks for arm64.
	if !strings.Contains(string(wf), "go-version: stable") {
		t.Errorf("release workflow does not build with Go stable:\n%s", wf)
	}
	script := workflowStep(t, string(wf), "build archives")

	// `go build -o F` only has to leave a file behind.
	bin := t.TempDir()
	stub := "#!/bin/sh\nwhile [ $# -gt 0 ]; do\n\tif [ \"$1\" = -o ]; then : >\"$2\"; fi\n\tshift\ndone\n"
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "veduta.json"))
	if err != nil {
		t.Fatal(err)
	}
	withIcon := strings.Replace(string(manifest), `"veduta": "project/1",`, `"veduta": "project/1", "icon": "art/tile.png",`, 1)
	if withIcon == string(manifest) {
		t.Fatalf("cannot add an icon to veduta.json:\n%s", manifest)
	}
	iconPNG := []byte("\x89PNG\r\n\x1a\n tile")
	templateCard, err := os.ReadFile(filepath.Join(dir, "card.json"))
	if err != nil {
		t.Fatal(err)
	}

	const tag = "v1.2.3"
	run := func(t *testing.T, card, manifest string, icon bool) (string, error) {
		t.Helper()
		for _, p := range []string{"build", "dist", "art"} {
			os.RemoveAll(filepath.Join(dir, p))
		}
		os.WriteFile(filepath.Join(dir, "card.json"), []byte(card), 0o644)
		os.WriteFile(filepath.Join(dir, "veduta.json"), []byte(manifest), 0o644)
		if icon {
			os.MkdirAll(filepath.Join(dir, "art"), 0o755)
			os.WriteFile(filepath.Join(dir, "art", "tile.png"), iconPNG, 0o644)
		}
		cmd := exec.Command("sh", "-c", script)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GITHUB_REF_NAME="+tag, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	archive := func(t *testing.T, arch string) map[string][]byte {
		t.Helper()
		f, err := os.Open(filepath.Join(dir, "dist", "demo_"+tag+"_linux_"+arch+".tar.gz"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		zr, err := gzip.NewReader(f)
		if err != nil {
			t.Fatal(err)
		}
		files := map[string][]byte{}
		tr := tar.NewReader(zr)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				return files
			}
			if err != nil {
				t.Fatal(err)
			}
			if h.Typeflag == tar.TypeReg {
				files[h.Name], _ = io.ReadAll(tr)
			}
		}
	}

	for _, c := range []struct {
		name, card string
		icon       bool
		want       map[string]string
	}{
		{"template", string(templateCard), false,
			map[string]string{"veduta": "card/1", "title": "demo", "name": "demo", "exec": "demo", "version": tag}},
		{"one line", `{"veduta": "card/1", "title": "demo", "name": "demo", "exec": "demo"}`, false,
			map[string]string{"veduta": "card/1", "title": "demo", "name": "demo", "exec": "demo", "version": tag}},
		{"compact, veduta last", `{"title":"Caverna delle Gemme","exec":"demo","veduta":"card/1"}`, false,
			map[string]string{"veduta": "card/1", "title": "Caverna delle Gemme", "exec": "demo", "version": tag}},
		{"versioned, with icon", "{ \"veduta\": \"card/1\", \"title\": \"demo\", \"name\": \"demo\", \"version\": \"v1.2.0\",\n  \"exec\": \"demo\", \"icon\": \"old.png\" }\n", true,
			map[string]string{"veduta": "card/1", "title": "demo", "name": "demo", "exec": "demo", "version": tag, "icon": "icon.png"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := string(manifest)
			if c.icon {
				m = withIcon
			}
			if out, err := run(t, c.card, m, c.icon); err != nil {
				t.Fatalf("build archives: %v\n%s", err, out)
			}
			for _, arch := range []string{"arm64", "amd64"} {
				files := archive(t, arch)
				got, err := flatObject(files["demo/card.json"])
				if err != nil {
					t.Fatalf("%s card.json: %v\n%s", arch, err, files["demo/card.json"])
				}
				if !reflect.DeepEqual(got, c.want) {
					t.Errorf("%s card.json = %v, want %v", arch, got, c.want)
				}
				if png, ok := files["demo/icon.png"]; ok != c.icon || (c.icon && !bytes.Equal(png, iconPNG)) {
					t.Errorf("%s icon.png shipped %v (%q), want %v", arch, ok, png, c.icon)
				}
				for _, f := range []string{"demo/demo", "demo/veduta.json", "demo/README.md"} {
					if _, ok := files[f]; !ok {
						t.Errorf("%s archive has no %s", arch, f)
					}
				}
			}
		})
	}

	for _, c := range []struct {
		name, card string
		icon       bool
		manifest   string
		msg        string
	}{
		{"trailing comma", `{"veduta": "card/1", "exec": "demo",}`, false, string(manifest), `whose "veduta" is "card/1"`},
		{"not an object", `[{"veduta": "card/1"}]`, false, string(manifest), `whose "veduta" is "card/1"`},
		{"two objects", `{"veduta": "card/1"} {"veduta": "card/1"}`, false, string(manifest), `whose "veduta" is "card/1"`},
		{"empty", "", false, string(manifest), `whose "veduta" is "card/1"`},
		{"another format", `{"veduta": "card/2", "exec": "demo"}`, false, string(manifest), `whose "veduta" is "card/1"`},
		{"missing icon", string(templateCard), false, withIcon, "names the icon art/tile.png, which does not exist"},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, err := run(t, c.card, c.manifest, c.icon)
			if err == nil || !strings.Contains(out, c.msg) {
				t.Fatalf("build archives accepted a bad card (%v), want a message with %q:\n%s", err, c.msg, out)
			}
			if _, err := os.Stat(filepath.Join(dir, "build", "linux_arm64")); err == nil {
				t.Error("the job built a target before refusing the card")
			}
		})
	}
}
