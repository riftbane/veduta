# Changelog

## 0.9.0

- **The map editor**: a `*.vmap` opens painted (**Veduta: Open as JSON** in its title bar
  shows the text; **Veduta: Open in the Map Editor** goes back). Brush (1, 2, 3 or 5 cells),
  rectangle, bucket fill, eraser, eyedropper (Alt+click) and objects (drag to make one, a
  form for its name, cells, tags and props); a palette of the terrains with their textures,
  to add from the project's textures, rename, re-key, tag or delete; layers to show or
  hide (in the editor only), add, rename, reorder, delete, with their z and draw order; the
  map's size, tile and origin. Zoom with the wheel, pan with the middle button or
  Space+drag, a grid, the cell under the pointer with its terrains and tags, and the
  textures' clips played at the game's tick rate. It draws what the engine draws (the tests
  compare it with the engine's pictures pixel by pixel, borders and all) and every stroke
  is one edit of the text, written the one canonical way: undo, redo and save are VS
  Code's. A map the engine would refuse shows its errors, with line and column.
- The Project view has Maps, with **New Map…** (`veduta new map`).
- The texture preview draws sheets (`grid`, `frames`) and image `rect`s, and reports clips,
  `play` and `edge` as the engine does.
- Needs veduta v2.0.0-rc.11 or later.

## 0.8.0

- Says when it is older than the extension released with the `veduta` tool, and
  **Update the Extension** installs that one (fetched by `veduta extension`, checksum
  verified). Needs veduta v2.0.0-rc.10 or later for the check; older tools do not say.

## 0.7.0

- The Project view, in the activity bar: the game's scripts, scenes, worlds, prefabs,
  models, materials, textures, images and scenarios in their folders, and the project's
  own files, without what the tool writes. Right-click to make a new one of each (with
  `veduta new`, so it builds as it is) or a folder, rename, delete, play from a scene or a
  world, run or debug a scenario, preview a texture. Needs veduta v2.0.0-rc.9 or later.

## 0.6.0

- Sources have an extension per format (`.vmodel`, `.vtex`, `.vmat`, `.vscene`,
  `.vscenario`, `.vprefab`, `.vworld`, engine v2.0.0-rc.8): they open as JSON, saving one
  builds, and the texture preview is for `*.vtex`.

## 0.5.0

- **Veduta: Preview the Texture** (the eye in the title bar of a `*.tex.json`): a panel
  beside the source that draws the texture while you write it, without saving and without
  running the engine. A grid and a ruler to measure it by, the texel and the colour under
  the pointer, a drag to measure a rectangle, a switch per layer, and the 2x2 repeat of a
  tiling texture. What it draws is the engine's own layer program, compared pixel by pixel
  with the engine's golden images; a source the engine would refuse is not drawn, the panel
  names the field instead.

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
