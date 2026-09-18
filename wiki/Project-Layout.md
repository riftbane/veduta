# Project Layout

`veduta init mygame` (or **Veduta: New Game**) creates:

```
mygame/
├── main.lua                  the game's code
├── veduta.json               the manifest: name, engine version, defaults
├── card.json                 how the console's dashboard lists the game
├── assets/
│   ├── scenes/main.vscene
│   ├── models/quad.vmodel          a 1 × 1 square, the shape of every sprite
│   └── materials/sprite.vmat       the settings a sprite material needs
├── tests/scenarios/start.vscenario
├── .github/workflows/release.yml   publishes the game when you tag a version
├── .veduta/, .vscode/, .luarc.json editor setup: API definitions and JSON schemas
├── CLAUDE.md, .mcp.json            for an AI agent working on the game (veduta mcp)
├── README.md, CHANGELOG.md, .gitignore
```

A `.git` repository is created too. The tool writes what it produces (test runs, renders,
fuzz results, and the simulator's [saves](Saves)) under `out/`, and compiled assets under `assets/.cooked/`: both are ignored
by git and never need editing.

## The Project view

In VS Code, the Veduta icon in the activity bar opens the game as a tree: **Game**
(`veduta.json`, `card.json`, README, CHANGELOG), **Scripts**, **Scenes**, **Worlds**,
**Maps**, **Prefabs**, **Models**, **Materials**, **Textures**, **Images** (the PNG files under
`assets/`) and **Scenarios**, each with its folders. What the tool writes (`out/`,
`assets/.cooked/`, the editor files) is not there.

Right-click a section or a folder to make something in it: **New Scene…**, **New
Prefab…**, **New Script…** and so on, and **New Folder…**. You type the name only; the
file is written by `veduta new`, the same as in a terminal, so it builds as it is:

| New | Is |
|-----|----|
| scene | an empty scene with an orthographic camera |
| world | a flat world of one biome; the first one also brings the `ground` material and its tiling texture |
| map | a 16 × 12 map of one terrain, opened in the [map editor](Maps#drawing-it-with-the-editor); the first one also brings the `ground` texture |
| prefab | an empty prefab, 1 × 1 m |
| model | a 1 m box standing on its base |
| material | a light grey material |
| texture | a 32 × 32 grey texture |
| scenario | a run of 20 ticks from the game's start (or, **New Scenario Starting Here…** on a scene or world, from there) that checks the invariants |
| script | an empty Lua module, with the `require` that loads it |

A scene or a world also has **Play from Here**, a scenario **Run Scenario** and **Debug
Scenario**, a texture its preview; every file and folder can be renamed (an asset keeps its
extension, and its name stays unique in its kind) and deleted. Renaming an asset does not
change what refers to it: the build that follows shows those places in Problems.

## Where things go

| Path | Holds |
|------|-------|
| `*.lua` anywhere in the project | scripts; `main.lua` is the entry point, the others are modules loaded with `require` |
| `assets/scenes/<name>.vscene` | [scenes](Scenes) |
| `assets/models/<name>.vmodel` | [models](Graphics-Assets#models) |
| `assets/materials/<name>.vmat` | [materials](Graphics-Assets#materials) |
| `assets/textures/<name>.vtex` | [textures](Graphics-Assets#textures); PNG files they use can sit anywhere under `assets/` |
| `assets/worlds/<name>.vworld` | [worlds](3D-Worlds-and-Blocks#streamed-worlds) |
| `assets/maps/<name>.vmap` | [tile maps](Maps) |
| `assets/prefabs/<name>.vprefab` | groups of entities placed by worlds |
| `tests/scenarios/<name>.vscenario` | [scenarios](Testing) |
| `tests/golden/` | the recorded outcome of each scenario, written by `veduta test` |

An asset's name is its file name without the extension: `assets/materials/hero.vmat`
is the material `hero`. Names are 1 to 64 characters of `a-z`, `0-9`, `_` and `-`,
starting with a letter or a digit. Sources can be sorted into folders under their kind's
directory (`assets/materials/enemies/bat.vmat` is still the material `bat`), so a name
must be unique across the folders; `tests/scenarios/` stays flat. See
[Structuring a Large Game](Structuring-a-Large-Game).

Every source is a JSON file with the extension of its format (`.vscene`, `.vmodel`,
`.vmat`, `.vtex`, `.vworld`, `.vmap`, `.vprefab`, `.vscenario`; VS Code opens them as JSON) and
starts with a `"veduta"` header naming its format (`"scene/1"`, `"material/1"`, …). A
project made before v2.0.0-rc.8 has `crate.model.json` and so on: `veduta upgrade`
renames them. Decoding is strict: an unknown field, a duplicate key or a wrong type is
an error with its file, line and column, and VS Code underlines it as you type.

## veduta.json

```json
{
  "veduta": "project/1",
  "name": "gemcave",
  "title": "Gem Cave",
  "engine": "v2.0.0-rc.5",
  "script": "main.lua",
  "icon": "icon.png"
}
```

| Field | Default | Meaning |
|-------|---------|---------|
| `veduta` | required | `"project/1"` |
| `name` | required | lowercase identifier, used for folders and archives |
| `title` | the `name` | what players see on the dashboard: spaces and accents allowed |
| `engine` | required | the engine version the game targets; `veduta upgrade` changes it |
| `script` | none | the main Lua file. **Its presence is what makes the project a Lua game.** |
| `api` | `1` | the Lua [API level](Publishing-to-the-Console#api-levels) the game needs |
| `icon` | none | a PNG outside `assets/`, shown beside the title on the dashboard |
| `resolution` | `[320, 240]` | the frame the game is designed for |
| `tick_rate` | `20` | ticks per second; the console shows 20 frames a second, keep 20 |
| `default_scene` | `"main"` | the scene the game starts in |
| `default_world` | none | a world to start in instead of a scene |
| `default_seed` | `1` | seed of the random generator when none is given |
| `invariants` | `[]` | checks run in every test, see [Testing](Testing#invariants) |
| `bounds` | ±100 m | the box the `within_bounds` invariant keeps entities in |

The full list is in the [project format](https://riftbane.github.io/veduta/project.html).

## card.json

The console's dashboard reads it to list the game:

```json
{
  "veduta": "card/1",
  "title": "Gem Cave",
  "name": "gemcave"
}
```

`title` and `name` must agree with `veduta.json`; `veduta doctor` says when they do not.
`veduta deploy` adds the version and the icon to the copy it puts on the card.

## Editor files

`.veduta/lua/veduta.d.lua` describes the API to the Lua extension (completion, hover,
checks), `.veduta/schema/` holds the JSON schemas of every format, and `.vscode/` and
`.luarc.json` point the editor at them. `veduta upgrade` refreshes them when the engine
changes: do not edit them by hand.
