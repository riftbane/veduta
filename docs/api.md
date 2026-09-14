# Veduta game API (`api`)

A Veduta game is a Go program whose `main` calls `veduta.Run(&game.Game{})`. The same
binary opens the player window on a desktop and, with `-headless`, renders, simulates and
answers queries for the `veduta` tool on a server with no display. Game logic lives only
in the game binary; the tool never contains game code.

```go
package main

import (
	"github.com/riftbane/veduta"
	"mygame/game"
)

func main() { veduta.Run(&game.Game{}) }
```

## Interfaces

```go
type Game interface {
	Init(ctx *veduta.Context) error              // once, after the first scene is loaded
	Update(ctx *veduta.Context, in veduta.Input) // exactly once per tick, before behaviours
	Draw(ctx *veduta.Context, dl *gfx.DrawList)  // once per rendered frame, after the scene
}

type Behaviour interface {
	Update(ctx *veduta.Context, e *scene.Entity, in veduta.Input)
}

func RegisterKind(name string, ctor func(*scene.Entity) veduta.Behaviour)
```

- `RegisterKind` binds a scene entity `kind` to the constructor of its behaviour. Call it
  from an `init` function. Names are lowercase `[a-z0-9_-]`; `static`, `camera` and `light`
  are built-in kinds without behaviour. Loading a scene with an unregistered kind fails.
- The constructor runs when an entity of that kind is loaded or spawned; it may set
  `e.State`. `veduta.BehaviourFunc` adapts a plain function.
- Behaviour values must be stateless: keep per-entity state in `e.State` (a pointer to a
  struct with exported fields). The trace records it as `state.<json field>` and snapshots
  save it with `encoding/gob`.

## Context

| Member | Meaning |
|--------|---------|
| `Scene *scene.Scene` | the world: `Find(name)`, `Get(id)`, `Tagged(tag)`, `Entities()` (id order) |
| `Tick uint64` | 0 during `Init`, then 1, 2, … during `Update` |
| `RNG *sim.RNG` | seeded xoshiro256**: `Float32()`, `Range(lo, hi)`, `Intn(n)`, `Bool()`, `Chance(p)` — the only allowed randomness |
| `DT float32` | seconds per tick (`1 / tick_rate`, 1/20 by default) |
| `Headless bool` | true when run by the tool |
| `Project *asset.Project` | the parsed `veduta.json` |
| `Width, Height int` | a frame size in pixels, never 0 once `Init` runs. In `Draw`: the size of the frame being drawn (the panel in the player, `--width`×`--height` in `render`, the screenshot tile in `simulate`). In `Init`, `Update` and behaviours: the project's `resolution` in every mode (player, `render`, `simulate`, `snapshot`), so logic that reads them gives the same trace everywhere and `HUD` layout can be computed during `Update`. |
| `Font`, `FontTexture` | built-in 8×8 font for HUD text |
| `Trace(event, fields)` | emit a game event into the current tick's trace |
| `Invariant(name, pred)` | register a predicate checked after every tick when listed |
| `RegisterState(codec)` | save/restore the game's own state in snapshots |
| `Spawn(tmpl) *scene.Entity` | add an entity (next id); its behaviour starts next tick |
| `Despawn(e)` | remove `e` and its children at the end of the tick |
| `LoadScene(name)` | replace the scene (ids restart at 1), e.g. to reset a level |
| `Overlapping(e)` | live entities whose AABB overlaps `e`'s (last tick's bounds) |
| `HUD(dl) *sprite.Batch` | start a HUD batch covering the frame; call `End()` |
| `Text(b, x, y, scale, s, color)` | draw text with the built-in font |

```go
type StateCodec interface {
	SaveState(enc *gob.Encoder) error
	LoadState(dec *gob.Decoder) error
}
```

## Input

`veduta.Input` is a value type, identical whether it comes from a window or a script.

| Field / method | Meaning |
|----------------|---------|
| `Pressed`, `Held`, `Released` | key sets by W3C `KeyboardEvent.code` (`KeyW`, `Space`, `ArrowLeft`, `ShiftLeft`, …) |
| `Down(code)`, `JustPressed(code)`, `JustReleased(code)` | key queries |
| `Axis(neg, pos)` | −1, 0 or +1, e.g. `in.Axis("KeyA", "KeyD")` |
| `Mouse gmath.Vec2` | cursor in window pixels, origin top-left |
| `MouseDelta gmath.Vec2` | how far the cursor moved during this tick, in pixels (mouse look) |
| `Buttons`, `ButtonsPressed`, `ButtonsReleased`, `Button(name)` | mouse buttons `left`, `middle`, `right` |
| `Text string` | characters typed during the tick |

A key pressed and released within one tick appears in `Pressed` and `Released` but not
in `Held`.

### Mouse

`Mouse`, `MouseDelta` and the buttons are filled from a scenario's `mouse` and `buttons`
entries (`MouseDelta` is the difference between consecutive positions), so logic that
reads them can be simulated and tested. The console has no mouse: its player reads a
gamepad and a keyboard, and no player backend produces mouse movement or honours
`ctx.LockPointer`. A game meant to be played should be driven by keys, which is what the
pad produces (D-pad → `ArrowUp`/`ArrowDown`/`ArrowLeft`/`ArrowRight`, A → `Space`,
B → `Escape`, Start → `Enter`). `Camera.LookFrom(eye, yawDeg, pitchDeg)` still builds an
eye camera from the scene camera, keeping its projection; yaw 0 looks along −Z and grows
counter-clockwise seen from above, positive pitch looks up.

## Cameras

`ctx.Scene.Camera` is the camera the player and `render --camera scene` draw with; a game
may replace it during `Update`.

- `scene.Camera2D(center gmath.Vec2, height float32) scene.Camera` is the camera of a 2D
  game played in the XY plane: orthographic, looking down −Z at `(center.X, center.Y, 0)`
  from z = `scene.Camera2DDistance` (100), +X to the right and +Y up on screen, `height`
  world units visible vertically (the width follows from the aspect ratio), near 0.1 and
  far 200, so z from −100 to 99.9 is visible and a larger z is nearer the camera. Follow a
  hero with `p := hero.WorldPosition(); ctx.Scene.Camera = scene.Camera2D(gmath.V2(p.X, p.Y), 12)`.
  It is the same camera as a scene file's `"type": "orthographic", "size": 12,
  "position": [x, y, 100], "look_at": [x, y, 0]`. The `2d` docs topic is the recipe for a
  2D game: camera, sprites, layers, hitboxes, HUD, pad input and the traps.
- `Camera.LookFrom(eye, yawDeg, pitchDeg)` places a camera at `eye` looking along yaw and
  pitch, keeping the projection.

## Entities

`scene.Entity` fields: `ID`, `Name`, `Kind`, `Transform` (`Position`, `Rotation` quaternion,
`Scale`, relative to `Parent`), `Model`, `Material`, `Tags`, `AABB` (world bounds of the
hitbox, else of the model, recomputed after every tick), `Visible`, `State`, `Parent`,
`Hitbox *gmath.AABB` (the scene file's `hitbox`: a local-space box that replaces the model
bounds as the source of `AABB`, so collisions, `Overlapping`, `no_overlap` and the trace's
`aabb` use it; nil for none; `Spawn` copies it), `Layer int` (the scene file's `layer`:
the first key of the draw order, lower layers drawn first; within a layer opaque parts in
id order, then blended parts back to front by depth along the view axis; opaque and cutout
parts write depth, so among them the nearest is in front whatever the layer, while blended
parts write none, so an opaque part on a higher layer is drawn over a blended part on a
lower one whatever their depth). Useful methods: `WorldPosition()`, `World()`, `HasTag(t)`,
`Alive()`. Use `gmath` for math: its `Sin/Cos/Atan2` are deterministic on every platform;
never use `math.Sin` in game logic.

Coordinates: right-handed, Y up, −Z forward, meters. Angles are degrees in files and
radians in code. `rotation_deg: [x, y, z]` means R = Ry·Rx·Rz.

## Tick timeline

1. Tick 0: the scene is loaded (ids 1…n in scene-file order), `Init` runs, then the
   engine records tick 0 (events `scene_load` and any spawns from `Init`).
2. Each tick t = 1, 2, …: `Game.Update(ctx, in)`, then every live entity's behaviour in id
   order (entities spawned during the tick start next tick), then despawns are applied,
   transforms and AABBs are recomputed, collisions and invariants are checked, and tick t
   is recorded.

Input events of a script at tick t are part of the Input of tick t; events at tick 0 apply
before the first update and appear in tick 1. The simulation runs at `tick_rate` (20 Hz by
default); there is no interpolation and the player renders once per tick.

## Determinism rules

- Randomness only through `ctx.RNG`; no `time.Now`, no environment or file access after
  `Init`.
- Never let map iteration order affect state or trace: sort keys first.
- Round every float product that feeds an addition or subtraction with a conversion to its
  own type: `y += float32(v * ctx.DT)`, `float32(a*b) + float32(c*d)`. On linux/arm64, the
  console, the compiler may fuse `a*b + c` into one instruction that rounds once, where
  amd64 rounds twice; about a quarter of all inputs then differ in the last bit and the
  trace drifts from goldens recorded on a PC. A conversion is a rounding point the
  compiler may not fuse across, and it costs nothing on amd64. This holds across
  statements and through inlined calls, so `p := a * b` followed by `q := p + c` needs it
  too. `gmath` already rounds inside its own operations (`Vec3.Scale`, `Mul`, `Dot`,
  `Cross`, `Lerp`, matrix and quaternion products), so `pos.Add(vel.Scale(ctx.DT))` is
  safe as written. `veduta doctor` builds the game for linux/arm64 and lists every line of
  the game's code where a fusion happened.
- Use `gmath`'s trigonometry, never `math.Sin`, `math.Exp`, `math.Pow` or `math.Log`: those
  are not the same on every architecture. `math.Sqrt`, `Abs`, `Floor`, `Ceil`, `Trunc`
  and `Mod` are exact everywhere.
- Same seed + same input ⇒ same trace hash and identical frames on linux/amd64,
  linux/arm64 and windows/amd64 (the engine's CI runs its goldens on all three).

## Trace

One canonical JSON object per tick in `trace.jsonl`:

```json
{"entities":[{"aabb":{"max":[0.5,1,-0.75],"min":[-0.5,0,-1.75]},"id":2,"kind":"player","material":"hero","model":"hero","name":"player","parent":"","position":[0,0,-1.25],"rotation_deg":[0,180,0],"scale":[1,1,1],"state":{"score":1},"tags":["player"],"visible":true}],"events":[{"event":"gem_collected","gem":"gem_1","score":1}],"tick":75}
```

Keys are sorted, no whitespace, floats use the shortest form that round-trips (float32
values as float32), non-finite numbers are the strings `"NaN"`, `"+Inf"`, `"-Inf"`.
Entity summaries hold: `id`, `name`, `kind`, `position` (world), `rotation_deg` (local
Euler angles, R = Ry·Rx·Rz, pitch in [-90, 90]), `scale` (local), `visible`, `tags`,
`model`, `material`, `parent` (name, `""` for none), `aabb` (`{min, max}`, world; only
entities with a model or a hitbox, and the hitbox's bounds when one is set) and `state`
(only when set). Scenario expectation paths address
these keys (`position.z`, `aabb.max.y`, `state.score`). The trace hash is the SHA-256 of
the file's bytes.

Built-in events: `scene_load` (`scene`, `entities`), `spawn` and `despawn` (`id`, `name`,
`kind`), `collision` (`a`, `b`, `a_id`, `b_id`: two AABBs started overlapping this tick;
touching does not count; two `static` entities never collide), `invariant_violation`
(`name`, `detail`: an invariant started failing this tick). Game events come from
`ctx.Trace`.

## Invariants

Checked after every tick (including tick 0). A scenario's `invariants` list is used when
present, else the project's. Built-ins: `finite_positions`, `within_bounds` (project
`bounds`), `entity_count_max:N`, `no_overlap:tagA,tagB`. Any other name must be registered
by the game with `ctx.Invariant(name, pred)`. A violation is reported when an invariant
starts failing, not again while it keeps failing.

## Headless subcommands

Every game binary accepts `-project DIR` (default `.`), `-version`, and:

```
game -headless render   --scene S --tick T --seed N --camera P --mode M --width W --height H --out F [--bundle] [--input F]
game -headless simulate --scenario F | --scene S --ticks N --seed N [--input F] [--screenshots 0,60] [--invariants a,b] --out DIR
game -headless query    --frame F --at x,y | --coverage
game -headless snapshot --scene S --tick T --out F [--input F]
game -headless snapshot --restore F --ticks N [--input F] [--out trace.jsonl]
game -headless describe
```

- Each prints one JSON report on stdout. Errors print `{"ok":false,"error":...,"errors":[{file,line,col,msg}]}`.
- Exit codes: 0 success, 1 error, 2 bad command line, 3 simulate ran and the verdict is `fail`.
- Camera presets: `scene`, `top`, `front`, `back`, `left`, `right`, `iso`, `orbit:<deg>`, or
  the name of a `camera` entity. Render modes: `color`, `wireframe`, `normals`, `depth`,
  `ids`, `silhouette`, `overdraw`, `uv_checker`, `collision`.
- `simulate` writes `trace.jsonl`, `result.json` and one `sheet.png` (screenshots at the
  requested ticks plus a trajectory tile: seen from the top, or in the XY plane when the
  scene camera is orthographic and looks along −Z, as a 2D game's does) into the output
  directory. The verdict is `fail` when an expectation fails or an invariant is violated.
- An input file (`--input`) is a JSON array of input events or an object with an
  `inputs` array (a scenario file works).
- `describe` lists registered kinds with their state fields, game invariants, whether a
  state codec is registered, scenes, render modes and camera presets.
