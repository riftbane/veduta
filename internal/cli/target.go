package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
// path, which the caller removes. Compile errors come back located, as from build.
func (s *Session) consoleBuild() (string, []CompileError, error) {
	f, err := os.CreateTemp("", "veduta-console-*")
	if err != nil {
		return "", nil, err
	}
	bin := f.Name()
	f.Close()
	cmd := s.goCmd("build", "-o", bin, s.Project.Entry)
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

// fusedSites lists the lines of the project's own code that compiled to fused
// multiply-adds in the console binary at bin, as root-relative "file:line".
func (s *Session) fusedSites(bin string) ([]string, error) {
	root := filepath.ToSlash(s.Root) + "/"
	sites, err := fused.Scan(bin, func(st fused.Site) bool { return strings.HasPrefix(filepath.ToSlash(st.File), root) })
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(sites))
	for _, st := range sites {
		out = append(out, fmt.Sprintf("%s:%d", strings.TrimPrefix(filepath.ToSlash(st.File), root), st.Line))
	}
	return out, nil
}

// releaseTargetsConsole reports whether the project's release workflow builds for the
// console.
func (s *Session) releaseTargetsConsole() (bool, string) {
	p := filepath.Join(s.Root, ".github", "workflows", "release.yml")
	data, err := os.ReadFile(p)
	if err != nil {
		return false, "no .github/workflows/release.yml"
	}
	if strings.Contains(string(data), targetOS+"/"+targetArch) || strings.Contains(string(data), "GOARCH="+targetArch) {
		return true, ".github/workflows/release.yml builds " + targetOS + "/" + targetArch
	}
	return false, ".github/workflows/release.yml builds no " + targetOS + "/" + targetArch + " archive"
}

// cardProblem compares card.json, the description the console lists, with the manifest.
// It returns "" when they agree.
func (s *Session) cardProblem() string {
	data, err := os.ReadFile(filepath.Join(s.Root, "card.json"))
	if os.IsNotExist(err) {
		return "no card.json: the console lists a game by it (veduta init writes one)"
	}
	if err != nil {
		return err.Error()
	}
	var c map[string]any
	if err := json.Unmarshal(data, &c); err != nil {
		return "card.json is not a JSON object: " + err.Error()
	}
	var diffs []string
	if v, _ := c["veduta"].(string); v != "card/1" {
		diffs = append(diffs, fmt.Sprintf(`"veduta" is %q, want "card/1"`, v))
	}
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
			shown := sites
			if len(shown) > 10 {
				shown = append(shown[:10:10], fmt.Sprintf("and %d more", len(sites)-10))
			}
			cs = append(cs, Check{Name: "arm64", OK: true, Warning: true,
				Detail: fmt.Sprintf("builds, but %d line(s) of the game compile to fused multiply-adds on arm64, so the console computes different bits than this machine and traces, goldens and scenarios can drift: %s", len(sites), strings.Join(shown, ", ")),
				Fix:    "wrap each product that feeds + or - in a conversion to its own type, float32(a*b) + c; gmath's Vec.Scale, Mul, Dot and matrix products already do (docs: api, Determinism rules)"})
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

// consoleReady is the release gate: a release nobody can install on the console is not a
// release. It returns "" when the game builds for the console and the workflow publishes
// that build.
func (s *Session) consoleReady() string {
	bin, errs, err := s.consoleBuild()
	if err != nil {
		if len(errs) > 0 {
			return fmt.Sprintf("%v: %s:%d: %s", err, errs[0].File, errs[0].Line, errs[0].Msg)
		}
		return err.Error()
	}
	os.Remove(bin)
	if ok, detail := s.releaseTargetsConsole(); !ok {
		return detail
	}
	return ""
}

// errorOrNil turns a non-empty explanation into an error.
func errorOrNil(why string) error {
	if why == "" {
		return nil
	}
	return errors.New(why)
}
