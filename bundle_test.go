package veduta

import (
	"path/filepath"
	"testing"
)

func TestRenderBundleAndQuery(t *testing.T) {
	useTestProject(t, walkSpec(t))
	dir := t.TempDir()
	png := filepath.Join(dir, "f.png")
	code, rep := runCmd(t, "-headless", "render", "--bundle", "--width", "160", "--height", "90", "--out", png)
	if code != exitOK {
		t.Fatalf("render --bundle: %v", rep)
	}
	bundle := rep["bundle"].(string)
	if bundle != filepath.Join(dir, "f.vframe") {
		t.Fatalf("bundle path %s", bundle)
	}
	code, rep = runCmd(t, "-headless", "query", "--frame", bundle, "--coverage")
	if code != exitOK {
		t.Fatalf("coverage: %v", rep)
	}
	ents := rep["coverage"].(map[string]any)["entities"].([]any)
	var player map[string]any
	for _, e := range ents {
		if m := e.(map[string]any); m["name"] == "player" {
			player = m
		}
	}
	if player == nil || player["pixels"].(float64) == 0 || player["projected"].(float64) < player["pixels"].(float64) {
		t.Fatalf("player coverage %v", player)
	}
	if r := player["occlusion_ratio"].(float64); r <= 0 || r > 1 {
		t.Fatalf("occlusion ratio %v", r)
	}
	bbox := player["bbox"].([]any)
	cx := int((bbox[0].(float64) + bbox[2].(float64)) / 2)
	cy := int((bbox[1].(float64) + bbox[3].(float64)) / 2)
	code, rep = runCmd(t, "-headless", "query", "--frame", bundle, "--at", itoa(cx)+","+itoa(cy))
	if code != exitOK {
		t.Fatalf("at: %v", rep)
	}
	px := rep["pixel"].(map[string]any)
	if px["entity"] == nil || px["entity"].(map[string]any)["name"] != "player" || px["normal"] == nil || px["distance"] == nil {
		t.Fatalf("pixel %v", px)
	}
	if code, _ := runCmd(t, "-headless", "query", "--frame", bundle); code != exitUsage {
		t.Fatal("query without --at/--coverage should be a usage error")
	}
}

func itoa(i int) string {
	return string(rune('0'+i/100%10)) + string(rune('0'+i/10%10)) + string(rune('0'+i%10))
}
