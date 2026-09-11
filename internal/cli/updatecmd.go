package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/riftbane/veduta/internal/update"
)

// UpdateReport is the result of update.
type UpdateReport struct {
	Current   string         `json:"current"`
	Latest    string         `json:"latest"`
	Available bool           `json:"available"`
	Updated   bool           `json:"updated"`
	Result    *update.Result `json:"result,omitempty"`
}

// Human prints one line.
func (r *UpdateReport) Human() string {
	switch {
	case r.Updated:
		return fmt.Sprintf("updated %s → %s (%s verified, sha256 %s)\n", r.Result.From, r.Result.To, r.Result.Archive, r.Result.SHA256)
	case r.Available:
		return fmt.Sprintf("%s is available (current %s): run veduta update\n", r.Latest, r.Current)
	}
	return fmt.Sprintf("up to date (%s, latest release %s)\n", r.Current, r.Latest)
}

// Update checks for (and, unless check is set, installs) the latest release of the tool.
func Update(ctx context.Context, env *Env, check, force bool) (*UpdateReport, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	rel, err := update.Latest(ctx, client)
	if err != nil {
		return nil, err
	}
	r := &UpdateReport{Current: env.Version, Latest: rel.Tag, Available: update.Compare(env.Version, rel.Tag) < 0}
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
// It returns the result when the binary was replaced.
func autoUpdate(env *Env) (*update.Result, error) {
	cfg, err := update.LoadConfig()
	if err != nil || cfg.AutoUpdate != "auto" || !update.IsVersion(env.Version) {
		return nil, err
	}
	st := update.Check(context.Background(), env.Version, time.Duration(cfg.CheckIntervalHours)*time.Hour, time.Now())
	if !st.Available {
		return nil, nil
	}
	r, err := Update(context.Background(), env, false, false)
	if err != nil || !r.Updated {
		return nil, err
	}
	return r.Result, nil
}

// UpgradeReport is the result of upgrade.
type UpgradeReport struct {
	From       string   `json:"from"`
	To         string   `json:"to"`
	Changed    []string `json:"changed"`
	Migrations []string `json:"migrations"`
}

// Human prints what changed.
func (r *UpgradeReport) Human() string {
	if len(r.Changed) == 0 {
		return fmt.Sprintf("already on %s\n", r.To)
	}
	return fmt.Sprintf("upgraded %s → %s (%s)\n", r.From, r.To, strings.Join(r.Changed, ", "))
}

// Upgrade moves the project to the tool's engine version: veduta.json, go.mod (go get +
// go mod tidy), format migrations (none in v0.1.0) and an entry in the project's
// CHANGELOG.md.
func (s *Session) Upgrade(env *Env, force bool) (*UpgradeReport, error) {
	to := env.Version
	if !update.IsVersion(to) {
		return nil, fmt.Errorf("upgrade: this tool is a development build (%s) and has no engine version to move to", to)
	}
	from := s.Project.Engine
	r := &UpgradeReport{From: from, To: to, Changed: []string{}, Migrations: []string{}}
	if c := update.Compare(from, to); c > 0 && !force {
		return nil, fmt.Errorf("upgrade: the project targets %s, newer than this tool (%s); run veduta update, or --force to downgrade", from, to)
	}
	manifest := filepath.Join(s.Root, "veduta.json")
	data, err := os.ReadFile(manifest)
	if err != nil {
		return nil, err
	}
	re := regexp.MustCompile(`("engine"\s*:\s*)"[^"]*"`)
	if next := re.ReplaceAll(data, []byte(`${1}"`+to+`"`)); string(next) != string(data) {
		if err := os.WriteFile(manifest, next, 0o644); err != nil {
			return nil, err
		}
		r.Changed = append(r.Changed, "veduta.json")
	}
	mod, _ := os.ReadFile(filepath.Join(s.Root, "go.mod"))
	if !regexp.MustCompile(`github\.com/riftbane/veduta\s+` + regexp.QuoteMeta(to) + `\b`).Match(mod) {
		for _, args := range [][]string{{"get", "github.com/riftbane/veduta@" + to}, {"mod", "tidy"}} {
			cmd := s.goCmd(args...)
			if out, err := cmd.CombinedOutput(); err != nil {
				return nil, fmt.Errorf("upgrade: go %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
			}
		}
		r.Changed = append(r.Changed, "go.mod")
	}
	if len(r.Changed) == 0 {
		return r, nil
	}
	entry := fmt.Sprintf("- Upgrade the Veduta engine from %s to %s (`veduta upgrade`; no format migrations).\n", from, to)
	if err := addChangelogEntry(filepath.Join(s.Root, "CHANGELOG.md"), entry); err != nil {
		return nil, err
	}
	r.Changed = append(r.Changed, "CHANGELOG.md")
	return r, nil
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
		s = s[:i] + "## Unreleased\n\n" + line + s[j:]
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
			return Check{Name: "update", OK: true, Detail: "update checks disabled (auto_update: off)"}
		}
		st := update.Check(context.Background(), env.Version, time.Duration(cfg.CheckIntervalHours)*time.Hour, time.Now())
		switch {
		case st.Error != "":
			return Check{Name: "update", OK: true, Detail: "could not check for updates: " + st.Error}
		case st.Available:
			return Check{Name: "update", OK: true, Detail: fmt.Sprintf("%s is available (current %s)", st.Latest, st.Current), Fix: "run veduta update"}
		}
		return Check{Name: "update", OK: true, Detail: fmt.Sprintf("up to date (latest release %s)", st.Latest)}
	}
	register(command{
		name: "update", usage: "update [--check] [--force]", summary: "update the tool binary from GitHub Releases (checksum verified, atomic)",
		run: func(env *Env, _ *Session, args []string) (any, error) {
			fs := newFlags("update", env.Stderr)
			check := fs.Bool("check", false, "only report whether an update is available")
			force := fs.Bool("force", false, "reinstall even when up to date or on a dev build")
			if err := parseFlags(fs, args); err != nil {
				return nil, err
			}
			return Update(context.Background(), env, *check, *force)
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
