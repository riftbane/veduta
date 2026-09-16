// Command game is the engine's test game: a fixture its tests build and run headless. It
// is not shipped by veduta init.
package main

import (
	"github.com/riftbane/veduta"
	"github.com/riftbane/veduta/internal/testgame/game"
)

func main() { veduta.Run(&game.Game{}) }
