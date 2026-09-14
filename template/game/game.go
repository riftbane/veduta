// Package game is the Veduta demo: WASD moves the hero on the ground, Space jumps, gems
// are collected on contact, KeyR resets the level, and the HUD shows score and tick.
package game

import (
	"encoding/gob"
	"fmt"

	"github.com/riftbane/veduta"
	"github.com/riftbane/veduta/gfx"
)

// Game is the demo's global state: everything outside the scene.
type Game struct {
	Score  int // gems collected since the last reset
	Resets int
}

// Init registers the game's invariants and state codec.
func (g *Game) Init(ctx *veduta.Context) error {
	if ctx.Scene.Find("player") == nil {
		return fmt.Errorf("scene %s has no entity named player", ctx.Scene.Name)
	}
	current = g
	ctx.Invariant("score_non_negative", func() bool { return g.Score >= 0 })
	ctx.RegisterState(g)
	return nil
}

// Update handles the reset key; entity logic lives in the kinds (kinds.go).
func (g *Game) Update(ctx *veduta.Context, in veduta.Input) {
	if in.JustPressed("KeyR") {
		g.Score = 0
		g.Resets++
		if err := ctx.LoadScene(ctx.Scene.Name); err != nil {
			ctx.Trace("error", map[string]any{"msg": err.Error()})
			return
		}
		ctx.Trace("reset", map[string]any{"resets": g.Resets})
	}
}

// Draw adds the HUD: score and gems left on the left, tick on the right (or below the
// score when the frame is too narrow). Text scales with the frame height.
func (g *Game) Draw(ctx *veduta.Context, dl *gfx.DrawList) {
	hud := ctx.HUD(dl)
	scale := max(1, ctx.Height/360)
	cell, margin := 8*scale, 6*scale
	score := fmt.Sprintf("SCORE %d  GEMS %d", g.Score, len(ctx.Scene.Tagged("gem")))
	tick := fmt.Sprintf("TICK %05d", ctx.Tick)
	ctx.Text(hud, float32(margin), float32(margin), scale, score, 0xfff4f0e0)
	x, y := ctx.Width-margin-len(tick)*cell, margin
	if x < margin+(len(score)+1)*cell {
		x, y = margin, margin+cell+2*scale
	}
	ctx.Text(hud, float32(x), float32(y), scale, tick, 0xffa0b0c0)
	hud.End()
}

// SaveState writes the game state into a snapshot.
func (g *Game) SaveState(enc *gob.Encoder) error { return enc.Encode(g) }

// LoadState restores the game state from a snapshot.
func (g *Game) LoadState(dec *gob.Decoder) error { return dec.Decode(g) }

// current is the running game, used by behaviours to update the score. There is one game
// per process; Init sets it.
var current *Game

func (g *Game) collect() int {
	g.Score++
	return g.Score
}
