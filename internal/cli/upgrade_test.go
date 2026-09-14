package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/riftbane/veduta/asset"
)

// upgradeProject writes a project holding manifest, with a go.mod that already requires
// the engine at version to (so the upgrade has no go command to run), and opens it.
func upgradeProject(t *testing.T, manifest, to string) *Session {
	t.Helper()
	dir := t.TempDir()
	mod := "module example.com/mygame\n\ngo 1.25\n\nrequire github.com/riftbane/veduta " + to + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, asset.ProjectFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := OpenSession(dir, &Env{Version: to})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// The migrations that pin each v0.x default, as the report names them.
const (
	pinResolution        = `pin "resolution": [1280, 720] in veduta.json (the default before v1.0.0)`
	pinInspectResolution = `pin "inspect_resolution": [640, 360] in veduta.json (the default before v1.0.0)`
	pinTickRate          = `pin "tick_rate": 60 in veduta.json (the default before v1.0.0)`
)

// TestUpgradePinsTheV0Defaults checks that a project crossing v1.0.0 keeps the
// resolution, inspection size and tick rate it ran with: every one its manifest left to
// the default (absent, or set to its zero value) is written out with the v0.x value,
// named in the report and in the changelog, and every other byte of veduta.json stays
// where it was. Projects that do not cross v1.0.0 only get their engine field rewritten.
func TestUpgradePinsTheV0Defaults(t *testing.T) {
	for _, c := range []struct {
		name, to, in, want string
		migrations         []string
		changelog          string // the entry written under Unreleased
	}{
		{
			name:       "minimal manifest",
			to:         "v1.0.0",
			in:         "{\n  \"veduta\": \"project/1\",\n  \"name\": \"mygame\",\n  \"engine\": \"v0.2.0\"\n}\n",
			want:       "{\n  \"veduta\": \"project/1\",\n  \"name\": \"mygame\",\n  \"engine\": \"v1.0.0\",\n  \"resolution\": [1280, 720],\n  \"inspect_resolution\": [640, 360],\n  \"tick_rate\": 60\n}\n",
			migrations: []string{pinResolution, pinInspectResolution, pinTickRate},
			changelog:  "- Upgrade the Veduta engine from v0.2.0 to v1.0.0 (`veduta upgrade`), pinning the defaults `veduta.json` relied on before v1.0.0 so the game keeps its size and tick rate: `\"resolution\": [1280, 720]`, `\"inspect_resolution\": [640, 360]`, `\"tick_rate\": 60`.\n",
		},
		{
			name:       "minimal manifest on one line",
			to:         "v1.0.0",
			in:         `{ "veduta": "project/1", "name": "mygame", "engine": "v0.1.0" }` + "\n",
			want:       `{ "veduta": "project/1", "name": "mygame", "engine": "v1.0.0", "resolution": [1280, 720], "inspect_resolution": [640, 360], "tick_rate": 60 }` + "\n",
			migrations: []string{pinResolution, pinInspectResolution, pinTickRate},
			changelog:  "- Upgrade the Veduta engine from v0.1.0 to v1.0.0 (`veduta upgrade`), pinning the defaults `veduta.json` relied on before v1.0.0 so the game keeps its size and tick rate: `\"resolution\": [1280, 720]`, `\"inspect_resolution\": [640, 360]`, `\"tick_rate\": 60`.\n",
		},
		{
			// The new keys go after entry, in the file's own indentation and key spacing;
			// the fields around them keep their bytes.
			name:       "tabs and other fields",
			to:         "v1.0.0",
			in:         "{\n\t\"veduta\":\"project/1\",\n\t\"name\":\"cave\",\n\t\"title\":\"Cave of Gems\",\n\t\"engine\":\"v0.2.0\",\n\t\"entry\":\"./cmd/cave\",\n\t\"default_scene\":\"level1\",\n\t\"invariants\":[ \"finite_positions\" ],\n\t\"bounds\":[[-10,-5,-10],[10,10,10]]\n}",
			want:       "{\n\t\"veduta\":\"project/1\",\n\t\"name\":\"cave\",\n\t\"title\":\"Cave of Gems\",\n\t\"engine\":\"v1.0.0\",\n\t\"entry\":\"./cmd/cave\",\n\t\"resolution\":[1280, 720],\n\t\"inspect_resolution\":[640, 360],\n\t\"tick_rate\":60,\n\t\"default_scene\":\"level1\",\n\t\"invariants\":[ \"finite_positions\" ],\n\t\"bounds\":[[-10,-5,-10],[10,10,10]]\n}",
			migrations: []string{pinResolution, pinInspectResolution, pinTickRate},
			changelog:  "- Upgrade the Veduta engine from v0.2.0 to v1.0.0 (`veduta upgrade`), pinning the defaults `veduta.json` relied on before v1.0.0 so the game keeps its size and tick rate: `\"resolution\": [1280, 720]`, `\"inspect_resolution\": [640, 360]`, `\"tick_rate\": 60`.\n",
		},
		{
			name:       "windows line endings",
			to:         "v1.0.0",
			in:         "{\r\n  \"veduta\": \"project/1\",\r\n  \"name\": \"mygame\",\r\n  \"engine\": \"v0.2.0\"\r\n}\r\n",
			want:       "{\r\n  \"veduta\": \"project/1\",\r\n  \"name\": \"mygame\",\r\n  \"engine\": \"v1.0.0\",\r\n  \"resolution\": [1280, 720],\r\n  \"inspect_resolution\": [640, 360],\r\n  \"tick_rate\": 60\r\n}\r\n",
			migrations: []string{pinResolution, pinInspectResolution, pinTickRate},
			changelog:  "- Upgrade the Veduta engine from v0.2.0 to v1.0.0 (`veduta upgrade`), pinning the defaults `veduta.json` relied on before v1.0.0 so the game keeps its size and tick rate: `\"resolution\": [1280, 720]`, `\"inspect_resolution\": [640, 360]`, `\"tick_rate\": 60`.\n",
		},
		{
			name:       "explicit values",
			to:         "v1.0.0",
			in:         "{\n  \"veduta\": \"project/1\",\n  \"name\": \"demo\",\n  \"engine\": \"v0.2.0\",\n  \"entry\": \"./cmd/game\",\n  \"resolution\": [1280, 720],\n  \"inspect_resolution\": [640, 360],\n  \"tick_rate\": 60,\n  \"default_scene\": \"main\"\n}\n",
			want:       "{\n  \"veduta\": \"project/1\",\n  \"name\": \"demo\",\n  \"engine\": \"v1.0.0\",\n  \"entry\": \"./cmd/game\",\n  \"resolution\": [1280, 720],\n  \"inspect_resolution\": [640, 360],\n  \"tick_rate\": 60,\n  \"default_scene\": \"main\"\n}\n",
			migrations: []string{},
			changelog:  "- Upgrade the Veduta engine from v0.2.0 to v1.0.0 (`veduta upgrade`).\n",
		},
		{
			// The two sizes go where they belong, before the tick rate the file sets.
			name:       "only tick_rate set",
			to:         "v1.0.0",
			in:         "{\n  \"veduta\": \"project/1\",\n  \"name\": \"mygame\",\n  \"engine\": \"v0.2.0\",\n  \"tick_rate\": 30,\n  \"default_seed\": 7\n}\n",
			want:       "{\n  \"veduta\": \"project/1\",\n  \"name\": \"mygame\",\n  \"engine\": \"v1.0.0\",\n  \"resolution\": [1280, 720],\n  \"inspect_resolution\": [640, 360],\n  \"tick_rate\": 30,\n  \"default_seed\": 7\n}\n",
			migrations: []string{pinResolution, pinInspectResolution},
			changelog:  "- Upgrade the Veduta engine from v0.2.0 to v1.0.0 (`veduta upgrade`), pinning the defaults `veduta.json` relied on before v1.0.0 so the game keeps its size and tick rate: `\"resolution\": [1280, 720]`, `\"inspect_resolution\": [640, 360]`.\n",
		},
		{
			// A zero value also means the default: it is replaced where it stands.
			name:       "zero values",
			to:         "v1.0.0",
			in:         "{\n  \"veduta\": \"project/1\",\n  \"name\": \"mygame\",\n  \"engine\": \"v0.2.0\",\n  \"tick_rate\": 0,\n  \"resolution\": null,\n  \"inspect_resolution\": [320, 180]\n}\n",
			want:       "{\n  \"veduta\": \"project/1\",\n  \"name\": \"mygame\",\n  \"engine\": \"v1.0.0\",\n  \"tick_rate\": 60,\n  \"resolution\": [1280, 720],\n  \"inspect_resolution\": [320, 180]\n}\n",
			migrations: []string{pinResolution, pinTickRate},
			changelog:  "- Upgrade the Veduta engine from v0.2.0 to v1.0.0 (`veduta upgrade`), pinning the defaults `veduta.json` relied on before v1.0.0 so the game keeps its size and tick rate: `\"resolution\": [1280, 720]`, `\"tick_rate\": 60`.\n",
		},
		{
			// A replaced zero value is the anchor of the keys that follow it.
			name:       "zero value followed by missing fields",
			to:         "v1.0.0",
			in:         "{\n  \"veduta\": \"project/1\",\n  \"name\": \"mygame\",\n  \"engine\": \"v0.2.0\",\n  \"resolution\": null\n}\n",
			want:       "{\n  \"veduta\": \"project/1\",\n  \"name\": \"mygame\",\n  \"engine\": \"v1.0.0\",\n  \"resolution\": [1280, 720],\n  \"inspect_resolution\": [640, 360],\n  \"tick_rate\": 60\n}\n",
			migrations: []string{pinResolution, pinInspectResolution, pinTickRate},
			changelog:  "- Upgrade the Veduta engine from v0.2.0 to v1.0.0 (`veduta upgrade`), pinning the defaults `veduta.json` relied on before v1.0.0 so the game keeps its size and tick rate: `\"resolution\": [1280, 720]`, `\"inspect_resolution\": [640, 360]`, `\"tick_rate\": 60`.\n",
		},
		{
			// The decoder also reads a key spelled in another case: that is the field the
			// game ran with, so its value is the one replaced, and a lowercase key added
			// before it would lose to it.
			name:       "keys in another case",
			to:         "v1.0.0",
			in:         "{\n  \"veduta\": \"project/1\",\n  \"name\": \"mygame\",\n  \"Engine\": \"v0.2.0\",\n  \"Tick_Rate\": 0,\n  \"Resolution\": null\n}\n",
			want:       "{\n  \"veduta\": \"project/1\",\n  \"name\": \"mygame\",\n  \"Engine\": \"v1.0.0\",\n  \"Tick_Rate\": 60,\n  \"Resolution\": [1280, 720],\n  \"inspect_resolution\": [640, 360]\n}\n",
			migrations: []string{pinResolution, pinInspectResolution, pinTickRate},
			changelog:  "- Upgrade the Veduta engine from v0.2.0 to v1.0.0 (`veduta upgrade`), pinning the defaults `veduta.json` relied on before v1.0.0 so the game keeps its size and tick rate: `\"resolution\": [1280, 720]`, `\"inspect_resolution\": [640, 360]`, `\"tick_rate\": 60`.\n",
		},
		{
			// Of a field given twice, the decoder keeps the last value.
			name:       "a field given twice",
			to:         "v1.0.0",
			in:         "{\n  \"veduta\": \"project/1\",\n  \"name\": \"mygame\",\n  \"engine\": \"v0.2.0\",\n  \"tick_rate\": 30,\n  \"TICK_RATE\": 0\n}\n",
			want:       "{\n  \"veduta\": \"project/1\",\n  \"name\": \"mygame\",\n  \"engine\": \"v1.0.0\",\n  \"resolution\": [1280, 720],\n  \"inspect_resolution\": [640, 360],\n  \"tick_rate\": 30,\n  \"TICK_RATE\": 60\n}\n",
			migrations: []string{pinResolution, pinInspectResolution, pinTickRate},
			changelog:  "- Upgrade the Veduta engine from v0.2.0 to v1.0.0 (`veduta upgrade`), pinning the defaults `veduta.json` relied on before v1.0.0 so the game keeps its size and tick rate: `\"resolution\": [1280, 720]`, `\"inspect_resolution\": [640, 360]`, `\"tick_rate\": 60`.\n",
		},
		{
			// A blank line that groups the fields stays in front of the field it was in
			// front of; the new keys join the group of the field they follow.
			name:       "blank lines between groups",
			to:         "v1.0.0",
			in:         "{\n\n  \"veduta\": \"project/1\",\n  \"name\": \"mygame\",\n  \"engine\": \"v0.2.0\",\n\n  \"default_seed\": 3\n\n}\n",
			want:       "{\n\n  \"veduta\": \"project/1\",\n  \"name\": \"mygame\",\n  \"engine\": \"v1.0.0\",\n  \"resolution\": [1280, 720],\n  \"inspect_resolution\": [640, 360],\n  \"tick_rate\": 60,\n\n  \"default_seed\": 3\n\n}\n",
			migrations: []string{pinResolution, pinInspectResolution, pinTickRate},
			changelog:  "- Upgrade the Veduta engine from v0.2.0 to v1.0.0 (`veduta upgrade`), pinning the defaults `veduta.json` relied on before v1.0.0 so the game keeps its size and tick rate: `\"resolution\": [1280, 720]`, `\"inspect_resolution\": [640, 360]`, `\"tick_rate\": 60`.\n",
		},
		{
			name:       "blank line before the last field, windows line endings",
			to:         "v1.0.0",
			in:         "{\r\n  \"veduta\": \"project/1\",\r\n  \"name\": \"mygame\",\r\n\r\n  \"engine\": \"v0.2.0\"\r\n}\r\n",
			want:       "{\r\n  \"veduta\": \"project/1\",\r\n  \"name\": \"mygame\",\r\n\r\n  \"engine\": \"v1.0.0\",\r\n  \"resolution\": [1280, 720],\r\n  \"inspect_resolution\": [640, 360],\r\n  \"tick_rate\": 60\r\n}\r\n",
			migrations: []string{pinResolution, pinInspectResolution, pinTickRate},
			changelog:  "- Upgrade the Veduta engine from v0.2.0 to v1.0.0 (`veduta upgrade`), pinning the defaults `veduta.json` relied on before v1.0.0 so the game keeps its size and tick rate: `\"resolution\": [1280, 720]`, `\"inspect_resolution\": [640, 360]`, `\"tick_rate\": 60`.\n",
		},
		{
			name:       "keys in another order",
			to:         "v1.0.0",
			in:         "{\"engine\": \"v0.2.0\", \"name\": \"mygame\", \"veduta\": \"project/1\"}",
			want:       "{\"engine\": \"v1.0.0\", \"resolution\": [1280, 720], \"inspect_resolution\": [640, 360], \"tick_rate\": 60, \"name\": \"mygame\", \"veduta\": \"project/1\"}",
			migrations: []string{pinResolution, pinInspectResolution, pinTickRate},
			changelog:  "- Upgrade the Veduta engine from v0.2.0 to v1.0.0 (`veduta upgrade`), pinning the defaults `veduta.json` relied on before v1.0.0 so the game keeps its size and tick rate: `\"resolution\": [1280, 720]`, `\"inspect_resolution\": [640, 360]`, `\"tick_rate\": 60`.\n",
		},
		{
			// The release candidates of v1.0.0 already have the new defaults.
			name:       "to a v1.0.0 release candidate",
			to:         "v1.0.0-rc.1",
			in:         "{\n  \"veduta\": \"project/1\",\n  \"name\": \"mygame\",\n  \"engine\": \"v0.2.0\"\n}\n",
			want:       "{\n  \"veduta\": \"project/1\",\n  \"name\": \"mygame\",\n  \"engine\": \"v1.0.0-rc.1\",\n  \"resolution\": [1280, 720],\n  \"inspect_resolution\": [640, 360],\n  \"tick_rate\": 60\n}\n",
			migrations: []string{pinResolution, pinInspectResolution, pinTickRate},
			changelog:  "- Upgrade the Veduta engine from v0.2.0 to v1.0.0-rc.1 (`veduta upgrade`), pinning the defaults `veduta.json` relied on before v1.0.0 so the game keeps its size and tick rate: `\"resolution\": [1280, 720]`, `\"inspect_resolution\": [640, 360]`, `\"tick_rate\": 60`.\n",
		},
		{
			name:       "from a v1.0.0 release candidate",
			to:         "v1.0.0",
			in:         "{\n  \"veduta\": \"project/1\",\n  \"name\": \"mygame\",\n  \"engine\": \"v1.0.0-rc.1\"\n}\n",
			want:       "{\n  \"veduta\": \"project/1\",\n  \"name\": \"mygame\",\n  \"engine\": \"v1.0.0\"\n}\n",
			migrations: []string{},
			changelog:  "- Upgrade the Veduta engine from v1.0.0-rc.1 to v1.0.0 (`veduta upgrade`).\n",
		},
		{
			// For a project made on v1.0.0 or later, the new defaults are what it asked for.
			name:       "already on v1",
			to:         "v1.1.0",
			in:         "{\n  \"veduta\": \"project/1\",\n  \"name\": \"mygame\",\n  \"engine\": \"v1.0.0\"\n}\n",
			want:       "{\n  \"veduta\": \"project/1\",\n  \"name\": \"mygame\",\n  \"engine\": \"v1.1.0\"\n}\n",
			migrations: []string{},
			changelog:  "- Upgrade the Veduta engine from v1.0.0 to v1.1.0 (`veduta upgrade`).\n",
		},
		{
			name:       "between v0.x versions",
			to:         "v0.2.0",
			in:         "{\n  \"veduta\": \"project/1\",\n  \"name\": \"mygame\",\n  \"engine\": \"v0.1.0\"\n}\n",
			want:       "{\n  \"veduta\": \"project/1\",\n  \"name\": \"mygame\",\n  \"engine\": \"v0.2.0\"\n}\n",
			migrations: []string{},
			changelog:  "- Upgrade the Veduta engine from v0.1.0 to v0.2.0 (`veduta upgrade`).\n",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := upgradeProject(t, c.in, c.to)
			r, err := s.Upgrade(&Env{Version: c.to}, false)
			if err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(filepath.Join(s.Root, asset.ProjectFile))
			if string(got) != c.want {
				t.Errorf("veduta.json:\n got %q\nwant %q", got, c.want)
			}
			if _, err := asset.ParseProject(asset.ProjectFile, got); err != nil {
				t.Errorf("the upgraded manifest does not parse: %v", err)
			}
			if !reflect.DeepEqual(r.Migrations, c.migrations) {
				t.Errorf("migrations:\n got %q\nwant %q", r.Migrations, c.migrations)
			}
			if want := []string{"veduta.json", "CHANGELOG.md"}; !reflect.DeepEqual(r.Changed, want) {
				t.Errorf("changed %q, want %q", r.Changed, want)
			}
			log, _ := os.ReadFile(filepath.Join(s.Root, "CHANGELOG.md"))
			if want := "# Changelog\n\n## Unreleased\n\n" + c.changelog; string(log) != want {
				t.Errorf("CHANGELOG.md:\n got %q\nwant %q", log, want)
			}
			if strings.Contains(string(log), "no format migrations") {
				t.Errorf("the changelog still says there are no migrations: %q", log)
			}
		})
	}
}

// TestUpgradeManifestChecksItsEdit checks that a rewrite that does not set the engine is
// an error, not a manifest reported as upgraded.
func TestUpgradeManifestChecksItsEdit(t *testing.T) {
	in := `{"veduta": "project/1", "name": "mygame"}`
	if out, _, err := upgradeManifest([]byte(in), "v1.0.0", true); err == nil || !strings.Contains(err.Error(), "engine") {
		t.Fatalf("manifest without engine: err %v, out %q", err, out)
	}
}

// TestUpgradeTwiceIsANoOp checks that a second run finds nothing to do: the pinned
// manifest is on the tool's version and pins nothing again.
func TestUpgradeTwiceIsANoOp(t *testing.T) {
	s := upgradeProject(t, "{\n  \"veduta\": \"project/1\",\n  \"name\": \"mygame\",\n  \"engine\": \"v0.2.0\"\n}\n", "v1.0.0")
	env := &Env{Version: "v1.0.0"}
	if _, err := s.Upgrade(env, false); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(s.Root, asset.ProjectFile))
	s, err := OpenSession(s.Root, env)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Upgrade(env, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Changed) != 0 || len(r.Migrations) != 0 {
		t.Fatalf("second upgrade: %+v", r)
	}
	if after, _ := os.ReadFile(filepath.Join(s.Root, asset.ProjectFile)); string(after) != string(before) {
		t.Fatalf("second upgrade rewrote veduta.json:\n%s", after)
	}
}

// TestUpgradeKeepsTheManifestWhenGoFails checks that veduta.json is written only after go
// get succeeded, so that the next run still sees the old engine and pins (and records) the
// same fields.
func TestUpgradeKeepsTheManifestWhenGoFails(t *testing.T) {
	in := "{\n  \"veduta\": \"project/1\",\n  \"name\": \"mygame\",\n  \"engine\": \"v0.2.0\"\n}\n"
	s := upgradeProject(t, in, "v0.2.0") // go.mod still on v0.2.0: go get must run
	t.Setenv("PATH", t.TempDir())        // and cannot: there is no go command
	if _, err := s.Upgrade(&Env{Version: "v1.0.0"}, false); err == nil || !strings.Contains(err.Error(), "go get") {
		t.Fatalf("upgrade without a go command: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(s.Root, asset.ProjectFile)); string(got) != in {
		t.Fatalf("veduta.json written although go get failed:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(s.Root, "CHANGELOG.md")); err == nil {
		t.Fatal("CHANGELOG.md written although go get failed")
	}
}

func TestUpgradeReportHuman(t *testing.T) {
	for _, c := range []struct {
		name string
		r    UpgradeReport
		want string
	}{
		{"already", UpgradeReport{From: "v1.0.0", To: "v1.0.0", Changed: []string{}, Migrations: []string{}}, "already on v1.0.0\n"},
		{
			"plain",
			UpgradeReport{From: "v0.1.0", To: "v0.2.0", Changed: []string{"veduta.json", "go.mod", "CHANGELOG.md"}, Migrations: []string{}},
			"upgraded v0.1.0 → v0.2.0 (veduta.json, go.mod, CHANGELOG.md)\n",
		},
		{
			"pinned",
			UpgradeReport{From: "v0.2.0", To: "v1.0.0", Changed: []string{"veduta.json", "CHANGELOG.md"}, Migrations: []string{pinResolution, pinTickRate}},
			"upgraded v0.2.0 → v1.0.0 (veduta.json, CHANGELOG.md)\nmigration: " + pinResolution + "\nmigration: " + pinTickRate + "\n",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := c.r.Human(); got != c.want {
				t.Errorf("Human() = %q\n     want   %q", got, c.want)
			}
		})
	}
}
