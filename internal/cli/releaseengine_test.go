package cli

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestReleaseEngineArm64 runs the engine's release checklist on a stand-in engine module
// whose test fails only on linux/arm64: with qemu-aarch64-static on PATH the checklist
// stops at the arm64 step before the changelog and the tag; without it the step records
// that the suite did not run there.
func TestReleaseEngineArm64(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go vet, go test and go build on a module; skipped in -short mode")
	}
	for _, tool := range []string{"go", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not available", tool)
		}
	}
	// The checklist's own test step runs natively even when this test runs under
	// qemu-user with GOARCH=arm64 in the environment.
	t.Setenv("GOARCH", "")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")

	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	write("go.mod", "module github.com/riftbane/veduta\n\ngo 1.25\n")
	write("cmd/veduta/main.go", "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(`{\"version\": \"dev\"}`) }\n")
	write("golden_test.go", "package veduta\n\nimport (\n\t\"runtime\"\n\t\"testing\"\n)\n\n"+
		"func TestGolden(t *testing.T) {\n\tif runtime.GOARCH == \"arm64\" {\n\t\tt.Fatal(\"golden differs on arm64\")\n\t}\n}\n")
	write("CHANGELOG.md", "# Changelog\n\n## Unreleased\n\n- Things.\n")
	remote := filepath.Join(t.TempDir(), "remote.git")
	git("init", "-q", "--bare", remote)
	git("init", "-q")
	git("checkout", "-q", "-b", "main")
	git("add", "-A")
	git("commit", "-q", "-m", "Start")
	git("remote", "add", "origin", remote)

	env := &Env{Version: "dev", Stdout: io.Discard, Stderr: io.Discard}
	release := func() (*ReleaseReport, ReleaseStep) {
		t.Helper()
		r, err := Release(env, dir, ReleaseOptions{Version: "v9.0.0", DryRun: true})
		if err != nil || r.Kind != "engine" {
			t.Fatalf("release: %+v %v", r, err)
		}
		for _, st := range r.Steps {
			if st.Name == "arm64" {
				return r, st
			}
		}
		t.Fatalf("no arm64 step:\n%s", r.Human())
		return nil, ReleaseStep{}
	}

	if _, err := exec.LookPath("qemu-aarch64-static"); err == nil {
		r, st := release()
		if r.OK || r.Steps[len(r.Steps)-1].Name != "arm64" || st.OK || !strings.Contains(st.Detail, "golden differs on arm64") {
			t.Fatalf("a suite that fails on arm64 was not refused:\n%s", r.Human())
		}
		// Without qemu-user on PATH the step cannot run: it says so and lets the release on.
		bin := t.TempDir()
		for _, tool := range []string{"go", "git"} {
			p, _ := exec.LookPath(tool)
			if err := os.Symlink(p, filepath.Join(bin, tool)); err != nil {
				t.Skipf("cannot link %s: %v", tool, err)
			}
		}
		t.Setenv("PATH", bin)
	}
	r, st := release()
	if !st.OK || !strings.Contains(st.Detail, "skipped: qemu-aarch64-static is not on PATH") || r.Steps[len(r.Steps)-1].Name == "arm64" {
		t.Fatalf("release without qemu-user:\n%s", r.Human())
	}
}
