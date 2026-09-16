package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/internal/update"
)

// UpdateReport is the result of update.
type UpdateReport struct {
	Current   string         `json:"current"`
	Channel   string         `json:"channel"`   // the channel this run followed
	Following string         `json:"following"` // the channel the configuration names
	Latest    string         `json:"latest"`
	Available bool           `json:"available"`
	Downgrade bool           `json:"downgrade"` // the channel's newest release is older than this build
	Updated   bool           `json:"updated"`
	Warnings  []string       `json:"warnings"`
	Result    *update.Result `json:"result,omitempty"`
}

// Human prints one line, after any warning.
func (r *UpdateReport) Human() string {
	var b strings.Builder
	for _, w := range r.Warnings {
		fmt.Fprintln(&b, "warning:", w)
	}
	return b.String() + r.line()
}

func (r *UpdateReport) line() string {
	cmd := "veduta update"
	if r.Channel != r.Following { // previewing another channel: the flag is part of the command
		cmd += " --channel " + r.Channel
	}
	switch {
	case r.Updated:
		return fmt.Sprintf("updated %s → %s on the %s channel (%s verified, sha256 %s)\n", r.Result.From, r.Result.To, r.Channel, r.Result.Archive, r.Result.SHA256)
	case r.Available:
		return fmt.Sprintf("%s is available on the %s channel (current %s): run %s\n", r.Latest, r.Channel, r.Current, cmd)
	case r.Downgrade:
		return fmt.Sprintf("%s is the newest %s release, older than this build (%s): run %s --force to go back\n", r.Latest, r.Channel, r.Current, cmd)
	}
	return fmt.Sprintf("up to date (%s, newest %s release %s)\n", r.Current, r.Channel, r.Latest)
}

// Update checks for (and, unless check is set, installs) the newest release of the tool on
// a channel. An empty channel means the configured one; naming one follows it from then on,
// but the configuration is written only when a release is actually installed, so --check
// stays a preview.
func Update(ctx context.Context, env *Env, channel string, check, force bool) (*UpdateReport, error) {
	cfg, cfgErr := update.LoadConfig()
	following := cfg.Channel
	if following == "" {
		following = update.ChannelStable
	}
	if channel == "" {
		channel = following
	}
	// Naming another channel subscribes to it, which means writing the configuration.
	subscribe := !check && channel != following
	if cfgErr != nil && subscribe {
		// LoadConfig hands back the defaults with its error, so saving now would replace
		// whatever the file holds with them. Refuse before the binary is touched.
		return nil, fmt.Errorf("%w (fix it, or drop --channel to update on the %s channel)", cfgErr, following)
	}
	r := &UpdateReport{Current: env.Version, Channel: channel, Following: following, Warnings: []string{}}
	if cfgErr != nil {
		r.Warnings = append(r.Warnings, fmt.Sprintf("%v; following the %s channel", cfgErr, following))
	}
	if subscribe {
		// Saved before installing: the subscription is what was asked for, and a failed
		// install must not be able to lose it (nor a failed write to undo an install).
		cfg.Channel = channel
		if _, err := update.SaveConfig(cfg); err != nil {
			return nil, err
		}
		r.Following = channel
	}
	client := &http.Client{Timeout: 60 * time.Second}
	rel, err := update.Latest(ctx, client, channel)
	if err != nil {
		return nil, err
	}
	c := update.Compare(env.Version, rel.Tag)
	r.Latest, r.Available, r.Downgrade = rel.Tag, c < 0, c > 0
	if check || (!r.Available && !force) {
		return r, nil
	}
	if !update.IsVersion(env.Version) && !force {
		return nil, fmt.Errorf("update: this is a development build (%s); use --force to replace it with %s", env.Version, rel.Tag)
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	res, err := update.Apply(ctx, client, rel, exe, env.Version)
	if err != nil {
		return nil, err
	}
	r.Updated, r.Result = true, res
	return r, nil
}

// autoUpdate applies an available update when the configuration says auto (spec §13.3).
// It returns the report when the binary was replaced.
func autoUpdate(env *Env) (*UpdateReport, error) {
	cfg, err := update.LoadConfig()
	if err != nil || cfg.AutoUpdate != "auto" || !update.IsVersion(env.Version) {
		return nil, err
	}
	st := update.Check(context.Background(), env.Version, cfg.Channel, time.Duration(cfg.CheckIntervalHours)*time.Hour, time.Now())
	if !st.Available {
		return nil, nil
	}
	r, err := Update(context.Background(), env, cfg.Channel, false, false)
	if err != nil || !r.Updated {
		return nil, err
	}
	return r, nil
}

// UpgradeReport is the result of upgrade.
type UpgradeReport struct {
	From    string   `json:"from"`
	To      string   `json:"to"`
	Changed []string `json:"changed"` // files changed, as applicable: veduta.json, go.mod, CHANGELOG.md
	// Migrations describes each change made to keep the project's behaviour, one sentence
	// each, such as `pin "tick_rate": 60 in veduta.json (the default before v1.0.0)`.
	Migrations []string `json:"migrations"`
	// Next lists what the project still lacks for the console after crossing from a v0.x
	// engine, one sentence each, because upgrade does not write it and veduta release
	// refuses until it is there: a workflow that builds linux/arm64, a card.json.
	Next []string `json:"next"`
}

// Human prints what changed, then one line per migration.
func (r *UpgradeReport) Human() string {
	if len(r.Changed) == 0 {
		return fmt.Sprintf("already on %s\n", r.To)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "upgraded %s → %s (%s)\n", r.From, r.To, strings.Join(r.Changed, ", "))
	for _, m := range r.Migrations {
		fmt.Fprintln(&b, "migration:", m)
	}
	for _, n := range r.Next {
		fmt.Fprintln(&b, "next:", n)
	}
	return b.String()
}

// Upgrade moves the project to the tool's engine version: veduta.json, go.mod (go get +
// go mod tidy) and an entry in the project's CHANGELOG.md. A project that crosses from a
// v0.x engine to v1.0.0 or later keeps the resolution, inspect_resolution and tick_rate it
// ran with: every one its manifest left to the default is written out with the v0.x value
// (v0Defaults), and the report and the changelog entry name them.
func (s *Session) Upgrade(env *Env, force bool) (*UpgradeReport, error) {
	to := env.Version
	if !update.IsVersion(to) {
		return nil, fmt.Errorf("upgrade: this tool is a development build (%s) and has no engine version to move to", to)
	}
	from := s.Project.Engine
	r := &UpgradeReport{From: from, To: to, Changed: []string{}, Migrations: []string{}, Next: []string{}}
	if c := update.Compare(from, to); c > 0 && !force {
		return nil, fmt.Errorf("upgrade: the project targets %s, newer than this tool (%s); run veduta update, or --force to downgrade", from, to)
	}
	manifest := filepath.Join(s.Root, "veduta.json")
	data, err := os.ReadFile(manifest)
	if err != nil {
		return nil, err
	}
	next, pinned, err := upgradeManifest(data, to, v0Engine(from) && !v0Engine(to))
	if err != nil {
		return nil, fmt.Errorf("upgrade %s: %w", manifest, err)
	}
	manifestChanged := string(next) != string(data)
	if manifestChanged {
		// The edit touches only the text of the fields it writes; a manifest that no
		// longer parses would mean it went wrong, and is never written.
		if _, err := asset.ParseProject(asset.ProjectFile, next); err != nil {
			return nil, fmt.Errorf("upgrade %s: the rewritten manifest does not parse: %w", manifest, err)
		}
		r.Changed = append(r.Changed, "veduta.json")
		for _, p := range pinned {
			r.Migrations = append(r.Migrations, "pin "+p+" in veduta.json (the default before v1.0.0)")
		}
	}
	mod, err := os.ReadFile(filepath.Join(s.Root, "go.mod"))
	if !(s.IsScript() && os.IsNotExist(err)) && !goModRequires(mod, to) {
		// Another major version is another module path: the imports move to it first,
		// so go mod tidy drops the old one.
		files, err := rewriteEngineImports(s.Root, engineModule(to))
		if err != nil {
			return nil, fmt.Errorf("upgrade: %w", err)
		}
		if len(files) > 0 {
			r.Changed = append(r.Changed, files...)
			r.Migrations = append(r.Migrations, fmt.Sprintf("import %s in %d Go files", engineModule(to), len(files)))
		}
		for _, args := range [][]string{{"get", engineModule(to) + "@" + to}, {"mod", "tidy"}} {
			cmd := s.goCmd(args...)
			if out, err := cmd.CombinedOutput(); err != nil {
				return nil, fmt.Errorf("upgrade: go %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
			}
		}
		r.Changed = append(r.Changed, "go.mod")
	}
	// Written after go get, which is what fails in practice (the network): a run that stops
	// there leaves veduta.json on the old engine, so the next one pins the same fields and
	// still names them in the changelog.
	if manifestChanged {
		if err := os.WriteFile(manifest, next, 0o644); err != nil {
			return nil, err
		}
	}
	if len(r.Changed) == 0 {
		return r, nil
	}
	entry := fmt.Sprintf("- Upgrade the Veduta engine from %s to %s (`veduta upgrade`).\n", from, to)
	if len(pinned) > 0 {
		entry = fmt.Sprintf("- Upgrade the Veduta engine from %s to %s (`veduta upgrade`), pinning the defaults `veduta.json` relied on before v1.0.0 so the game keeps its size and tick rate: `%s`.\n", from, to, strings.Join(pinned, "`, `"))
	}
	if err := addChangelogEntry(filepath.Join(s.Root, "CHANGELOG.md"), entry); err != nil {
		return nil, err
	}
	r.Changed = append(r.Changed, "CHANGELOG.md")
	if v0Engine(from) && !v0Engine(to) {
		r.Next = s.consoleNext()
	}
	return r, nil
}

// consoleNext lists what a project that just left a v0.x engine still needs before veduta
// release publishes it for the console.
func (s *Session) consoleNext() []string {
	next := []string{}
	if ok, detail := s.releaseTargetsConsole(); !ok {
		next = append(next, detail+": "+workflowFix+"; veduta release refuses until then")
	}
	if _, why := s.readCard(); why != "" {
		next = append(next, why+": "+s.cardFix()+"; veduta release refuses until then")
	}
	return next
}

// engineRepo is the engine's module path up to v1; from v2 on the path ends in /vN.
const engineRepo = "github.com/riftbane/veduta"

// engineModule is the engine's module path at version: github.com/riftbane/veduta/v2 for
// v2.x.
func engineModule(version string) string {
	major, _, _ := strings.Cut(strings.TrimPrefix(version, "v"), ".")
	if n, err := strconv.Atoi(major); err == nil && n >= 2 {
		return engineRepo + "/v" + major
	}
	return engineRepo
}

// engineRequire matches the engine in a go.mod, at any major version; the second group is
// the version.
var engineRequire = regexp.MustCompile(`github\.com/riftbane/veduta(/v\d+)?\s+(v\S+)`)

// goModRequires reports whether a go.mod names the engine at exactly version, under the
// module path of that version. The version must end the word: v1.0.0-rc.1 is not v1.0.0,
// nor is v1.0.0-rc.10 v1.0.0-rc.1.
func goModRequires(mod []byte, version string) bool {
	return regexp.MustCompile(regexp.QuoteMeta(engineModule(version)) + `\s+` + regexp.QuoteMeta(version) + `(\s|$)`).Match(mod)
}

// engineImport matches a quoted engine package path at any major version; the second group
// is the package inside the engine.
var engineImport = regexp.MustCompile(`"github\.com/riftbane/veduta(/v\d+)?(/[^"]*)?"`)

// rewriteEngineImports makes every engine import of the project's Go files use module, and
// returns the files it changed, slash-separated and relative to root. Hidden directories,
// out, bin and vendor are skipped.
func rewriteEngineImports(root, module string) ([]string, error) {
	var changed []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "out" || d.Name() == "bin" || d.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		next := engineImport.ReplaceAll(src, []byte(`"`+module+`$2"`))
		if string(next) == string(src) {
			return nil
		}
		if err := os.WriteFile(p, next, 0o644); err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		changed = append(changed, filepath.ToSlash(rel))
		return nil
	})
	return changed, err
}

// addChangelogEntry adds a line under "## Unreleased", creating the file or section.
func addChangelogEntry(path, line string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return os.WriteFile(path, []byte("# Changelog\n\n## Unreleased\n\n"+line), 0o644)
	}
	if err != nil {
		return err
	}
	s := string(data)
	if i := strings.Index(s, "## Unreleased"); i >= 0 {
		j := i + len("## Unreleased")
		for j < len(s) && s[j] == '\n' {
			j++
		}
		rest, sep := s[j:], ""
		if strings.HasPrefix(rest, "## ") { // empty section right before a release
			sep = "\n"
		}
		s = s[:i] + "## Unreleased\n\n" + line + sep + rest
	} else if i := strings.Index(s, "\n## "); i >= 0 {
		s = s[:i+1] + "## Unreleased\n\n" + line + "\n" + s[i+1:]
	} else {
		s = strings.TrimRight(s, "\n") + "\n\n## Unreleased\n\n" + line
	}
	return os.WriteFile(path, []byte(s), 0o644)
}

func init() {
	updateCheck = func(env *Env) Check {
		cfg, err := update.LoadConfig()
		if err != nil {
			return Check{Name: "update", OK: false, Detail: err.Error(), Fix: "fix ~/.config/veduta/config.json"}
		}
		if cfg.AutoUpdate == "off" {
			return Check{Name: "update", OK: true, Detail: fmt.Sprintf("update checks disabled (auto_update: off; channel %s)", cfg.Channel)}
		}
		st := update.Check(context.Background(), env.Version, cfg.Channel, time.Duration(cfg.CheckIntervalHours)*time.Hour, time.Now())
		switch {
		case st.Error != "":
			return Check{Name: "update", OK: true, Detail: fmt.Sprintf("could not check the %s channel for updates: %s", st.Channel, st.Error)}
		case st.Available:
			// doctor prints the fix only for a failing check, so the call to action goes
			// in the detail; Fix stays for --json and the MCP status tool.
			return Check{Name: "update", OK: true, Detail: fmt.Sprintf("%s is available on the %s channel (current %s): run veduta update", st.Latest, st.Channel, st.Current), Fix: "run veduta update"}
		case st.Downgrade:
			return Check{Name: "update", OK: true, Detail: fmt.Sprintf("this build is %s, newer than the newest %s release %s: run veduta update --force to go back", st.Current, st.Channel, st.Latest), Fix: "run veduta update --force"}
		}
		return Check{Name: "update", OK: true, Detail: fmt.Sprintf("up to date on the %s channel (newest release %s)", st.Channel, st.Latest)}
	}
	register(command{
		name: "update", usage: "update [--check] [--force] [--channel stable|beta]", summary: "update the tool binary from GitHub Releases (checksum verified, atomic)",
		run: func(env *Env, _ *Session, args []string) (any, error) {
			fs := newFlags("update", env.Stderr)
			check := fs.Bool("check", false, "only report whether an update is available")
			force := fs.Bool("force", false, "reinstall even when up to date, on a dev build, or to go back to an older release")
			channel := fs.String("channel", "", "release channel to follow from now on: stable or beta (default: the configured one)")
			if err := parseFlags(fs, args); err != nil {
				return nil, err
			}
			if *channel != "" && !update.ValidChannel(*channel) {
				return nil, usagef("update: channel %q (want stable or beta)", *channel)
			}
			return Update(context.Background(), env, *channel, *check, *force)
		},
	})
	register(command{
		name: "upgrade", usage: "upgrade [--force]", summary: "move the project (go.mod, veduta.json) to this tool's engine version and run migrations", project: true,
		run: func(env *Env, s *Session, args []string) (any, error) {
			fs := newFlags("upgrade", env.Stderr)
			force := fs.Bool("force", false, "allow downgrades")
			if err := parseFlags(fs, args); err != nil {
				return nil, err
			}
			return s.Upgrade(env, *force)
		},
	})
}
