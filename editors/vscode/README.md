# Veduta for VS Code

Make games for the Veduta console in VS Code. The extension runs the `veduta` tool of the
project for you; install the tool first (on Windows, in PowerShell:
`irm https://raw.githubusercontent.com/riftbane/veduta/main/install.ps1 | iex`).

**The Project view** (the Veduta icon in the activity bar) shows the game as you work on
it: Game (`veduta.json`, `card.json`, README, CHANGELOG), Scripts, Scenes, Worlds, Maps,
Prefabs, Models, Materials, Textures, Images and Scenarios, each in its own folders, without what
the tool writes (`out/`, cooked assets, editor files). Right-click a section or one of its
folders for **New Scene…**, **New Map…**, **New Tile…**, **New Prefab…**, **New Script…** and so on, or **New Folder…**:
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
| **Veduta: Open as JSON** / **Open in the Map Editor** (title bar of a `*.vmap`) | the map's text, or the map editor again |
| **Veduta: Open in the Tile Editor** / **Open as JSON** (title bar of a `*.vtex`) | the texture's pixels, or its text |
| **Veduta: Deploy to the Console's Card** | `veduta deploy`: the game onto the card, found by its label |

The preview runs no engine: it draws the layer program in the panel itself, the same way
the engine does (its tests compare the two pixel by pixel), so it follows every keystroke.
It has a grid and a ruler to measure the picture by, reads the texel and the colour under
the pointer, measures a rectangle you drag over it, switches layers off one at a time, and
shows a tiling texture repeated. A source the engine would refuse is not drawn: the panel
names the field that is wrong.

**The map editor** opens every map (`*.vmap`, [the format](https://github.com/riftbane/veduta/blob/main/docs/map.md)):
the map as the game draws it, with the textures of the project, borders and all.

| Tool | Key | Does |
|---|---|---|
| Brush | B, `[` `]` for its size (1, 2, 3, 5) | paints the terrain chosen in the palette on the layer chosen |
| Rect | R | fills the rectangle dragged |
| Fill | F | fills the cells of the same terrain around, side by side, on the layer |
| Erase | E | empties cells |
| Pick | I, or Alt+click | takes the terrain of a cell (and its layer) |
| Object | O | drag to make an object (`object_1`…), click one to edit its name, cells, tags and props (`key = value` lines), Delete to remove it |

1–9 choose a terrain, H the grid; the wheel zooms around the pointer, the middle button or
Space+drag pans, **Fit** shows the whole map; **Animate** plays the textures' clips at the
game's tick rate. The status line names the cell under the pointer and the terrain of every
layer there, with its tags. The palette shows every terrain with its texture: **Add** one
from a texture of the project, change its name, key or tags, or delete it (its cells are
emptied, after asking). Layers are listed top first: choose the one to paint on, hide one
while you paint (only in the editor), add, rename, move, delete, set its z and draw order.
**Map** resizes it (from the top-left corner), and sets its tile and origin.

Every gesture is one edit of the file's text, written the same way every time (a terrain,
a row, an object per line), so Ctrl+Z, Ctrl+Y and Ctrl+S work as in any file, and the text
can be edited beside it. A map the engine would refuse is not edited: the editor lists its
errors with line and column, and **Open as JSON** opens the text to fix it.

**The tile editor** draws textures that are one PNG, pixel by pixel: **New Tile…** (on
Textures in the Project view, or its +) makes a **tile**, an **animated tile** or an
**autotile** (an island and a lake, 17 tiles a map picks by neighbours:
[the format](https://github.com/riftbane/veduta/blob/main/docs/texture.md#autotiles)), and
a tile opens in it from the Project view (**Open in the Tile Editor** in the title bar of
any `*.vtex`; **Open as JSON** goes back).

| Tool | Key | Does |
|---|---|---|
| Pencil | B, Shift+click for a line from the last point | draws with the first color (left button) or the second (right button) |
| Erase | E | transparent pixels |
| Line, Rect | L, R (Shift fills) | the line or rectangle dragged |
| Fill | G | the pixels of the same color around |
| Pick | I, or Alt+click | takes a pixel's color |
| Select | M | drag a rectangle, drag it to move it; Ctrl+C, Ctrl+X, Ctrl+V (between tile editors too), Delete, F / Shift+F to flip, T to turn, Enter to place, Escape to put it back |

X swaps the two colors, H the grid; the wheel zooms, the middle button or Space pans.
Frames sit below the image: **Copy frame** (D) puts a copy of the frame after it to change,
**New frame**, **Delete**, ◀ ▶ to reorder, `[` `]` to go through them, **Onion skin** (O)
shows the frame before faintly, and the fps. The preview repeats a tile 3 × 3 times (its
seams show), plays the frames (P), and paints a small map with an autotile the way the
engine does; **Start the lake from the island** copies the island's sides into the lake,
for you to draw the inner corners. Ctrl+S writes the PNG (frames side by side) and the
`.vtex` (its size, grid, the `play` clip at your fps, `autotile`; its other fields kept);
undo and redo are VS Code's.

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
