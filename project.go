package veduta

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/asset/cook"
	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/scene"
)

// projectDir is the directory of the project: dir, or for "." without a manifest there, the
// directory of the executable, where a released game ships its veduta.json.
func projectDir(dir string) string {
	if dir == "." {
		if _, err := os.Stat(asset.ProjectFile); errors.Is(err, fs.ErrNotExist) {
			if exe, err := os.Executable(); err == nil {
				alt := filepath.Dir(exe)
				if _, err := os.Stat(filepath.Join(alt, asset.ProjectFile)); err == nil {
					return alt
				}
			}
		}
	}
	return dir
}

// loadProject reads veduta.json in dir and loads every asset of the project (fresh cooked
// files, else compiled from source in memory). When dir is "." and holds no manifest, the
// directory of the executable is tried, so a player can start the game from anywhere.
func loadProject(dir string) (*asset.Project, *Assets, error) {
	dir = projectDir(dir)
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

// renderWith is render with an extra hook that can add to the draw list (for example
// debug lines) after the scene and before the HUD.
func (e *engine) renderWith(cam scene.Camera, w, h int, mode gfx.RenderMode, normals bool, extra func(dl *gfx.DrawList, view int)) (*frame, error) {
	e.extra = extra
	defer func() { e.extra = nil }()
	return e.render(cam, w, h, mode, normals)
}
