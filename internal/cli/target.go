package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/riftbane/veduta/internal/fused"
)

// The player target of this release line: a console built on a 64-bit ARM board with a
// 320x240 SPI panel refreshed 20 times a second. Everything else — Windows, macOS, a Linux
// desktop — is an authoring machine that builds, cooks, inspects, simulates and tests.
const (
	targetOS, targetArch = "linux", "arm64"
	panelW, panelH       = 320, 240
	panelHz              = 20
)

// targetLine describes where a game plays, in the words doctor and status use.
func targetLine() string {
	return fmt.Sprintf("plays on the console: %s/%s, %dx%d panel at %d Hz, gamepad", targetOS, targetArch, panelW, panelH, panelHz)
}

// sysRoot is where findPanel looks for /sys/class/graphics. Tests replace it.
var sysRoot = "/"

// findPanel reports the framebuffer a player on this machine would draw on, the way the
// platform layer finds it (the tool never links that layer, so the probe is repeated
// here): a /sys/class/graphics/fbN with 16 or 32 bits per pixel, or whatever VEDUTA_FB
// names. It returns "" when there is none, and always on anything but Linux.
func findPanel() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	if want := os.Getenv("VEDUTA_FB"); want != "" {
		return "VEDUTA_FB=" + want
	}
	dir := filepath.Join(sysRoot, "sys", "class", "graphics")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var found []string
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "fb") || strings.Contains(e.Name(), "con") {
			continue
		}
		bits, err := os.ReadFile(filepath.Join(dir, e.Name(), "bits_per_pixel"))
		if err != nil {
			continue
		}
		if b, _ := strconv.Atoi(strings.TrimSpace(string(bits))); b != 16 && b != 32 {
			continue
		}
		name, _ := os.ReadFile(filepath.Join(dir, e.Name(), "name"))
		found = append(found, fmt.Sprintf("%s (%s, %s bpp)", e.Name(), strings.TrimSpace(string(name)), strings.TrimSpace(string(bits))))
	}
	sort.Strings(found)
	return strings.Join(found, ", ")
}

// runRefusal explains why `veduta run` cannot show the game of a project on the given
// engine version on this machine, or returns "" when it can. A project still on a v0.x
// engine has the player of that engine, which opens a desktop window, so it is checked as
// v0.x tools checked it: updating the tool must not stop an un-upgraded game from running.
func runRefusal(engine string) string {
	if v0Engine(engine) {
		if hasDisplay() {
			return ""
		}
		return "run: no display on this machine, and the project's engine " + engine + " plays in a window; use render and simulate, or veduta upgrade to move the game to the console's framebuffer player"
	}
	if runtime.GOOS == "windows" {
		return "" // the simulator window
	}
	if runtime.GOOS != "linux" {
		return fmt.Sprintf("run: the player draws on a Linux framebuffer, or on Windows in the simulator, and this is %s/%s; build, render, simulate and test here, and play the game on the console (its release builds %s/%s)",
			runtime.GOOS, runtime.GOARCH, targetOS, targetArch)
	}
	if findPanel() == "" {
		return "run: no framebuffer on this machine (none with 16 or 32 bits per pixel in /sys/class/graphics); the player draws on the console's panel or a Linux text console. " +
			"Set VEDUTA_FB to name a framebuffer and VEDUTA_SCALE to divide it; use render and simulate to see the game here"
	}
	return ""
}

// hasDisplay reports whether a v0.x player could open its window here, as the v0.x tools
// decided: always on Windows and macOS, and on Linux when DISPLAY or WAYLAND_DISPLAY is set.
func hasDisplay() bool {
	switch runtime.GOOS {
	case "windows", "darwin":
		return true
	}
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

// consoleBuild compiles the game for the console into a temporary file and returns its
// path, which the caller removes. Compile errors come back located, as from build. The
// build is -trimpath, as the release workflow's is: the file names it records are then
// module paths (demo/game/kinds.go), which fusedSites matches whatever directory, symlink
// or GOFLAGS the tool runs with (a flag on the command line overrides GOFLAGS).
func (s *Session) consoleBuild() (string, []CompileError, error) {
	f, err := os.CreateTemp("", "veduta-console-*")
	if err != nil {
		return "", nil, err
	}
	bin := f.Name()
	f.Close()
	cmd := s.goCmd("build", "-trimpath", "-o", bin, s.Project.Entry)
	cmd.Env = append(cmd.Env, "GOOS="+targetOS, "GOARCH="+targetArch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		os.Remove(bin)
		errs, rest := parseCompileErrors(string(out))
		if len(errs) == 0 {
			errs = []CompileError{{Msg: strings.TrimSpace(firstNonEmpty(rest, err.Error()))}}
		}
		return "", errs, fmt.Errorf("go build for %s/%s: %w", targetOS, targetArch, err)
	}
	return bin, nil, nil
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// fusedSites lists the places in the game's own code that compiled to fused multiply-adds
// in the console binary at bin, built by consoleBuild. The game's code is its main module
// and its entry package. A line of the game is listed as root-relative "file:line"; a line
// of another module (a gmath helper, say) that was inlined into a function of the game,
// where the product the game passed in fused with the helper's addition, is listed with
// that function: "gmath/vec.go:105 inlined in demo/game.updatePlayer". Lines of the game
// come first, each once, in file and line order.
func (s *Session) fusedSites(bin string) ([]string, error) {
	mod, modDir, err := s.mainModule()
	if err != nil {
		return nil, err
	}
	files, err := fused.Files(bin)
	if err != nil {
		return nil, err
	}
	if !slices.ContainsFunc(files, func(f string) bool { return fused.InModule(f, mod) }) {
		return nil, fmt.Errorf("no file of module %s in the console build, so its lines cannot be told apart", mod)
	}
	sites, err := fused.Scan(bin, func(st fused.Site) bool {
		return fused.InModule(st.File, mod) || fused.InModule(st.Package, mod) || st.Package == "main"
	})
	if err != nil {
		return nil, err
	}
	var lines, inlined []string
	listed := map[string]bool{}
	for _, st := range sites {
		if fused.InModule(st.File, mod) {
			p := fmt.Sprintf("%s:%d", s.Rel(filepath.Join(modDir, filepath.FromSlash(strings.TrimPrefix(st.File, mod+"/")))), st.Line)
			if !listed[p] { // a line of the game inlined into several of its functions
				listed[p] = true
				lines = append(lines, p)
			}
			continue
		}
		inlined = append(inlined, fmt.Sprintf("%s:%d inlined in %s", engineFile(st.File), st.Line, st.Func))
	}
	return append(lines, inlined...), nil
}

// engineFile shortens a file of the engine module, as a -trimpath build records it with or
// without a version, to its path inside the engine (gmath/vec.go). Other files are kept.
func engineFile(f string) string {
	const engine = "github.com/riftbane/veduta"
	rest, ok := strings.CutPrefix(f, engine)
	if !ok {
		return f
	}
	if strings.HasPrefix(rest, "@") {
		_, rest, _ = strings.Cut(rest, "/")
		return rest
	}
	if r, ok := strings.CutPrefix(rest, "/"); ok {
		return r
	}
	return f
}

// mainModule finds the go.mod that governs the project root, as the go command does, and
// returns its module path and directory.
func (s *Session) mainModule() (path, dir string, err error) {
	for d := s.Root; ; {
		data, err := os.ReadFile(filepath.Join(d, "go.mod"))
		if err == nil {
			if m := modulePath(data); m != "" {
				return m, d, nil
			}
			return "", "", fmt.Errorf("%s has no module line", filepath.Join(d, "go.mod"))
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", "", fmt.Errorf("no go.mod in %s or any parent directory", s.Root)
		}
		d = parent
	}
}

// modulePath returns the path the module line of a go.mod names, or "".
func modulePath(gomod []byte) string {
	for _, line := range strings.Split(string(gomod), "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module")
		if !ok || rest == "" || !strings.ContainsAny(rest[:1], " \t\"") {
			continue
		}
		rest = strings.TrimSpace(rest)
		if p, err := strconv.Unquote(rest); err == nil {
			return p
		}
		return rest
	}
	return ""
}

// releaseTargetsConsole reports whether a workflow of the project builds for the console:
// the first .github/workflows/*.yml or *.yaml, in name order, that buildsConsole accepts.
// Any workflow counts, not only release.yml, since a project may name or split its own.
func (s *Session) releaseTargetsConsole() (bool, string) {
	const dir = ".github/workflows"
	what := targetOS + "/" + targetArch
	if s.IsScript() {
		what = "console" // a script game's archive is the same for every console
	}
	built := what
	if s.IsScript() {
		built = "the console archive"
	}
	entries, _ := os.ReadDir(filepath.Join(s.Root, filepath.FromSlash(dir)))
	found := false
	for _, e := range entries { // ReadDir sorts by name
		name := e.Name()
		if e.IsDir() || !(strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")) {
			continue
		}
		found = true
		data, err := os.ReadFile(filepath.Join(s.Root, filepath.FromSlash(dir), name))
		if err == nil && publishes(string(data)) && (s.IsScript() || buildsConsole(string(data))) {
			return true, dir + "/" + name + " builds " + built
		}
	}
	if !found {
		return false, "no workflow in " + dir + ", so no " + what + " archive"
	}
	return false, dir + " has no " + what + " build"
}

var (
	consoleTargetRe = regexp.MustCompile(`\b` + targetOS + `/` + targetArch + `\b`)
	// GOOS=linux, GOOS: linux, or GOOS set from an expression (a matrix).
	targetOSRe = regexp.MustCompile(`(?i)\bgoos\s*[:=]\s*["']?(` + targetOS + `\b|\$\{\{)`)
	// GOARCH=arm64 or GOARCH: arm64.
	targetArchRe = regexp.MustCompile(`(?i)\bgoarch\s*[:=]\s*["']?` + targetArch + `\b`)
	// GOARCH set from an expression, and a matrix list of architectures holding arm64.
	matrixArchRe = regexp.MustCompile(`(?i)\bgoarch\s*[:=]\s*["']?\$\{\{`)
	matrixListRe = regexp.MustCompile(`(?i)\b\w*arch\w*\s*:\s*\[[^\]\n]*\b` + targetArch + `\b`)
	// A trigger that publishes: a pushed tag or a GitHub release.
	publishRe = regexp.MustCompile(`(?m)^\s*(tags|release)\s*:`)
)

// publishes reports whether a workflow runs when a release is made (on a pushed tag or a
// GitHub release), rather than on every push: a CI job that only tests arm64 under qemu
// publishes nothing.
func publishes(workflow string) bool { return publishRe.MatchString(workflowCode(workflow)) }

// buildsConsole reports whether the text of a GitHub Actions workflow, its comments left
// out, builds for the console: it names linux/arm64 (a "for target in linux/arm64 ..."
// list), or sets GOOS to linux and GOARCH to arm64, directly or from a matrix whose list
// of architectures holds arm64. It reads text, not YAML, so a workflow can still mislead
// it, but a comment, or arm64 built for another system, does not count.
func buildsConsole(workflow string) bool {
	text := workflowCode(workflow)
	if consoleTargetRe.MatchString(text) {
		return true
	}
	return targetOSRe.MatchString(text) &&
		(targetArchRe.MatchString(text) || matrixArchRe.MatchString(text) && matrixListRe.MatchString(text))
}

// workflowCode is the text of a workflow with its comments cut.
func workflowCode(workflow string) string {
	var code strings.Builder
	for _, line := range strings.Split(workflow, "\n") {
		code.WriteString(stripYAMLComment(line))
		code.WriteByte('\n')
	}
	return code.String()
}

// stripYAMLComment cuts a line at a # that starts it or follows white space, which begins
// a comment in YAML and in the shell of a run block alike (${x#y} and $# are not cut).
func stripYAMLComment(line string) string {
	for i := 0; i < len(line); i++ {
		if line[i] == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t') {
			return line[:i]
		}
	}
	return line
}

// fusedCheck is the arm64 warning for the places fusedSites found. The text names the
// first ten; Sites carries every one, for --json and the agents that fix them all at once.
func fusedCheck(sites []string) Check {
	shown := sites
	if len(shown) > 10 {
		shown = append(shown[:10:10], fmt.Sprintf("and %d more (sites in the JSON report lists them all)", len(sites)-10))
	}
	return Check{Name: "arm64", OK: true, Warning: true, Sites: sites,
		Detail: fmt.Sprintf("builds, but %d place(s) in the game's code compile to fused multiply-adds on arm64, so the console computes different bits than this machine and traces, goldens and scenarios can drift: %s", len(sites), strings.Join(shown, ", ")),
		Fix: "wrap each product that feeds + or - in a conversion to its own type, float32(a*b) + c; gmath's Vec.Scale, Mul, Dot and matrix products already do. " +
			"A line of another package inlined in a game function adds a product that function passes in: round it there, pos.Add(gmath.V3(float32(a*b), 0, 0)) (docs: api, Determinism rules)"}
}

// cardFields are the fields card/1 defines besides "veduta"; each is a string when present.
var cardFields = []string{"title", "name", "version", "exec", "icon"}

// readCard reads card.json, the description the console lists a game by. It returns the
// card, or why the console could not read it: the file is missing, is not a JSON object,
// is not card/1 or has a field of the wrong type. The release gate refuses such a card.
func (s *Session) readCard() (map[string]any, string) {
	data, err := os.ReadFile(filepath.Join(s.Root, "card.json"))
	if os.IsNotExist(err) {
		return nil, "no card.json: the console lists a game by it (veduta init writes one)"
	}
	if err != nil {
		return nil, err.Error()
	}
	var c map[string]any
	if err := json.Unmarshal(data, &c); err != nil || c == nil {
		why := "null"
		if err != nil {
			why = err.Error()
		}
		return nil, "card.json is not a JSON object: " + why
	}
	if v, _ := c["veduta"].(string); v != "card/1" {
		return nil, fmt.Sprintf(`card.json's "veduta" is %q, want "card/1"`, v)
	}
	for _, f := range cardFields {
		if v, ok := c[f]; ok {
			if _, isString := v.(string); !isString {
				return nil, fmt.Sprintf("card.json's %q is not a string", f)
			}
		}
	}
	return c, ""
}

// workflowFix is what to do about a project whose workflows build no console archive.
const workflowFix = "build " + targetOS + "/" + targetArch + " in .github/workflows/release.yml (compare with the workflow veduta init writes)"

// cardFix is what to do about a card.json that is missing, unreadable or disagrees with
// the manifest: the card veduta init would write for it.
func (s *Session) cardFix() string {
	return fmt.Sprintf(`write card.json as {"veduta": "card/1", "title": %q, "name": %q, "exec": %q}`, s.Project.Title, s.Project.Name, s.Project.Name)
}

// cardProblem compares card.json with the manifest. It returns "" when the card is valid
// and agrees with veduta.json; a card that is not valid is marked as refused by release.
func (s *Session) cardProblem() string {
	c, why := s.readCard()
	if why != "" {
		if v0Engine(s.Project.Engine) {
			return why + "; veduta release will refuse it once the project is upgraded to v1"
		}
		return why + "; veduta release refuses until it is fixed"
	}
	var diffs []string
	if v, ok := c["title"].(string); ok && v != s.Project.Title {
		diffs = append(diffs, fmt.Sprintf("title %q differs from veduta.json's %q", v, s.Project.Title))
	}
	if v, ok := c["name"].(string); ok && v != s.Project.Name {
		diffs = append(diffs, fmt.Sprintf("name %q differs from veduta.json's %q", v, s.Project.Name))
	}
	return strings.Join(diffs, "; ")
}

// consoleChecks are doctor's checks of a project against the console it plays on.
func (s *Session) consoleChecks() []Check {
	var cs []Check
	here := "no framebuffer on this machine: render and simulate show the game here"
	if p := findPanel(); p != "" {
		here = "framebuffer here: " + p
	}
	cs = append(cs, Check{Name: "target", OK: true, Detail: targetLine() + "; " + here})

	var notes []string
	if r := s.Project.TickRate; r > panelHz {
		notes = append(notes, fmt.Sprintf("tick_rate %d is above the panel's %d Hz: the player draws one frame per tick, so the extra ticks are frames the console spends time on and never shows", r, panelHz))
	}
	if w, h := s.Project.Resolution[0], s.Project.Resolution[1]; w*panelH != h*panelW {
		notes = append(notes, fmt.Sprintf("resolution %dx%d is not the panel's 4:3 shape (%dx%d)", w, h, panelW, panelH))
	}
	if len(notes) > 0 {
		cs = append(cs, Check{Name: "frame", OK: true, Warning: true, Detail: strings.Join(notes, "; "),
			Fix: fmt.Sprintf(`set "resolution": [%d, %d] and "tick_rate": %d in veduta.json (this changes every trace hash)`, panelW, panelH, panelHz)})
	}

	cs = append(cs, s.arm64Check())

	if ok, detail := s.releaseTargetsConsole(); ok {
		cs = append(cs, Check{Name: "release", OK: true, Detail: detail})
	} else {
		cs = append(cs, Check{Name: "release", OK: true, Warning: true, Detail: detail + ": no release of this project can be installed on the console",
			Fix: workflowFix})
	}
	if p := s.cardProblem(); p != "" {
		cs = append(cs, Check{Name: "card", OK: true, Warning: true, Detail: p,
			Fix: s.cardFix()})
	} else {
		cs = append(cs, Check{Name: "card", OK: true, Detail: "card.json agrees with veduta.json"})
	}
	return cs
}

// lookGo finds the go command. Tests replace it.
var lookGo = func() error { _, err := exec.LookPath("go"); return err }

// arm64Check builds the game for the console and scans the build for fused lines. A script
// game has nothing to build: its scripts must compile, and the console's runtime runs them.
func (s *Session) arm64Check() Check {
	if s.IsScript() {
		b, err := s.Build(false)
		if err != nil || !b.OK {
			return Check{Name: "scripts", OK: false, Detail: buildDetail(b, err), Fix: "fix the located errors (veduta build lists them)"}
		}
		return Check{Name: "scripts", OK: true, Detail: "the scripts compile; the console runs them as they are"}
	}
	if err := lookGo(); err != nil {
		// The go check already fails and says how to install Go.
		return Check{Name: "arm64", OK: true, Warning: true, Detail: "not checked: go not found on PATH"}
	}
	bin, errs, err := s.consoleBuild()
	if err != nil {
		detail, fix := s.consoleBuildFailure(errs, err)
		return Check{Name: "arm64", OK: false, Detail: detail, Fix: fix}
	}
	sites, serr := s.fusedSites(bin)
	os.Remove(bin)
	switch {
	case serr != nil:
		return Check{Name: "arm64", OK: true, Warning: true, Detail: "builds for " + targetOS + "/" + targetArch + ", but its code could not be checked: " + serr.Error()}
	case len(sites) > 0:
		return fusedCheck(sites)
	}
	return Check{Name: "arm64", OK: true, Detail: "builds for " + targetOS + "/" + targetArch + " with no fused multiply-add in the game's code"}
}

// consoleBuildFailure describes a failed console build: the error with the first compiler
// error, located when the compiler gave a place, and a fix chosen from what failed. Only a
// build that cgo broke gets the pure Go advice; modules that cannot be resolved (offline, a
// missing go.sum entry, an unknown engine revision) and a missing go command get their own.
func (s *Session) consoleBuildFailure(errs []CompileError, err error) (detail, fix string) {
	detail = err.Error()
	var text strings.Builder
	text.WriteString(detail)
	for _, e := range errs {
		text.WriteString("\n" + e.Msg)
	}
	if len(errs) > 0 {
		e := errs[0]
		switch msg, _, _ := strings.Cut(strings.TrimSpace(e.Msg), "\n"); {
		case e.File != "":
			detail = fmt.Sprintf("%s: %s:%d:%d: %s", detail, e.File, e.Line, e.Col, e.Msg)
		case msg != "" && !strings.Contains(detail, msg):
			detail += ": " + msg
		}
	}
	all := strings.ToLower(text.String())
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(all, w) {
				return true
			}
		}
		return false
	}
	build := fmt.Sprintf("GOOS=%s GOARCH=%s CGO_ENABLED=0 go build %s", targetOS, targetArch, s.Project.Entry)
	switch {
	case has("executable file not found"):
		fix = "install Go 1.25 or newer (install.sh --with-go does it)"
	case has(`import "c"`, "cgo", "c source files not allowed"):
		fix = "the console runs " + targetOS + "/" + targetArch + " and every build is CGO_ENABLED=0: keep the game and its dependencies pure Go"
	case has("go.sum", "go.mod", "unknown revision", "no required module", "cannot find module", "module lookup disabled",
		"dial tcp", "no such host", "connection refused", "i/o timeout", "tls handshake", "proxy.golang.org", "goproxy"):
		fix = "the modules could not be resolved: run go mod tidy (or go mod download) with a network connection, or point go.mod at a local engine checkout (veduta init --engine-dir)"
	case len(errs) > 0 && errs[0].File != "":
		fix = "fix the located error, which appears only when the game builds for " + targetOS + "/" + targetArch + " (" + build + "): code behind a build constraint that leaves out linux or arm64 does not reach the console"
	default:
		fix = "run " + build + " to see the whole output"
	}
	return detail, fix
}

// consoleReleasable is the first half of the release gate, a release nobody can install on
// the console is not a release: it returns "" when a workflow publishes a linux/arm64
// archive and card.json is one the console can read, and otherwise why not. It only reads
// files, so release runs it before the tests.
func (s *Session) consoleReleasable() string {
	if ok, detail := s.releaseTargetsConsole(); !ok {
		return detail + "; fix: " + workflowFix
	}
	if _, why := s.readCard(); why != "" {
		return why + "; fix: " + s.cardFix()
	}
	return ""
}

// consoleBuilds is the second half of the release gate: it returns "" when the game builds
// for the console, and otherwise the located error.
func (s *Session) consoleBuilds() string {
	bin, errs, err := s.consoleBuild()
	if err != nil {
		detail, fix := s.consoleBuildFailure(errs, err)
		return detail + "; fix: " + fix
	}
	os.Remove(bin)
	return ""
}
