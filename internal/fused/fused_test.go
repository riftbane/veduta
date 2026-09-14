package fused

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// engineModule is the module path of the engine, as a -trimpath build records its files.
const engineModule = "github.com/riftbane/veduta"

func TestIsFused(t *testing.T) {
	for _, c := range []struct {
		w    uint32
		want bool
		what string
	}{
		{0x1f4120a8, true, "FMADDD"},
		{0x1f0c356b, true, "FMADDS"},
		{0x1f2cb570, true, "FNMSUBS"},
		{0x1f608821, true, "FNMSUBD"},
		{0x1e29096b, false, "FMULS"},
		{0x1e2c100c, false, "FMOVS $0.5"},
		{0x1e331929, false, "FDIVS"},
		{0x1e214129, false, "FNEGS"},
		{0x9104b3fb, false, "ADD $300, RSP"},
		{0xd503201f, false, "NOP"},
	} {
		if got := IsFused(c.w); got != c.want {
			t.Errorf("IsFused(%#08x) [%s] = %v, want %v", c.w, c.what, got, c.want)
		}
	}
}

func TestInModule(t *testing.T) {
	for _, c := range []struct {
		name, module string
		want         bool
	}{
		{"demo/game/kinds.go", "demo", true},
		{"demo", "demo", true},
		{"demo/game", "demo", true},
		{"demolition/game/kinds.go", "demo", false},
		{"demo@v1.2.0/game/kinds.go", "demo", false},
		{"github.com/riftbane/veduta/gmath/vec.go", engineModule, true},
		{"github.com/riftbane/veduta@v1.0.0/gmath/vec.go", engineModule, false},
		{"/home/me/veduta/gmath/vec.go", engineModule, false},
		{"runtime/proc.go", "", false},
	} {
		if got := InModule(c.name, c.module); got != c.want {
			t.Errorf("InModule(%q, %q) = %v, want %v", c.name, c.module, got, c.want)
		}
	}
}

// goTool returns the go command, or skips when there is none.
func goTool(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("builds arm64 binaries")
	}
	p, err := exec.LookPath("go")
	if err != nil {
		p = filepath.Join(runtime.GOROOT(), "bin", "go")
		if _, err := os.Stat(p); err != nil {
			t.Skip("no go command")
		}
	}
	return p
}

// buildArm64 builds pkg for the console the way a release does: -trimpath, so that file
// names are module paths whatever directory or symlink it is reached through, and without
// the caller's GOFLAGS, which could turn -trimpath off or change the code generated.
func buildArm64(t *testing.T, gocmd, dir, out, pkg string, flags ...string) {
	t.Helper()
	args := append([]string{"build", "-trimpath"}, flags...)
	cmd := exec.Command(gocmd, append(args, "-o", out, pkg)...)
	cmd.Dir = dir
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GOFLAGS=") {
			env = append(env, kv)
		}
	}
	cmd.Env = append(env, "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s for linux/arm64: %v\n%s", pkg, err, b)
	}
}

// inModule is the filter of the engine gate, and of the control that proves it can match.
func inModule(module string) func(Site) bool {
	return func(s Site) bool { return InModule(s.File, module) }
}

// linesIn reports whether the binary's line table holds any file of module: a filter that
// matches none of them would find no fused site and pass whatever the code does.
func linesIn(t *testing.T, bin, module string) bool {
	t.Helper()
	tab, err := open(bin)
	if err != nil {
		t.Fatal(err)
	}
	for f := range tab.syms.Files {
		if InModule(f, module) {
			return true
		}
	}
	return false
}

// TestScanFindsAKnownFusion is the control for the engine check below: a scanner or a
// filter that silently found nothing would make that check pass forever. It builds a
// probe module the way the gate builds the engine, through a symlink and with GOFLAGS
// asking for full paths, and filters with the same module-path match.
func TestScanFindsAKnownFusion(t *testing.T) {
	gocmd := goTool(t)
	probe := t.TempDir()
	src := `package main

import "os"

//go:noinline
func bare(a, b, c float32) float32 {
	return a*b + c
}

//go:noinline
func rounded(a, b, c float32) float32 {
	return float32(a*b) + c
}

func helper(a, b, c float32) float32 {
	return a*b + c
}

//go:noinline
func callerA(a, b, c float32) float32 { return helper(a, b, c) }

//go:noinline
func callerB(a, b, c float32) float32 { return helper(c, b, a) }

func main() {
	os.Exit(int(bare(1, 2, 3) + rounded(1, 2, 3) + callerA(1, 2, 3) + callerB(1, 2, 3)))
}
`
	if err := os.WriteFile(filepath.Join(probe, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(probe, "go.mod"), []byte("module example.com/probe\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := probe
	if link := filepath.Join(t.TempDir(), "link"); os.Symlink(probe, link) == nil {
		dir = link
	}
	t.Setenv("GOFLAGS", "-trimpath=false")
	bin := filepath.Join(t.TempDir(), "probe")
	buildArm64(t, gocmd, dir, bin, ".")
	if !linesIn(t, bin, "example.com/probe") {
		t.Fatal("no file of example.com/probe in the line table: the module filter is blind")
	}
	sites, err := Scan(bin, inModule("example.com/probe"))
	if err != nil {
		t.Fatal(err)
	}
	// The helper's line is listed once per function it was inlined into, and the package
	// of those functions is known, so a caller can tell whose code holds the fusion.
	var got []string
	for _, s := range sites {
		got = append(got, s.String()+" in "+s.Package)
	}
	want := []string{
		"example.com/probe/main.go:7 (main.bare) in main",
		"example.com/probe/main.go:16 (main.callerA) in main",
		"example.com/probe/main.go:16 (main.callerB) in main",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("sites:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if _, err := Scan(filepath.Join(probe, "main.go"), nil); err == nil {
		t.Fatal("Scan of a source file succeeded")
	}
}

// TestEngineHasNoFusedMultiplyAdd is the gate that keeps the engine deterministic on
// arm64, the console's architecture: no line of the engine may compile to a fused
// multiply-add in the veduta tool or in the template game, which between them link every
// package that draws, simulates, cooks or inspects.
func TestEngineHasNoFusedMultiplyAdd(t *testing.T) {
	gocmd := goTool(t)
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Skip("not inside the engine repository")
	}
	dir := t.TempDir()
	for _, pkg := range []string{"./cmd/veduta", "./template/cmd/game"} {
		bin := filepath.Join(dir, filepath.Base(pkg))
		buildArm64(t, gocmd, root, bin, pkg)
		scanEngine(t, pkg, bin)
	}
}

// scanEngine fails the test for every engine line that fused in bin.
func scanEngine(t *testing.T, what, bin string) {
	t.Helper()
	if !linesIn(t, bin, engineModule) {
		t.Fatalf("%s: no engine file in the line table, so the check cannot see the engine", what)
	}
	sites, err := Scan(bin, inModule(engineModule))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sites {
		t.Errorf("%s: %s:%d in %s compiles to %d fused multiply-add(s) on arm64; round the product with an explicit conversion, e.g. float32(a*b) + c",
			what, strings.TrimPrefix(s.File, engineModule+"/"), s.Line, s.Func, s.Count)
	}
}
