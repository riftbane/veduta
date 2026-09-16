# Veduta for VS Code

Make games for the Veduta console in VS Code. The extension runs the `veduta` tool of the
project for you; install the tool first (on Windows, in PowerShell:
`irm https://raw.githubusercontent.com/riftbane/veduta/main/install.ps1 | iex`).

| Command | Does |
|---|---|
| **Veduta: New Game** | `veduta init`: an empty Lua game (or a Go one), opened in a new window |
| **Veduta: Play in the Simulator** (F5, or ▶ Veduta in the status bar) | `veduta sim` on Windows, `veduta run` on Linux, in the terminal panel |
| **Veduta: Test** | `veduta test`: the game's scenarios |
| **Veduta: Build** | `veduta build`, its errors in Problems; also on every save of a script or an asset |
| **Veduta: Deploy to the Console's Card** | `veduta deploy`: the game onto the card, found by its label |

The same commands are tasks of type `veduta` (`"command": "sim"`, `"test"`, `"build"`,
`"deploy"`) for `tasks.json`, and `$veduta` is a problem matcher for their output.

A project made by `veduta init` also recommends the Lua extension (`sumneko.lua`) and points
it at the API's definitions, and maps the JSON Schemas of `veduta.json`, scenes, scenarios
and assets: completion and checks while typing.

Settings: `veduta.path` (the program, when it is not on PATH or where the installer puts it)
and `veduta.buildOnSave` (default on).
