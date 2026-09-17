# Project Layout

`veduta init mygame` (or **Veduta: New Game**) creates:

```
mygame/
├── main.lua                  the game's code
├── veduta.json               the manifest: name, engine version, defaults
├── card.json                 how the console's dashboard lists the game
├── assets/
│   ├── scenes/main.scene.json
│   ├── models/quad.model.json      a 1 × 1 square, the shape of every sprite
│   └── materials/sprite.mat.json   the settings a sprite material needs
├── tests/scenarios/start.scenario.json
├── .github/workflows/release.yml   publishes the game when you tag a version
├── .veduta/, .vscode/, .luarc.json editor setup: API definitions and JSON schemas
├── CLAUDE.md, .mcp.json            for an AI agent working on the game (veduta mcp)
├── README.md, CHANGELOG.md, .gitignore
```

A `.git` repository is created too. The tool writes what it produces (test runs, renders,
fuzz results) under `out/`, and compiled assets under `assets/.cooked/`: both are ignored
by git and never need editing.

## Where things go

| Path | Holds |
|------|-------|
| `*.lua` anywhere in the project | scripts; `main.lua` is the entry point, the others are modules loaded with `require` |
| `assets/scenes/<name>.scene.json` | [scenes](Scenes) |
| `assets/models/<name>.model.json` | [models](Graphics-Assets#models) |
| `assets/materials/<name>.mat.json` | [materials](Graphics-Assets#materials) |
| `assets/textures/<name>.tex.json` | [textures](Graphics-Assets#textures); PNG files they use can sit anywhere under `assets/` |
| `assets/worlds/<name>.world.json` | [worlds](3D-Worlds-and-Blocks#streamed-worlds) |
| `assets/prefabs/<name>.prefab.json` | groups of entities placed by worlds |
| `tests/scenarios/<name>.scenario.json` | [scenarios](Testing) |
| `tests/golden/` | the recorded outcome of each scenario, written by `veduta test` |

An asset's name is its file name without the extension: `assets/materials/hero.mat.json`
is the material `hero`. Names are 1 to 64 characters of `a-z`, `0-9`, `_` and `-`,
starting with a letter or a digit. Sources can be sorted into folders under their kind's
directory (`assets/materials/enemies/bat.mat.json` is still the material `bat`), so a name
must be unique across the folders; `tests/scenarios/` stays flat. See
[Structuring a Large Game](Structuring-a-Large-Game).

Every JSON file starts with a `"veduta"` header naming its format (`"scene/1"`,
`"material/1"`, …). Decoding is strict: an unknown field, a duplicate key or a wrong type is
an error with its file, line and column, and VS Code underlines it as you type.

## veduta.json

```json
{
  "veduta": "project/1",
  "name": "gemcave",
  "title": "Gem Cave",
  "engine": "v2.0.0-rc.4",
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
