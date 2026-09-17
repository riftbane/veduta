# How a Game Runs

## The three callbacks

`main.lua` runs once when the game starts. It defines functions in the global `game` table,
which the engine calls:

```lua
function game.init()    -- once, after the first scene is loaded
end

function game.update()  -- once per tick, before the entities
end

function game.draw()    -- once per frame shown, after the scene: the HUD
end
```

All three are optional. The scene does not exist yet while `main.lua` itself runs: call
`scene.*` functions from `game.init` on.

## Ticks

The game advances in **ticks**, 20 per second by default. `engine.tick` counts them: 0 in
`game.init`, then 1, 2, 3… `engine.dt` is the length of one tick in seconds (0.05). Move
things by `speed * engine.dt` and their speed stays in units per second.

Each tick runs in this order:

1. `game.update()`.
2. The `update` of every live entity's kind, in id order. An entity spawned during the tick
   starts updating on the next one.
3. Entities despawned during the tick are removed; positions and bounds are recomputed.
4. Collisions and [invariants](Testing#invariants) are checked, and the tick is recorded in
   the trace.

The player draws one frame per tick: the scene, then `game.draw()` over it.

## Kinds

An entity's `kind` names its behaviour. A kind is a table in the global `kinds`:

```lua
kinds.coin = {
  init = function(e)     -- when the entity is loaded or spawned
    e.state.value = 1
  end,
  update = function(e)   -- once per tick, after game.update
    e:set_rotation(0, engine.tick * 4.5, 0)
  end,
}
```

Both functions are optional. `static`, `camera` and `light` are built-in kinds with no
behaviour. A scene that names a kind no script defines fails to load, with a message
naming it. Every entity of a kind shares the same functions; what differs from one entity
to another goes in `e.state` (see [Entities](Entities#state)).

## Modules

`require("name")` runs `name.lua` once and returns what it returned; later calls return
the same value. Dots are folders relative to `main.lua`: `require("enemies.bat")` loads
`enemies/bat.lua`.

```lua enemies/bat.lua
local bat = {}

function bat.update(e)
  e:move(e.state.dir * 3 * engine.dt, 0, 0)
end

return bat
```

```lua
local bat = require("enemies.bat")

kinds.bat = { update = bat.update }
```

## Determinism

Given the same seed and the same buttons at the same ticks, a game does exactly the same
thing on every run and every machine: the PC, the test runner and the console. That is what
makes [scenarios](Testing) reliable. The Lua runtime keeps it so:

- `math.random` draws from the run's seeded generator, seeded by the scenario or the
  project (`default_seed`). `math.randomseed(n)` reseeds it, still deterministically.
- `pairs` visits keys **in the order they were first set**, not in a hash order, so a loop
  over a table gives the same order every time.
- There is no clock, no file access and no environment: no `os`, `io`, `load` or `debug`.

## The Lua dialect

Scripts are Lua 5.4, run by a Lua virtual machine written in Go inside the engine. The
differences:

| Lua 5.4 | Veduta |
|---------|--------|
| `string`, `table`, `math`, `utf8` | the same |
| `print` | writes to the tool's error output (the Debug Console in VS Code) |
| `pairs` order | order of insertion |
| `math.random` | the run's seeded generator |
| `io`, `os`, `debug`, `load`, `dofile` | not available |
| `string.pack`, `string.unpack`, `string.dump` | not available |
| coroutines, `goto` | not available |

## Errors and endless loops

A Lua error (a call on `nil`, a bad argument, `error("...")`) in any callback or kind stops
the run and reports the script's file, line and traceback: in the Debug Console when the
game runs from VS Code, in the test's result when a scenario hits it. A syntax error is
reported earlier, in the Problems panel, when the file is saved.

Setting an entity field that does not exist (`e.speed = 3`) is an error too: your own
values go in `e.state`.

A single callback that runs more than 20 million steps (loop iterations and calls) without
returning is stopped as an endless loop.

## Switching scenes

`scene.load(name)` replaces the whole scene, as a reset: every entity is removed, the scene
file's entities are loaded again with fresh ids, and their kinds' `init` run. Lua variables
are **not** reset: a script resets its own (score, lives) when it loads a level, as in
[Your First Game](Your-First-Game). `game.init` does not run again.
