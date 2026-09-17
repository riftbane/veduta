# Lua games (`lua`)

A script game is written in Lua 5.4. `veduta.json` names its main script, and the tool
runs it itself: no Go code, no build, the same commands (`build` checks the scripts,
`test`, `simulate`, `render`, `bench`, `fuzz`, `run`) and the same MCP tools.

```json
{ "veduta": "project/1", "name": "mygame", "engine": "v2.0.0", "script": "main.lua" }
```

Every `.lua` file of the project is read when a run starts (the directories `out`, `bin`,
the cooked assets and hidden ones are skipped). The main script runs once per run, before
the scene is loaded; it fills the `game` and `kinds` tables. A run is deterministic: the
same seed and the same inputs give the same trace on every machine.

## Playing it

`veduta sim` (Windows) plays the game in the simulator: the console's panel at a whole scale,
in its 16-bit colors, with the keyboard pressing the buttons (arrows or W A S D, Space or Z
for A, X or Shift for B, Enter or Tab for Select, Escape or Backspace for Cancel; Ctrl+Q
leaves) or any pad. Three keys work the player itself:

| Key | Does |
|-----|------|
| F1 | shows update, render and frame milliseconds against the tick's budget, and triangles against the console's 1200 |
| F5 | restarts the game and records the buttons; F5 again saves them as `tests/scenarios/recorded-<time>.scenario.json`, a scenario to add expectations to |
| F9 | reads the scripts and assets again and restarts the game from the start |

### Editing while it plays

The simulator watches the scripts and the asset sources. When one is saved it reads
everything again (a changed scene, texture or material is compiled on the spot) and
restarts **in place**: in the scene the game was in, or in its world around the cell the
player stands in, with the same seed. So a level is laid out with the scene file open next
to the simulator, and a hud is written in `game.draw` while it shows: every save shows the
result a second later, without playing back to it. The run starts again at tick 0 there;
`game.init` runs again, so a game whose `init` loads its title scene comes back to the
title.

`veduta sim --scene level3` (or `--world land --at 4,-2`, and `--seed N`) opens the
simulator in that scene rather than the project's default; the debugger's `"scene"` does
the same in VS Code (the snippet "Veduta: Play a scene"). F5 records from there, and the
scenario it writes names that scene.

An error stops the run but not the simulator: the last frame stays, the error (a script's
file, line and traceback; an asset that does not compile; a scene that does not load) is
written over it, and the next save that fixes it reloads. F9 does the same from the start.

## Debugging

`veduta dap` is a debug adapter (the Debug Adapter Protocol, on stdin and stdout), which VS
Code's Veduta extension starts on F5. It plays the game (`"mode": "play"`, the simulator on
Windows) or runs a scenario headless (`"mode": "scenario"`, `"scenario": "collect"`) with a
debugger on the scripts: breakpoints, pause, stepping over, into and out of calls (into a
module `require` runs too), the call stack, each call's locals and upvalues and the
globals, tables and entities opened field by field, and a name with its fields
(`e.state.score`) evaluated on hover or as a watch. `"stopOnEntry": true` stops before the
first statement. Ctrl+F5 runs without it. While the game is stopped the simulator's window
does not redraw. A run under the debugger gives the same trace as one without.

## Editing

`veduta init` sets an editor up, and `veduta upgrade` brings it to the tool's version:
`.veduta/lua/veduta.d.lua` describes this API for the Lua Language Server (VS Code's
`sumneko.lua`, which `.vscode/extensions.json` recommends; `.luarc.json` points it there
and turns off `io`, `os`, `debug` and `package`), and `.veduta/schema/` holds a
JSON Schema of every source format, which `.vscode/settings.json` maps to `veduta.json`,
`*.scene.json`, `*.scenario.json` and the other asset files. The editor then completes the
API and the fields of every file, shows their descriptions, and marks a misspelt function,
a button that does not exist or a field the format does not have. `upgrade` sets only its
own keys in `.vscode/settings.json`, `.vscode/extensions.json` and `.luarc.json`, and leaves
one with comments in it as it is.

## The game

```lua
function game.init()        -- once, after the first scene is loaded
end

function game.update()      -- once per tick (20 per second), before the entities
end

function game.draw()        -- once per rendered frame, after the scene: the hud
end
```

All three are optional. A Lua error in any of them, or in a kind, stops the run with the
script's file, line and traceback.

## Kinds

An entity whose `kind` names an entry of `kinds` gets its behaviour from it:

```lua
kinds.coin = {
  init = function(e)         -- when the entity is loaded or spawned
    e.state.value = 1
  end,
  update = function(e)       -- once per tick, in entity id order, after game.update
    e:set_rotation(0, engine.tick * 4.5, 0)
  end,
}
```

`static`, `camera` and `light` are built-in kinds. A scene that names a kind no script
defines fails to load.

## Entities

An entity is a value with fields and methods. The same entity is always the same value, so
`a == b` compares entities.

| Field | Meaning |
|-------|---------|
| `id`, `name`, `kind` | read only |
| `alive` | false once despawned (read only) |
| `x`, `y`, `z` | position, relative to the parent |
| `visible` | drawn or not |
| `model`, `material` | asset names, or nil |
| `layer` | first key of the draw order |
| `frame` | the frame shown when the material has a `grid` (a sprite sheet): an integer from 0, left to right then top to bottom, wrapping around the grid's frames. Animate with `e.frame = engine.tick // 4 % 6` |
| `parent` | the parent entity, or nil; set it to an entity (or its name) or nil. The position, rotation and scale are relative to the parent, so an entity given a parent moves with it from then on |
| `hitbox` | `{{min x, y, z}, {max x, y, z}}` in the entity's own space, or nil: replaces the model's bounds for collisions, as a scene file's `hitbox` |
| `state` | a table of your own values; the trace records it as `state.<key>`, so scenarios can check it (`state.score`). Entities of a Lua kind start with an empty one. |

Setting any other field is an error: keep your own values in `state`.

| Method | Meaning |
|--------|---------|
| `e:position()` | x, y, z |
| `e:set_position(x, y, z)` | |
| `e:move(dx, dy, dz)` | adds to the position |
| `e:world_position()` | x, y, z in the world |
| `e:rotation()` | Euler angles in degrees, x, y, z |
| `e:set_rotation(x, y, z)` | degrees |
| `e:scale()`, `e:set_scale(x, y, z)` | |
| `e:has_tag(t)`, `e:add_tag(t)`, `e:remove_tag(t)`, `e:tags()` | |
| `e:overlapping([tag])` | the live entities whose bounds overlap this one's (last tick's), in id order, only those with `tag` when given |
| `e:bounds()` | min x, y, z, max x, y, z, or nil for an entity without bounds |
| `e:children()` | the live entities whose parent is this one, in id order |
| `e:despawn()` | removed at the end of the tick, with its children |

## scene

| Function | Meaning |
|----------|---------|
| `scene.name()` | the scene's name |
| `scene.find(name)` | the entity, or nil |
| `scene.tagged(tag)` | a list of entities, in id order |
| `scene.entities()` | every live entity, in id order |
| `scene.spawn{...}` | adds an entity and returns it; fields `kind` (default `static`), `name`, `model`, `material`, `position`, `rotation` (degrees), `scale` (each `{x, y, z}`), `tags` (a list), `visible`, `layer`, `frame`, `parent` (an entity or a name), `hitbox` (`{{min}, {max}}`), `state` (a table merged into the kind's) |
| `scene.spawn_prefab(name, x, y, z [, rotation [, prefix]])` | adds the entities of `assets/prefabs/<name>.prefab.json` with the min corner of its footprint at (x, y, z), turned by `rotation` (0, 90, 180 or 270 degrees about +Y) as a world places it, named `<prefix>_<entity>` (prefix defaults to the prefab's name) and parented as the prefab says. Returns a table listing them in prefab order that also holds each under its name in the prefab (`house.door`) |
| `scene.load(name)` | replaces the scene, as a reset |

The scene does not exist while the main script runs: call these from `game.init` on.

## input

The console has eight buttons: `up`, `down`, `left`, `right`, `a`, `b`, `select` (the
game's menu) and `cancel` (back). Home leaves the game and never reaches it.

| Function | Meaning |
|----------|---------|
| `input.down(button)` | held |
| `input.pressed(button)` | went down this tick |
| `input.released(button)` | went up this tick |
| `input.dpad()` | x (−1 left, 0, 1 right), y (−1 down, 0, 1 up) |

## camera

| Function | Meaning |
|----------|---------|
| `camera.get()` | `{position = {x, y, z}, target = {x, y, z}, ortho, fov, size, near, far}` |
| `camera.set{...}` | changes the fields given, the others stay |
| `camera.follow2d(x, y, height)` | the orthographic camera of a 2D game, looking at (x, y) and showing `height` units |

## world

| Function | Meaning |
|----------|---------|
| `world.load(name, cx, cz)` | replaces the scene with a world streamed around cell (cx, cz) |
| `world.name()` | the loaded world, or nil |
| `world.focus(x, y, z)` | where the chunks follow; call it every tick |
| `world.height(x, z)` | the ground's height |
| `world.water(x, z)` | the water level there, or nil |

## hud

Only inside `game.draw`, except `hud.text_width` and `hud.wrap`, which measure. Coordinates
are pixels from the top left of the frame.

| Function | Meaning |
|----------|---------|
| `hud.text(x, y, text [, color [, scale]])` | the built-in 8×8 font; returns the width drawn. The font has ASCII, Latin-1 (à è é ì ò ù ç ñ ä ö ü ß …) and Windows-1252's extra characters (€ ‘ ’ “ ” – — …); others draw as `?`. `\n` starts a new line |
| `hud.text_width(text [, scale])` | the width in pixels `hud.text` would draw (count characters, not bytes: `#text` counts the two bytes of `è`); usable anywhere |
| `hud.wrap(text, width [, scale])` | the text broken into lines at most `width` pixels wide, at spaces (a longer word is cut), and the number of lines; usable anywhere |
| `hud.rect(x, y, w, h [, color])` | a filled rectangle |
| `hud.image(texture, x, y [, options])` | a texture asset, or a part of it, with its texels sharp. `options`: `src = {x, y, w, h}` the part in texels (an icon of a sheet; default all of it), `w`, `h` the size drawn in pixels (default the part's), `color` multiplying the texels, `flip_x`, `flip_y` |
| `hud.panel(texture, x, y, w, h, border [, options])` | a nine-slice panel of any size: the corners, `border` texels wide (one number, or `{left, top, right, bottom}`), keep their size; edges and middle stretch. `options`: `src`, `color` |
| `hud.image_size(texture)` | the texture's width and height in texels |

A color is `"#rrggbb"`, `"#rrggbbaa"` or an integer `0xrrggbb`; the default is white. A texture
drawn in the hud is a texture asset like any other (`assets/textures/<name>.tex.json`,
often a PNG image layer): an icon sheet is one texture and `src` picks each icon.

## engine

`engine.tick` (0 in `game.init`), `engine.dt` (seconds per tick), `engine.width`,
`engine.height` (the frame: the project resolution, or in `game.draw` the frame being
drawn), `engine.headless`, the project's `engine.name` and `engine.title`, and `engine.api`,
the API level of the runtime (1 in v2.0.0). A game that needs a later level says so with
`"api"` in `veduta.json`, and an older console refuses it with a message.

## mesh and volume

Models built while the game runs: the terrain of a block world, a shape that changes. The
loops run in the engine, not in Lua, so a whole chunk of blocks is one call.

| Function | Meaning |
|----------|---------|
| `mesh.new()` | an empty mesh |
| `m:part([material])` | what follows is drawn with `material` (a material's name); without one, with the entity's |
| `m:quad(x1, y1, z1, x2, y2, z2, x3, y3, z3, x4, y4, z4)` | a quad, its corners counter-clockwise seen from the side that shows; UVs (0, 0), (1, 0), (1, 1), (0, 1) |
| `m:triangle(x1, y1, z1, x2, y2, z2, x3, y3, z3)` | a triangle, likewise |
| `m:box(x, y, z, w, h, d [, faces])` | the box from (x, y, z), w × h × d; `faces` names the faces to add, run together (`"+y-y"`), default all six |
| `m:triangles()` | how many triangles the mesh has |
| `mesh.set(name, m)` | makes `m` the model `name`, which entities name as `model` like an asset: the name contains `:` (`"game:chunk_0_0"`) and does not start with `world:`. It copies the mesh: change `m` and set it again to change the model. Models set here outlive `scene.load` |
| `mesh.remove(name)` | forgets the model; entities naming it draw nothing |
| `volume.new(x, y, z)` | a grid of blocks, each a block id from 0 (empty) to 255; at most 4 194 304 blocks |
| `v:get(x, y, z)`, `v:set(x, y, z, id)` | one block; cells count from 0 |
| `v:fill(x1, y1, z1, x2, y2, z2, id)` | every block of the box between two cells |
| `v:size()` | x, y, z |
| `mesh.voxels(v, materials [, size])` | a mesh of the faces between a block and an empty cell or the edge: one part per block id, drawn with `materials[id]` (a table of id → material name), each block `size` units (default 1), cell (0, 0, 0) at the origin |

```lua
local world = volume.new(16, 8, 16)
world:fill(0, 0, 0, 15, 2, 15, 1)       -- three layers of stone
world:fill(0, 3, 0, 15, 3, 15, 2)       -- grass on top
mesh.set("game:chunk", mesh.voxels(world, {[1] = "stone", [2] = "grass"}))
scene.spawn{name = "chunk", model = "game:chunk"}
```

## save

Saves outlive a run: progress, settings, high scores. A save is a table of numbers,
strings, booleans and tables of them, stored under a name (a valid asset name: `slot1`,
`settings`). Integers and floats come back as they went in, and tables in the order of their
keys, sorted.

| Function | Meaning |
|----------|---------|
| `save.write(name, table)` | stores the table as the save `name`, replacing any; `true`, or `nil` and a message when the storage fails. A value a save cannot hold (a function, an entity, a key that is neither a string nor part of a list, a table that holds itself) or a save over 1 MiB of JSON is an error |
| `save.read(name)` | the save as a new table, or `nil` when there is none (and a message when it cannot be read) |
| `save.remove(name)` | deletes the save; `true`, or `nil` and a message |
| `save.list()` | the names of the saves, sorted |

Where saves live depends on the run. The simulator and the console keep them in files, one
per save: the console on its card (`saves/<game>/`), the simulator in `out/saves` of the
project, or wherever `VEDUTA_SAVE_DIR` says. Tests never touch those: a run starts with the
saves its scenario lists (`"saves"` in the scenario topic) and keeps its writes in memory,
recording `save_write` and `save_remove` events a scenario can count.

```lua
function game.init()
  local data = save.read("slot1")
  if data then
    gold, level = data.gold, data.level
  end
end

local function on_checkpoint()
  local ok, err = save.write("slot1", {gold = gold, level = level, party = {"mira", "tobi"}})
  if not ok then
    message = "COULD NOT SAVE"   -- the card is full or missing; the game goes on
    print(err)
  end
end
```

## trace, invariant, require

| Function | Meaning |
|----------|---------|
| `trace(name [, fields])` | adds an event to the tick's trace; scenarios count them |
| `invariant(name, predicate)` | a check scenarios and `veduta.json` can list by name; the predicate returns true while it holds |
| `require(module)` | runs `module.lua` (dots are directories, relative to the main script) once and returns what it returned |

## The standard library

`string`, `table`, `math` and `utf8` behave as in Lua 5.4. `math.random` draws from the run's
seeded generator, so it is deterministic. `print` writes to the tool's error output.
`coroutine` works as in Lua 5.4, and a coroutine may even yield from inside a function the
engine calls back (a `table.sort` comparison). Differences from Lua 5.4: `pairs` visits keys
in the order they were first set; there is no `goto`, no `io`, `os`, `debug` or `load`, and
no `string.pack`, `string.unpack`, `string.packsize` or `string.dump`. A callback that runs for 20
million steps without returning is stopped as an endless loop.

## Full example

`main.lua` of a game where the D-pad moves a hero that collects coins:

```lua
local SPEED = 4
local score = 0

function game.init()
  invariant("score_non_negative", function() return score >= 0 end)
end

function game.draw()
  hud.text(4, 4, "SCORE " .. score)
end

kinds.hero = {
  update = function(e)
    local dx, dy = input.dpad()
    e:move(dx * SPEED * engine.dt, dy * SPEED * engine.dt, 0)
    for _, coin in ipairs(e:overlapping("coin")) do
      score = score + 1
      trace("coin_collected", {coin = coin.name, score = score})
      coin:despawn()
    end
    e.state.score = score
  end,
}
```
