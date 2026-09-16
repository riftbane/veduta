package cli

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/riftbane/veduta/asset"
	projtemplate "github.com/riftbane/veduta/template"
)

// InitOptions configures init.
type InitOptions struct {
	Dir       string // target directory (created; must be empty if it exists)
	Name      string // game name (default: base of Dir)
	Module    string // Go module path (default: Name)
	Engine    string // engine version to require (default: the tool's version, or the template's for dev builds)
	EngineDir string // local engine checkout: adds a replace directive (development and CI)
	NoTidy    bool   // skip `go mod tidy`

	// game is the tree the game, its assets and scenarios come from, laid out as a project,
	// and gamePackage the import path its Go files use for the game package; the project
	// files always come from the template. Unset, both are the template's. The engine's
	// tests set them to create projects from their test game.
	game        fs.FS
	gamePackage string
}

// InitReport describes the created project.
type InitReport struct {
	OK       bool     `json:"ok"`
	Dir      string   `json:"dir"`
	Name     string   `json:"name"`
	Module   string   `json:"module"`
	Engine   string   `json:"engine"`
	Files    int      `json:"files"`
	Warnings []string `json:"warnings"`
	Next     []string `json:"next"`
}

// Human prints the next steps.
func (r *InitReport) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "created %s (%d files, module %s, engine %s)\n", r.Dir, r.Files, r.Module, r.Engine)
	for _, w := range r.Warnings {
		fmt.Fprintln(&b, "warning:", w)
	}
	fmt.Fprintln(&b, "next:")
	for _, n := range r.Next {
		fmt.Fprintln(&b, "  "+n)
	}
	return b.String()
}

var semver = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

// templateEngine is the engine version the embedded template's veduta.json names.
func templateEngine() string {
	data, err := projtemplate.FS.ReadFile(asset.ProjectFile)
	if err == nil {
		if m := regexp.MustCompile(`"engine":\s*"([^"]+)"`).FindSubmatch(data); m != nil {
			return string(m[1])
		}
	}
	return "v1.0.0"
}

// projectFiles maps templated sources to their destination.
var projectFiles = map[string]string{
	"project/go.mod.tmpl":       "go.mod",
	"project/CLAUDE.md.tmpl":    "CLAUDE.md",
	"project/README.md.tmpl":    "README.md",
	"project/CHANGELOG.md.tmpl": "CHANGELOG.md",
	"project/gitignore.tmpl":    ".gitignore",
	"project/mcp.json.tmpl":     ".mcp.json",
	"project/release.yml.tmpl":  ".github/workflows/release.yml",
	"project/card.json.tmpl":    "card.json",
}

// Init creates a game project from the embedded template (spec §12).
func Init(env *Env, o InitOptions) (*InitReport, error) {
	if o.Dir == "" && o.Name == "" {
		return nil, usagef("init: give a directory or --name")
	}
	if o.Dir == "" {
		o.Dir = o.Name
	}
	if o.Name == "" {
		o.Name = strings.ToLower(filepath.Base(filepath.Clean(o.Dir)))
	}
	if err := asset.ValidName(o.Name); err != nil {
		return nil, usagef("init: %v (use --name)", err)
	}
	if o.Module == "" {
		o.Module = o.Name
	}
	if strings.ContainsAny(o.Module, " \t\"`") {
		return nil, usagef("init: invalid module path %q", o.Module)
	}
	if o.Engine == "" {
		o.Engine = env.Version
		if !semver.MatchString(o.Engine) {
			o.Engine = templateEngine()
		}
	}
	if o.EngineDir != "" {
		abs, err := filepath.Abs(o.EngineDir)
		if err != nil {
			return nil, err
		}
		if !exists(filepath.Join(abs, "go.mod")) {
			return nil, fmt.Errorf("init: --engine-dir %s has no go.mod", abs)
		}
		o.EngineDir = filepath.ToSlash(abs)
	}
	dir, err := filepath.Abs(o.Dir)
	if err != nil {
		return nil, err
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return nil, fmt.Errorf("init: %s exists and is not empty", dir)
	}
	r := &InitReport{Dir: dir, Name: o.Name, Module: o.Module, Engine: o.Engine, Warnings: []string{}}
	data := map[string]string{"Name": o.Name, "Module": o.Module, "Engine": o.Engine, "EngineDir": o.EngineDir}
	if o.game == nil {
		o.game, o.gamePackage = projtemplate.FS, projtemplate.GamePackage
	}
	write := func(tree fs.FS, p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || p == "embed.go" {
			return err
		}
		src, err := fs.ReadFile(tree, p)
		if err != nil {
			return err
		}
		dst := p
		switch {
		case projectFiles[p] != "":
			dst = projectFiles[p]
			t, err := template.New(p).Delims("[[", "]]").Option("missingkey=error").Parse(string(src))
			if err != nil {
				return fmt.Errorf("template %s: %w", p, err)
			}
			var buf bytes.Buffer
			if err := t.Execute(&buf, data); err != nil {
				return fmt.Errorf("template %s: %w", p, err)
			}
			src = buf.Bytes()
		case strings.HasPrefix(p, "project/"):
			return nil
		case strings.HasSuffix(p, ".go"):
			src = bytes.ReplaceAll(src, []byte(`"`+o.gamePackage+`"`), []byte(`"`+path.Join(o.Module, "game")+`"`))
		case p == asset.ProjectFile:
			src = regexp.MustCompile(`"name":\s*"[^"]*"`).ReplaceAll(src, []byte(fmt.Sprintf(`"name": %q`, o.Name)))
			src = regexp.MustCompile(`"engine":\s*"[^"]*"`).ReplaceAll(src, []byte(fmt.Sprintf(`"engine": %q`, o.Engine)))
		}
		out := filepath.Join(dir, filepath.FromSlash(dst))
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		r.Files++
		return os.WriteFile(out, src, 0o644)
	}
	err = fs.WalkDir(projtemplate.FS, "project", func(p string, d fs.DirEntry, err error) error {
		return write(projtemplate.FS, p, d, err)
	})
	if err == nil {
		err = fs.WalkDir(o.game, ".", func(p string, d fs.DirEntry, err error) error {
			if p == "project" && d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return write(o.game, p, d, err)
		})
	}
	if err != nil {
		return nil, fmt.Errorf("init: %w", err)
	}
	if !o.NoTidy {
		cmd := exec.Command("go", "mod", "tidy")
		cmd.Dir = dir
		cmd.Env = goEnv(os.Environ())
		if out, err := cmd.CombinedOutput(); err != nil {
			r.Warnings = append(r.Warnings, fmt.Sprintf("go mod tidy failed (%v): %s — run it again when the engine version %s is reachable", err, strings.TrimSpace(string(out)), o.Engine))
		}
	}
	if _, err := exec.LookPath("git"); err == nil && !exists(filepath.Join(dir, ".git")) {
		cmd := exec.Command("git", "init", "-q")
		cmd.Dir = dir
		if err := cmd.Run(); err != nil {
			r.Warnings = append(r.Warnings, "git init failed: "+err.Error())
		}
	}
	rel := o.Dir
	r.Next = []string{"cd " + rel, "veduta test", "claude"}
	r.OK = true
	return r, nil
}

func init() {
	register(command{
		name:    "init",
		usage:   "init [dir] --name N [--module M] [--engine vX.Y.Z] [--engine-dir PATH]",
		summary: "create a game project from the embedded template (empty game and scene, sprite assets, one scenario, Claude Code setup)",
		run: func(env *Env, _ *Session, args []string) (any, error) {
			fs := newFlags("init", env.Stderr)
			var o InitOptions
			fs.StringVar(&o.Name, "name", "", "game name")
			fs.StringVar(&o.Module, "module", "", "Go module path (default: the name)")
			fs.StringVar(&o.Engine, "engine", "", "engine version to require (default: this tool's)")
			fs.StringVar(&o.EngineDir, "engine-dir", "", "use a local engine checkout (replace directive)")
			fs.BoolVar(&o.NoTidy, "no-tidy", false, "do not run go mod tidy")
			var dirs []string
			for {
				if err := fs.Parse(args); err != nil {
					return nil, UsageError{err}
				}
				if fs.NArg() == 0 {
					break
				}
				dirs = append(dirs, fs.Arg(0))
				args = fs.Args()[1:]
			}
			if len(dirs) > 1 {
				return nil, usagef("init: one directory at most, got %v", dirs)
			}
			if len(dirs) == 1 {
				o.Dir = dirs[0]
			}
			return Init(env, o)
		},
	})
}
