// Package cli implements the veduta tool: every command of spec §10 as a Session method
// returning a report value, plus the command-line front end that prints reports as text
// or JSON. The MCP server wraps the same methods.
//
// The tool never contains game logic: operations that need the game (render, simulate)
// build the game binary and run it with -headless.
package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/riftbane/veduta/asset"
)

// Env is the environment of one tool invocation.
type Env struct {
	Version string // tool version, "dev" for local builds
	Commit  string
	Date    string
	Stdout  io.Writer
	Stderr  io.Writer
	Stdin   io.Reader
}

// command is one veduta subcommand.
type command struct {
	name    string
	usage   string
	summary string
	project bool // needs a project (veduta.json)
	run     func(env *Env, s *Session, args []string) (any, error)
}

var commands []command

func register(c command) { commands = append(commands, c) }

// UsageError marks a bad command line (exit code 2).
type UsageError struct{ Err error }

func (e UsageError) Error() string { return e.Err.Error() }
func (e UsageError) Unwrap() error { return e.Err }

func usagef(format string, args ...any) error { return UsageError{fmt.Errorf(format, args...)} }

// exitCoder lets a report choose the exit code (for example a failing test run).
type exitCoder interface{ ExitCode() int }

// humaner renders a report as short text for people; reports without it print JSON.
type humaner interface{ Human() string }

// Main runs the tool with args (without the program name) and returns the exit code:
// 0 success, 1 failure (with a report or an error), 2 bad usage, 3 simulate verdict fail.
func Main(args []string, env Env) int {
	if env.Stdout == nil {
		env.Stdout = os.Stdout
	}
	if env.Stderr == nil {
		env.Stderr = os.Stderr
	}
	if env.Stdin == nil {
		env.Stdin = os.Stdin
	}
	jsonOut := false
	projectDir := ""
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--json" || a == "-json":
			jsonOut = true
		case (a == "--project" || a == "-project") && i+1 < len(args) && len(rest) == 0:
			projectDir = args[i+1]
			i++
		case strings.HasPrefix(a, "--project=") && len(rest) == 0:
			projectDir = strings.TrimPrefix(a, "--project=")
		default:
			rest = append(rest, a)
		}
	}
	if len(rest) == 0 || rest[0] == "help" || rest[0] == "-h" || rest[0] == "--help" {
		printUsage(env.Stdout, rest)
		if len(rest) == 0 {
			return 2
		}
		return 0
	}
	cmd := findCommand(rest[0])
	if cmd == nil {
		fmt.Fprintf(env.Stderr, "veduta: unknown command %q (run veduta help)\n", rest[0])
		return 2
	}
	mcpProjectDir = projectDir
	var s *Session
	if cmd.project {
		var err error
		if s, err = OpenSession(projectDir, &env); err != nil {
			return report(&env, jsonOut, nil, err)
		}
	}
	res, err := cmd.run(&env, s, rest[1:])
	return report(&env, jsonOut, res, err)
}

func findCommand(name string) *command {
	for i := range commands {
		if commands[i].name == name {
			return &commands[i]
		}
	}
	return nil
}

func printUsage(w io.Writer, rest []string) {
	if len(rest) > 1 {
		if c := findCommand(rest[1]); c != nil {
			fmt.Fprintf(w, "usage: veduta %s\n\n%s\n", c.usage, c.summary)
			return
		}
	}
	fmt.Fprintln(w, "veduta — headless, deterministic game engine toolchain")
	fmt.Fprintln(w, "\nusage: veduta [--json] [--project DIR] <command> [flags]")
	fmt.Fprintln(w, "\ncommands:")
	list := append([]command(nil), commands...)
	sort.Slice(list, func(i, j int) bool { return list[i].name < list[j].name })
	for _, c := range list {
		fmt.Fprintf(w, "  %-9s %s\n", c.name, c.summary)
	}
	fmt.Fprintln(w, "\nrun 'veduta help <command>' for the flags of one command")
}

// report prints the result or the error and returns the exit code.
func report(env *Env, jsonOut bool, res any, err error) int {
	if err != nil {
		var ue UsageError
		code := 1
		if errors.As(err, &ue) {
			code = 2
		}
		if jsonOut {
			out := map[string]any{"ok": false, "error": err.Error()}
			if errs := sourceErrors(err); errs != nil {
				out["errors"] = errs
			}
			writeJSON(env.Stdout, out, false)
		} else {
			fmt.Fprintln(env.Stderr, "veduta:", err)
		}
		return code
	}
	if res != nil {
		if jsonOut {
			writeJSON(env.Stdout, res, false)
		} else if h, ok := res.(humaner); ok {
			fmt.Fprint(env.Stdout, h.Human())
		} else {
			writeJSON(env.Stdout, res, true)
		}
	}
	if ec, ok := res.(exitCoder); ok {
		return ec.ExitCode()
	}
	return 0
}

// sourceErrors extracts located errors from err.
func sourceErrors(err error) []*asset.SourceError {
	var list asset.Errors
	var one *asset.SourceError
	switch {
	case errors.As(err, &list):
		return list
	case errors.As(err, &one):
		return []*asset.SourceError{one}
	}
	return nil
}

// writeJSON encodes v without HTML escaping, compact or indented.
func writeJSON(w io.Writer, v any, indent bool) {
	w.Write(marshal(v, indent))
}

func marshal(v any, indent bool) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(v); err != nil {
		buf.Reset()
		enc.Encode(map[string]any{"ok": false, "error": err.Error()})
	}
	return buf.Bytes()
}
