// Package veduta is a headless, deterministic game engine written in pure Go.
//
// A game implements [Game] and calls Run from its main function. The same binary plays on
// a console, drawing on its panel's framebuffer, and, with -headless, renders, simulates
// and answers queries for the veduta tool on a server with no display.
package veduta
