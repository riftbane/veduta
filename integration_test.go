package veduta_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riftbane/veduta"
	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/internal/golden"
	"github.com/riftbane/veduta/internal/testgame/game"
	"github.com/riftbane/veduta/script"
	skeleton "github.com/riftbane/veduta/template/go/game"
)

// TestGameScenarios runs every scenario of the engine's test game (internal/testgame)
// through the real headless path and compares its trace hash and contact
// sheet with the committed goldens. CI runs this on linux/amd64 and windows/amd64, so the
// goldens make the two platforms byte-identical transitively (spec §7.6, §15.4).
func TestGameScenarios(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("internal", "testgame", "tests", "scenarios", "*.scenario.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no test game scenarios: %v", err)
	}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".scenario.json")
		t.Run(name, func(t *testing.T) {
			out := t.TempDir()
			res := simulate(t, "-project", testGame, "-headless", "simulate", "--scenario", f, "--out", out)
			if res.Verdict != "pass" {
				t.Fatalf("verdict %s: %s", res.Verdict, res.FirstFailure)
			}
			// A second run must give the same trace hash (§15.4).
			again := simulate(t, "-project", testGame, "-headless", "simulate", "--scenario", f, "--out", t.TempDir())
			if again.TraceHash != res.TraceHash {
				t.Fatalf("two runs differ: %s vs %s", res.TraceHash, again.TraceHash)
			}
			golden.Text(t, "scenario_"+name+".hash", []byte(res.TraceHash+"\n"))
			data, err := os.ReadFile(filepath.Join(out, "sheet.png"))
			if err != nil {
				t.Fatal(err)
			}
			img, err := gfx.DecodePNG(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			golden.Image(t, "scenario_"+name+"_sheet", img)
		})
	}
}

// testGame is the directory of the engine's test game.
var testGame = filepath.Join("internal", "testgame")

// TestTemplateStarts runs the scenario veduta init ships, in the Lua game and in the Go
// game: a new project passes its own tests before anything is added to it.
func TestTemplateStarts(t *testing.T) {
	for _, lang := range []string{"lua", "go"} {
		t.Run(lang, func(t *testing.T) {
			dir := t.TempDir()
			for _, part := range []string{"common", lang} {
				src := filepath.Join("template", part)
				err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
					if err != nil {
						return err
					}
					rel, _ := filepath.Rel(src, p)
					if info.IsDir() {
						return os.MkdirAll(filepath.Join(dir, rel), 0o755)
					}
					data, err := os.ReadFile(p)
					if err != nil {
						return err
					}
					return os.WriteFile(filepath.Join(dir, rel), data, 0o644)
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			args := []string{"-project", dir, "-headless", "simulate", "--scenario", filepath.Join(dir, "tests", "scenarios", "start.scenario.json"), "--out", t.TempDir()}
			var stdout, stderr bytes.Buffer
			var code int
			if lang == "lua" {
				code = script.Run(args, &stdout, &stderr)
			} else {
				code = veduta.RunArgs(&skeleton.Game{}, args, &stdout, &stderr)
			}
			lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
			var res simResult
			if err := json.Unmarshal([]byte(lines[len(lines)-1]), &res); err != nil || code != 0 || res.Verdict != "pass" {
				t.Fatalf("exit %d, verdict %q %s, stdout %q, stderr %q", code, res.Verdict, res.FirstFailure, stdout.String(), stderr.String())
			}
		})
	}
}

type simResult struct {
	Verdict      string `json:"verdict"`
	FirstFailure string `json:"first_failure"`
	TraceHash    string `json:"trace_hash"`
}

func simulate(t *testing.T, args ...string) simResult {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := veduta.RunArgs(&game.Game{}, args, &stdout, &stderr)
	var res simResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
	if code != 0 && code != 3 {
		t.Fatalf("exit %d: %s %s", code, stdout.String(), stderr.String())
	}
	return res
}
