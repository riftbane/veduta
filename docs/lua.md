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
| `e:despawn()` | removed at the end of the tick |

## scene

| Function | Meaning |
|----------|---------|
| `scene.name()` | the scene's name |
| `scene.find(name)` | the entity, or nil |
| `scene.tagged(tag)` | a list of entities, in id order |
| `scene.entities()` | every live entity, in id order |
| `scene.spawn{...}` | adds an entity and returns it; fields `kind` (default `static`), `name`, `model`, `material`, `position`, `rotation` (degrees), `scale` (each `{x, y, z}`), `tags` (a list), `visible`, `layer`, `state` (a table merged into the kind's) |
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
`goto`, no coroutines, no `io`, `os`, `debug` or `load`. A callback that runs for 20
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
