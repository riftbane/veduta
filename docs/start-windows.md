# Getting started on Windows (`start-windows`)

From nothing to a game playing in the simulator, then on the console. A Lua game needs
the `veduta` tool and VS Code, nothing else: no compiler, no build.

## 1. Install

Install [VS Code](https://code.visualstudio.com) first, then in PowerShell:

```powershell
$env:VEDUTA_CHANNEL = "beta"
irm https://raw.githubusercontent.com/riftbane/veduta/main/install.ps1 | iex
```

The first line takes release candidates: v2, the version these pages describe, is one
until v2.0.0 is out; the stable channel still gives v1.

It puts `veduta.exe` in `%LOCALAPPDATA%\Programs\veduta`, adds that folder to your PATH
(no administrator rights) and installs the Veduta extension into VS Code. Open a new
terminal afterwards so it sees the PATH. `veduta version` answers when it worked. Running
the same lines again updates both.

The extension recommends the Lua extension (`sumneko.lua`) when you open a game: accept
it, and scripts get completion and checks against the API.

## 2. Make a game

In VS Code, Ctrl+Shift+P, **Veduta: New Game**, then Lua, a folder and a name: the game
opens in a new window. From a terminal it is `veduta init mygame` and `code mygame`.

| In the project | Is |
|---|---|
| `main.lua` | the game's code ([Lua API](lua.md)) |
| `assets/` | scenes, models, materials, textures: JSON files the editor completes |
| `tests/scenarios/` | runs with scripted buttons and what must hold ([Scenarios](scenario.md)) |
| `veduta.json` | the game's name, title, engine version ([veduta.json](project.md)) |
| `card.json` | how the console lists the game |
| `.veduta/`, `.vscode/`, `.luarc.json` | the editor's setup; `veduta upgrade` keeps it current |
| `CLAUDE.md`, `.mcp.json` | for an AI agent working on the game with `veduta mcp` |

A texture (`assets/textures/<name>.vtex`) is drawn beside its source while you write it:
the eye in the editor's title bar, or **Veduta: Preview the Texture** ([textures](texture.md)).

## 3. Play it

**F5** opens the simulator: the console's 320 × 240 panel
at a whole scale, in its 16-bit colours, 20 frames a second. The console's buttons are on
the keyboard, and any gamepad works too:

| Console | Keyboard |
|---|---|
| D-pad | arrows, or W A S D |
| A | Space or Z |
| B | X or Shift |
| Select (the game's menu) | Enter or Tab |
| Cancel (back) | Escape or Backspace |
| Home (leave the game) | Ctrl+Q |

Three keys work the simulator itself: **F1** shows the time each tick takes against the
console's budget, **F5** records what you play as a scenario (F5 again saves it), **F9**
reloads scripts and assets and restarts from the start. Saving a file reloads by itself,
in place: the game starts again in the scene it was in, so a level or a hud is edited
while it shows. An error stays on the screen until a save fixes it. `veduta sim --scene
NAME` (or a debug configuration with `"scene"`) opens the simulator in one scene.

Breakpoints work in the same run: click left of a line number in `main.lua`, and the game
stops there when it reaches it. The Run and Debug panel shows where it stopped, the local
variables, the entities opened field by field; F10 steps over, F11 into a call, Shift+F11
out, F5 continues. Ctrl+F5 plays without the debugger. To debug a scenario instead, add a
configuration in `.vscode/launch.json` (**Veduta: Scenario**).

## 4. Build and test

Saving a script or an asset builds the game: mistakes show in the Problems panel at their
line. **Veduta: Test** runs every scenario without a window; each run is the same on every
machine, so a scenario that passes here passes on the console. `veduta test` does the same
in a terminal.

## 5. On the console

The console reads games from its SD card. Take the card out, put it in the PC (it shows as
a drive named `VEDUTAOS`), then **Veduta: Deploy to the Console's Card** or `veduta
deploy`: the game lands in the card's `games` folder. Put the card back and the dashboard
lists it.

## 6. Keep going

- [Your first game](first-game.md): a small complete game, step by step.
- [Lua API](lua.md) and [2D games](2d.md).
- `veduta update` updates the tool; `veduta upgrade` in a project moves it to the tool's
  engine version.
- `veduta help` lists every command.
