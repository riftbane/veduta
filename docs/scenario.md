# Scenario — `tests/scenarios/<name>.vscenario`

A scenario is an automated play test: it loads a scene (or a world), simulates a fixed number of ticks
from a seed with scripted inputs, and checks expectations and invariants along the way,
capturing screenshots at chosen ticks. The same seed and inputs always produce the same
trace and the same frames, so a scenario is a regression test. Run one with
`veduta simulate --scenario tests/scenarios/<name>.vscenario` (MCP tool `simulate`);
`veduta test` runs all of them.

## File and name

- Location: `tests/scenarios/<name>.vscenario` (relative to the project root, not to
  `assets/`). The scenario's name is the file name without `.vscenario`.
- Names (scenario, scene, entity, trace event, tag, invariant) are 1–64 characters of
  `a-z`, `0-9`, `_` and `-`, starting with a letter or a digit.
- The file is one JSON object. Decoding is strict: unknown fields, duplicate keys, wrong
  types and trailing data are errors. Every error carries `file:line:col` and the JSON path
  (for example `inputs[3].press[0]`), and all problems in a file are reported together.

## Ticks

The simulation runs at the project's `tick_rate` (20 Hz by default). Tick 0 is the scene
just loaded, before any game update. Ticks 1, 2, …, `ticks` each run exactly one `Update`
of the game and every entity behaviour. "The state at tick t" is the state after the
update of tick t; at tick 0 it is the initial state. Every tick field in a scenario
(`inputs`, `expect`, `screenshots`) must be in the range 0 to `ticks` inclusive.

## Top-level fields

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `veduta` | string | required | Must be exactly `"scenario/1"`. |
| `scene` | string | required unless `world` | Name of the scene to load (`assets/scenes/<scene>.vscene`). |
| `world` | string | none | Name of a world to load instead of a scene (`assets/worlds/<world>.vworld`, `world` topic). Exactly one of `scene` and `world`. Since v1.2.0. |
| `at` | `[x, z]` | `[0, 0]` | Start cell of the world (only with `world`): the camera and the persistent entities are placed relative to it and the loaded chunks begin there. |
| `seed` | integer | `0` | Seed of the simulation's random number generator, 0 to 18446744073709551615. |
| `ticks` | integer | required | Number of ticks to simulate, 1 to 1000000 (20 Hz: 20 ticks = 1 s). |
| `inputs` | array of objects | `[]` | Scripted input events; see Inputs. |
| `expect` | array of objects | `[]` | Checks; see Expectations. A scenario passes when every expectation holds and no invariant is violated. |
| `invariants` | array of strings | `[]` | Invariants checked after every tick, in addition to the project's `invariants` (`veduta.json`); see Invariants. No duplicates. |
| `screenshots` | array of integers | `[]` | Ticks at which a frame is rendered (at the state of that tick) into the run's contact sheet. Strictly increasing, each in 0 to `ticks`. |
| `saves` | object | `{}` | The game's saves when the run starts, by save name: each a JSON object or array of at most 1 MiB, as the game wrote it (`{"slot1": {"gold": 120, "level": 3}}`). The run keeps what the game writes in memory and never touches the saves of the simulator or the console. |

## Inputs

Each event applies at one tick. Events must be listed in non-decreasing `tick` order;
several events may share a tick (they apply in file order). Events at tick t are part of
the input the game receives in the update of tick t; events at tick 0 are applied to the
first update (tick 1), because no update runs at tick 0.

| Field | Type | Meaning |
|-------|------|---------|
| `tick` | integer | Tick of the event, 0 to `ticks`. |
| `press` | array of button names | Buttons that go down at this tick. A pressed button stays held until a later event releases it. A button that is already held cannot be pressed again. |
| `release` | array of button names | Held buttons that go up at this tick. Releasing a button that is not held is an error. A button cannot be pressed and released in the same event. |

Each event must set `press`, `release` or both. Within a list, names must not repeat.

### Button names

The console's game buttons, case-sensitive:

| Name | Button |
|------|--------|
| `up`, `down`, `left`, `right` | the D-pad |
| `a` | A: the main action, confirm |
| `b` | B: the second action |
| `select` | Select: the game's menu |
| `cancel` | Cancel: back, close a menu |

Home is not a game button: it returns to the console's home and never reaches a game, so a
scenario cannot press it. A name from engine v1, a W3C key code, is reported with the
button that key presses now: `"ArrowUp"` → `up`, `"KeyW"` → `up`, `"Space"` → `a`,
`"Escape"` → `cancel`.

## Expectations

Each expectation is exactly one of two forms. Mixing fields of both forms, or leaving a
required field out, is an error.

**Entity comparison** — `{ "tick", "entity", "path", "op", "value" }`: at `tick`, read
`path` from the named entity's summary and compare it with `value` using `op`.

| Field | Type | Meaning |
|-------|------|---------|
| `tick` | integer | Tick whose state is checked, 0 to `ticks`. |
| `entity` | string | Entity name (as in the scene, or given to an entity spawned by the game). The expectation fails if no entity has this name at that tick. |
| `path` | string | What to read; see Paths. |
| `op` | string | One of `<`, `<=`, `==`, `!=`, `>=`, `>`, `contains`. |
| `value` | number, string, boolean, or array | The value to compare with (not an object, not `null`). |

**Trace count** — `{ "tick", "trace", "count_min", "count_max" }`: count the trace events
named `trace` emitted from tick 0 through `tick` inclusive; the count must be at least
`count_min` and at most `count_max`. At least one bound is required; both are integers
≥ 0 and `count_min` ≤ `count_max`. Event names are the game's `ctx.Trace` names and the
built-in events `spawn`, `despawn`, `collision`, `scene_load`, `invariant_violation`,
`save_write` (`{name, bytes}`) and `save_remove` (`{name}`).
`{"trace": "gem_collected", "count_min": 1}` means "at least once";
`{"trace": "invariant_violation", "count_max": 0}` means "never".

### Paths

A path is identifiers separated by dots. The available paths and the type of value they
yield:

| Path | Type | Meaning |
|------|------|---------|
| `position`, `position.x`, `position.y`, `position.z` | vector / number | Entity position in meters, relative to its parent if it has one (as in the scene file). |
| `rotation_deg`, `rotation_deg.x` / `.y` / `.z` | vector / number | Entity rotation in degrees (as in the scene file). |
| `scale`, `scale.x` / `.y` / `.z` | vector / number | Entity scale. |
| `aabb.min`, `aabb.max`, `aabb.min.x` … `aabb.max.z` | vector / number | World-space axis-aligned bounding box of the entity's hitbox when it has one, else of its model. |
| `visible` | boolean | Whether the entity is drawn. |
| `frame` | number | The frame of its material's sprite sheet (0 when it has none). The trace records it only when it is not 0. |
| `tags` | list of strings | The entity's tags. |
| `kind`, `model`, `material`, `parent` | string | As in the scene file (`""` when unset). |
| `state.<field>`, `state.<field>.<field>` … | game value | A field of the entity's exported game state (the behaviour's state), for example `state.score` or `state.health`. Its type is known only when the scenario runs. |

### Operators by type

| Path type | Allowed `op` | `value` |
|-----------|--------------|---------|
| number | `<` `<=` `==` `!=` `>=` `>` | number |
| vector | `==` `!=` | `[x, y, z]` (3 numbers) |
| boolean | `==` `!=` | `true` or `false` |
| string | `==` `!=` `contains` (substring) | string |
| tags | `contains` (has this tag); `==` `!=` (same set of tags, any order) | string for `contains`; array of strings for `==` / `!=` |
| game value | all | a number for `<` `<=` `>=` `>`; a string, number or boolean for `contains` (list element or substring); anything but an object for `==` / `!=` |

Numbers are compared as float32 values (entity data is float32): `==` is exact, so prefer
`<` / `>` with a margin for positions. Any other combination is an error when the
scenario is compiled.

## Invariants

Invariants are predicates checked after every tick. A violation is recorded as an
`invariant_violation` trace event (with the invariant and the tick) and fails the
scenario. Specs:

| Spec | Meaning |
|------|---------|
| `finite_positions` | Every entity position, rotation and scale is finite (no NaN or infinity). |
| `within_bounds` | Every entity position is inside the project's `bounds` (`veduta.json`). |
| `entity_count_max:N` | At most N entities exist (N is an integer ≥ 1, for example `entity_count_max:500`). |
| `no_overlap:tagA,tagB` | No entity tagged `tagA` overlaps (AABB intersection) an entity tagged `tagB`. The two tags may be equal: `no_overlap:wall,wall`. No spaces. |
| any other name | An invariant the game registers in `Init` with `ctx.Invariant("name", pred)`, for example `"score_never_negative"`. It must be a plain name (no `:`); the run fails if the game did not register it. |

## Screenshots

Frames rendered with the scene camera at the listed ticks. `simulate` combines them into
one contact sheet image, labelled with their ticks.

## Errors (examples)

```
move.vscenario:7:33: inputs[1].press[0]: unknown button "Up" (did you mean "up"? the buttons are up, down, left, right, a, b, select, cancel)
move.vscenario:9:16: inputs[3].tick: tick 5 is before the previous input's tick 80 (inputs must be in tick order)
move.vscenario:13:5: expect[1]: mixes an entity comparison (entity, path, op, value) with a trace count (trace, count_min, count_max); use two expectations
move.vscenario:14:75: expect[2].op: op contains needs a string, list or state path (path position.x is a number)
move.vscenario:17:47: invariants[2]: invariant "entity_count_max:0": N must be in [1, 2147483647]
```

## Full example

```json
{
  "veduta": "scenario/1",
  "scene": "main",
  "seed": 42,
  "ticks": 300,
  "inputs": [
    { "tick": 10, "press": ["up"] },
    { "tick": 70, "release": ["up"] },
    { "tick": 80, "press": ["a"] },
    { "tick": 81, "release": ["a"] },
    { "tick": 90, "press": ["up", "right"] },
    { "tick": 110, "release": ["up", "right"] }
  ],
  "expect": [
    { "tick": 120, "entity": "player", "path": "position.z", "op": "<", "value": -1 },
    { "tick": 120, "entity": "player", "path": "tags", "op": "contains", "value": "player" },
    { "tick": 120, "entity": "player", "path": "visible", "op": "==", "value": true },
    { "tick": 200, "entity": "player", "path": "state.score", "op": ">=", "value": 1 },
    { "tick": 300, "trace": "gem_collected", "count_min": 1 },
    { "tick": 300, "trace": "invariant_violation", "count_max": 0 }
  ],
  "invariants": ["finite_positions", "within_bounds", "entity_count_max:500", "no_overlap:player,wall"],
  "screenshots": [0, 60, 120, 300]
}
```
