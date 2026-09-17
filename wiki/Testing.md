# Testing

A Veduta game is deterministic: the same seed and the same buttons at the same ticks always
give the same game, on every machine. So a play session can be written down, replayed
without a window in a fraction of a second, and checked. That is a **scenario**.

## Scenarios

`tests/scenarios/<name>.scenario.json`:

```json
{
  "veduta": "scenario/1",
  "scene": "level",
  "seed": 1,
  "ticks": 120,
  "inputs": [
    { "tick": 1, "press": ["right"] },
    { "tick": 40, "release": ["right"] },
    { "tick": 45, "press": ["a"] },
    { "tick": 46, "release": ["a"] }
  ],
  "expect": [
    { "tick": 40, "entity": "hero", "path": "position.x", "op": ">", "value": 2.5 },
    { "tick": 120, "entity": "hero", "path": "state.lives", "op": "==", "value": 3 },
    { "tick": 120, "trace": "coin_collected", "count_min": 1 },
    { "tick": 120, "trace": "invariant_violation", "count_max": 0 }
  ],
  "invariants": ["finite_positions", "entity_count_max:500"],
  "screenshots": [0, 40, 120]
}
```

| Field | Meaning |
|-------|---------|
| `scene` (or `world` and `at`) | what to load |
| `seed` | seed of `math.random` |
| `ticks` | how long to run, 20 ticks per second |
| `inputs` | buttons pressed and released at given ticks; a pressed button stays held until released |
| `expect` | checks, see below |
| `invariants` | rules checked after every tick, in addition to the project's |
| `screenshots` | ticks at which a frame is drawn into the run's contact sheet |
| `saves` | the game's saves when the run starts, by name (see [Saves](Saves#saves-in-tests)) |

Tick 0 is the scene just loaded; inputs at tick t are seen by the update of tick t.

## Expectations

**An entity's value at a tick**: `entity`, `path`, `op`, `value`.

| Path | Value |
|------|-------|
| `position`, `position.x` … `.z` | numbers (or `[x, y, z]`) |
| `rotation_deg`, `scale` and their `.x .y .z` | numbers |
| `aabb.min.x` … `aabb.max.z` | the box in the world |
| `visible` | boolean |
| `tags` | list: `contains` a tag, or `==` a list |
| `kind`, `model`, `material`, `parent` | strings |
| `frame` | number: the sprite sheet frame |
| `state.<key>` (and deeper, `state.inventory.keys`) | whatever the script stored in `e.state` |

Operators: `<`, `<=`, `==`, `!=`, `>=`, `>` for numbers; `==`, `!=` and `contains` for
strings and lists. Positions are float32: prefer `<` and `>` with a margin over `==`.

**How many times something happened, up to a tick**: `trace`, `count_min`, `count_max`.
The names are the ones your script passes to `trace(name, fields)`, plus the engine's own
events: `spawn`, `despawn`, `collision`, `scene_load`, `invariant_violation`,
`chunk_load` and `chunk_unload` in worlds, `save_write` and `save_remove`.

```lua
trace("coin_collected", {coin = coin.name, score = score})
```

## Invariants

Rules that must hold after **every** tick of every scenario, fuzz run and simulation:

| Invariant | Holds while |
|-----------|-------------|
| `finite_positions` | no position, rotation or scale is NaN or infinite |
| `within_bounds` | every entity is inside the project's `bounds` |
| `entity_count_max:N` | there are at most N entities |
| `no_overlap:tagA,tagB` | no entity tagged `tagA` overlaps one tagged `tagB` |
| any other name | a check the game registers |

A game registers its own in `game.init`:

```lua
local score, lives = 0, 3

function game.init()
  invariant("score_not_negative", function() return score >= 0 end)
  invariant("lives_in_range", function() return lives >= 0 and lives <= 3 end)
end
```

List them in `veduta.json` (`"invariants": ["finite_positions", "score_not_negative"]`) to
check them in every run, or in a scenario's `invariants` for that scenario only.

## Running tests

**Veduta: Test** in VS Code, or `veduta test`:

```
scenario first_gem            pass  golden match
scenario walk                 fail  golden -   tick 40: hero.position.x = 6, expected < 6
FAIL: scenario walk
```

Each run writes its trace (`trace.jsonl`, every tick's entities and events), its result and
its contact sheet to `out/runs/<scenario>-<n>/`. Open the sheet to see the frames at the
`screenshots` ticks and the entities' trajectories.

### Goldens

A scenario's expectations check what you thought of; a **golden** checks everything else.
`veduta test --update-golden` stores a fingerprint of each scenario's whole run (its trace)
and its contact sheet in `tests/golden/`. From then on `veduta test` compares every run
against them: a change to the code or the assets that alters anything in a run (a position
a thousandth off, one more spawn) fails with `golden mismatch`, even when every expectation
still holds. A scenario without a golden yet shows `golden new`.

When a change is intended, look at the new contact sheets under `out/runs/`, then record
them again:

```sh
veduta test --update-golden
```

Commit `tests/golden/` with the change.

## Recording instead of writing

In the simulator press **F5** to start recording, play, and **F5** again: the session is
saved as a scenario in `tests/scenarios/`. Open it and add the `expect` entries that
matter.

## One run by hand

```sh
veduta simulate --scenario tests/scenarios/walk.scenario.json
veduta simulate --scene level --ticks 200 --seed 3 --screenshots 0,100,200
veduta render --scene level --tick 60 --out out/level.png
```

`simulate` prints the verdict, the event counts and the path of the contact sheet;
`render` draws one frame, optionally after scripted input (`--input inputs.json`, a list of
`{"tick", "press", "release"}` events).

## Fuzzing

```sh
veduta fuzz --scene level --games 200 --ticks 400
```

Plays hundreds of games with random buttons and checks every invariant after every tick.
When one breaks, it writes the shortest input it could find that still breaks it as a
scenario under `out/fuzz/`: a ready-made regression test. Invariants are what make fuzzing
useful: the more rules the game states, the more bugs random play finds.

## Debugging a scenario

To step through a failing scenario, add a configuration in `.vscode/launch.json`
(Add Configuration → **Veduta: Scenario**) naming it, and press F5: breakpoints stop inside
the scenario's run. See [Debugging and Performance](Debugging-and-Performance).
