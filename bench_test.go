package veduta

import (
	"testing"
)

// bench times every tick of a scenario and reports the frame budget, the timings and what
// each frame drew.
func TestHeadlessBench(t *testing.T) {
	useTestProject(t, walkSpec(t))
	code, rep := runCmd(t, "-headless", "bench", "--scenario", "walk.scenario.json", "--cpus", "2", "--width", "96", "--height", "72")
	if code != exitOK || rep["ok"] != true {
		t.Fatalf("code %d report %v", code, rep)
	}
	if rep["ticks"] != float64(120) || rep["cpus"] != float64(2) || rep["width"] != float64(96) || rep["budget_ms"] != float64(16.667) {
		t.Fatalf("report %v", rep)
	}
	// A tiny scene may update faster than a coarse clock ticks (Windows), so only the
	// order of the statistics is checked, and that rendering took some time.
	for _, k := range []string{"update_ms", "render_ms", "frame_ms"} {
		s, ok := rep[k].(map[string]any)
		if !ok || s["max"].(float64) < s["p95"].(float64) || s["p95"].(float64) < s["p50"].(float64) || s["mean"].(float64) < 0 {
			t.Fatalf("%s %v", k, rep[k])
		}
	}
	if rep["frame_ms"].(map[string]any)["max"].(float64) <= 0 {
		t.Fatalf("frames took no time: %v", rep["frame_ms"])
	}
	if tri := rep["triangles"].(map[string]any); tri["max"].(float64) <= 0 {
		t.Fatalf("triangles %v", tri)
	}
	if st := rep["slowest_tick"].(float64); st < 1 || st > 120 {
		t.Fatalf("slowest tick %v", st)
	}
	if code, _ := runCmd(t, "-headless", "bench", "--scene", "main", "--ticks", "0"); code != exitUsage {
		t.Fatalf("ticks 0: code %d", code)
	}
	if code, _ := runCmd(t, "-headless", "bench", "--scene", "main", "--cpus", "0"); code != exitUsage {
		t.Fatalf("cpus 0: code %d", code)
	}
}
