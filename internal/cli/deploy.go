package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// cardLabels are the volume labels of a console's card: a USB stick the console prefers,
// and the image's own volume.
var cardLabels = []string{"VEDUTA", "VEDUTAOS"}

// DeployReport says where deploy put the game.
type DeployReport struct {
	OK      bool   `json:"ok"`
	Card    string `json:"card"`    // the card's root
	Dir     string `json:"dir"`     // the game's folder on it
	Version string `json:"version"` // the version written into its card.json
	Files   int    `json:"files"`
}

// Human prints one line.
func (r *DeployReport) Human() string {
	return fmt.Sprintf("deployed %s (%s, %d files): put the card back in the console\n", r.Dir, r.Version, r.Files)
}

// Deploy puts the game onto a console's card, as its release archive would unpack there:
// games/<name> with veduta.json, README.md, card.json (version and icon set as the release
// sets them), the icon, the assets without the cooked files, and the scripts of a Lua game
// or the linux/arm64 program of a Go game. The folder of the same name is replaced whole,
// and only once the new one is complete. card is the card's root; empty, the one volume
// labelled VEDUTA or VEDUTAOS this machine has mounted.
func (s *Session) Deploy(card string) (*DeployReport, error) {
	if card == "" {
		found := findCards()
		switch len(found) {
		case 0:
			return nil, fmt.Errorf("deploy: no console card here: plug the card in (a drive labelled %s) or name its folder", strings.Join(cardLabels, " or "))
		case 1:
			card = found[0]
		default:
			return nil, fmt.Errorf("deploy: several console cards (%s): name one", strings.Join(found, ", "))
		}
	}
	games := filepath.Join(card, "games")
	if fi, err := os.Stat(games); err != nil || !fi.IsDir() {
		if fi, err := os.Stat(filepath.Join(card, "vedutaos")); err != nil || !fi.IsDir() {
			return nil, fmt.Errorf("deploy: %s is not a console card (no games or vedutaos folder); make %s to deploy there anyway", card, games)
		}
	}
	desc, why := s.readCard()
	if why != "" {
		return nil, fmt.Errorf("deploy: %s: %s", why, s.cardFix())
	}
	r := &DeployReport{Card: card, Dir: filepath.Join(games, s.Project.Name), Version: s.gitVersion()}
	desc["version"] = r.Version
	if s.Project.Icon != "" {
		desc["icon"] = "icon.png"
	}
	partial := filepath.Join(games, "."+s.Project.Name+".partial")
	if err := os.RemoveAll(partial); err != nil {
		return nil, err
	}
	stage := func() error {
		if err := os.MkdirAll(partial, 0o755); err != nil {
			return err
		}
		put := func(name string, data []byte, perm fs.FileMode) error {
			p := filepath.Join(partial, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			r.Files++
			return os.WriteFile(p, data, perm)
		}
		if s.IsScript() {
			if err := s.copyInto(put, ".lua", s.Root, ""); err != nil {
				return err
			}
		} else {
			bin, errs, err := s.consoleBuild()
			if err != nil {
				if len(errs) > 0 {
					e := errs[0]
					return fmt.Errorf("%w: %s:%d:%d: %s", err, e.File, e.Line, e.Col, e.Msg)
				}
				return err
			}
			defer os.Remove(bin)
			data, err := os.ReadFile(bin)
			if err != nil {
				return err
			}
			if err := put(s.Project.Name, data, 0o755); err != nil {
				return err
			}
		}
		for _, f := range []string{"veduta.json", "README.md"} {
			if data, err := os.ReadFile(filepath.Join(s.Root, f)); err == nil {
				if err := put(f, data, 0o644); err != nil {
					return err
				}
			} else if !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
		if s.Project.Icon != "" {
			data, err := os.ReadFile(filepath.Join(s.Root, filepath.FromSlash(s.Project.Icon)))
			if err != nil {
				return fmt.Errorf("veduta.json names the icon %s: %w", s.Project.Icon, err)
			}
			if err := put("icon.png", data, 0o644); err != nil {
				return err
			}
		}
		cardJSON, err := json.MarshalIndent(desc, "", "  ")
		if err != nil {
			return err
		}
		if err := put("card.json", append(cardJSON, '\n'), 0o644); err != nil {
			return err
		}
		assets := filepath.Join(s.Root, filepath.FromSlash(s.Project.Assets))
		if _, err := os.Stat(assets); err != nil {
			return nil
		}
		return s.copyInto(put, "", assets, "assets")
	}
	if err := stage(); err != nil {
		os.RemoveAll(partial)
		return nil, fmt.Errorf("deploy: %w", err)
	}
	if err := os.RemoveAll(r.Dir); err != nil {
		return nil, fmt.Errorf("deploy: %w", err)
	}
	if err := os.Rename(partial, r.Dir); err != nil {
		return nil, fmt.Errorf("deploy: %w", err)
	}
	r.OK = true
	return r, nil
}

// copyInto copies the files under dir (only those ending in suffix, when it is set) to
// under prefix, leaving out hidden folders, out, bin, build and the cooked assets.
func (s *Session) copyInto(put func(string, []byte, fs.FileMode) error, suffix, dir, prefix string) error {
	cooked := filepath.Clean(filepath.Join(s.Root, filepath.FromSlash(s.Project.Cooked)))
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			n := d.Name()
			if p != dir && (strings.HasPrefix(n, ".") || n == "out" || n == "bin" || n == "build" || filepath.Clean(p) == cooked) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !strings.HasSuffix(d.Name(), suffix) {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		return put(filepath.ToSlash(filepath.Join(prefix, rel)), data, 0o644)
	})
}

// gitVersion names what is deployed: git describe when the project is a repository with a
// commit, else "dev".
func (s *Session) gitVersion() string {
	cmd := exec.Command("git", "describe", "--tags", "--always", "--dirty")
	cmd.Dir = s.Root
	if out, err := cmd.Output(); err == nil {
		if v := strings.TrimSpace(string(out)); v != "" {
			return v
		}
	}
	return "dev"
}

func init() {
	register(command{
		name: "deploy", usage: "deploy [card]", summary: "put the game on a console's card (a drive labelled VEDUTAOS or VEDUTA, found by itself, or the folder named): games/<name> as its release unpacks, a Go game built for linux/arm64", project: true,
		run: func(env *Env, s *Session, args []string) (any, error) {
			fs := newFlags("deploy", env.Stderr)
			if err := parseFlags(fs, args); err != nil {
				return nil, err
			}
			if fs.NArg() > 1 {
				return nil, usagef("deploy takes at most one card")
			}
			return s.Deploy(fs.Arg(0))
		},
	})
}
