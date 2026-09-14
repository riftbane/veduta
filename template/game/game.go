// Package game is the Veduta demo: WASD moves the hero on the ground, Space jumps, gems
// are collected on contact, KeyF switches between the scene camera and a first-person
// view, KeyR resets the level, and the HUD shows score and tick.
package game

import (
	"encoding/gob"
	"fmt"

	"github.com/riftbane/veduta"
	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gmath"
	"github.com/riftbane/veduta/scene"
)

// Game is the demo's global state: everything outside the scene.
type Game struct {
	Score  int // gems collected since the last reset
	Resets int

	// First-person view (KeyF): the camera sits at the hero's eyes and the mouse looks
	// around. Yaw 0 looks along -Z, positive pitch looks up; both are degrees.
	FirstPerson bool
	Yaw, Pitch  float32
	ThirdCam    scene.Camera // the scene camera, kept while in first person
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

// Update handles the reset and view keys and aims the first-person camera; entity logic
// lives in the kinds (kinds.go).
func (g *Game) Update(ctx *veduta.Context, in veduta.Input) {
	if in.JustPressed("KeyR") {
		g.Score = 0
		g.Resets++
		if err := ctx.LoadScene(ctx.Scene.Name); err != nil {
			ctx.Trace("error", map[string]any{"msg": err.Error()})
			return
		}
		ctx.Trace("reset", map[string]any{"resets": g.Resets})
		if g.FirstPerson { // the fresh scene brings its own camera and a visible hero
			g.setView(ctx, true)
		}
	}
	if in.JustPressed("KeyF") {
		g.setView(ctx, !g.FirstPerson)
	}
	if g.FirstPerson {
		// A product that feeds a sum is wrapped in float32(...). The explicit rounding keeps
		// arm64 from fusing it into one multiply-add, so the trace hash and the goldens are
		// the same on arm64 and amd64. The kinds follow the same rule.
		g.Yaw = gmath.Wrap(g.Yaw-float32(in.MouseDelta.X*MouseSensitivity), 360)
		g.Pitch = min(MaxPitchDeg, max(-MaxPitchDeg, g.Pitch-float32(in.MouseDelta.Y*MouseSensitivity)))
		if p := ctx.Scene.Find("player"); p != nil {
			eye := p.WorldPosition().Add(gmath.V3(0, EyeHeight, 0))
			ctx.Scene.Camera = g.ThirdCam.LookFrom(eye, g.Yaw, g.Pitch)
		}
	}
}

// setView switches between the scene camera and a camera at the hero's eyes. Seen from
// the inside the hero's own model would fill the screen, so it is hidden in first person;
// the scene camera is kept to come back to.
func (g *Game) setView(ctx *veduta.Context, firstPerson bool) {
	p := ctx.Scene.Find("player")
	if firstPerson {
		g.ThirdCam = ctx.Scene.Camera
		g.Pitch = 0
		if p != nil {
			g.Yaw = p.Transform.Rotation.EulerDeg().Y
			p.Visible = false
		}
	} else {
		ctx.Scene.Camera = g.ThirdCam
		if p != nil {
			p.Visible = true
		}
	}
	g.FirstPerson = firstPerson
	// In the player window the cursor is hidden and kept inside while looking around.
	ctx.LockPointer(firstPerson)
	ctx.Trace("view", map[string]any{"first_person": firstPerson})
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
	if g.FirstPerson {
		// Halving and doubling are products too: rounded explicitly, as in Update.
		cx, cy := float32(float32(ctx.Width)/2), float32(float32(ctx.Height)/2)
		arm, thick := float32(4*scale), float32(scale)
		hud.Rect(gmath.R(cx-arm, cy-float32(thick/2), float32(2*arm), thick), 0xfff4f0e0)
		hud.Rect(gmath.R(cx-float32(thick/2), cy-arm, thick, float32(2*arm)), 0xfff4f0e0)
		// Bottom right: contact sheets label their tiles at the bottom left.
		hint := "FIRST PERSON  F: BACK"
		ctx.Text(hud, float32(ctx.Width-margin-len(hint)*cell), float32(ctx.Height-margin-cell), scale, hint, 0xff8090a0)
	}
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
