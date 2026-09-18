package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/riftbane/veduta/v2/asset/cook"
	"github.com/riftbane/veduta/v2/inspect"
	"github.com/riftbane/veduta/v2/script"
)

// CookResult wraps the cook report for printing.
type CookResult struct{ *cook.Report }

// ExitCode is 1 when an asset failed to compile.
func (r CookResult) ExitCode() int {
	if r.Failed > 0 {
		return 1
	}
	return 0
}

// Human lists compiled and failed assets.
func (r CookResult) Human() string {
	var b strings.Builder
	for _, it := range r.Items {
		switch it.Status {
		case cook.StatusCompiled, cook.StatusRemoved, cook.StatusStale:
			fmt.Fprintf(&b, "%-8s %s/%s\n", it.Status, it.Kind, it.Name)
		case cook.StatusError:
			for _, e := range it.Errors {
				fmt.Fprintln(&b, e.Error())
			}
		}
	}
	for _, w := range r.Warnings {
		fmt.Fprintln(&b, "warning:", w)
	}
	fmt.Fprintf(&b, "cook: %d compiled, %d fresh, %d failed, %d removed\n", r.Compiled, r.Fresh, r.Failed, r.Removed)
	return b.String()
}

// Cook compiles changed asset sources.
func (s *Session) Cook(force bool) (CookResult, error) {
	r, err := cook.Run(cook.Options{Root: s.Root, Project: s.Project, Force: force})
	if err != nil {
		return CookResult{}, err
	}
	return CookResult{r}, nil
}

// Query answers an ID-buffer question about a frame bundle (no game needed).
func Query(frame, at string, coverage bool) (map[string]any, error) {
	if frame == "" || (at == "") == !coverage {
		return nil, usagef("query: need --frame and exactly one of --at x,y or --coverage")
	}
	f, err := inspect.ReadFrame(frame)
	if err != nil {
		return nil, err
	}
	if coverage {
		return map[string]any{"ok": true, "frame": frame, "coverage": f.Coverage()}, nil
	}
	xs, ys, ok := strings.Cut(at, ",")
	x, e1 := strconv.Atoi(strings.TrimSpace(xs))
	y, e2 := strconv.Atoi(strings.TrimSpace(ys))
	if !ok || e1 != nil || e2 != nil {
		return nil, usagef("query: --at wants x,y, got %q", at)
	}
	p, err := f.At(x, y)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "frame": frame, "pixel": p}, nil
}

// Diff compares two images (PNG or .vframe) and writes the a|b|heat sheet.
func Diff(a, b, out string, threshold int) (*inspect.DiffReport, error) {
	ia, err := inspect.LoadImage(a)
	if err != nil {
		return nil, err
	}
	ib, err := inspect.LoadImage(b)
	if err != nil {
		return nil, err
	}
	r, sheet := inspect.Diff(ia, ib, threshold)
	if out == "" {
		base := func(p string) string { return strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)) }
		out = filepath.Join("out", "diff_"+base(a)+"_vs_"+base(b)+".png")
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return nil, err
	}
	f, err := os.Create(out)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if err := sheet.EncodePNG(f); err != nil {
		return nil, err
	}
	r.Sheet = filepath.ToSlash(out)
	return &r, nil
}

// ExtensionVersion is the version of the VS Code extension released with this tool
// (editors/vscode/package.json; a test keeps the two equal). The extension compares it
// with its own and offers to update itself when it is older.
const ExtensionVersion = "0.9.1"

// VersionInfo is printed by version.
type VersionInfo struct {
	Version   string `json:"version"`
	Extension string `json:"extension"` // ExtensionVersion
	Commit    string `json:"commit"`
	Date      string `json:"date"`
	Go        string `json:"go"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

// Human prints one line.
func (v VersionInfo) Human() string {
	return fmt.Sprintf("veduta %s (commit %s, built %s, %s, %s/%s)\n", v.Version, v.Commit, v.Date, v.Go, v.OS, v.Arch)
}

func versionInfo(env *Env) VersionInfo {
	v := VersionInfo{Version: env.Version, Extension: ExtensionVersion, Commit: env.Commit, Date: env.Date, Go: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH}
	if v.Commit == "" {
		v.Commit = "unknown"
	}
	if v.Date == "" {
		v.Date = "unknown"
	}
	return v
}

func init() {
	register(command{
		name: "cook", usage: "cook [--force]", summary: "compile changed asset sources to .vda", project: true,
		run: func(env *Env, s *Session, args []string) (any, error) {
			fs := newFlags("cook", env.Stderr)
			force := fs.Bool("force", false, "recompile everything")
			if err := parseFlags(fs, args); err != nil {
				return nil, err
			}
			return s.Cook(*force)
		},
	})
	register(command{
		name: "query", usage: "query --frame F --at x,y | --coverage", summary: "ID-buffer queries on a frame bundle (.vframe)",
		run: func(env *Env, _ *Session, args []string) (any, error) {
			fs := newFlags("query", env.Stderr)
			frame := fs.String("frame", "", "frame bundle written by render --bundle")
			at := fs.String("at", "", "pixel x,y")
			cov := fs.Bool("coverage", false, "per-entity coverage and occlusion ratios")
			if err := parseFlags(fs, args); err != nil {
				return nil, err
			}
			return Query(*frame, *at, *cov)
		},
	})
	register(command{
		name: "diff", usage: "diff A B [--out f.png] [--threshold N]", summary: "compare two images: changed pixels, bbox, max delta, a|b|heat sheet",
		run: func(env *Env, _ *Session, args []string) (any, error) {
			fs := newFlags("diff", env.Stderr)
			out := fs.String("out", "", "sheet path (default out/diff_<a>_vs_<b>.png)")
			thr := fs.Int("threshold", 0, "ignore channel differences up to this value")
			var files []string
			for len(args) > 0 {
				if err := fs.Parse(args); err != nil {
					return nil, UsageError{err}
				}
				if fs.NArg() == 0 {
					break
				}
				files = append(files, fs.Arg(0))
				args = fs.Args()[1:]
			}
			if len(files) != 2 {
				return nil, usagef("diff: want two images, got %d", len(files))
			}
			return Diff(files[0], files[1], *out, *thr)
		},
	})
	register(command{
		name: "version", usage: "version", summary: "print version, commit and build date",
		run: func(env *Env, _ *Session, args []string) (any, error) {
			if len(args) > 0 {
				return nil, usagef("version takes no arguments")
			}
			return versionInfo(env), nil
		},
	})
	register(command{
		name: "run", usage: "run [--scene S | --world W --at x,z] [--seed N]", summary: "build and run the player: on Windows the simulator window, on Linux the framebuffer; refuses where there is none (no 16 or 32 bpp /sys/class/graphics/fbN, no VEDUTA_FB). Starts in the scene or world given, else the project's default", project: true,
		run: func(env *Env, s *Session, args []string) (any, error) { return runGameCommand(env, s, "run", args) },
	})
	register(command{
		name: "sim", usage: "sim [--scene S | --world W --at x,z] [--seed N]", summary: "play the game in the simulator (Windows): the console's panel at a whole scale (VEDUTA_SCALE, 3), its 16-bit colors (VEDUTA_PANEL=0 turns them off) and its buttons on the keyboard. Starts in the scene or world given, else the project's default; saving a script or an asset reloads it in place", project: true,
		run: func(env *Env, s *Session, args []string) (any, error) {
			if runtime.GOOS != "windows" {
				return nil, fmt.Errorf("sim: the simulator is a Windows window, and this is %s/%s; on Linux, veduta run plays on a framebuffer", runtime.GOOS, runtime.GOARCH)
			}
			return runGameCommand(env, s, "sim", args)
		},
	})
}

// playArgs reads the flags of run and sim and returns the player's arguments for them:
// where it starts (-scene, or -world and -at) and its -seed.
func playArgs(name string, w io.Writer, args []string) ([]string, error) {
	fs := newFlags(name, w)
	sceneName := fs.String("scene", "", "start in this scene (default: the project's default world or scene)")
	worldName := fs.String("world", "", "start in this world instead of a scene")
	at := fs.String("at", "", "with --world: the start cell x,z")
	seed := fs.Uint64("seed", 0, "RNG seed (default: the project's)")
	if err := parseFlags(fs, args); err != nil {
		return nil, err
	}
	if *sceneName != "" && *worldName != "" {
		return nil, usagef("%s: --scene and --world are exclusive", name)
	}
	if *at != "" && *worldName == "" {
		return nil, usagef("%s: --at needs --world (the start cell)", name)
	}
	var out []string
	if *sceneName != "" {
		out = append(out, "-scene", *sceneName)
	}
	if *worldName != "" {
		out = append(out, "-world", *worldName)
	}
	if *at != "" {
		out = append(out, "-at", *at)
	}
	if *seed != 0 {
		out = append(out, "-seed", strconv.FormatUint(*seed, 10))
	}
	return out, nil
}

// runGameCommand builds the game and plays it: a Go game's binary, or a script game in this
// process.
func runGameCommand(env *Env, s *Session, name string, args []string) (any, error) {
	play, err := playArgs(name, env.Stderr, args)
	if err != nil {
		return nil, err
	}
	if why := runRefusal(s.Project.Engine); why != "" {
		return nil, errors.New(why)
	}
	bin, err := s.ensureGame()
	if err != nil {
		return nil, err
	}
	gameArgs := append([]string{"-project", s.Root}, play...)
	if s.IsScript() {
		if code := script.Run(gameArgs, env.Stdout, env.Stderr); code != 0 {
			return nil, fmt.Errorf("the game stopped with exit code %d", code)
		}
		return nil, nil
	}
	cmd := exec.Command(bin, gameArgs...)
	cmd.Dir, cmd.Stdout, cmd.Stderr, cmd.Stdin = s.Root, env.Stdout, env.Stderr, env.Stdin
	return nil, cmd.Run()
}
