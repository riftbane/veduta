# Lua API Reference

API level 1 (Veduta v2.0). Everything a script can use besides the standard library. In VS
Code the same reference appears as completion and hover text.

Conventions: positions and sizes are in units (metres), angles in degrees, HUD coordinates
in pixels. A `color` is `"#rrggbb"`, `"#rrggbbaa"` or an integer `0xrrggbb`.

## game

| Callback | Called |
|----------|--------|
| `game.init()` | once, after the first scene is loaded |
| `game.update()` | once per tick, before the entities' kinds |
| `game.draw()` | once per frame shown, after the scene; the only place for `hud.*` |

## kinds

`kinds.<name> = { init = function(e) end, update = function(e) end }`

| Function | Called |
|----------|--------|
| `init(e)` | when an entity of the kind is loaded with a scene or spawned |
| `update(e)` | once per tick, in entity id order, after `game.update` |

Built-in kinds without behaviour: `static`, `camera`, `light`.

## engine

| Field | Value |
|-------|-------|
| `engine.tick` | the current tick: 0 in `game.init`, then 1, 2, … |
| `engine.dt` | seconds per tick (0.05 at 20 ticks per second) |
| `engine.width`, `engine.height` | the frame's size in pixels: the project's resolution, or in `game.draw` the frame being drawn |
| `engine.headless` | `true` when no window shows the game (tests, simulate, render) |
| `engine.name`, `engine.title` | from `veduta.json` |
| `engine.api` | the runtime's API level |

## Entity

Fields:

| Field | Access | Value |
|-------|--------|-------|
| `id` | read | integer |
| `name` | read | string |
| `kind` | read | string |
| `alive` | read | `false` once despawned |
| `x`, `y`, `z` | read/write | position relative to the parent |
| `visible` | read/write | boolean |
| `model`, `material` | read/write | asset name or `nil` |
| `layer` | read/write | integer, draw order |
| `state` | read/write | table of the entity's own values |

Methods:

| Method | Returns / does |
|--------|----------------|
| `e:position()` | x, y, z |
| `e:set_position(x, y, z)` | |
| `e:move(dx, dy, dz)` | adds to the position |
| `e:world_position()` | x, y, z in the world |
| `e:rotation()` | x, y, z in degrees |
| `e:set_rotation(x, y, z)` | degrees |
| `e:scale()` | x, y, z |
| `e:set_scale(x, y, z)` | |
| `e:has_tag(tag)` | boolean |
| `e:add_tag(tag)`, `e:remove_tag(tag)` | |
| `e:tags()` | list of strings |
| `e:overlapping([tag])` | list of live entities whose bounds overlap (last tick's bounds), id order |
| `e:bounds()` | min x, y, z, max x, y, z; or `nil` |
| `e:despawn()` | removed at the end of the tick |

## scene

Available from `game.init` on.

| Function | Returns / does |
|----------|----------------|
| `scene.name()` | the loaded scene's name |
| `scene.find(name)` | entity or `nil` |
| `scene.tagged(tag)` | list of entities, id order |
| `scene.entities()` | every live entity, id order |
| `scene.spawn{...}` | the new entity; fields `kind` (default `static`), `name`, `model`, `material`, `position`, `rotation`, `scale` (each `{x, y, z}`), `tags` (list), `visible`, `layer`, `state` (table) |
| `scene.load(name)` | replaces the scene, as a reset |

## input

Buttons: `"up"`, `"down"`, `"left"`, `"right"`, `"a"`, `"b"`, `"select"`, `"cancel"`.

| Function | Returns |
|----------|---------|
| `input.down(button)` | held this tick |
| `input.pressed(button)` | went down this tick |
| `input.released(button)` | went up this tick |
| `input.dpad()` | x (−1, 0, 1: left to right), y (−1, 0, 1: down to up) |

## camera

| Function | Returns / does |
|----------|----------------|
| `camera.get()` | `{position = {x, y, z}, target = {x, y, z}, ortho, fov, size, near, far}` |
| `camera.set{...}` | changes the fields given |
| `camera.follow2d(x, y, height)` | orthographic camera looking down −Z at (x, y), `height` units visible |

## hud

Only inside `game.draw`. Pixels from the top left.

| Function | Returns / does |
|----------|----------------|
| `hud.text(x, y, text [, color [, scale]])` | draws with the 8 × 8 font; returns the width drawn |
| `hud.rect(x, y, w, h [, color])` | a filled rectangle |

## world

| Function | Returns / does |
|----------|----------------|
| `world.load(name, cx, cz)` | replaces the scene with a streamed world around cell (cx, cz) |
| `world.name()` | the loaded world or `nil` |
| `world.focus(x, y, z)` | where chunks load; call every tick |
| `world.height(x, z)` | ground height |
| `world.water(x, z)` | water level or `nil` |

## mesh

| Function | Returns / does |
|----------|----------------|
| `mesh.new()` | an empty mesh |
| `m:part([material])` | following faces use `material` (or the entity's) |
| `m:quad(x1, y1, z1, x2, y2, z2, x3, y3, z3, x4, y4, z4)` | a quad, counter-clockwise seen from the front |
| `m:triangle(x1, y1, z1, x2, y2, z2, x3, y3, z3)` | a triangle |
| `m:box(x, y, z, w, h, d [, faces])` | a box from (x, y, z); `faces` like `"+x-x+y-y+z-z"`, default all |
| `m:triangles()` | the triangle count |
| `mesh.set(name, m)` | makes a copy of `m` the model `name` (must contain `:`, not start with `world:`); survives `scene.load` |
| `mesh.remove(name)` | forgets the model |
| `mesh.voxels(v, materials [, size])` | a mesh of a volume's visible faces, one part per block id, `materials[id]` names each; blocks `size` units wide (default 1) |

## volume

| Function | Returns / does |
|----------|----------------|
| `volume.new(x, y, z)` | a grid of block ids 0–255, all 0; at most 4 194 304 cells |
| `v:get(x, y, z)` | the id; cells count from 0 |
| `v:set(x, y, z, id)` | |
| `v:fill(x1, y1, z1, x2, y2, z2, id)` | every cell of the box |
| `v:size()` | x, y, z |

## Globals

| Function | Does |
|----------|------|
| `trace(name [, fields])` | adds an event to the tick's trace; scenarios count events by name |
| `invariant(name, predicate)` | registers a check; lists in `veduta.json` or a scenario enable it |
| `require(module)` | runs `module.lua` once (dots are folders) and returns its result |
| `print(...)` | writes to the tool's error output / VS Code's Debug Console |

## Standard library

`string`, `table`, `math` and `utf8` as in Lua 5.4, with these differences:

- `pairs` visits keys in insertion order.
- `math.random` and `math.randomseed` use the run's deterministic generator.
- Missing: `math.deg`, `math.rad` (use `x * 180 / math.pi`, `x * math.pi / 180`),
  `string.pack`, `string.unpack`, `string.packsize`, `string.dump`.
- Not available by design: `io`, `os`, `debug`, `load`, `loadfile`, `dofile`, coroutines,
  `goto`.
