package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

	// A release the console cannot install is refused, before the tests run, with the
	// reason and nothing else: a workflow that builds no linux/arm64 archive, or a project
	// without a card.json for the console to list it by.
	refused := func(what, want string) {
		t.Helper()
		r, err := Release(env, dir, ReleaseOptions{Version: "v0.1.0", DryRun: true})
		var names []string
		for _, st := range r.Steps {
			names = append(names, st.Name)
		}
		last := r.Steps[len(r.Steps)-1]
		if err != nil || r.OK || strings.Join(names, ",") != "version,clean,branch,console" || !strings.Contains(last.Detail, want) || !strings.Contains(last.Detail, "; fix: ") || strings.Contains(last.Detail, "publishes the") {
			t.Fatalf("release %s: %s %v", what, r.Human(), err)
		}
		git(dir, "reset", "-q", "--hard", "HEAD~1")
	}
	wf := filepath.Join(dir, ".github", "workflows", "release.yml")
	w, _ := os.ReadFile(wf)
	os.WriteFile(wf, []byte(strings.Replace(string(w), "for target in linux/arm64 linux/amd64", "for target in linux/amd64", 1)), 0o644)
	git(dir, "commit", "-q", "-am", "Build no console archive")
	refused("without a console build", "no linux/arm64")
	git(dir, "rm", "-q", "card.json")
	git(dir, "commit", "-q", "-m", "Drop the card")
	refused("without card.json", "no card.json")
	os.WriteFile(filepath.Join(dir, "card.json"), []byte(`{"veduta": "card/2", "title": "demo"}`), 0o644)
	git(dir, "commit", "-q", "-am", "Write a card the console cannot read")
	refused("with a card that is not card/1", `"veduta" is "card/2"`)

	// A project still on a v0.x engine predates the console: the tool it was made with did
	// not check the console, and a newer tool does not start refusing it.
	os.Remove(filepath.Join(dir, "card.json"))
	os.WriteFile(wf, []byte(strings.Replace(string(w), "for target in linux/arm64 linux/amd64", "for target in linux/amd64 windows/amd64", 1)), 0o644)
	manifest := filepath.Join(dir, "veduta.json")
	m, _ := os.ReadFile(manifest)
	os.WriteFile(manifest, regexp.MustCompile(`"engine": "v[^"]*"`).ReplaceAll(m, []byte(`"engine": "v0.2.0"`)), 0o644)
	git(dir, "commit", "-q", "-am", "Stay on v0.2.0")
	r, err = Release(env, dir, ReleaseOptions{Version: "v0.1.0", DryRun: true})
	if st := findStep(r, "console"); err != nil || !r.OK || st == nil || !st.OK || !strings.Contains(st.Detail, "veduta upgrade") || findStep(r, "arm64") != nil {
		t.Fatalf("release of a v0.x project: %s %v", r.Human(), err)
	}
	git(dir, "reset", "-q", "--hard", "HEAD~1")

	r, err = Release(env, dir, ReleaseOptions{Version: "v0.1.0"})
	if err != nil || !r.OK || !r.Pushed {
		t.Fatalf("release: %s %v", r.Human(), err)
	}
	for _, name := range []string{"console", "arm64"} {
		if st := findStep(r, name); st == nil || !st.OK || strings.Contains(st.Detail, "failed") {
			t.Fatalf("%s step: %s", name, r.Human())
		}
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

// findStep returns the release step called name, or nil.
func findStep(r *ReleaseReport, name string) *ReleaseStep {
	for i := range r.Steps {
		if r.Steps[i].Name == name {
			return &r.Steps[i]
		}
	}
	return nil
}

func TestReleaseTemplate(t *testing.T) {
	got, err := releaseTemplate([]byte("{\n  \"veduta\": \"project/1\",\n  \"engine\": \"v1.0.0\",\n  \"x\": 1\n}\n"), "v1.2.3")
	if err != nil || string(got) != "{\n  \"veduta\": \"project/1\",\n  \"engine\": \"v1.2.3\",\n  \"x\": 1\n}\n" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := releaseTemplate([]byte("{}"), "v1.2.3"); err == nil {
		t.Fatal("a manifest without an engine field was accepted")
	}
}

// The template's engine is the latest released engine: init falls back to it when the tool
// has no version, and the template's code is written against it.
func TestTemplateEngineIsTheLatestRelease(t *testing.T) {
	cl, err := os.ReadFile(filepath.Join("..", "..", "CHANGELOG.md"))
	if err != nil {
		t.Skip("no CHANGELOG.md beside the module")
	}
	m := regexp.MustCompile(`(?m)^## (v\d+\.\d+\.\d+)`).FindSubmatch(cl)
	if m == nil {
		t.Fatal("CHANGELOG.md names no release")
	}
	if got := templateEngine(); got != string(m[1]) {
		t.Fatalf("template/veduta.json names engine %s, the latest release is %s (veduta release moves it)", got, m[1])
	}
}
