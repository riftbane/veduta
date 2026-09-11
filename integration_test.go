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
	"github.com/riftbane/veduta/template/game"
)

// TestTemplateScenarios runs every scenario of the demo project (the template shipped by
// veduta init) through the real headless path and compares its trace hash and contact
// sheet with the committed goldens. CI runs this on linux/amd64 and windows/amd64, so the
// goldens make the two platforms byte-identical transitively (spec §7.6, §15.4).
func TestTemplateScenarios(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("template", "tests", "scenarios", "*.scenario.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no template scenarios: %v", err)
	}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".scenario.json")
		t.Run(name, func(t *testing.T) {
			out := t.TempDir()
			res := simulate(t, "-project", "template", "-headless", "simulate", "--scenario", f, "--out", out)
			if res.Verdict != "pass" {
				t.Fatalf("verdict %s: %s", res.Verdict, res.FirstFailure)
			}
			// A second run must give the same trace hash (§15.4).
			again := simulate(t, "-project", "template", "-headless", "simulate", "--scenario", f, "--out", t.TempDir())
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
