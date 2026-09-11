package veduta

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/sim"
)

// useTestProject points the loaders at the in-memory test game.
func useTestProject(t *testing.T, spec *scenarioSpec) {
	t.Helper()
	oldP, oldS, oldI := loadProjectFunc, loadScenarioFunc, loadInputFunc
	t.Cleanup(func() { loadProjectFunc, loadScenarioFunc, loadInputFunc = oldP, oldS, oldI })
	loadProjectFunc = func(string) (*asset.Project, *Assets, error) {
		p, a := testAssets()
		return p, a, nil
	}
	loadScenarioFunc = func(string) (*scenarioSpec, error) {
		c := *spec
		return &c, nil
	}
	loadInputFunc = func(string) ([]sim.InputEvent, error) { return walkScript(t).Events(), nil }
}

func walkSpec(t *testing.T) *scenarioSpec {
	one := 1
	return &scenarioSpec{
		Name: "walk", Scene: "main", Seed: 42, Ticks: 120,
		Inputs: walkScript(t).Events(),
		Expect: []sim.Expectation{
			{Tick: 60, Entity: "player", Path: "position.z", Op: "<", Value: -1.0},
			{Tick: 120, Trace: "gem_collected", CountMin: &one},
		},
		Screenshots: []int{0, 60, 120},
	}
}

func runCmd(t *testing.T, args ...string) (int, map[string]any) {
	t.Helper()
	var out, errb bytes.Buffer
	code := runMain(&testGame{}, args, &out, &errb)
	var rep map[string]any
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil && code != exitUsage {
		t.Fatalf("%v: stdout is not JSON: %q (stderr %q)", args, out.String(), errb.String())
	}
	return code, rep
}

func TestHeadlessSimulate(t *testing.T) {
	useTestProject(t, walkSpec(t))
	dir := t.TempDir()
	code, rep := runCmd(t, "-headless", "simulate", "--scenario", "walk.scenario.json", "--out", dir)
	if code != exitOK || rep["verdict"] != "pass" {
		t.Fatalf("code %d report %v", code, rep)
	}
	for _, f := range []string{"trace.jsonl", "result.json", "sheet.png"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatal(err)
		}
	}
	sheet1, _ := os.ReadFile(filepath.Join(dir, "sheet.png"))
	dir2 := t.TempDir()
	_, rep2 := runCmd(t, "-headless", "simulate", "--scenario", "walk.scenario.json", "--out", dir2)
	sheet2, _ := os.ReadFile(filepath.Join(dir2, "sheet.png"))
	if rep["trace_hash"] != rep2["trace_hash"] || !bytes.Equal(sheet1, sheet2) {
		t.Fatal("two identical runs differ")
	}
	if ev := rep["events"].(map[string]any); ev["gem_collected"].(float64) != 1 {
		t.Fatalf("events %v", ev)
	}
}

func TestHeadlessSimulateFailVerdict(t *testing.T) {
	spec := walkSpec(t)
	spec.Expect = append(spec.Expect, sim.Expectation{Tick: 100, Entity: "player", Path: "position.x", Op: ">", Value: 50.0})
	useTestProject(t, spec)
	code, rep := runCmd(t, "-headless", "simulate", "--scenario", "x", "--out", t.TempDir())
	if code != exitVerdict || rep["verdict"] != "fail" || !strings.Contains(rep["first_failure"].(string), "player.position.x") {
		t.Fatalf("code %d report %v", code, rep)
	}
}

func TestHeadlessRenderSnapshotDescribe(t *testing.T) {
	useTestProject(t, walkSpec(t))
	dir := t.TempDir()
	code, rep := runCmd(t, "-headless", "render", "--tick", "30", "--mode", "ids", "--out", filepath.Join(dir, "r.png"), "--input", "walk")
	if code != exitOK || rep["ok"] != true {
		t.Fatalf("render: %d %v", code, rep)
	}
	if ents := rep["entities"].([]any); len(ents) < 2 {
		t.Fatalf("render saw entities %v", ents)
	}
	snap := filepath.Join(dir, "s.snap")
	if code, rep = runCmd(t, "-headless", "snapshot", "--tick", "60", "--input", "walk", "--out", snap); code != exitOK {
		t.Fatalf("snapshot: %v", rep)
	}
	code, rep = runCmd(t, "-headless", "snapshot", "--restore", snap, "--ticks", "60", "--input", "walk")
	if code != exitOK || rep["to_tick"].(float64) != 120 {
		t.Fatalf("restore: %v", rep)
	}
	code, rep = runCmd(t, "-headless", "describe")
	if code != exitOK {
		t.Fatalf("describe: %v", rep)
	}
	b, _ := json.Marshal(rep)
	for _, want := range []string{`"name":"tgem","state":{"Value":"int"}`, `"game":["score_non_negative"]`, `"state_codec":true`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("describe lacks %s: %s", want, b)
		}
	}
}

func TestHeadlessErrors(t *testing.T) {
	useTestProject(t, walkSpec(t))
	code, rep := runCmd(t, "-headless", "render", "--scene", "nope", "--out", filepath.Join(t.TempDir(), "x.png"))
	if code != exitError || rep["ok"] != false || !strings.Contains(rep["error"].(string), "unknown scene") {
		t.Fatalf("code %d rep %v", code, rep)
	}
	if code, _ := runCmd(t, "-headless", "render", "--bogus"); code != exitUsage {
		t.Fatalf("bad flag: code %d", code)
	}
	if code, _ := runCmd(t, "-headless", "teleport"); code != exitUsage {
		t.Fatalf("bad subcommand: code %d", code)
	}
}
