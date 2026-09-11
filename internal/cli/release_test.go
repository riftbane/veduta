package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseChangelog(t *testing.T) {
	in := "# Changelog\n\nIntro.\n\n## Unreleased\n\n### Added\n\n- Things.\n\n## v0.0.1 — 2026-01-01\n\n- Old.\n"
	out, err := releaseChangelog([]byte(in), "v0.1.0", "2026-09-12")
	if err != nil {
		t.Fatal(err)
	}
	want := "# Changelog\n\nIntro.\n\n## Unreleased\n\n## v0.1.0 — 2026-09-12\n\n### Added\n\n- Things.\n\n## v0.0.1 — 2026-01-01\n\n- Old.\n"
	if string(out) != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}
	if _, err := releaseChangelog(out, "v0.2.0", "2026-09-13"); err == nil {
		t.Fatal("empty Unreleased section accepted")
	}
	if _, err := releaseChangelog([]byte("# Changelog\n"), "v0.1.0", "x"); err == nil {
		t.Fatal("missing section accepted")
	}
}

func TestAddChangelogEntry(t *testing.T) {
	p := filepath.Join(t.TempDir(), "CHANGELOG.md")
	if err := addChangelogEntry(p, "- one\n"); err != nil {
		t.Fatal(err)
	}
	if err := addChangelogEntry(p, "- two\n"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	if string(data) != "# Changelog\n\n## Unreleased\n\n- two\n- one\n" {
		t.Fatalf("changelog:\n%s", data)
	}
	// Right after a release the Unreleased section is empty and followed by the
	// release's heading, which must stay a separate paragraph.
	os.WriteFile(p, []byte("# Changelog\n\n## Unreleased\n\n## v0.1.0 — x\n\n- one\n"), 0o644)
	if err := addChangelogEntry(p, "- two\n"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(p)
	if string(data) != "# Changelog\n\n## Unreleased\n\n- two\n\n## v0.1.0 — x\n\n- one\n" {
		t.Fatalf("changelog after a release:\n%s", data)
	}
}

// TestReleaseProjectFlow runs the whole release of a game project against a local bare
// remote: checklist, changelog, commit, tag and push.
func TestReleaseProjectFlow(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir, env := newProject(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	git := func(d string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = d
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git(t.TempDir(), "init", "-q", "--bare", remote)
	git(dir, "checkout", "-q", "-b", "main")
	git(dir, "add", "-A")
	git(dir, "commit", "-q", "-m", "Start")
	git(dir, "remote", "add", "origin", remote)
	git(dir, "push", "-q", "origin", "main")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")

	// A dirty tree stops the release.
	os.WriteFile(filepath.Join(dir, "scratch.txt"), []byte("x"), 0o644)
	r, err := Release(env, dir, ReleaseOptions{Version: "v0.1.0", DryRun: true})
	if err != nil || r.OK || r.Steps[len(r.Steps)-1].Name != "clean" {
		t.Fatalf("dirty tree: %+v %v", r, err)
	}
	os.Remove(filepath.Join(dir, "scratch.txt"))

	r, err = Release(env, dir, ReleaseOptions{Version: "v0.1.0"})
	if err != nil || !r.OK || !r.Pushed {
		t.Fatalf("release: %s %v", r.Human(), err)
	}
	if tags := git(remote, "tag", "--list"); tags != "v0.1.0" {
		t.Fatalf("remote tags %q", tags)
	}
	cl, _ := os.ReadFile(filepath.Join(dir, "CHANGELOG.md"))
	if !strings.Contains(string(cl), "## v0.1.0 — ") {
		t.Fatalf("changelog:\n%s", cl)
	}
	// The same version cannot be released twice.
	if r, _ := Release(env, dir, ReleaseOptions{Version: "v0.1.0", DryRun: true}); r.OK {
		t.Fatal("released the same version twice")
	}
}
