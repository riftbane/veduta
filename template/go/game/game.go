// Package game is the game: an empty scene and its name on the screen, the starting point
// of a new project.
package game

import (
	"encoding/gob"

	"github.com/riftbane/veduta"
	"github.com/riftbane/veduta/gfx"
)

// Game is the game's global state: everything outside the scene.
type Game struct{}

// Init runs once, after the first scene is loaded.
func (g *Game) Init(ctx *veduta.Context) error {
	ctx.RegisterState(g)
	return nil
}

// Update runs once per tick, before the entities' behaviours.
func (g *Game) Update(ctx *veduta.Context, in veduta.Input) {}

// Draw adds the HUD to every rendered frame.
func (g *Game) Draw(ctx *veduta.Context, dl *gfx.DrawList) {
	hud := ctx.HUD(dl)
	name := ctx.Project.Name
	ctx.Text(hud, float32((ctx.Width-8*len(name))/2), float32(ctx.Height/2-4), 1, name, 0xfff4f0e0)
	hud.End()
}

// SaveState writes the game state into a snapshot.
func (g *Game) SaveState(enc *gob.Encoder) error { return enc.Encode(g) }

// LoadState restores the game state from a snapshot.
func (g *Game) LoadState(dec *gob.Decoder) error { return dec.Decode(g) }
