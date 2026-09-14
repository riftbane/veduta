package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeGraphics builds a /sys/class/graphics under a temporary root and points sysRoot at
// it for the test.
func fakeGraphics(t *testing.T, fbs map[string][2]string) {
	t.Helper()
	root := t.TempDir()
	for node, attrs := range fbs {
		dir := filepath.Join(root, "sys", "class", "graphics", node)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if attrs[0] != "" {
			os.WriteFile(filepath.Join(dir, "bits_per_pixel"), []byte(attrs[0]+"\n"), 0o644)
		}
		os.WriteFile(filepath.Join(dir, "name"), []byte(attrs[1]+"\n"), 0o644)
	}
	old := sysRoot
	sysRoot = root
	t.Cleanup(func() { sysRoot = old })
}

func TestFindPanel(t *testing.T) {
	if runtime.GOOS != "linux" {
		if findPanel() != "" || !strings.Contains(runRefusal(), "Linux framebuffer") {
			t.Fatalf("off Linux: panel %q, refusal %q", findPanel(), runRefusal())
		}
		return
	}
	t.Setenv("VEDUTA_FB", "")
	fakeGraphics(t, map[string][2]string{"fbcon": {"", "fbcon"}, "fb0": {"8", "vga16fb"}})
	if p := findPanel(); p != "" {
		t.Fatalf("an 8-bit framebuffer and fbcon count as a panel: %q", p)
	}
	if why := runRefusal(); !strings.Contains(why, "no framebuffer on this machine") || !strings.Contains(why, "VEDUTA_FB") {
		t.Fatalf("refusal without a panel: %q", why)
	}
	fakeGraphics(t, map[string][2]string{"fb0": {"32", "simpledrmdrmfb"}, "fb1": {"16", "mi0283qtdrmfb"}})
	if p := findPanel(); p != "fb0 (simpledrmdrmfb, 32 bpp), fb1 (mi0283qtdrmfb, 16 bpp)" {
		t.Fatalf("panels = %q", p)
	}
	if why := runRefusal(); why != "" {
		t.Fatalf("refused with a framebuffer: %q", why)
	}
	fakeGraphics(t, nil)
	t.Setenv("VEDUTA_FB", "fb3")
	if p := findPanel(); p != "VEDUTA_FB=fb3" {
		t.Fatalf("VEDUTA_FB is not trusted: %q", p)
	}
}

// check returns the doctor check called name, failing when there is none.
func check(t *testing.T, r *DoctorReport, name string) Check {
	t.Helper()
	for _, c := range r.Checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no %s check in %s", name, r.Human())
	return Check{}
}

func TestDoctorConsoleChecks(t *testing.T) {
	dir, env := newProject(t)
	r := Doctor(env, dir)
	if c := check(t, r, "target"); !c.OK || !strings.Contains(c.Detail, "linux/arm64, 320x240 panel at 20 Hz") {
		t.Fatalf("target: %+v", c)
	}
	for _, name := range []string{"arm64", "release", "card"} {
		if c := check(t, r, name); !c.OK || c.Warning {
			t.Fatalf("%s on a fresh project: %+v\n%s", name, c, r.Human())
		}
	}
	for _, c := range r.Checks {
		if c.Name == "frame" {
			t.Fatalf("frame warning on the default manifest: %+v", c)
		}
	}

	// A desktop-shaped manifest, a product left to fuse, a workflow with no console build
	// and a card that disagrees each get a warning that names the problem.
	manifest := filepath.Join(dir, "veduta.json")
	m, _ := os.ReadFile(manifest)
	m = []byte(strings.Replace(strings.Replace(string(m), `"tick_rate": 20`, `"tick_rate": 60`, 1), `"resolution": [320, 240]`, `"resolution": [1280, 720]`, 1))
	os.WriteFile(manifest, m, 0o644)
	kinds := filepath.Join(dir, "game", "kinds.go")
	src, _ := os.ReadFile(kinds)
	fusedSrc := strings.Replace(string(src), "st.VelY -= float32(Gravity * ctx.DT)", "st.VelY -= Gravity * ctx.DT * st.VelY", 1)
	if fusedSrc == string(src) {
		t.Fatal("kinds.go no longer holds the gravity line this test edits")
	}
	os.WriteFile(kinds, []byte(fusedSrc+"\n//go:noinline\nfunc fusedForTest(a, b, c float32) float32 { return a*b + c }\n\nvar _ = fusedForTest\n"), 0o644)
	wf := filepath.Join(dir, ".github", "workflows", "release.yml")
	w, _ := os.ReadFile(wf)
	os.WriteFile(wf, []byte(strings.ReplaceAll(string(w), "linux/arm64 ", "")), 0o644)
	os.WriteFile(filepath.Join(dir, "card.json"), []byte(`{"veduta": "card/1", "title": "Gems", "name": "demo", "exec": "demo"}`), 0o644)

	r = Doctor(env, dir)
	if c := check(t, r, "frame"); !c.Warning || !strings.Contains(c.Detail, "tick_rate 60") || !strings.Contains(c.Detail, "1280x720") {
		t.Fatalf("frame: %+v", c)
	}
	if c := check(t, r, "arm64"); !c.OK || !c.Warning || !strings.Contains(c.Detail, "game/kinds.go:") {
		t.Fatalf("arm64: %+v", c)
	}
	if c := check(t, r, "release"); !c.Warning || !strings.Contains(c.Detail, "no linux/arm64") {
		t.Fatalf("release: %+v", c)
	}
	if c := check(t, r, "card"); !c.Warning || !strings.Contains(c.Detail, `title "Gems" differs`) {
		t.Fatalf("card: %+v", c)
	}
	if !strings.Contains(r.Human(), "warn arm64") {
		t.Fatalf("human output does not mark warnings:\n%s", r.Human())
	}

	// A game that does not build for the console fails the check outright.
	os.WriteFile(filepath.Join(dir, "game", "desktop_only.go"), []byte("//go:build !arm64\n\npackage game\n\nconst onlyHere = 1\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "game", "uses.go"), []byte("package game\n\nvar _ = onlyHere\n"), 0o644)
	if c := check(t, Doctor(env, dir), "arm64"); c.OK || !strings.Contains(c.Detail, "game/uses.go:3") {
		t.Fatalf("arm64 build failure: %+v", c)
	}
}
