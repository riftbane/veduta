// Package testgame is the engine's test game: a scene and a world with a hero, gems and
// obstacles, their assets and scenarios. The engine's tests build it, run its scenarios
// against goldens and create projects from it; veduta init never ships it.
package testgame

import "embed"

// FS is the project tree, laid out as veduta init writes a project.
//
//go:embed veduta.json cmd game assets tests
var FS embed.FS

// GamePackage is the import path of the game package inside the engine module, rewritten
// to "<module>/game" when a project is created from FS.
const GamePackage = "github.com/riftbane/veduta/internal/testgame/game"
