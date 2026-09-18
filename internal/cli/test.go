package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/riftbane/veduta/v2/asset/cook"
	"github.com/riftbane/veduta/v2/gfx"
)

// ScenarioResult is one scenario of a test run.
type ScenarioResult struct {
	Name         string `json:"name"`
	Verdict      string `json:"verdict"`
	FirstFailure string `json:"first_failure,omitempty"`
	TraceHash    string `json:"trace_hash,omitempty"`
	Golden       string `json:"golden"` // match, mismatch, new, updated
	RunID        string `json:"run_id,omitempty"`
	Sheet        string `json:"sheet,omitempty"`
	Error        string `json:"error,omitempty"`
}

// TestReport is the result of test.
type TestReport struct {
	OK        bool             `json:"ok"`
	GoTest    GoTestResult     `json:"go_test"`
	Scenarios []ScenarioResult `json:"scenarios"`
	Failed    []string         `json:"failed"`
	Millis    int64            `json:"ms"`
}

// GoTestResult summarizes `go test ./...`.
type GoTestResult struct {
	OK       bool     `json:"ok"`
	Packages int      `json:"packages"`
	Failed   []string `json:"failed"`
	Output   string   `json:"output,omitempty"` // tail of the output when something failed
}

// ExitCode is 1 when anything failed.
func (r *TestReport) ExitCode() int {
	if r.OK {
		return 0
	}
	return 1
}

// Human prints one line per scenario and the failures.
func (r *TestReport) Human() string {
	var b strings.Builder
	status := "ok"
	if !r.GoTest.OK {
		status = "FAIL " + strings.Join(r.GoTest.Failed, ", ")
	}
	fmt.Fprintf(&b, "go test: %s (%d packages)\n", status, r.GoTest.Packages)
	if !r.GoTest.OK && r.GoTest.Output != "" {
		b.WriteString(r.GoTest.Output)
	}
	for _, sc := range r.Scenarios {
		fmt.Fprintf(&b, "scenario %-20s %-5s golden %-8s %s\n", sc.Name, sc.Verdict, sc.Golden, sc.FirstFailure+sc.Error)
	}
	if r.OK {
		fmt.Fprintf(&b, "PASS (%d ms)\n", r.Millis)
	} else {
		fmt.Fprintf(&b, "FAIL: %s\n", strings.Join(r.Failed, ", "))
	}
	return b.String()
}

// Test runs go test ./..., every scenario in tests/scenarios and their golden files in
// tests/golden (<scenario>.hash and <scenario>.png). With update, golden files are
// rewritten (and VEDUTA_UPDATE_GOLDEN=1 is passed to go test).
func (s *Session) Test(update bool) (*TestReport, error) {
	if err := cook.CheckNames(s.Root, s.Project); err != nil {
		return nil, fmt.Errorf("test: %w", err)
	}
	start := time.Now()
	r := &TestReport{Scenarios: []ScenarioResult{}, Failed: []string{}}
	r.GoTest = s.goTest(update)
	if !r.GoTest.OK {
		r.Failed = append(r.Failed, "go test")
	}
	files, err := filepath.Glob(filepath.Join(s.Root, "tests", "scenarios", "*.vscenario"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	for _, f := range files {
		sc := s.testScenario(f, update)
		if sc.Verdict != "pass" || sc.Golden == "mismatch" || sc.Error != "" {
			r.Failed = append(r.Failed, "scenario "+sc.Name)
		}
		r.Scenarios = append(r.Scenarios, sc)
	}
	r.OK = len(r.Failed) == 0
	r.Millis = time.Since(start).Milliseconds()
	return r, nil
}

func (s *Session) goTest(update bool) GoTestResult {
	if s.IsScript() && !exists(filepath.Join(s.Root, "go.mod")) {
		return GoTestResult{OK: true, Failed: []string{}} // a script game has no Go code to test
	}
	var out bytes.Buffer
	cmd := s.goCmd("test", "./...")
	if update {
		cmd.Env = append(cmd.Env, "VEDUTA_UPDATE_GOLDEN=1")
	}
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	res := GoTestResult{OK: err == nil, Failed: []string{}}
	for _, line := range strings.Split(out.String(), "\n") {
		f := strings.Fields(line)
		switch {
		case len(f) >= 2 && (f[0] == "ok" || f[0] == "?"):
			res.Packages++
		case len(f) >= 2 && f[0] == "FAIL" && strings.Contains(f[1], "/") || len(f) >= 2 && f[0] == "FAIL" && !strings.HasPrefix(f[1], "("):
			if f[1] != "" {
				res.Packages++
				res.Failed = append(res.Failed, f[1])
			}
		}
	}
	if err != nil {
		res.Output = tail(out.String(), 4000)
	}
	return res
}

func (s *Session) testScenario(file string, update bool) ScenarioResult {
	name := strings.TrimSuffix(filepath.Base(file), ".vscenario")
	sc := ScenarioResult{Name: name, Golden: "-"}
	rep, err := s.Simulate(SimulateOptions{Scenario: file})
	if err != nil {
		sc.Verdict = "error"
		sc.Error = err.Error()
		return sc
	}
	sc.Verdict = fmt.Sprint(rep["verdict"])
	sc.FirstFailure, _ = rep["first_failure"].(string)
	sc.TraceHash, _ = rep["trace_hash"].(string)
	sc.RunID, _ = rep["run_id"].(string)
	sc.Sheet, _ = rep["sheet"].(string)
	if sc.Verdict != "pass" {
		return sc
	}
	gdir := filepath.Join(s.Root, "tests", "golden")
	hashFile := filepath.Join(gdir, name+".hash")
	pngFile := filepath.Join(gdir, name+".png")
	sheet := filepath.Join(s.Root, filepath.FromSlash(sc.Sheet))
	if update {
		if err := os.MkdirAll(gdir, 0o755); err != nil {
			sc.Error = err.Error()
			return sc
		}
		data, err := os.ReadFile(sheet)
		if err == nil {
			err = os.WriteFile(pngFile, data, 0o644)
		}
		if err == nil {
			err = os.WriteFile(hashFile, []byte(sc.TraceHash+"\n"), 0o644)
		}
		if err != nil {
			sc.Error = err.Error()
			return sc
		}
		sc.Golden = "updated"
		return sc
	}
	want, err := os.ReadFile(hashFile)
	if err != nil {
		sc.Golden = "new" // no golden yet: run veduta test --update-golden to record one
		return sc
	}
	sc.Golden = "match"
	if strings.TrimSpace(string(want)) != sc.TraceHash {
		sc.Golden = "mismatch"
		sc.FirstFailure = fmt.Sprintf("trace hash %s differs from tests/golden/%s.hash (%s); the simulation changed — look at the trace, then run veduta test --update-golden if intended", sc.TraceHash, name, strings.TrimSpace(string(want)))
		return sc
	}
	if same, err := samePixels(pngFile, sheet); err == nil && !same {
		sc.Golden = "mismatch"
		sc.FirstFailure = fmt.Sprintf("contact sheet differs from tests/golden/%s.png (use veduta diff to see where)", name)
	}
	return sc
}

// samePixels compares two PNGs by decoded pixels (a missing golden image counts as same).
func samePixels(a, b string) (bool, error) {
	da, err := os.ReadFile(a)
	if err != nil {
		return true, nil
	}
	db, err := os.ReadFile(b)
	if err != nil {
		return false, err
	}
	ia, err := gfx.DecodePNG(bytes.NewReader(da))
	if err != nil {
		return false, err
	}
	ib, err := gfx.DecodePNG(bytes.NewReader(db))
	if err != nil {
		return false, err
	}
	if ia.W != ib.W || ia.H != ib.H {
		return false, nil
	}
	for i := range ia.Pix {
		if ia.Pix[i] != ib.Pix[i] {
			return false, nil
		}
	}
	return true, nil
}

func init() {
	register(command{
		name:    "test",
		usage:   "test [--update-golden]",
		summary: "go test ./... + every scenario in tests/scenarios + golden hashes and sheets in tests/golden",
		project: true,
		run: func(env *Env, s *Session, args []string) (any, error) {
			fs := newFlags("test", env.Stderr)
			update := fs.Bool("update-golden", false, "rewrite golden files")
			if err := parseFlags(fs, args); err != nil {
				return nil, err
			}
			return s.Test(*update)
		},
	})
}
