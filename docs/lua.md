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
| F9 | reads the scripts and assets again and restarts; the simulator also does it by itself when a file changes |

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
and turns off `io`, `os`, `debug`, `coroutine` and `package`), and `.veduta/schema/` holds a
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
| `scene.spawn{...}` | adds an entity and returns it; fields `kind` (default `static`), `name`, `model`, `material`, `position`, `rotation` (degrees), `scale` (each `{x, y, z}`), `tags` (a list), `visible`, `layer`, `parent` (an entity or a name), `hitbox` (`{{min}, {max}}`), `state` (a table merged into the kind's) |
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

Only inside `game.draw`. Coordinates are pixels from the top left of the frame.

| Function | Meaning |
|----------|---------|
| `hud.text(x, y, text [, color [, scale]])` | the built-in 8×8 font; returns the width drawn |
| `hud.rect(x, y, w, h [, color])` | a filled rectangle |

A color is `"#rrggbb"`, `"#rrggbbaa"` or an integer `0xrrggbb`; the default is white.

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

## trace, invariant, require

| Function | Meaning |
|----------|---------|
| `trace(name [, fields])` | adds an event to the tick's trace; scenarios count them |
| `invariant(name, predicate)` | a check scenarios and `veduta.json` can list by name; the predicate returns true while it holds |
| `require(module)` | runs `module.lua` (dots are directories, relative to the main script) once and returns what it returned |

## The standard library

`string`, `table`, `math` and `utf8` behave as in Lua 5.4. `math.random` draws from the run's
seeded generator, so it is deterministic. `print` writes to the tool's error output.
Differences from Lua 5.4: `pairs` visits keys in the order they were first set; there is no
`goto`, no coroutines, no `io`, `os`, `debug` or `load`, and no `string.pack`,
`string.unpack`, `string.packsize` or `string.dump`. A callback that runs for 20
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
