// Package template holds the project created by `veduta init`: an empty game in Lua (lua/)
// or in Go (go/, with --go), the parts both share (common/: an empty scene, the sprite
// building blocks, one scenario), and the templated project files (project/: CHANGELOG.md,
// .gitignore, .mcp.json and card.json for both, and in project/lua and project/go the
// README.md, CLAUDE.md and release workflow of each, and go.mod). new/ holds what `veduta
// new` writes: one source of each format, the ground a new world stands on, and
// a Lua module.
//
// The Go game is a real package of this module, so the engine's own tests build and run it;
// init rewrites its import paths to the new module.
package template

import "embed"

// FS is the template tree.
//
//go:embed common go lua project new
var FS embed.FS

// GamePackage is the import path of the Go game package inside the engine module,
// rewritten to "<module>/game" by init.
const GamePackage = "github.com/riftbane/veduta/v2/template/go/game"
