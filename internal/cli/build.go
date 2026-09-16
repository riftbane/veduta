package cli

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/asset/cook"
	"github.com/riftbane/veduta/script"
)

// CompileError is a located compiler or vet message.
type CompileError struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Col  int    `json:"col"`
	Msg  string `json:"msg"`
}

// BuildReport is the result of build.
type BuildReport struct {
	OK         bool                 `json:"ok"`
	Binary     string               `json:"binary,omitempty"`
	Errors     []CompileError       `json:"errors"`
	Vet        bool                 `json:"vet"`
	VetErrors  []CompileError       `json:"vet_errors,omitempty"`
	CookErrors []*asset.SourceError `json:"cook_errors,omitempty"`
	Cooked     int                  `json:"cooked"`
	Output     string               `json:"output,omitempty"` // compiler output that is not a located error
	Millis     int64                `json:"ms"`
}

// ExitCode is 1 when the build failed.
func (r *BuildReport) ExitCode() int {
	if r.OK {
		return 0
	}
	return 1
}

// Human prints errors like the compiler does.
func (r *BuildReport) Human() string {
	var b strings.Builder
	for _, e := range r.CookErrors {
		fmt.Fprintln(&b, e.Error())
	}
	for _, e := range r.Errors {
		fmt.Fprintf(&b, "%s:%d:%d: %s\n", e.File, e.Line, e.Col, e.Msg)
	}
	for _, e := range r.VetErrors {
		fmt.Fprintf(&b, "vet: %s:%d:%d: %s\n", e.File, e.Line, e.Col, e.Msg)
	}
	if r.Output != "" {
		b.WriteString(r.Output)
		if !strings.HasSuffix(r.Output, "\n") {
			b.WriteByte('\n')
		}
	}
	if r.OK {
		fmt.Fprintf(&b, "ok: %s (%d ms)\n", r.Binary, r.Millis)
	}
	return b.String()
}

// errLine matches "path/file.go:12:5: message" and "path/file.go:12: message".
var errLine = regexp.MustCompile(`^(?:vet: )?(\S+?\.go):(\d+)(?::(\d+))?: (.*)$`)

// parseCompileErrors splits go build / go vet output into located errors and the rest.
func parseCompileErrors(out string) (errs []CompileError, rest string) {
	var other strings.Builder
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if m := errLine.FindStringSubmatch(line); m != nil {
			ln, _ := strconv.Atoi(m[2])
			col, _ := strconv.Atoi(m[3])
			errs = append(errs, CompileError{File: strings.TrimPrefix(filepath.ToSlash(m[1]), "./"), Line: ln, Col: col, Msg: m[4]})
			continue
		}
		if strings.HasPrefix(line, "# ") || strings.TrimSpace(line) == "" {
			continue
		}
		other.WriteString(line)
		other.WriteByte('\n')
	}
	return errs, other.String()
}

// Build cooks stale assets and compiles the game binary (and runs go vet when vet is
// set). Compile errors are part of the report, not an error.
func (s *Session) Build(vet bool) (*BuildReport, error) {
	start := time.Now()
	r := &BuildReport{Errors: []CompileError{}, Vet: vet}
	cr, err := cook.Run(cook.Options{Root: s.Root, Project: s.Project})
	if err != nil {
		return nil, err
	}
	r.Cooked = cr.Compiled
	r.CookErrors = cr.Errors()
	if s.IsScript() {
		return s.checkScripts(r, start)
	}
	bin := s.GameBinary()
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	cmd := s.goCmd("build", "-o", bin, s.Project.Entry)
	cmd.Stdout, cmd.Stderr = &out, &out
	buildErr := cmd.Run()
	r.Errors, r.Output = parseCompileErrors(out.String())
	if r.Errors == nil {
		r.Errors = []CompileError{}
	}
	if buildErr == nil {
		r.Binary = s.Rel(bin)
		r.Output = ""
	} else if len(r.Errors) == 0 && r.Output == "" {
		r.Output = buildErr.Error()
	}
	if vet && buildErr == nil {
		var vout bytes.Buffer
		vcmd := s.goCmd("vet", "./...")
		vcmd.Stdout, vcmd.Stderr = &vout, &vout
		if err := vcmd.Run(); err != nil {
			r.VetErrors, _ = parseCompileErrors(vout.String())
			if len(r.VetErrors) == 0 {
				r.VetErrors = []CompileError{{Msg: strings.TrimSpace(vout.String())}}
			}
		}
	}
	r.OK = buildErr == nil && len(r.VetErrors) == 0 && len(r.CookErrors) == 0
	r.Millis = time.Since(start).Milliseconds()
	return r, nil
}

// checkScripts is build for a script game: every Lua file of the project must compile.
func (s *Session) checkScripts(r *BuildReport, start time.Time) (*BuildReport, error) {
	g, err := script.Load(s.Root, s.Project, io.Discard)
	if err != nil {
		r.Output = err.Error()
	} else {
		for _, se := range g.SyntaxErrors() {
			r.Errors = append(r.Errors, CompileError{File: se.Chunk, Line: se.Line, Msg: se.Msg})
		}
		if len(r.Errors) == 0 {
			r.Binary = s.Project.Script
		}
	}
	r.OK = r.Binary != "" && len(r.CookErrors) == 0
	r.Millis = time.Since(start).Milliseconds()
	return r, nil
}

// BuildFailed is returned by operations that need the game when it does not compile.
type BuildFailed struct{ Report *BuildReport }

func (e *BuildFailed) Error() string {
	var b strings.Builder
	b.WriteString("the game does not build:")
	for i, ce := range e.Report.Errors {
		if i == 10 {
			fmt.Fprintf(&b, "\n  … %d more", len(e.Report.Errors)-10)
			break
		}
		fmt.Fprintf(&b, "\n  %s:%d:%d: %s", ce.File, ce.Line, ce.Col, ce.Msg)
	}
	for _, se := range e.Report.CookErrors {
		fmt.Fprintf(&b, "\n  %s", se.Error())
	}
	if e.Report.Output != "" {
		fmt.Fprintf(&b, "\n  %s", strings.TrimSpace(e.Report.Output))
	}
	return b.String()
}

// ensureGame builds the game and returns the binary path.
func (s *Session) ensureGame() (string, error) {
	r, err := s.Build(false)
	if err != nil {
		return "", err
	}
	if r.Binary == "" {
		return "", &BuildFailed{r}
	}
	return s.GameBinary(), nil
}

func init() {
	register(command{
		name:    "build",
		usage:   "build [--vet]",
		summary: "cook stale assets and go build the game (CGO_ENABLED=0), or compile the scripts of a script game; compile errors as {file,line,col,msg}",
		project: true,
		run: func(env *Env, s *Session, args []string) (any, error) {
			fs := newFlags("build", env.Stderr)
			vet := fs.Bool("vet", false, "also run go vet ./...")
			if err := parseFlags(fs, args); err != nil {
				return nil, err
			}
			return s.Build(*vet)
		},
	})
}

// newFlags returns a flag set that reports errors (usage is printed by Main).
func newFlags(name string, w io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(w)
	return fs
}

// parseFlags parses args and rejects positional arguments.
func parseFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return UsageError{err}
	}
	if fs.NArg() > 0 {
		return usagef("%s: unexpected argument %q", fs.Name(), fs.Arg(0))
	}
	return nil
}
