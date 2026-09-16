package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/riftbane/veduta/asset"
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
	const engine = "v1.0.0"
	switch runtime.GOOS {
	case "windows": // the simulator
		if findPanel() != "" || runRefusal(engine) != "" || runRefusal("v0.2.0") != "" {
			t.Fatalf("on Windows: panel %q, refusal %q, v0.x refusal %q", findPanel(), runRefusal(engine), runRefusal("v0.2.0"))
		}
		return
	case "linux":
	default:
		if findPanel() != "" || !strings.Contains(runRefusal(engine), "Linux framebuffer") || runRefusal("v0.2.0") != "" {
			t.Fatalf("off Linux: panel %q, refusal %q, v0.x refusal %q", findPanel(), runRefusal(engine), runRefusal("v0.2.0"))
		}
		return
	}
	t.Setenv("VEDUTA_FB", "")
	fakeGraphics(t, map[string][2]string{"fbcon": {"", "fbcon"}, "fb0": {"8", "vga16fb"}})
	if p := findPanel(); p != "" {
		t.Fatalf("an 8-bit framebuffer and fbcon count as a panel: %q", p)
	}
	if why := runRefusal(engine); !strings.Contains(why, "no framebuffer on this machine") || !strings.Contains(why, "VEDUTA_FB") {
		t.Fatalf("refusal without a panel: %q", why)
	}
	fakeGraphics(t, map[string][2]string{"fb0": {"32", "simpledrmdrmfb"}, "fb1": {"16", "mi0283qtdrmfb"}})
	if p := findPanel(); p != "fb0 (simpledrmdrmfb, 32 bpp), fb1 (mi0283qtdrmfb, 16 bpp)" {
		t.Fatalf("panels = %q", p)
	}
	if why := runRefusal(engine); why != "" {
		t.Fatalf("refused with a framebuffer: %q", why)
	}
	// A project on a v0.x engine plays in a window: a display is what it needs, not a
	// framebuffer.
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	if why := runRefusal("v0.2.0"); !strings.Contains(why, "no display") || !strings.Contains(why, "veduta upgrade") {
		t.Fatalf("v0.x project without a display: %q", why)
	}
	t.Setenv("DISPLAY", ":0")
	fakeGraphics(t, nil)
	if why := runRefusal("v0.2.0"); why != "" {
		t.Fatalf("v0.x project refused with a display: %q", why)
	}
	fakeGraphics(t, nil)
	t.Setenv("VEDUTA_FB", "fb3")
	if p := findPanel(); p != "VEDUTA_FB=fb3" {
		t.Fatalf("VEDUTA_FB is not trusted: %q", p)
	}
}

func TestModulePathAndEngineFile(t *testing.T) {
	for in, want := range map[string]string{
		"module demo\n\ngo 1.25\n":                         "demo",
		"// a game\nmodule \"example.com/my.game\" // x\n": "example.com/my.game",
		"go 1.25\nmodule\texample.com/g\n":                 "example.com/g",
		"modules x\n":                                      "",
		"go 1.25\n":                                        "",
	} {
		if got := modulePath([]byte(in)); got != want {
			t.Errorf("modulePath(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{
		"github.com/riftbane/veduta/gmath/vec.go":        "gmath/vec.go",
		"github.com/riftbane/veduta@v1.0.0/gmath/vec.go": "gmath/vec.go",
		"github.com/riftbane/veduta-extra/x.go":          "github.com/riftbane/veduta-extra/x.go",
		"example.com/lib@v1.2.0/lib.go":                  "example.com/lib@v1.2.0/lib.go",
	} {
		if got := engineFile(in); got != want {
			t.Errorf("engineFile(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFusedCheckListsEverySite(t *testing.T) {
	var sites []string
	for i := 1; i <= 12; i++ {
		sites = append(sites, fmt.Sprintf("game/kinds.go:%d", i))
	}
	c := fusedCheck(sites)
	if !c.OK || !c.Warning || !strings.Contains(c.Detail, "12 place(s)") || !strings.Contains(c.Detail, "game/kinds.go:10, and 2 more") || strings.Contains(c.Detail, "game/kinds.go:11") {
		t.Fatalf("detail: %+v", c)
	}
	data, _ := json.Marshal(c)
	var back struct{ Sites []string }
	if json.Unmarshal(data, &back); !reflect.DeepEqual(back.Sites, sites) {
		t.Fatalf("--json sites = %q, want all %d", back.Sites, len(sites))
	}
}

func TestBuildsConsole(t *testing.T) {
	for _, c := range []struct {
		name, workflow string
		want           bool
	}{
		{"target loop", "run: |\n  for target in linux/arm64 linux/amd64; do\n", true},
		{"comment only", "# linux/arm64 is the console\nrun: |\n  # GOARCH=arm64 later\n  for target in linux/amd64; do # linux/arm64 too\n", false},
		{"env", "env:\n  GOOS: linux\n  GOARCH: arm64\n", true},
		{"inline env", "run: GOOS=linux GOARCH=arm64 go build ./cmd/game\n", true},
		{"matrix", "strategy:\n  matrix:\n    goarch: [arm64, amd64]\nenv:\n  GOOS: linux\n  GOARCH: ${{ matrix.goarch }}\n", true},
		{"shell expansion", "run: |\n  os=\"${target%/*}\"; arch=\"${target#*/}\"\n  for target in linux/amd64; do echo $#; done\n", false},
		{"other arm", "run: GOOS=linux GOARCH=arm go build\n", false},
		{"no linux", "run: GOOS=darwin GOARCH=arm64 go build\n", false},
		{"arm64 for other systems", "run: |\n  for target in linux/amd64 darwin/arm64 windows/arm64; do\n", false},
		{"an arm64 runner", "runs-on: ubuntu-24.04-arm64\nrun: GOOS=linux go build\n", false},
	} {
		if got := buildsConsole(c.workflow); got != c.want {
			t.Errorf("%s: buildsConsole = %v, want %v", c.name, got, c.want)
		}
	}

	// Every workflow file counts, whatever its name or extension.
	dir := t.TempDir()
	s := &Session{Root: dir}
	if ok, detail := s.releaseTargetsConsole(); ok || !strings.Contains(detail, "no workflow") {
		t.Fatalf("no workflows: %v %q", ok, detail)
	}
	wfs := filepath.Join(dir, ".github", "workflows")
	os.MkdirAll(wfs, 0o755)
	os.WriteFile(filepath.Join(wfs, "ci.yml"), []byte("run: go test ./...\n"), 0o644)
	if ok, detail := s.releaseTargetsConsole(); ok || !strings.Contains(detail, "no linux/arm64") {
		t.Fatalf("ci only: %v %q", ok, detail)
	}
	os.WriteFile(filepath.Join(wfs, "ci.yml"), []byte("on: push\nrun: GOOS=linux GOARCH=arm64 go test -exec qemu-aarch64-static ./...\n"), 0o644)
	if ok, detail := s.releaseTargetsConsole(); ok || !strings.Contains(detail, "no linux/arm64") {
		t.Fatalf("an arm64 test job that publishes nothing: %v %q", ok, detail)
	}
	os.WriteFile(filepath.Join(wfs, "publish.yaml"), []byte("on:\n  push:\n    tags: [\"v*\"]\nenv:\n  GOOS: linux\n  GOARCH: arm64\n"), 0o644)
	if ok, detail := s.releaseTargetsConsole(); !ok || detail != ".github/workflows/publish.yaml builds linux/arm64" {
		t.Fatalf("publish.yaml: %v %q", ok, detail)
	}
}

func TestConsoleBuildFailure(t *testing.T) {
	s := &Session{Project: &asset.Project{Entry: "./cmd/game"}}
	exit := errors.New("go build for linux/arm64: exit status 1")
	for _, c := range []struct {
		name       string
		errs       []CompileError
		err        error
		detail     string
		fixWords   string
		notInFixes string
	}{
		{"located", []CompileError{{File: "game/uses.go", Line: 3, Col: 9, Msg: "undefined: onlyHere"}}, exit,
			"go build for linux/arm64: exit status 1: game/uses.go:3:9: undefined: onlyHere", "fix the located error", "pure Go"},
		{"cgo", []CompileError{{File: "game/c.go", Line: 3, Col: 8, Msg: `could not import C (cgo preprocessing failed)`}}, exit,
			"go build for linux/arm64: exit status 1: game/c.go:3:8: could not import C (cgo preprocessing failed)", "pure Go", "go mod tidy"},
		{"offline", []CompileError{{Msg: "go: github.com/riftbane/veduta@v1.0.0: Get \"https://proxy.golang.org/...\": dial tcp: lookup proxy.golang.org: no such host\nmore"}}, exit,
			"go build for linux/arm64: exit status 1: go: github.com/riftbane/veduta@v1.0.0: Get \"https://proxy.golang.org/...\": dial tcp: lookup proxy.golang.org: no such host", "go mod tidy", "pure Go"},
		{"go.sum", []CompileError{{File: "game/kinds.go", Line: 5, Col: 2, Msg: "missing go.sum entry for module providing package github.com/riftbane/veduta/gmath"}}, exit,
			"go build for linux/arm64: exit status 1: game/kinds.go:5:2: missing go.sum entry for module providing package github.com/riftbane/veduta/gmath", "go mod tidy", "pure Go"},
		{"no go", []CompileError{{Msg: `go build for linux/arm64: exec: "go": executable file not found in $PATH`}}, errors.New(`go build for linux/arm64: exec: "go": executable file not found in $PATH`),
			`go build for linux/arm64: exec: "go": executable file not found in $PATH`, "install Go", "pure Go"},
		{"nothing said", []CompileError{{Msg: ""}}, exit, "go build for linux/arm64: exit status 1", "GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build ./cmd/game", "pure Go"},
	} {
		detail, fix := s.consoleBuildFailure(c.errs, c.err)
		if detail != c.detail || !strings.Contains(fix, c.fixWords) || strings.Contains(fix, c.notInFixes) || strings.Contains(detail, ":0:0:") {
			t.Errorf("%s:\n detail %q\n   want %q\n fix %q (want %q, not %q)", c.name, detail, c.detail, fix, c.fixWords, c.notInFixes)
		}
	}

	old := lookGo
	lookGo = func() error { return errors.New("not found") }
	defer func() { lookGo = old }()
	if c := s.arm64Check(); !c.OK || !c.Warning || c.Detail != "not checked: go not found on PATH" {
		t.Fatalf("arm64 without go: %+v", c)
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
	// Ordinary movement code: the products fuse with the addition inside gmath's Add once
	// it is inlined into updatePlayer, so the fused instruction sits on gmath/vec.go.
	fusedSrc = strings.Replace(fusedSrc, "e.Transform.Position.Add(dir.Scale(PlayerSpeed * ctx.DT))", "e.Transform.Position.Add(gmath.V3(dir.X*ctx.DT, 0, dir.Z*ctx.DT))", 1)
	if strings.Count(fusedSrc, "ctx.DT * st.VelY")+strings.Count(fusedSrc, "gmath.V3(dir.X*ctx.DT") != 2 {
		t.Fatal("kinds.go no longer holds the gravity and movement lines this test edits")
	}
	os.WriteFile(kinds, []byte(fusedSrc+"\n//go:noinline\nfunc fusedForTest(a, b, c float32) float32 { return a*b + c }\n\nvar _ = fusedForTest\n"), 0o644)
	wf := filepath.Join(dir, ".github", "workflows", "release.yml")
	w, _ := os.ReadFile(wf)
	// Only the build loop loses the console; the comment that names linux/arm64 stays.
	noConsole := strings.Replace(string(w), "for target in linux/arm64 linux/amd64", "for target in linux/amd64", 1)
	if noConsole == string(w) || !strings.Contains(noConsole, "linux/arm64") {
		t.Fatal("release.yml no longer has the build loop and comment this test edits")
	}
	os.WriteFile(wf, []byte(noConsole), 0o644)
	os.WriteFile(filepath.Join(dir, "card.json"), []byte(`{"veduta": "card/1", "title": "Gems", "name": "demo", "exec": "demo"}`), 0o644)

	// The fused lines are found whatever the paths look like: GOFLAGS asks every build for
	// module-relative file names, and doctor runs in a subdirectory of the project reached
	// through a symlink, where the go command records the physical path unless told
	// otherwise.
	t.Setenv("GOFLAGS", "-trimpath")
	from := dir
	if link := filepath.Join(t.TempDir(), "link"); os.Symlink(dir, link) == nil {
		from = link
	}
	r = Doctor(env, filepath.Join(from, "game"))
	if c := check(t, r, "frame"); !c.Warning || !strings.Contains(c.Detail, "tick_rate 60") || !strings.Contains(c.Detail, "1280x720") {
		t.Fatalf("frame: %+v", c)
	}
	c := check(t, r, "arm64")
	if !c.OK || !c.Warning || !strings.Contains(c.Detail, "game/kinds.go:") || !strings.Contains(c.Detail, "gmath/vec.go:") || !strings.Contains(c.Detail, " inlined in demo/game.updatePlayer") {
		t.Fatalf("arm64: %+v", c)
	}
	if len(c.Sites) != 2 || !strings.HasPrefix(c.Sites[0], "game/kinds.go:") || !strings.HasPrefix(c.Sites[1], "gmath/vec.go:") {
		t.Fatalf("arm64 sites: %q", c.Sites)
	}
	if strings.Contains(c.Detail, from) || strings.Contains(c.Detail, "github.com/") {
		t.Fatalf("arm64 lists full paths: %+v", c)
	}
	if c := check(t, r, "release"); !c.Warning || !strings.Contains(c.Detail, "no linux/arm64") {
		t.Fatalf("release: %+v", c)
	}
	if c := check(t, r, "card"); !c.Warning || !strings.Contains(c.Detail, `title "Gems" differs`) || !strings.Contains(c.Fix, `"title": "demo"`) {
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
