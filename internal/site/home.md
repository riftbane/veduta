# Veduta

Veduta is a small game console and the kit to make its games. The console is an Orange Pi
Zero 2W behind a 320 × 240 panel with eight buttons, running
[VedutaOS](https://github.com/riftbane/vedutaos); its games are written in Lua and made on a
PC with VS Code, played in a simulator that behaves as the console does, and tested by
scenarios that give the same result on every machine.

## Start here

- [Getting started on Windows](start-windows.md): install, make a game, play it, put it on
  the console.
- [Your first game](first-game.md): star catcher, a complete small game, step by step.
- [Lua API](lua.md): everything a game's script can do.

## The console

- A 320 × 240 panel, 20 frames a second, 16-bit colour.
- Eight buttons for games: the D-pad, A, B, Select (the game's menu) and Cancel (back);
  Home leaves a game.
- Games live on its SD card, one folder each; there is no store and no network.

## Reference

The pages under Assets describe the JSON formats of scenes, models, materials, textures,
prefabs and worlds; Scenarios the tests; Tools the inspection reports. The same pages are
the `docs` tool that AI agents read through `veduta mcp`.
