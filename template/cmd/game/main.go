// Command game is the demo game. On a desktop it opens the player window; the veduta
// tool runs it with -headless to render, simulate and query.
package main

import (
	"github.com/riftbane/veduta"
	"github.com/riftbane/veduta/template/game"
)

func main() { veduta.Run(&game.Game{}) }
