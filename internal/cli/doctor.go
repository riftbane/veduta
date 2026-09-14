package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/riftbane/veduta/asset/cook"
)

// Check is one doctor finding.
type Check struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Warning bool   `json:"warning,omitempty"` // passes, but deserves attention
	Detail  string `json:"detail"`
	Fix     string `json:"fix,omitempty"`
	// Sites lists every place a check found, when its Detail names only the first few (the
	// arm64 check's fused lines).
	Sites []string `json:"sites,omitempty"`
}

// DoctorReport is the result of doctor.
type DoctorReport struct {
	OK     bool    `json:"ok"`
	Checks []Check `json:"checks"`
}

// ExitCode is 1 when a check failed.
func (r *DoctorReport) ExitCode() int {
	if r.OK {
		return 0
	}
	return 1
}

// Human prints one line per check.
func (r *DoctorReport) Human() string {
	var b strings.Builder
	for _, c := range r.Checks {
		mark := "ok  "
		switch {
		case !c.OK:
			mark = "FAIL"
		case c.Warning:
			mark = "warn"
		}
		fmt.Fprintf(&b, "%s %-8s %s\n", mark, c.Name, c.Detail)
		if (!c.OK || c.Warning) && c.Fix != "" {
			fmt.Fprintf(&b, "     fix: %s\n", c.Fix)
		}
	}
	return b.String()
}

var goVersionRe = regexp.MustCompile(`go(\d+)\.(\d+)`)

// Doctor checks the toolchain, the project and versions (spec §10, §13.3).
func Doctor(env *Env, projectDir string) *DoctorReport {
	r := &DoctorReport{OK: true}
	add := func(c Check) {
		r.Checks = append(r.Checks, c)
		if !c.OK {
			r.OK = false
		}
	}
	add(Check{Name: "veduta", OK: true, Detail: strings.TrimSpace(versionInfo(env).Human()[len("veduta "):])})
	if out, err := exec.Command("go", "env", "GOVERSION").Output(); err != nil {
		add(Check{Name: "go", OK: false, Detail: "go not found on PATH", Fix: "install Go 1.25 or newer (install.sh --with-go does it)"})
	} else {
		v := strings.TrimSpace(string(out))
		m := goVersionRe.FindStringSubmatch(v)
		ok := false
		if m != nil {
			maj, _ := strconv.Atoi(m[1])
			min, _ := strconv.Atoi(m[2])
			ok = maj > 1 || maj == 1 && min >= 25
		}
		c := Check{Name: "go", OK: ok, Detail: v}
		if !ok {
			c.Fix = "Veduta needs Go 1.25 or newer"
		}
		add(c)
	}
	if out, err := exec.Command("git", "--version").Output(); err != nil {
		add(Check{Name: "git", OK: false, Detail: "git not found on PATH", Fix: "install git (apt install git)"})
	} else {
		add(Check{Name: "git", OK: true, Detail: strings.TrimSpace(string(out))})
	}
	s, err := OpenSession(projectDir, env)
	if err != nil {
		add(Check{Name: "project", OK: true, Detail: "not in a project (" + err.Error() + ")"})
	} else {
		add(Check{Name: "project", OK: true, Detail: fmt.Sprintf("%s at %s", s.Project.Name, s.Root)})
		add(s.engineCheck(env))
		if cr, err := cook.Run(cook.Options{Root: s.Root, Project: s.Project, DryRun: true}); err != nil {
			add(Check{Name: "assets", OK: false, Detail: err.Error()})
		} else {
			c := Check{Name: "assets", OK: cr.Failed == 0, Detail: fmt.Sprintf("%d fresh, %d stale, %d failing", cr.Fresh, cr.Stale, cr.Failed)}
			if cr.Failed > 0 {
				c.Fix = "run veduta cook to see the located errors"
			}
			add(c)
		}
		for _, c := range s.consoleChecks() {
			add(c)
		}
	}
	add(updateCheck(env))
	return r
}

// engineCheck compares the engine versions of go.mod and veduta.json with the tool's.
func (s *Session) engineCheck(env *Env) Check {
	c := Check{Name: "engine", OK: true}
	mod, err := os.ReadFile(filepath.Join(s.Root, "go.mod"))
	required := ""
	if err == nil {
		if m := regexp.MustCompile(`github\.com/riftbane/veduta\s+(v\S+)`).FindSubmatch(mod); m != nil {
			required = string(m[1])
		}
		if bytes.Contains(mod, []byte("replace github.com/riftbane/veduta")) {
			required += " (replaced by a local checkout)"
		}
	}
	c.Detail = fmt.Sprintf("go.mod requires %s, veduta.json says %s, tool is %s", orNone(required), s.Project.Engine, env.Version)
	if d := minorDiff(s.Project.Engine, env.Version); d != "" {
		c.OK = false
		c.Detail += "; " + d
		c.Fix = "run veduta upgrade to move the project to the tool's version, or veduta update to change the tool"
	}
	return c
}

func orNone(s string) string {
	if s == "" {
		return "nothing"
	}
	return s
}

// minorDiff explains a difference of a minor version or more between two versions ("" if
// they agree or one is not a release version).
func minorDiff(a, b string) string {
	pa, pb := parseSemver(a), parseSemver(b)
	if pa == nil || pb == nil {
		return ""
	}
	if pa[0] != pb[0] || pa[1] != pb[1] {
		return fmt.Sprintf("project and tool differ by a minor version or more (%s vs %s)", a, b)
	}
	return ""
}

func parseSemver(v string) []int {
	m := regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)`).FindStringSubmatch(v)
	if m == nil {
		return nil
	}
	out := make([]int, 3)
	for i := range out {
		out[i], _ = strconv.Atoi(m[i+1])
	}
	return out
}

// updateCheck reports update availability (filled in by the update package).
var updateCheck = func(env *Env) Check {
	return Check{Name: "update", OK: true, Detail: "not checked"}
}

func init() {
	register(command{
		name:    "doctor",
		usage:   "doctor",
		summary: "check Go, git, the project manifest, engine/tool versions, assets and updates",
		run: func(env *Env, _ *Session, args []string) (any, error) {
			if len(args) > 0 {
				return nil, usagef("doctor takes no arguments")
			}
			return Doctor(env, mcpProjectDir), nil
		},
	})
}
