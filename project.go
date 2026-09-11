package veduta

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/asset/cook"
	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/scene"
)

// loadProject reads veduta.json in dir and loads every asset of the project (fresh cooked
// files, else compiled from source in memory). When dir is "." and holds no manifest, the
// directory of the executable is tried, so a player can start the game from anywhere.
func loadProject(dir string) (*asset.Project, *Assets, error) {
	if dir == "." {
		if _, err := os.Stat(asset.ProjectFile); errors.Is(err, fs.ErrNotExist) {
			if exe, err := os.Executable(); err == nil {
				alt := filepath.Dir(exe)
				if _, err := os.Stat(filepath.Join(alt, asset.ProjectFile)); err == nil {
					dir = alt
				}
			}
		}
	}
	p, err := cook.ReadProject(dir)
	if err != nil {
		return nil, nil, err
	}
	lib, err := cook.LoadProject(dir, p)
	if err != nil {
		return nil, nil, err
	}
	return p, lib, nil
}

// runPlayer opens the game window.
func runPlayer(g Game, p *asset.Project, a *Assets) error {
	return errors.New("the player window is not available in this build")
}

// renderWith is render with an extra hook that can add to the draw list (for example
// debug lines) after the scene and before the HUD.
func (e *engine) renderWith(cam scene.Camera, w, h int, mode gfx.RenderMode, normals bool, extra func(dl *gfx.DrawList, view int)) (*frame, error) {
	e.extra = extra
	defer func() { e.extra = nil }()
	return e.render(cam, w, h, mode, normals)
}
