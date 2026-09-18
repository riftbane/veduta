# Veduta for VS Code

Make games for the Veduta console in VS Code. The extension runs the `veduta` tool of the
project for you; install the tool first (on Windows, in PowerShell:
`irm https://raw.githubusercontent.com/riftbane/veduta/main/install.ps1 | iex`).

**The Project view** (the Veduta icon in the activity bar) shows the game as you work on
it: Game (`veduta.json`, `card.json`, README, CHANGELOG), Scripts, Scenes, Worlds, Prefabs,
Models, Materials, Textures, Images and Scenarios, each in its own folders, without what
the tool writes (`out/`, cooked assets, editor files). Right-click a section or one of its
folders for **New Scene…**, **New Prefab…**, **New Script…** and so on, or **New Folder…**:
`veduta new` writes a file the engine accepts as it is (a world comes with the ground it
stands on) and it opens. A name is checked as you type it: the engine's rule, and free in
every folder of its kind. On a scene or a world, **Play from Here** and **New Scenario
Starting Here…**; on a scenario **Run** and **Debug**; on a texture the preview; on any
file or folder **Rename…** (an asset keeps its extension) and **Delete** (to the trash).
The view follows files made or removed anywhere else.

| Command | Does |
|---|---|
| **Veduta: New Game** | `veduta init`: an empty Lua game (or a Go one), opened in a new window |
| **F5** | plays the game with the debugger (`veduta dap`): breakpoints, stepping, the stack, locals and entities; Ctrl+F5 without it |
| **Veduta: Play in the Simulator** (▶ Veduta in the status bar) | `veduta sim` on Windows, `veduta run` on Linux, in the terminal panel |
| **Veduta: Test** | `veduta test`: the game's scenarios |
| **Veduta: Build** | `veduta build`, its errors in Problems; also on every save of a script or an asset |
| **Veduta: Preview the Texture** (the eye in the title bar of a `*.vtex`) | draws the texture beside its source while you write it |
| **Veduta: Deploy to the Console's Card** | `veduta deploy`: the game onto the card, found by its label |

The preview runs no engine: it draws the layer program in the panel itself, the same way
the engine does (its tests compare the two pixel by pixel), so it follows every keystroke.
It has a grid and a ruler to measure the picture by, reads the texel and the colour under
the pointer, measures a rectangle you drag over it, switches layers off one at a time, and
shows a tiling texture repeated. A source the engine would refuse is not drawn: the panel
names the field that is wrong.

The same commands are tasks of type `veduta` (`"command": "sim"`, `"test"`, `"build"`,
`"deploy"`, with `"args"` such as `["--scene", "level1"]`) for `tasks.json`, and `$veduta` is a problem matcher for their output.

A project made by `veduta init` also recommends the Lua extension (`sumneko.lua`) and points
it at the API's definitions, and maps the JSON Schemas of `veduta.json`, scenes, scenarios
and assets: completion and checks while typing.

Debug configurations (`.vscode/launch.json`) are of type `veduta`: `"mode": "play"` or
`"mode": "scenario"` with `"scenario": "<name>"`, and `"stopOnEntry"`.

The extension and the `veduta` tool are released together. When the extension is older
than the one that came with the tool, it says so, and **Update the Extension** installs
that one (`veduta extension` fetches it, checksum verified); reload the window after.

Settings: `veduta.path` (the program, when it is not on PATH or where the installer puts it)
and `veduta.buildOnSave` (default on).
