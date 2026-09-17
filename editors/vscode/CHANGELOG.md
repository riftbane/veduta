# Changelog

## 0.4.0

- Debug configurations of mode `play` take `scene` (or `world` and `at`) and `seed`: the
  game starts there, and the snippet "Veduta: Play a scene" writes one. Needs veduta
  v2.0.0-rc.6 or newer, whose simulator reloads a saved file in place, in the scene the
  game is in, and keeps its window open on a script error.

## 0.3.0

- A warning when the `veduta` tool is missing or is a v1 (which makes Go games), with a
  link to the installation page.

## 0.2.0

- Debugging: F5 plays the game under the debugger (`veduta dap`), Ctrl+F5 without; debug
  configurations of type `veduta` play the game or run a scenario. F5 no longer runs the
  Play task.

## 0.1.0

- New Game, Play in the Simulator (F5), Test, Build (errors in Problems, on save too) and
  Deploy, as commands and as `veduta` tasks.
