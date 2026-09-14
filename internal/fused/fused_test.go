package fused

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

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

func buildArm64(t *testing.T, gocmd, dir, out, pkg string) {
	t.Helper()
	cmd := exec.Command(gocmd, "build", "-o", out, pkg)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s for linux/arm64: %v\n%s", pkg, err, b)
	}
}

// TestScanFindsAKnownFusion is the control for the engine check below: a scanner that
// silently found nothing would make that check pass forever.
func TestScanFindsAKnownFusion(t *testing.T) {
	gocmd := goTool(t)
	dir := t.TempDir()
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

func main() {
	os.Exit(int(bare(1, 2, 3) + rounded(1, 2, 3)))
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module probe\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	buildArm64(t, gocmd, dir, bin, ".")
	sites, err := Scan(bin, func(f string) bool { return strings.HasSuffix(f, "main.go") })
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 1 || sites[0].Line != 7 || !strings.HasSuffix(sites[0].Func, "main.bare") {
		t.Fatalf("sites = %v, want exactly main.go:7 in main.bare", sites)
	}
	if _, err := Scan(filepath.Join(dir, "main.go"), nil); err == nil {
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
	prefix := filepath.ToSlash(root) + "/"
	for _, pkg := range []string{"./cmd/veduta", "./template/cmd/game"} {
		bin := filepath.Join(dir, filepath.Base(pkg))
		buildArm64(t, gocmd, root, bin, pkg)
		sites, err := Scan(bin, func(f string) bool { return strings.HasPrefix(filepath.ToSlash(f), prefix) })
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range sites {
			t.Errorf("%s: %s:%d in %s compiles to %d fused multiply-add(s) on arm64; round the product with an explicit conversion, e.g. float32(a*b) + c",
				pkg, strings.TrimPrefix(filepath.ToSlash(s.File), prefix), s.Line, s.Func, s.Count)
		}
	}
}
