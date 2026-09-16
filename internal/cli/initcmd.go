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

	veduta "github.com/riftbane/veduta/v2"
	"github.com/riftbane/veduta/v2/asset"
	projtemplate "github.com/riftbane/veduta/v2/template"
)

// InitOptions configures init.
type InitOptions struct {
	Dir       string // target directory (created; must be empty if it exists)
	Name      string // game name (default: base of Dir)
	Module    string // Go module path of a Go game (default: Name)
	Engine    string // engine version to require (default: the tool's version, or the engine source's for dev builds)
	EngineDir string // local engine checkout: adds a replace directive to a Go game's go.mod (development and CI)
	NoTidy    bool   // skip `go mod tidy` for a Go game
	Go        bool   // create a Go game instead of a Lua one

	// game is the tree the game, its assets and scenarios come from, laid out as a project,
	// and gamePackage the import path its Go files use for the game package; the project
	// files always come from the template. Unset, they are the template's. The engine's
	// tests set them to create Go projects from their test game.
	game        fs.FS
	gamePackage string
}

// InitReport describes the created project.
type InitReport struct {
	OK       bool     `json:"ok"`
	Dir      string   `json:"dir"`
	Name     string   `json:"name"`
	Language string   `json:"language"`
	Module   string   `json:"module,omitempty"`
	Engine   string   `json:"engine"`
	Files    int      `json:"files"`
	Warnings []string `json:"warnings"`
	Next     []string `json:"next"`
}

// Human prints the next steps.
func (r *InitReport) Human() string {
	var b strings.Builder
	if r.Module != "" {
		fmt.Fprintf(&b, "created %s (%d files, %s, module %s, engine %s)\n", r.Dir, r.Files, r.Language, r.Module, r.Engine)
	} else {
		fmt.Fprintf(&b, "created %s (%d files, %s, engine %s)\n", r.Dir, r.Files, r.Language, r.Engine)
	}
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

// templateManifests are the template's manifests, whose engine field a release moves.
var templateManifests = []string{"lua/" + asset.ProjectFile, "go/" + asset.ProjectFile}

// projectFiles maps templated project files to their destination.
var projectFiles = map[string]string{
	"go.mod.tmpl":       "go.mod",
	"CLAUDE.md.tmpl":    "CLAUDE.md",
	"README.md.tmpl":    "README.md",
	"CHANGELOG.md.tmpl": "CHANGELOG.md",
	"gitignore.tmpl":    ".gitignore",
	"mcp.json.tmpl":     ".mcp.json",
	"release.yml.tmpl":  ".github/workflows/release.yml",
	"card.json.tmpl":    "card.json",
}

// Init creates a game project from the embedded template (spec §12): a Lua game, or a Go
// game with Go set.
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
	if o.game != nil {
		o.Go = true
	}
	lang := "lua"
	if o.Go {
		lang = "go"
		if o.Module == "" {
			o.Module = o.Name
		}
		if strings.ContainsAny(o.Module, " \t\"`") {
			return nil, usagef("init: invalid module path %q", o.Module)
		}
	} else if o.Module != "" || o.EngineDir != "" {
		return nil, usagef("init: --module and --engine-dir are for a Go game (--go)")
	}
	if o.Engine == "" {
		o.Engine = env.Version
		if !semver.MatchString(o.Engine) {
			o.Engine = veduta.Version
		}
	}
	if module := strings.TrimSuffix(projtemplate.GamePackage, "/template/go/game"); o.Go && engineModule(o.Engine) != module {
		return nil, usagef("init: engine %s is the module %s, and the template's Go code imports %s", o.Engine, engineModule(o.Engine), module)
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
	r := &InitReport{Dir: dir, Name: o.Name, Language: lang, Module: o.Module, Engine: o.Engine, Warnings: []string{}}
	data := map[string]string{"Name": o.Name, "Module": o.Module, "Engine": o.Engine, "EngineModule": engineModule(o.Engine), "EngineDir": o.EngineDir}
	gamePackage := projtemplate.GamePackage
	if o.game != nil {
		gamePackage = o.gamePackage
	}
	// write copies one file of tree to dst in the project, templated or rewritten as needed.
	write := func(tree fs.FS, p, dst string) error {
		src, err := fs.ReadFile(tree, p)
		if err != nil {
			return err
		}
		base := path.Base(p)
		switch {
		case projectFiles[base] != "" && strings.HasPrefix(p, "project/"):
			t, err := template.New(p).Delims("[[", "]]").Option("missingkey=error").Parse(string(src))
			if err != nil {
				return fmt.Errorf("template %s: %w", p, err)
			}
			var buf bytes.Buffer
			if err := t.Execute(&buf, data); err != nil {
				return fmt.Errorf("template %s: %w", p, err)
			}
			src = buf.Bytes()
		case strings.HasSuffix(p, ".go"):
			src = bytes.ReplaceAll(src, []byte(`"`+gamePackage+`"`), []byte(`"`+path.Join(o.Module, "game")+`"`))
		case base == asset.ProjectFile:
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
	// copyTree copies every file under root of tree, at its path relative to root.
	copyTree := func(tree fs.FS, root string, rename func(rel string) string) error {
		return fs.WalkDir(tree, root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || p == "embed.go" {
				return err
			}
			rel := strings.TrimPrefix(strings.TrimPrefix(p, root), "/")
			if root == "." {
				rel = p
			}
			return write(tree, p, rename(rel))
		})
	}
	projectFile := func(rel string) string {
		if dst, ok := projectFiles[path.Base(rel)]; ok {
			return dst
		}
		return rel
	}
	tmpl := projtemplate.FS
	err = fs.WalkDir(tmpl, "project", func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == "project" {
			return err
		}
		if d.IsDir() {
			return fs.SkipDir // the language directories, copied below
		}
		return write(tmpl, p, projectFile(p))
	})
	if err == nil {
		err = copyTree(tmpl, "project/"+lang, projectFile)
	}
	switch {
	case err != nil:
	case o.game != nil:
		err = copyTree(o.game, ".", func(rel string) string { return rel })
	default:
		if err = copyTree(tmpl, "common", func(rel string) string { return rel }); err == nil {
			err = copyTree(tmpl, lang, func(rel string) string { return rel })
		}
	}
	if err != nil {
		return nil, fmt.Errorf("init: %w", err)
	}
	editor, _, err := writeEditorFiles(dir, !o.Go)
	if err != nil {
		return nil, fmt.Errorf("init: %w", err)
	}
	r.Files += len(editor)
	if o.Go && !o.NoTidy {
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
	r.Next = []string{"cd " + o.Dir, "veduta test", "claude"}
	r.OK = true
	return r, nil
}

func init() {
	register(command{
		name:    "init",
		usage:   "init [dir] --name N [--engine vX.Y.Z] [--go [--module M] [--engine-dir PATH]]",
		summary: "create a game project: an empty Lua game (or a Go one with --go), an empty scene, sprite assets, one scenario, Claude Code setup",
		run: func(env *Env, _ *Session, args []string) (any, error) {
			fs := newFlags("init", env.Stderr)
			var o InitOptions
			fs.StringVar(&o.Name, "name", "", "game name")
			fs.BoolVar(&o.Go, "go", false, "create a Go game instead of a Lua one")
			fs.StringVar(&o.Module, "module", "", "with --go: Go module path (default: the name)")
			fs.StringVar(&o.Engine, "engine", "", "engine version to require (default: this tool's)")
			fs.StringVar(&o.EngineDir, "engine-dir", "", "with --go: use a local engine checkout (replace directive)")
			fs.BoolVar(&o.NoTidy, "no-tidy", false, "with --go: do not run go mod tidy")
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
