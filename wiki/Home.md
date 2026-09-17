# Veduta: making games in Lua

Veduta is a game engine for a small handheld console: a 320 × 240 panel, a D-pad, A, B,
Select, Cancel and Home, 20 frames a second. You write the game in **Lua**, test it on your
PC in a simulator window, and copy it to the console's SD card. No compiler, no build step:
the engine runs your scripts directly, on the PC and on the console alike.

Every run is deterministic. The same buttons pressed at the same ticks always give the same
game, on every machine, so a play session can be saved as a test and replayed forever.

> These pages describe **Veduta v2**, whose Lua games are still in release candidates
> (`v2.0.0-rc.N`). Install from the beta channel, see [Installation](Installation).

## Start here

1. [Installation](Installation): the `veduta` tool and the VS Code extension.
2. [Your First Game](Your-First-Game): *Gem Cave*, a small complete game built step by step.
3. [How a Game Runs](How-a-Game-Runs): ticks, callbacks, kinds, and the rules scripts follow.

## Writing the game

| Page | What it covers |
|------|----------------|
| [Project Layout](Project-Layout) | the files of a game, `veduta.json`, `card.json` |
| [Entities](Entities) | the things in the game: fields, methods, spawning, collisions |
| [Scenes](Scenes) | scene files: camera, light, the starting entities |
| [Graphics Assets](Graphics-Assets) | models, materials and textures as JSON |
| [Input](Input) | the console's buttons, keyboard and gamepad |
| [Camera](Camera) | 2D and 3D cameras, following the player |
| [HUD](HUD) | text, icons and dialogue boxes over the frame |
| [2D Games](2D-Games) | sprites, depth, layers, hitboxes, the traps |
| [3D, Worlds and Blocks](3D-Worlds-and-Blocks) | perspective scenes, streamed worlds, block worlds built at runtime |
| [Structuring a Large Game](Structuring-a-Large-Game) | folders, modules, shared state, data tables, sequences |

## Making it solid

| Page | What it covers |
|------|----------------|
| [Testing](Testing) | scenarios, expectations, invariants, goldens, fuzzing |
| [Debugging and Performance](Debugging-and-Performance) | breakpoints, errors, the tick budget, `bench` |
| [Publishing to the Console](Publishing-to-the-Console) | the SD card, releases, API levels |

## Reference

- [Lua API Reference](Lua-API-Reference): every function and field a script can use.
- [Command Line](Command-Line): the `veduta` commands.
- [FAQ and Troubleshooting](FAQ-and-Troubleshooting): common problems and current limits.

The format references (every field of every JSON file) are on the documentation site,
<https://riftbane.github.io/veduta/>.
