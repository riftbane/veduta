// Package template holds the project created by `veduta init`: an empty game (game/,
// cmd/game), an empty scene, the sprite building blocks, one scenario, and the templated
// project files in project/ (go.mod, CLAUDE.md, README.md, .gitignore, .mcp.json,
// card.json, the release workflow).
//
// The game is a real package of this module, so the engine's own tests build and run it;
// init rewrites its import paths to the new module.
package template

import "embed"

// FS is the template tree.
//
//go:embed veduta.json cmd game assets tests project
var FS embed.FS

// GamePackage is the import path of the game package inside the engine module,
// rewritten to "<module>/game" by init.
const GamePackage = "github.com/riftbane/veduta/template/game"
