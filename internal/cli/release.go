package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/riftbane/veduta/internal/update"
	"github.com/riftbane/veduta/mcp"
)

// ReleaseOptions configures release.
type ReleaseOptions struct {
	Version  string
	DryRun   bool
	Trailers []string // extra lines appended to the release commit message
}

// ReleaseStep is one checklist item.
type ReleaseStep struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// ReleaseReport is the result of release.
type ReleaseReport struct {
	OK      bool          `json:"ok"`
	Version string        `json:"version"`
	Kind    string        `json:"kind"` // "project" or "engine"
	DryRun  bool          `json:"dry_run"`
	Steps   []ReleaseStep `json:"steps"`
	Tagged  bool          `json:"tagged"`
	Pushed  bool          `json:"pushed"`
}

// ExitCode is 1 when a step failed.
func (r *ReleaseReport) ExitCode() int {
	if r.OK {
		return 0
	}
	return 1
}

// Human prints the checklist.
func (r *ReleaseReport) Human() string {
	var b strings.Builder
	for _, st := range r.Steps {
		mark := "ok  "
		if !st.OK {
			mark = "FAIL"
		}
		fmt.Fprintf(&b, "%s %-10s %s\n", mark, st.Name, st.Detail)
	}
	switch {
	case !r.OK:
		fmt.Fprintf(&b, "release %s stopped: fix the failing step\n", r.Version)
	case r.DryRun:
		fmt.Fprintf(&b, "dry run: %s would be tagged and pushed\n", r.Version)
	case r.Pushed:
		fmt.Fprintf(&b, "tagged and pushed %s: CI publishes the release\n", r.Version)
	}
	return b.String()
}

// Release runs the release checklist (clean tree, tests, cook, smoke render), turns the
// CHANGELOG's Unreleased section into the version's entry, commits, tags and pushes; CI
// publishes the artifacts. It works in a game project (veduta.json) and in the engine
// repository itself.
func Release(env *Env, projectDir string, o ReleaseOptions) (*ReleaseReport, error) {
	if !update.IsVersion(o.Version) {
		return nil, usagef("release: %q is not a version like v0.1.0", o.Version)
	}
	if strings.Contains(o.Version, "-") {
		// The checklist moves the changelog's Unreleased section into the version being
		// released; a candidate would eat the section the release itself needs.
		return nil, usagef("release: %s is a pre-release; tag it by hand (git tag %s && git push origin %s), which keeps the Unreleased section for the release", o.Version, o.Version, o.Version)
	}
	dir := projectDir
	if dir == "" {
		dir, _ = os.Getwd()
	}
	r := &ReleaseReport{Version: o.Version, DryRun: o.DryRun}
	step := func(name string, ok bool, format string, args ...any) bool {
		r.Steps = append(r.Steps, ReleaseStep{Name: name, OK: ok, Detail: fmt.Sprintf(format, args...)})
		return ok
	}
	s, serr := OpenSession(dir, env)
	root := dir
	if serr == nil {
		r.Kind, root = "project", s.Root
	} else if isEngineRepo(dir) {
		r.Kind = "engine"
	} else {
		return nil, fmt.Errorf("release: %v (and %s is not the engine repository)", serr, dir)
	}
	git := func(args ...string) (string, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		err := cmd.Run()
		return strings.TrimSpace(out.String()), err
	}
	// 1. Version and tag.
	tags, _ := git("tag", "--list", "v*")
	latest := ""
	exists := false
	for _, t := range strings.Fields(tags) {
		if t == o.Version {
			exists = true
		}
		if update.IsVersion(t) && update.Compare(t, latest) > 0 {
			latest = t
		}
	}
	if !step("version", !exists && update.Compare(o.Version, latest) > 0, "%s (latest tag %s)", o.Version, orNone(latest)) {
		return finish(r), nil
	}
	// 2. Clean tree on a branch with a remote.
	status, err := git("status", "--porcelain")
	if !step("clean", err == nil && status == "", "working tree %s", map[bool]string{true: "clean", false: "has changes:\n" + status}[err == nil && status == ""]) {
		return finish(r), nil
	}
	branch, err := git("symbolic-ref", "--short", "HEAD")
	remote, rerr := git("remote", "get-url", "origin")
	if !step("branch", err == nil && rerr == nil, "branch %s, origin %s", branch, remote) {
		return finish(r), nil
	}
	// 3. Tests.
	if r.Kind == "project" {
		// A release nobody can install on the console is not a release. The workflow and the
		// card are files to read, so they are checked before the tests; the console build
		// comes after the smoke render. A project still on a v0.x engine predates the
		// console and releases as it did: updating the tool must not stop that.
		preConsole := v0Engine(s.Project.Engine)
		why := ""
		if preConsole {
			step("console", true, "not checked: the project's engine %s predates the console (v1.0.0); after veduta upgrade, release refuses a workflow without a %s/%s archive, a missing card.json and a game that does not build for the console", s.Project.Engine, targetOS, targetArch)
		} else if why = s.consoleReleasable(); !step("console", why == "", "%s", firstNonEmpty(why, "a workflow publishes the "+targetOS+"/"+targetArch+" archive and card.json is card/1")) {
			return finish(r), nil
		}
		tr, err := s.Test(false)
		if !step("test", err == nil && tr.OK, "%s", testDetail(tr, err)) {
			return finish(r), nil
		}
		cr, err := s.Cook(false)
		if !step("cook", err == nil && cr.Failed == 0, "%d fresh, %d compiled, %d failed", cr.Fresh, cr.Compiled, cr.Failed) {
			return finish(r), nil
		}
		rep, err := s.Render(RenderOptions{Out: s.Out("release_smoke.png")})
		if !step("smoke", err == nil, "render %v", renderDetail(rep, err)) {
			return finish(r), nil
		}
		if !preConsole {
			why = s.consoleBuilds()
			if !step("arm64", why == "", "%s", firstNonEmpty(why, "the game builds for "+targetOS+"/"+targetArch)) {
				return finish(r), nil
			}
		}
	} else {
		out, err := runIn(root, "go", "vet", "./...")
		if !step("vet", err == nil, "go vet ./... %s", okOr(out, err)) {
			return finish(r), nil
		}
		out, err = runIn(root, "go", "test", "./...")
		if !step("test", err == nil, "go test ./... %s", okOr(tail(out, 1500), err)) {
			return finish(r), nil
		}
		bin := filepath.Join(os.TempDir(), fmt.Sprintf("veduta-release-smoke-%d", time.Now().UnixNano()))
		out, err = runIn(root, "go", "build", "-o", bin, "./cmd/veduta")
		if err == nil {
			out, err = runIn(root, bin, "version", "--json")
			os.Remove(bin)
		}
		if !step("smoke", err == nil, "build and run cmd/veduta: %s", okOr(out, err)) {
			return finish(r), nil
		}
	}
	// 4. Changelog.
	clPath := filepath.Join(root, "CHANGELOG.md")
	cl, err := os.ReadFile(clPath)
	date := time.Now().UTC().Format("2006-01-02")
	var next []byte
	if err == nil {
		next, err = releaseChangelog(cl, o.Version, date)
	} else if os.IsNotExist(err) && r.Kind == "project" {
		next, err = []byte(fmt.Sprintf("# Changelog\n\n## Unreleased\n\n## %s — %s\n\n- First release.\n", o.Version, date)), nil
	}
	if !step("changelog", err == nil, "%s", okOr("Unreleased → "+o.Version+" — "+date, err)) {
		return finish(r), nil
	}
	if o.DryRun {
		r.OK = true
		return r, nil
	}
	// 5. Commit, tag, push.
	if err := os.WriteFile(clPath, next, 0o644); err != nil {
		return nil, err
	}
	msg := fmt.Sprintf("Release %s\n\nMove the Unreleased changelog section to %s.", o.Version, o.Version)
	if len(o.Trailers) > 0 {
		msg += "\n\n" + strings.Join(o.Trailers, "\n")
	}
	if out, err := git("add", "CHANGELOG.md"); !step("stage", err == nil, "%s", okOr(out, err)) {
		return finish(r), nil
	}
	if out, err := git("commit", "-q", "-m", msg); !step("commit", err == nil, "%s", okOr(out, err)) {
		return finish(r), nil
	}
	if out, err := git("tag", "-a", o.Version, "-m", "Release "+o.Version); !step("tag", err == nil, "%s", okOr(out, err)) {
		return finish(r), nil
	}
	r.Tagged = true
	out, err := git("push", "origin", "HEAD")
	if err == nil {
		out, err = git("push", "origin", o.Version)
	}
	if !step("push", err == nil, "%s", okOr(out, err)) {
		return finish(r), nil
	}
	r.Pushed = true
	r.OK = true
	return r, nil
}

func finish(r *ReleaseReport) *ReleaseReport {
	r.OK = false
	return r
}

func okOr(detail string, err error) string {
	if err != nil {
		return fmt.Sprintf("failed: %v %s", err, detail)
	}
	if detail == "" {
		return "ok"
	}
	return detail
}

func testDetail(tr *TestReport, err error) string {
	if err != nil {
		return err.Error()
	}
	if tr.OK {
		return fmt.Sprintf("%d scenarios pass, go test ok", len(tr.Scenarios))
	}
	return "failed: " + strings.Join(tr.Failed, ", ")
}

func renderDetail(rep map[string]any, err error) string {
	if err != nil {
		return err.Error()
	}
	return fmt.Sprint(rep["out"])
}

func runIn(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = goEnv(os.Environ())
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// isEngineRepo reports whether dir is (inside) the engine's own repository.
func isEngineRepo(dir string) bool {
	for d := dir; ; {
		if data, err := os.ReadFile(filepath.Join(d, "go.mod")); err == nil {
			return bytes.HasPrefix(bytes.TrimSpace(data), []byte("module github.com/riftbane/veduta"))
		}
		parent := filepath.Dir(d)
		if parent == d {
			return false
		}
		d = parent
	}
}

// releaseChangelog renames "## Unreleased" to "## <version> — <date>" and opens a new,
// empty Unreleased section above it. The Unreleased section must not be empty.
func releaseChangelog(cl []byte, version, date string) ([]byte, error) {
	s := string(cl)
	i := strings.Index(s, "## Unreleased")
	if i < 0 {
		return nil, fmt.Errorf("CHANGELOG.md has no \"## Unreleased\" section")
	}
	body := s[i+len("## Unreleased"):]
	if j := strings.Index(body, "\n## "); j >= 0 {
		body = body[:j]
	}
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("the Unreleased section of CHANGELOG.md is empty: describe the release first")
	}
	if strings.Contains(s, "## "+version+" ") || strings.Contains(s, "## "+version+"\n") {
		return nil, fmt.Errorf("CHANGELOG.md already has a %s section", version)
	}
	out := s[:i] + "## Unreleased\n\n## " + version + " — " + date + s[i+len("## Unreleased"):]
	return []byte(out), nil
}

func init() {
	register(command{
		name: "release", usage: "release vX.Y.Z [--dry-run]", summary: "checklist (clean tree, test, cook, smoke render) → CHANGELOG entry → tag → push; CI publishes",
		run: func(env *Env, _ *Session, args []string) (any, error) {
			fs := newFlags("release", env.Stderr)
			dry := fs.Bool("dry-run", false, "run the checklist without committing, tagging or pushing")
			var trailers stringList
			fs.Var(&trailers, "trailer", "line appended to the release commit message (repeatable)")
			var version string
			for {
				if err := fs.Parse(args); err != nil {
					return nil, UsageError{err}
				}
				if fs.NArg() == 0 {
					break
				}
				if version != "" {
					return nil, usagef("release: one version, got %q and %q", version, fs.Arg(0))
				}
				version = fs.Arg(0)
				args = fs.Args()[1:]
			}
			return Release(env, mcpProjectDir, ReleaseOptions{Version: version, DryRun: *dry, Trailers: trailers})
		},
	})
	prev := mcpExtraTools
	mcpExtraTools = func(m *mcpServer) []mcp.Tool {
		return append(prev(m), mcp.Tool{
			Name:        "release",
			Description: "Run the release checklist (clean tree, test, cook, smoke render); when everything passes and dry_run is false, move the CHANGELOG's Unreleased section to the version, commit, tag and push (CI publishes the archives).",
			InputSchema: schema(map[string]any{"version": str("version tag, e.g. v0.1.0"), "dry_run": boolean("only run the checklist")}, "version"),
			Handler: func(ctx context.Context, args json.RawMessage) (*mcp.Result, error) {
				var a struct {
					Version string `json:"version"`
					DryRun  bool   `json:"dry_run"`
				}
				if err := mcp.Strict(args, &a); err != nil {
					return errResult(err), nil
				}
				r, err := Release(m.env, m.projectDir, ReleaseOptions{Version: a.Version, DryRun: a.DryRun})
				if err != nil {
					return errResult(err), nil
				}
				res := textResult(r)
				res.IsError = !r.OK
				return res, nil
			},
		})
	}
}

// stringList is a repeatable string flag.
type stringList []string

func (l *stringList) String() string     { return strings.Join(*l, ", ") }
func (l *stringList) Set(v string) error { *l = append(*l, v); return nil }
