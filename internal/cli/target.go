package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
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

// runRefusal explains why `veduta run` cannot show the game on this machine, or returns ""
// when it can.
func runRefusal() string {
	if runtime.GOOS != "linux" {
		return fmt.Sprintf("run: the player draws on a Linux framebuffer and this is %s/%s; build, render, simulate and test here, and play the game on the console (its release builds %s/%s)",
			runtime.GOOS, runtime.GOARCH, targetOS, targetArch)
	}
	if findPanel() == "" {
		return "run: no framebuffer on this machine (none with 16 or 32 bits per pixel in /sys/class/graphics); the player draws on the console's panel or a Linux text console. " +
			"Set VEDUTA_FB to name a framebuffer and VEDUTA_SCALE to divide it; use render and simulate to see the game here"
	}
	return ""
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
	seen := false
	for _, f := range files {
		if fused.InModule(f, mod) {
			seen = true
			break
		}
	}
	if !seen {
		return nil, fmt.Errorf("no file of module %s in the console build, so its lines cannot be told apart", mod)
	}
	ours := func(st fused.Site) bool { return st.Package == "main" || fused.InModule(st.Package, mod) }
	sites, err := fused.Scan(bin, func(st fused.Site) bool { return fused.InModule(st.File, mod) || ours(st) })
	if err != nil {
		return nil, err
	}
	var lines, inlined []string
	listed := map[string]bool{}
	for _, st := range sites {
		var p string
		if fused.InModule(st.File, mod) {
			p = fmt.Sprintf("%s:%d", s.Rel(filepath.Join(modDir, filepath.FromSlash(strings.TrimPrefix(st.File, mod+"/")))), st.Line)
		} else {
			p = fmt.Sprintf("%s:%d inlined in %s", engineFile(st.File), st.Line, st.Func)
		}
		if listed[p] {
			continue
		}
		listed[p] = true
		if fused.InModule(st.File, mod) {
			lines = append(lines, p)
		} else {
			inlined = append(inlined, p)
		}
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
	entries, _ := os.ReadDir(filepath.Join(s.Root, filepath.FromSlash(dir)))
	found := false
	for _, e := range entries { // ReadDir sorts by name
		name := e.Name()
		if e.IsDir() || !(strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")) {
			continue
		}
		found = true
		data, err := os.ReadFile(filepath.Join(s.Root, filepath.FromSlash(dir), name))
		if err == nil && buildsConsole(string(data)) {
			return true, dir + "/" + name + " builds " + targetOS + "/" + targetArch
		}
	}
	if !found {
		return false, "no workflow in " + dir + ", so no " + targetOS + "/" + targetArch + " archive"
	}
	return false, dir + " has no " + targetOS + "/" + targetArch + " build"
}

var (
	consoleTargetRe = regexp.MustCompile(`\b` + targetOS + `/` + targetArch + `\b`)
	targetOSRe      = regexp.MustCompile(`\b` + targetOS + `\b`)
	targetArchRe    = regexp.MustCompile(`\b` + targetArch + `\b`)
)

// buildsConsole reports whether the text of a GitHub Actions workflow, its comments left
// out, builds for the console: it names linux/arm64 (a "for target in linux/arm64 ..."
// list), or has linux and arm64 as words of their own (GOOS=linux with GOARCH=arm64, or a
// matrix such as goarch: [arm64, amd64]). It reads text, not YAML, so a workflow can
// still mislead it, but a comment alone no longer counts.
func buildsConsole(workflow string) bool {
	var code strings.Builder
	for _, line := range strings.Split(workflow, "\n") {
		code.WriteString(stripYAMLComment(line))
		code.WriteByte('\n')
	}
	text := code.String()
	return consoleTargetRe.MatchString(text) || targetOSRe.MatchString(text) && targetArchRe.MatchString(text)
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

// cardProblem compares card.json with the manifest. It returns "" when the card is valid
// and agrees with veduta.json; a card that is not valid is marked as refused by release.
func (s *Session) cardProblem() string {
	c, why := s.readCard()
	if why != "" {
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

	bin, errs, err := s.consoleBuild()
	if err != nil {
		detail := err.Error()
		if len(errs) > 0 {
			e := errs[0]
			detail = fmt.Sprintf("%s: %s:%d:%d: %s", detail, e.File, e.Line, e.Col, e.Msg)
		}
		cs = append(cs, Check{Name: "arm64", OK: false, Detail: detail, Fix: "the console runs " + targetOS + "/" + targetArch + ": keep the game pure Go with CGO_ENABLED=0"})
	} else {
		sites, serr := s.fusedSites(bin)
		os.Remove(bin)
		switch {
		case serr != nil:
			cs = append(cs, Check{Name: "arm64", OK: true, Warning: true, Detail: "builds for " + targetOS + "/" + targetArch + ", but its code could not be checked: " + serr.Error()})
		case len(sites) > 0:
			cs = append(cs, fusedCheck(sites))
		default:
			cs = append(cs, Check{Name: "arm64", OK: true, Detail: "builds for " + targetOS + "/" + targetArch + " with no fused multiply-add in the game's code"})
		}
	}

	if ok, detail := s.releaseTargetsConsole(); ok {
		cs = append(cs, Check{Name: "release", OK: true, Detail: detail})
	} else {
		cs = append(cs, Check{Name: "release", OK: true, Warning: true, Detail: detail + ": no release of this project can be installed on the console",
			Fix: "build linux/arm64 in the release workflow (compare with the one veduta init writes)"})
	}
	if p := s.cardProblem(); p != "" {
		cs = append(cs, Check{Name: "card", OK: true, Warning: true, Detail: p,
			Fix: `write card.json as {"veduta": "card/1", "title": <title>, "name": <name>, "exec": <name>}`})
	} else {
		cs = append(cs, Check{Name: "card", OK: true, Detail: "card.json agrees with veduta.json"})
	}
	return cs
}

// consoleReleasable is the first half of the release gate, a release nobody can install on
// the console is not a release: it returns "" when a workflow publishes a linux/arm64
// archive and card.json is one the console can read, and otherwise why not. It only reads
// files, so release runs it before the tests.
func (s *Session) consoleReleasable() string {
	if ok, detail := s.releaseTargetsConsole(); !ok {
		return detail
	}
	if _, why := s.readCard(); why != "" {
		return why
	}
	return ""
}

// consoleBuilds is the second half of the release gate: it returns "" when the game builds
// for the console, and otherwise the located error.
func (s *Session) consoleBuilds() string {
	bin, errs, err := s.consoleBuild()
	if err != nil {
		if len(errs) > 0 {
			return fmt.Sprintf("%v: %s:%d: %s", err, errs[0].File, errs[0].Line, errs[0].Msg)
		}
		return err.Error()
	}
	os.Remove(bin)
	return ""
}
