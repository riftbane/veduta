// Command game is the game's entry point. On a console it plays on the panel's
// framebuffer; the veduta tool runs it with -headless to render, simulate and query.
package main

import (
	"github.com/riftbane/veduta"
	"github.com/riftbane/veduta/template/game"
)

func main() { veduta.Run(&game.Game{}) }
