package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/asset/cook"
)

// Session is a project the tool operates on.
type Session struct {
	Root    string // absolute project root (directory of veduta.json)
	Project *asset.Project
	Env     *Env
}

// OpenSession finds the project containing dir (the working directory when empty) by
// walking up to the nearest veduta.json, and parses the manifest.
func OpenSession(dir string, env *Env) (*Session, error) {
	if dir == "" {
		var err error
		if dir, err = os.Getwd(); err != nil {
			return nil, err
		}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	root := abs
	for {
		if _, err := os.Stat(filepath.Join(root, asset.ProjectFile)); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			return nil, fmt.Errorf("no %s in %s or any parent directory (run veduta init to create a project)", asset.ProjectFile, abs)
		}
		root = parent
	}
	p, err := cook.ReadProject(root)
	if err != nil {
		return nil, err
	}
	return &Session{Root: root, Project: p, Env: env}, nil
}

// Out returns a path under the project's out/ directory.
func (s *Session) Out(parts ...string) string {
	return filepath.Join(append([]string{s.Root, "out"}, parts...)...)
}

// Rel returns p relative to the project root with forward slashes when it lies inside it.
func (s *Session) Rel(p string) string {
	r, err := filepath.Rel(s.Root, p)
	if err != nil || strings.HasPrefix(r, "..") {
		return filepath.ToSlash(p)
	}
	return filepath.ToSlash(r)
}

// Library loads every asset of the project (fresh cooked files, stale ones compiled in
// memory).
func (s *Session) Library() (*asset.Library, error) {
	return cook.LoadProject(s.Root, s.Project)
}

// IsScript reports whether the project is a script game: Lua the tool runs itself, with
// no Go code to build.
func (s *Session) IsScript() bool { return s.Project != nil && s.Project.Script != "" }

// GameBinary is where build writes the game.
func (s *Session) GameBinary() string {
	name := "game"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(s.Root, "bin", name)
}

// goCmd prepares a go command in the project root. Every build is CGO_ENABLED=0; modules
// are fetched directly from their origin (GitHub for the engine) without the public proxy
// or checksum database, unless the user configured GOPROXY or GONOSUMDB themselves.
func (s *Session) goCmd(args ...string) *exec.Cmd {
	cmd := exec.Command("go", args...)
	cmd.Dir = s.Root
	cmd.Env = goEnv(os.Environ())
	return cmd
}

func goEnv(base []string) []string {
	env := append([]string(nil), base...)
	has := func(k string) bool {
		for _, e := range env {
			if strings.HasPrefix(e, k+"=") {
				return true
			}
		}
		return false
	}
	env = append(env, "CGO_ENABLED=0")
	if !has("GOPROXY") {
		env = append(env, "GOPROXY=direct")
	}
	if !has("GONOSUMDB") && !has("GOSUMDB") {
		env = append(env, "GONOSUMDB=github.com/riftbane/veduta")
	}
	return env
}

// exists reports whether p exists.
func exists(p string) bool {
	_, err := os.Stat(p)
	return !errors.Is(err, fs.ErrNotExist)
}

// absFrom makes p absolute relative to the working directory of the tool.
func absFrom(p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	a, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return a
}

// cookLoadPartial loads the project's assets, keeping those that compile.
var cookLoadPartial = cook.LoadPartial
