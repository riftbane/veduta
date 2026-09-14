# Veduta — Specification v1.0.0

> **veduta** (it.) — a highly detailed, faithful painting of a view.
>
> Veduta is a headless, deterministic game engine and asset toolchain written in pure Go,
> designed to be *looked at by machines*. Its primary user is an AI agent working on a
> server with no display. Every feature exists to let that agent see, measure, and fix
> what it is building. Humans play the result on a small console.

Status: specification of the first stable release. Module path: `github.com/riftbane/veduta`.

This document supersedes [SPEC-v0.1.0.md](SPEC-v0.1.0.md), which stays in the repository
unchanged as the record of what v0.1.0 promised and how its acceptance was certified. Where
the two disagree, this one is the source of truth. Sections that did not change are
repeated in full, so this file can be read on its own.

---

## 1. Principles

1. **Numbers before pixels.** Every visual check has a numeric counterpart. Reports are
   ranked by severity; images exist to confirm, not to discover.
2. **Declarative sources.** The agent edits text (JSON) that describes models, textures,
   materials, scenes and test scenarios. The toolchain compiles them. Most anomalies become
   impossible by construction; the rest are localized to a parameter.
3. **Headless everywhere except the console.** Nothing in the toolchain opens a window. The
   only place a game is drawn for a person is the console's panel.
4. **Determinism, across architectures.** Same seed + same inputs → same trace, same frames,
   bit for bit, on the machine that authors a game (linux/amd64, windows/amd64) and on the
   console that plays it (linux/arm64). This is what makes scenario tests, fuzzing and
   golden images mean the same thing on both.
5. **Standard library only.** No third-party Go modules. `cgo` is forbidden.
6. **Small outputs.** Images default to the panel's 320×240 and never exceed 640×480 unless
   they are contact sheets. Token cost is a design constraint.

## 2. Scope of v1.0.0

### The target

A game made with Veduta plays on the **Veduta console**: a Raspberry Pi Zero 2 W or Pi 5
(both ARMv8, `GOOS=linux GOARCH=arm64`) booting straight into a dashboard that lists the
games on its SD card, with a 320×240 ILI9341 SPI panel refreshed at 20 Hz and a wired USB
gamepad. The dashboard, the OS image and the card tooling live in a separate repository
(`github.com/riftbane/vedutaos`) that imports the engine as a game does. The engine keeps
what is engine: the framebuffer player, the pad input, the manifest fields a dashboard
shows, the card description a release ships, and the checks that keep a game playable on
the board.

Windows, macOS and desktop Linux are **authoring machines**: they build, cook, inspect,
render, simulate, test, fuzz and cross-compile. They do not open a window.

### In scope

- Everything of v0.1.0 §2 except the desktop player windows, plus v0.2.0's release
  channels.
- `gmath`: every float product that feeds an addition or subtraction is rounded explicitly,
  so results are identical on amd64 and arm64 (§3, §7).
- Player backend for the console: the panel's framebuffer found through sysfs, frames packed
  to RGB565 (or 32-bit XRGB), gamepads and keyboards read from evdev and translated to W3C
  key codes, hot-plug, exit chords (§6.6).
- Defaults of the panel: `resolution` and `inspect_resolution` 320×240, `tick_rate` 20.
- 2D support in the scene format: per-entity `hitbox` and `layer`, correct blended sorting
  under orthographic cameras, `scene.Camera2D`, a 2D guide (`docs/2d.md`).
- `card.json` written by `veduta init` and shipped in every release archive (§8.7).
- Console checks: `veduta run` refuses where there is no framebuffer, `veduta doctor` checks
  the project against the console, `veduta release` refuses a game the console cannot run.
- `veduta upgrade` pins the v0.x defaults of a project that relied on them.
- `internal/fused`: a scanner of linux/arm64 binaries for fused multiply-adds, used as an
  engine test gate and by `doctor` on a game's own code.

### Removed

- The X11 player window (Linux desktops) and the Win32 player window (Windows). The game
  binary still builds for windows/amd64, darwin and desktop Linux, and `-headless` works
  there; the player exits with an error that names the framebuffer it needs.
- The first-person view of the template demo. `Input.MouseDelta`, `scene.Camera.LookFrom` and
  `Context.LockPointer` remain in the API; no player backend produces mouse movement or
  implements pointer lock.
- The 1280×720 / 8 ms rasterizer target (v0.1.0 §15.8), replaced by §15.8 below.

### Non-goals (deferred)

GPU backend, desktop player windows, macOS player, audio, skeletal animation and skinning,
physics beyond AABB overlap (no solver: games resolve their own collisions), CSG booleans,
networking, scripting language, shadows, post-processing, Streamable HTTP MCP transport,
Windows installer for the tool, the console's dashboard and OS image (vedutaos).

## 3. Constraints

- Go 1.25 or newer. `go.mod` declares the minimum; CI tests the two latest releases.
- `CGO_ENABLED=0` for every build. `go vet ./...` and `gofmt -l .` clean.
- `GOAMD64=v1` in all release builds (amd64 then never fuses floating-point operations).
- **Determinism guarantee:** identical trace hash and identical frames for the same inputs on
  linux/amd64, windows/amd64 and linux/arm64. This is a hard requirement, gated in CI (§14).
- **Rounding rule:** every floating-point product (and division by a power-of-two constant,
  which the compiler turns into a product) whose result feeds an addition or subtraction is
  wrapped in a conversion to its own type: `float32(a*b) + c`. The Go specification makes
  such a conversion a rounding point the compiler may not fuse across; without it, arm64
  fuses `a*b + c` into one FMADD instruction with a single rounding. The rule holds across
  statements and inlined calls. `internal/fused` finds violations in a built binary.
- Out-of-range float-to-integer conversions (NaN, values beyond the target type) are
  implementation-defined and differ between amd64 and arm64: code that can meet them
  clamps or rejects in floating point first.
- Only math functions exact on every architecture are used where results are observable:
  `math.Sqrt`, `Abs`, `Floor`, `Ceil`, `Trunc`, `Round`, `Mod`, `Copysign`, `min`, `max`;
  trigonometry comes from `gmath`.
- No wall-clock reads inside `sim` or anything it calls. `time.Now` is allowed only in the
  player loop and CLI timing.
- No allocations in the rasterizer inner loops or the player's present path (verified by
  benchmarks with `-benchmem` and allocation tests).

## 4. Repository layout

```
veduta/
  go.mod                      module github.com/riftbane/veduta
  LICENSE                     MIT
  README.md
  CHANGELOG.md
  PROGRESS.md
  SPEC-v0.1.0.md              the v0.1.0 specification, as shipped
  SPEC-v1.0.0.md              this file
  CLAUDE.md                   agent instructions for developing the engine itself
  install.sh
  .github/workflows/ci.yml
  .github/workflows/release.yml

  veduta.go                   Run(Game), Context, Input, RegisterKind — the public API
  engine.go, headless.go, player.go
  gmath/
  gfx/                        backend interface, DrawList, PipelineState, Texture, Mesh
  gfx/soft/                   software rasterizer (the only backend)
  sprite/                     2D batcher on top of gfx
  scene/                      entities, transforms, cameras, scene loading
  sim/                        fixed-step loop, RNG, input script, trace, invariants, snapshot
  asset/                      source formats, compiler, .vda container, loaders
  inspect/                    reports, contact sheets, diff, query
  platform/                   the console player: framebuffer and evdev input (Linux)
    backend_linux.go          backend choice (VEDUTA_BACKEND)
    fb_linux.go               framebuffer discovery and pixel packing
    window_fb_linux.go        the player on a framebuffer
    evdev_linux.go            evdev reading
    padsource_linux.go        device discovery, hot-plug
    padmap_linux.go           gamepad buttons and D-pad → W3C codes
    keymap_evdev_linux.go     keyboard KEY_* → W3C codes
    window_other.go           stub that errors at runtime (every other GOOS)
  mcp/                        JSON-RPC 2.0 over stdio, tool registry
  internal/cli/               command implementations shared by cmd/veduta
  internal/fused/             fused multiply-add scanner for linux/arm64 binaries
  internal/update/            release lookup, download, checksum, atomic replace
  cmd/veduta/                 the tool binary
  template/                   files copied by `veduta init` (embedded with embed.FS)
  testdata/                   golden images, sample assets, scenarios
  docs/                       format references, served by the MCP docs tool
```

## 5. Architecture

Three roles, one module:

| Role | What it is | Where it runs |
|------|-----------|---------------|
| **Library** | `github.com/riftbane/veduta` and subpackages, imported by games | inside the game binary |
| **Tool** | `veduta` binary: CLI + MCP server, asset compiler, inspectors | the authoring machine (headless) |
| **Game** | A Go program calling `veduta.Run(game)` | the console (player) or anywhere (`-headless`) |

Key design decision: **the tool never contains game logic.** Every operation that needs the
game (render, simulate, query) is performed by the game binary in headless mode. The tool
builds the game (`go build ./cmd/game`), runs it with `-headless <subcommand>`, and wraps
the result. Consequences:

- The MCP server process stays alive while the agent edits code; compile errors are
  returned as tool results.
- The CLI and the MCP server expose the same operations; the MCP layer is a typed wrapper.
- A game binary can always reproduce any report on its own, without the tool.
- The tool never links the platform layer (`go list -deps ./cmd/veduta` holds no
  `platform`), so it builds and runs on every authoring OS.

### 5.1 Public API (`veduta.go`)

```go
type Game interface {
    Init(ctx *Context) error
    Update(ctx *Context, in Input)          // exactly once per tick
    Draw(ctx *Context, dl *gfx.DrawList)    // once per rendered frame; skipped when no frame is requested
}

func Run(g Game)                           // parses flags, loads veduta.json, runs player or headless mode
func RegisterKind(name string, ctor func(*scene.Entity) Behaviour)

type Behaviour interface {
    Update(ctx *Context, e *scene.Entity, in Input)
}

type Context struct {
    Scene   *scene.Scene
    Tick    uint64
    DT      float32                         // 1 / tick_rate
    RNG     *sim.RNG                        // seeded, deterministic
    Width, Height int                       // Draw: the frame drawn; Init, Update: the project resolution
    // Trace emits a structured event into the current tick's trace.
    Trace(event string, fields map[string]any)
    // Invariant registers a named predicate evaluated after every tick.
    Invariant(name string, pred func() bool)
    // Snapshot/Restore of user state: games register a codec once in Init.
    RegisterState(codec StateCodec)
    // LockPointer is kept for compatibility; no player backend implements it.
    LockPointer(on bool)
}
```

`Width` and `Height` are never 0 once `Init` runs. In `Draw` they are the frame being drawn
(the panel divided by `VEDUTA_SCALE` in the player, the requested size in `render`); in
`Init`, `Update` and behaviours they are the project's `resolution` in every mode, so logic
that reads them traces the same everywhere.

`Input` is a value type: pressed/held/released key sets (W3C `KeyboardEvent.code` names:
`KeyW`, `Space`, `ArrowLeft`…), mouse position and per-tick movement, mouse buttons, and a
`Text` string for typed characters. It is identical whether it comes from the console or a
scenario script. On the console the keys come from the gamepad and any keyboard; mouse
fields are only ever filled by scenarios.

### 5.2 Manifest `veduta.json`

```json
{
  "veduta": "project/1",
  "name": "mygame",
  "title": "My Game",
  "icon": "icon.png",
  "engine": "v1.0.0",
  "entry": "./cmd/game",
  "resolution": [320, 240],
  "inspect_resolution": [320, 240],
  "tick_rate": 20,
  "default_scene": "main",
  "default_seed": 1,
  "assets": "assets",
  "cooked": "assets/.cooked",
  "invariants": ["finite_positions", "within_bounds"],
  "bounds": [[-100, -50, -100], [100, 100, 100]]
}
```

Absent fields, and fields holding their zero value, take the defaults shown (`title`
defaults to `name`; `icon` has none). `resolution` is the frame the game is designed for:
the default render size and the size a player asks for. On the console the frame actually
drawn is the panel divided by `VEDUTA_SCALE`. The player renders exactly one frame per tick,
so a `tick_rate` above the panel's 20 Hz only spends time on frames nobody sees.

## 6. Engine modules

### 6.1 `gmath`
`Vec2/3/4`, `Mat3/4` (column-major, right-handed, Y up, -Z forward), `Quat`, `Rect`,
`AABB`. Deterministic `Sin/Cos/Tan/Atan/Atan2/Asin/Acos` implemented in Go. Every product
inside gmath's own operations is rounded (§3), so `pos.Add(vel.Scale(dt))`, `Dot`, `Cross`,
`Lerp` and matrix and quaternion products are safe in game code as written. Float32 for
storage and rasterization, float64 permitted for setup math.

### 6.2 `gfx` and `gfx/soft`
- `Backend` interface: `CreateTexture`, `CreateMesh`, `Begin(target)`, `Draw(DrawList)`,
  `End()`. Designed so a GPU backend can replace `soft` later.
- `PipelineState`: depth test/write, blend (`opaque|alpha|add`), cull (`back|none`).
- `Framebuffer`: `Color []uint32` (BGRA8), `Depth []float32`, `ID []uint32` (entity id,
  0 = none). Fixed-point 28.4 edge functions, top-left fill rule, perspective-correct
  attribute interpolation, clipping in clip space against all six planes.
- Textures: BGRA8 with mip chain; sampling nearest or bilinear, mip selected per triangle
  from UV derivatives.
- Lighting: one directional light + ambient, Lambert on vertex normals, optional `unlit`.
- Parallelism: screen split in 64×64 tiles, triangles binned per tile, one goroutine per
  tile via a worker pool. Deterministic output by construction (tiles never overlap and
  consume triangles in submission order).
- Debug render modes: `color`, `wireframe`, `normals`, `depth`, `ids`, `silhouette`,
  `overdraw`, `uv_checker`, `collision`.

### 6.3 `sprite`
Batches textured quads into the same `DrawList` with an orthographic camera and a
`PipelineState{Depth:false, Blend:alpha}`. Supports 9-slice and a built-in 8×8 bitmap font
(embedded PNG atlas, CC0) for HUD text. At 320×240 the font is drawn at scale 1.

### 6.4 `scene`
`Entity{ID uint32, Name string, Kind string, Transform, Model, Material, Tags, AABB,
Hitbox, Layer, Visible, State any}`. Parent/child transforms. Scene files load into this
structure; `kind` instantiates the registered Go behaviour. Built-in kinds: `static`,
`camera`, `light`.

- `AABB` is the world bounds of the model, or of `Hitbox` when one is set (a local-space box
  transformed by the entity's world transform); contacts, `no_overlap`, collision events and
  `ctx.Overlapping` use it.
- Drawing order: `Layer` first (lower layers first, range −1000…1000), then opaque parts in
  id order, then blended parts back to front by the depth of their centre along the
  camera's view axis (correct for both perspective and orthographic cameras). Opaque and
  cutout parts write depth, so among them the nearest surface wins whatever the layer;
  blended parts write none, so the layer decides which blended surface covers which.
- Camera presets: `scene`, `top`, `front`, `back`, `left`, `right`, `iso`, `orbit:<deg>`,
  camera entities. `scene.Camera2D(center, height)` builds an orthographic camera looking
  down −Z for 2D games; it is a helper, not a preset.

### 6.5 `sim`
- `Loop`: fixed `tick_rate` (20 by default). No interpolation; the player renders once per
  tick and sleeps to hold the rate.
- `RNG`: xoshiro256** with explicit seed; every consumer takes it from `Context`.
- `InputScript`: ordered list of `{tick, press[], release[], mouse{x,y}, buttons[]}`.
- `Trace`: one JSON object per tick: `{"tick":n,"events":[...],"entities":[...summary]}`.
  Built-in events: `spawn`, `despawn`, `collision` (AABB pairs), `scene_load`,
  `invariant_violation`. Game events via `ctx.Trace`.
  `TraceHash` = SHA-256 over the canonical trace (sorted keys, fixed float formatting).
- `Invariants`: built-ins `finite_positions`, `within_bounds`, `entity_count_max:N`,
  `no_overlap:tagA,tagB`; game-registered via `ctx.Invariant`.
- `Snapshot`: engine state (entities, RNG state, tick) plus game state via `StateCodec`
  (`encoding/gob`). `Restore` must yield an identical trace from that tick onward.

### 6.6 `platform` — the console player
One backend, Linux only, pure Go, no ioctl and no mmap for drawing:

- **Framebuffer.** The panel is found by reading `/sys/class/graphics/fbN/{name,
  bits_per_pixel, virtual_size, stride}`, never by assuming a device number: a 16-bit
  framebuffer is the panel; a 32-bit one (an emulator, a PC text console, HDMI) is used when
  there is no 16-bit one. `VEDUTA_FB` names one (node `fb1` or part of the driver name).
  Frames are packed to RGB565 or XRGB8888 honouring a padded stride and written with one
  `WriteAt`; packing allocates nothing. `VEDUTA_SCALE` (1–8) divides the panel: the game
  renders panel ÷ scale pixels and each is repeated scale×scale times.
- **Input.** `/dev/input/eventN` devices found through `/sys/class/input` capabilities:
  gamepads first, then keyboards, all read at once; `VEDUTA_PAD` names one. Everything
  becomes W3C key codes, so `sim.Input`, scenarios and goldens are unchanged by the pad:
  D-pad (hat, stick or buttons) → `ArrowUp/Down/Left/Right`, A → `Space`, B → `Escape`,
  X → `KeyF`, Y → `KeyR`, L1 → `KeyQ`, R1 → `KeyE`, Select → `Tab`, Start → `Enter`;
  keyboards by physical position (`KEY_Q` → `KeyQ`). Auto-repeat is dropped. Held keys are
  released when the kernel reports dropped events or a device is unplugged; devices are
  looked for again every second, including a pad plugged in while a keyboard is being read.
- **Exit.** Select+Start on a pad, or Ctrl+Q (either Ctrl) on a keyboard, closes the player: on a device
  with no keyboard it is the way back to the dashboard, and no game can swallow it.
- **Choice.** `VEDUTA_BACKEND` is `auto` (default) or `fbdev`; `x11` is refused with the
  reason. `platform.Open` fails with "no framebuffer" on a machine without one, and every
  other GOOS gets a stub that fails at runtime.
- Never imported by `cmd/veduta`.

### 6.7 `mcp`
JSON-RPC 2.0 over stdio, newline-delimited, logs to stderr only. Methods: `initialize`,
`notifications/initialized`, `ping`, `tools/list`, `tools/call`. Content blocks: `text`
and `image` (base64 PNG, `mimeType: image/png`). Errors from tools are returned as
`isError: true` results with a text explanation, never as transport errors. Negotiate the
protocol version requested by the client; support at least `2025-06-18`.

## 7. Determinism contract

1. `sim` reads no clock, no environment, no filesystem after `Init`.
2. All randomness comes from `Context.RNG`.
3. Map iteration order never influences state or trace (sort before use).
4. Entity IDs are assigned sequentially in scene-file order, then spawn order.
5. Goroutines inside the tick (rasterizer tiles) write to disjoint memory and are joined
   before the tick ends.
6. Every float product that feeds an addition or subtraction is rounded (§3), in the engine
   and in the template demo; `internal/fused` proves it for the veduta tool and the template
   game built for linux/arm64, and `veduta doctor` reports a game's own violations.
7. CI runs every test, scenario and golden of `testdata/` on ubuntu-latest, windows-latest
   and linux/arm64 (under qemu-user) and compares trace hashes and rendered frames with the
   same committed goldens.

## 8. Asset sources

All source files are JSON with a `"veduta": "<type>/<version>"` header. Unknown fields are
errors (strict decoding), so typos surface immediately. Paths are relative to `assets/`.
Colors are `#RRGGBB` or `#RRGGBBAA`. Angles are degrees in source files, radians in code.
Every error is located (`{file,line,col,msg}`) and all errors of a file are reported at once.

### 8.1 Model — `assets/models/<name>.model.json`

```json
{
  "veduta": "model/1",
  "name": "crate",
  "units": "m",
  "pivot": "bottom-center",
  "smooth_angle_deg": 30,
  "parts": [
    { "shape": "box", "size": [1, 1, 1], "position": [0, 0.5, 0], "material": "crate_wood" },
    { "shape": "cylinder", "radius": 0.1, "height": 1.2, "segments": 12, "position": [0.6, 0.6, 0] },
    { "shape": "sphere", "radius": 0.3, "segments": 16, "rings": 8 },
    { "shape": "plane", "size": [10, 10] },
    { "shape": "extrude", "profile": [[0,0],[1,0],[1,2],[0,2]], "depth": 0.5 },
    { "shape": "lathe", "profile": [[0,0],[0.5,0],[0.4,1],[0,1.2]], "segments": 16 },
    { "shape": "mirror", "axis": "x", "of": 1 }
  ]
}
```

Parts are unioned by concatenation (no booleans). Each part gets `position`,
`rotation_deg`, `scale`, `material`, `uv` (`box|planar|cylindrical|spherical`, default per
shape) and `flip_normals`. `pivot` ∈ `origin|center|bottom-center`. The compiler emits
positions, normals (per `smooth_angle_deg`), UVs, per-part material index and an AABB.
Geometry whose vertices leave the float32 range after sizes, positions, rotations and scales
are applied is an error on the offending part.

### 8.2 Texture — `assets/textures/<name>.tex.json`

```json
{
  "veduta": "texture/1",
  "size": [256, 256],
  "tiling": true,
  "mipmaps": true,
  "layers": [
    { "type": "solid",    "color": "#8a5a2b" },
    { "type": "noise",    "seed": 7, "scale": 16, "octaves": 3, "color": "#000000", "opacity": 0.25, "blend": "multiply" },
    { "type": "stripes",  "width": 8, "angle_deg": 90, "colors": ["#00000000", "#00000030"] },
    { "type": "rect",     "xy": [16, 16], "size": [224, 224], "color": "#ffffff20", "corner": 6, "outline": 2 },
    { "type": "circle",   "center": [128, 128], "radius": 40, "color": "#ff0000" },
    { "type": "gradient", "from": "#ffffff", "to": "#000000", "angle_deg": 45, "opacity": 0.3 },
    { "type": "checker",  "cells": 8, "colors": ["#c0c0c0", "#404040"] },
    { "type": "image",    "path": "textures/src/logo.png", "fit": "contain" }
  ]
}
```

Blend modes: `normal`, `multiply`, `screen`, `add`. Noise is value noise with a seeded
hash (own implementation), tileable when `tiling` is true. Layers render top to bottom in
list order (first is the bottom). Output: BGRA8 + mip chain.

### 8.3 Material — `assets/materials/<name>.mat.json`

```json
{ "veduta": "material/1", "albedo": "#ffffff", "texture": "crate_wood",
  "unlit": false, "alpha": "opaque", "cull": "back", "filter": "bilinear" }
```
`alpha` ∈ `opaque|blend|cutout` (`cutoff` default 0.5). `texture` is optional. A sprite
material is `"unlit": true, "alpha": "cutout", "filter": "nearest"`.

### 8.4 Scene — `assets/scenes/<name>.scene.json`

```json
{
  "veduta": "scene/1",
  "camera": { "type": "perspective", "fov_deg": 60, "near": 0.1, "far": 200,
              "position": [0, 5, 10], "look_at": [0, 0, 0] },
  "light":  { "direction": [-0.4, -1, -0.3], "color": "#ffffff", "ambient": "#404040" },
  "background": "#202830",
  "entities": [
    { "name": "ground", "kind": "static", "model": "ground", "material": "grass" },
    { "name": "player", "kind": "player", "model": "hero", "material": "hero",
      "position": [0, 0, 0], "rotation_deg": [0, 180, 0], "tags": ["player"] },
    { "name": "gem_1", "kind": "collectible", "model": "gem", "material": "gem",
      "position": [3, 0.5, -2], "tags": ["gem"] },
    { "name": "coin", "kind": "coin", "model": "quad", "material": "sprite",
      "position": [2, 1, 0], "hitbox": [[-0.4, -0.4, -0.5], [0.4, 0.4, 0.5]], "layer": 2 }
  ]
}
```

An orthographic camera has `"type": "orthographic"` and `size` (visible height) instead of
`fov_deg`. `hitbox` is an optional local-space box `[[min], [max]]` (finite, min ≤ max on
every axis) that replaces the model's bounds for collisions (an entity with a hitbox and no
model is a trigger zone); `layer` is an optional integer in −1000…1000 that is the first key
of the draw order (§6.4). Entity order in the file is id order, and ids are behaviour order:
an entity that must read another's position after it moved comes later in the file.

### 8.5 Scenario — `tests/scenarios/<name>.scenario.json`

```json
{
  "veduta": "scenario/1",
  "scene": "main",
  "seed": 42,
  "ticks": 100,
  "inputs": [
    { "tick": 3, "press": ["ArrowUp"] },
    { "tick": 23, "release": ["ArrowUp"] },
    { "tick": 27, "press": ["Space"] }
  ],
  "expect": [
    { "tick": 40, "entity": "player", "path": "position.z", "op": "<", "value": -1 },
    { "tick": 100, "trace": "gem_collected", "count_min": 1 }
  ],
  "invariants": ["finite_positions", "within_bounds", "entity_count_max:500"],
  "screenshots": [0, 20, 40, 100]
}
```

`ticks` counts ticks, not seconds: at the default 20 Hz, 20 ticks are one second.
`path` grammar: dotted access on the entity summary (`position.x`, `tags`, `visible`,
`state.<field>` for exported game state). `op` ∈ `< <= == != >= > contains`.

### 8.6 Compiled container — `.vda`

Little-endian, chunked, inspired by PNG: magic `VDA1`, then chunks `(type[4], length u32,
payload, crc32)`. Chunk types: `MESH`, `TEXR` (BGRA8 + mip levels), `MATL`, `SCEN`, `META`
(source path, source hash, compiler version). Unknown chunks are skipped. `veduta cook`
compiles only sources whose hash changed (hash stored in `META`). Cooking the same sources
yields the same bytes on every architecture.

### 8.7 Card — `card.json`

The description the console reads from a game folder, beside `veduta.json`. It is frozen:
`card/1` never gains a required field and never changes meaning, so a console built today
can list a game written years later.

```json
{ "veduta": "card/1", "title": "mygame", "name": "mygame", "version": "v1.2.0",
  "exec": "mygame", "icon": "icon.png" }
```

Every field except `veduta` is optional. `veduta init` writes `veduta`, `title`, `name` and
`exec`; the release workflow copies it into each archive and adds `version` from the tag.

## 9. Inspection

All inspectors produce a **report** (JSON) and zero or more **sheets** (PNG). Reports are
what the agent reads first.

```json
{
  "subject": "model:crate",
  "summary": { "errors": 1, "warnings": 2, "info": 3 },
  "issues": [
    { "severity": "error", "code": "MESH_FLIPPED_NORMALS", "count": 12,
      "where": { "part": 1, "triangles": [4, 5, 6] },
      "hint": "Part 1 (cylinder) has normals pointing inward. Set \"flip_normals\": true or check winding." }
  ],
  "metrics": { "triangles": 348, "vertices": 210, "aabb": [[-0.5,0,-0.5],[0.5,1,0.5]],
               "pivot_offset": [0, 0, 0], "texel_density_cv": 0.12, "symmetry_x": 0.998 },
  "sheets": [ "out/crate.turntable.png", "out/crate.silhouette.png" ]
}
```

### 9.1 Model issues
`MESH_DEGENERATE_TRIANGLE`, `MESH_DUPLICATE_VERTEX`, `MESH_NONMANIFOLD_EDGE`,
`MESH_OPEN_BOUNDARY` (hole loops), `MESH_FLIPPED_NORMALS`, `MESH_MIXED_WINDING`,
`MESH_UV_OUT_OF_RANGE`, `MESH_UV_OVERLAP`, `MESH_TEXEL_DENSITY_UNEVEN`,
`MESH_PIVOT_OFF`, `MESH_SCALE_SUSPICIOUS` (AABB outside 0.05–100 m), `MESH_ASYMMETRIC`
(when `symmetry` is requested), `MESH_TRIANGLE_BUDGET`.
Sheets: `turntable` (8 views + top + bottom), `silhouette`, `normals`, `wireframe`,
`uv_checker`, `sections` (three axis-aligned cuts).

### 9.2 Texture issues
`TEX_NOT_POWER_OF_TWO`, `TEX_SEAM` (edge mismatch when `tiling`), `TEX_LOW_CONTRAST`,
`TEX_ALPHA_UNUSED`, `TEX_MIP_ILLEGIBLE` (detail lost by mip 2), `TEX_LAYER_NO_EFFECT`.
Sheets: `single`, `tiled_2x2`, `channels`, `mips`, `on_model:<model>`.

### 9.3 Scene issues
`SCENE_MISSING_ASSET`, `SCENE_ENTITY_OUTSIDE_BOUNDS`, `SCENE_OVERLAP` (static AABBs),
`SCENE_CAMERA_SEES_NOTHING`, `SCENE_ENTITY_OFFSCREEN` (tagged `important`),
`SCENE_UNLIT`, `SCENE_ZFIGHT_RISK` (coplanar faces). Z-fighting is judged from what is
drawn (the models), not from hitboxes; `SCENE_OVERLAP` compares the static entities' AABBs
(the hitbox where one is set, so a model-less trigger counts). Translucent sprites stacked
by layer are reported by neither.
Sheets: `camera`, `top`, `ids` (entity id false-color with legend). Views are framed 4:3
like the panel: 640×480 single views, 317×238 summary tiles.

### 9.4 Diff
`diff a.png b.png` → `{"changed_pixels": n, "changed_ratio": r, "bbox": [...], "max_delta": d}`
plus a sheet: `a | b | heat` side by side.

### 9.5 Query
`query --frame f.vframe --at x,y` → entity at pixel, depth, normal.
`query --frame f --coverage` → per-entity pixel counts, screen-space bounding boxes,
occlusion ratio (visible pixels / projected pixels).

`render` can write a **frame bundle** (`.vframe`: color + depth + ids + camera) so
`query` and `diff` operate on it without re-rendering.

## 10. `veduta` CLI

| Command | Purpose |
|---------|---------|
| `veduta init [dir] --name N [--module M]` | create a game project from the embedded template |
| `veduta doctor` | check Go, git, the manifest, engine/tool versions, assets, updates, and the project against the console (§10.2) |
| `veduta build` | `go build` the game (`CGO_ENABLED=0`), cook assets first if stale |
| `veduta run` | run the player on this machine's framebuffer; refuses where there is none (§10.2) |
| `veduta cook [--force]` | compile changed asset sources to `.vda` |
| `veduta render --scene S [--tick T] [--seed N] [--camera preset] [--mode M] [--out f.png] [--bundle]` | render one frame headless |
| `veduta simulate --scenario F` or `--scene S --ticks N --seed N [--input script.json]` | run ticks (default 200), write trace, screenshots contact sheet, verdict |
| `veduta inspect model\|texture\|scene NAME [--focus ISSUE] [--sheets list]` | report + sheets |
| `veduta query --frame F --at x,y\|--coverage` | ID-buffer queries |
| `veduta diff A B [--out f.png]` | image diff |
| `veduta test [--update-golden]` | `go test ./...` + all scenarios + golden images |
| `veduta fuzz --scene S --games N --ticks T --seed N` | random input games (default 200 ticks), invariant violations, minimized repro written to `tests/scenarios/fuzz_<hash>.scenario.json` |
| `veduta release vX.Y.Z [--dry-run]` | checklist (clean tree, test, cook, smoke render, console build) → CHANGELOG entry → tag → push; CI publishes. Refuses pre-release tags, which are cut by hand |
| `veduta mcp` | serve the MCP server on stdio |
| `veduta update [--check] [--force] [--channel stable\|beta]` | update the tool binary from GitHub Releases |
| `veduta upgrade` | set the project's `go.mod` and `veduta.json` engine version to the binary's version, `go mod tidy`, run migrations (§13.4) |
| `veduta version` | print version, commit, build date |

Every command that produces files writes under `out/` (git-ignored) unless `--out` is
given, and prints the report JSON to stdout with `--json`.

### 10.1 Headless game subcommands

`veduta.Run` handles `-headless` in every game binary; the tool delegates to these:

```
game -headless render   --scene S --tick T --seed N --camera P --mode M --out F [--bundle]
game -headless simulate --scenario F | --scene S --ticks N --seed N --input F --out DIR
game -headless query    --frame F --at x,y | --coverage
game -headless snapshot --scene S --tick T --out F        # and --restore F --ticks N
game -headless describe                                    # registered kinds, invariants, state schema (JSON)
```

### 10.2 The console in the tool

- `veduta run` looks for what the player looks for — a `/sys/class/graphics/fbN` with 16 or
  32 bits per pixel, or `VEDUTA_FB` — and otherwise refuses, naming `VEDUTA_FB` and
  `VEDUTA_SCALE`; off Linux it refuses outright.
- `veduta doctor` prints the target (`linux/arm64, 320x240 panel at 20 Hz, gamepad`) and, as
  warnings that do not fail the run: `tick_rate` above 20 or a `resolution` that is not 4:3;
  every line of the game's own code that compiles to a fused multiply-add in its linux/arm64
  build (`file:line`); a release workflow that builds no linux/arm64 archive; a missing
  `card.json` or one whose title or name disagrees with `veduta.json`. A game that does not
  build for linux/arm64 fails the check, with the located compiler error.
- `veduta release` refuses a project that does not build for linux/arm64 or whose workflow
  publishes no linux/arm64 archive.

## 11. MCP server tools

Names and input schemas (JSON Schema, `additionalProperties: false`). Every tool returns a
`text` block with the report JSON and, when applicable, `image` blocks.

| Tool | Input | Output |
|------|-------|--------|
| `status` | `{}` | versions, project, target, cooked/stale assets, last build result, update availability |
| `build` | `{ "vet": bool }` | compile/vet errors as structured list `{file,line,col,msg}` |
| `cook` | `{ "force": bool }` | compiled assets and their reports (errors only) |
| `render` | `{ scene, tick?, seed?, camera?, mode?, width?, height?, bundle? }` | one image + camera/frame metadata |
| `simulate` | `{ scenario? , scene?, ticks?, seed?, inputs?, screenshots?, invariants? }` | verdict, expectations table, invariant violations with tick, trace summary (event counts), one contact sheet |
| `trace` | `{ run_id, from_tick, to_tick, events? }` | trace slice for a previous simulate (kept under `out/runs/<id>/`) |
| `inspect` | `{ kind: "model"\|"texture"\|"scene", name, focus?, sheets? }` | report + requested sheets (default: one summary sheet) |
| `query` | `{ frame, at? , coverage? }` | ID-buffer answers |
| `diff` | `{ a, b }` | diff metrics + one sheet |
| `test` | `{ update_golden: bool }` | test results, failed scenarios with first failing expectation |
| `fuzz` | `{ scene, games, ticks, seed }` | violations found, minimized repro path |
| `release` | `{ version, dry_run: bool }` | checklist results; tag pushed when all pass |
| `docs` | `{ topic }` | format reference text for `model\|texture\|material\|scene\|scenario\|api`, and `project`, `vda`, `inspect`, `config`, `2d` |

Rules: `render` images are 320×240 unless `width`/`height` are given (at most 640×480; one
side alone gets the other at 4:3). Contact sheets and inspection sheets are fitted inside
640×720. `query` draws its marker on the frame before any fitting, so it stays sharp at
every frame size. `simulate` never returns more than one image; ask for `render` at a specific tick
for detail. `trace` output is paginated (max 200 ticks per call).

Client configuration produced by `veduta init` (`.mcp.json` at the project root):

```json
{ "mcpServers": { "veduta": { "type": "stdio", "command": "veduta", "args": ["mcp"] } } }
```
Equivalent: `claude mcp add --scope project veduta -- veduta mcp`.

## 12. Project template (`veduta init`)

```
mygame/
  veduta.json
  card.json                   the console's card/1 description (§8.7)
  go.mod                      require github.com/riftbane/veduta vX.Y.Z
  cmd/game/main.go            func main() { veduta.Run(&game.Game{}) }
  game/game.go                Game impl; registers invariants, state codec, HUD
  game/kinds.go               player, collectible behaviours
  assets/models/*.model.json  ground, hero, gem, crate, quad (a thin box for sprites)
  assets/textures/*.tex.json
  assets/materials/*.mat.json including sprite (unlit, cutout, nearest)
  assets/scenes/main.scene.json
  tests/scenarios/*.scenario.json   idle, move, collect, pad
  .mcp.json
  CLAUDE.md                   agent instructions for building a game (Appendix A)
  .gitignore                  out/ assets/.cooked/ bin/
  .github/workflows/release.yml     builds linux/arm64 and linux/amd64 on tag
  README.md
```

Demo game: WASD or the arrows (the pad's D-pad) move the hero on a ground plane, Space (A)
jumps, gems are collected on AABB contact, `KeyR` (Y) resets, a HUD shows score and tick.
Four scenarios pass out of the box; `pad` drives the game with the codes the gamepad
produces.

## 13. Distribution

### 13.1 Releases
Tag `vX.Y.Z` → `release.yml` builds `cmd/veduta` for `linux/amd64`, `linux/arm64`,
`windows/amd64` with `-trimpath -ldflags "-s -w -X main.version=$TAG"`, `CGO_ENABLED=0`,
`GOAMD64=v1`; archives `veduta_<tag>_<os>_<arch>.tar.gz` (`.zip` on Windows); writes
`checksums.txt` (sha256); publishes with `gh release create "$TAG" --generate-notes dist/*`
(no third-party actions besides `actions/checkout` and `actions/setup-go`). A tag with a
pre-release suffix is published as a pre-release.

A game's `release.yml` builds `linux/arm64` and `linux/amd64` archives
`<name>_<tag>_linux_<arch>.tar.gz`, each unpacking to a folder `<name>/` holding the binary,
`veduta.json`, `card.json` (with `version`), `README.md` and `assets/` without cooked files.

### 13.2 `install.sh`
POSIX `sh`, usable as `curl -fsSL https://raw.githubusercontent.com/riftbane/veduta/main/install.sh | sh`.

1. Detect OS/arch (`uname -s`, `uname -m`); refuse unsupported combos with a clear message.
2. Resolve version: `$VEDUTA_VERSION` if set, else the newest release of the channel
   (`--channel stable|beta`, `$VEDUTA_CHANNEL`) via the GitHub API.
3. Download archive + `checksums.txt`; verify with `sha256sum` or `shasum -a 256`; abort on
   mismatch.
4. Install to `$VEDUTA_HOME/bin` (default `~/.local/bin`); `--prefix DIR` overrides.
   Print a PATH hint if the directory is not on `PATH`.
5. Check `go` ≥ 1.25. If missing and `--with-go` (default when non-interactive), download
   the official tarball from `go.dev/dl` into `~/.local/go` and print the PATH line.
6. Check `git`; print instructions if missing.
7. Run `veduta doctor` and print next steps: `veduta init mygame && cd mygame && claude`.

Flags: `--prefix`, `--version`, `--channel`, `--with-go`, `--no-go`, `--yes`. No `sudo`
unless `--prefix` points to a root-owned dir. Idempotent.

### 13.3 Update policy
- Config file `~/.config/veduta/config.json`:
  `{ "auto_update": "check" | "auto" | "off", "check_interval_hours": 24, "channel": "stable" | "beta" }`;
  defaults `check`, 24, `stable`.
- `check`: `veduta mcp` and `veduta doctor` look up the newest release of the channel at most
  once per interval (cache in `~/.cache/veduta/update.json`, offline-safe, 3 s timeout) and
  report availability in `status`.
- `auto`: `veduta mcp` applies the update **before** serving (never mid-session), then
  execs the new binary. Also applied by `veduta doctor`. Never downgrades, never crosses
  channels.
- `veduta update`: download, verify checksum, write to a temp file next to the binary,
  atomic rename. `--channel` subscribes; leaving beta for an older stable release needs
  `--force`.
- The engine library version is pinned by the game's `go.mod`; updating the tool never
  changes a project. `veduta upgrade` does, explicitly, and records the change in the
  project's `CHANGELOG.md`. `doctor`/`status` warn when tool and project versions differ
  by a minor or more.

### 13.4 Compatibility
Semantic versioning. From v1.0.0:

- **Source formats** keep their `"veduta": "<type>/<n>"` versions within 1.x, and every file
  valid for 1.0 stays valid with the same meaning. A minor release may add optional fields;
  a file that uses them is rejected by older tools (decoding is strict), and the changelog
  says which release introduced them. A change that would alter the meaning of a valid
  file needs a new format version and a `veduta upgrade` migration, which means 2.0.
- **Trace hashes and frames** of a project do not change in patch releases. A minor release
  may change them only to fix a bug, with a changelog entry that names the affected cases.
- **`card/1`** is frozen forever.
- **Defaults** of the manifest do not change within 1.x. `veduta upgrade` from a 0.x engine
  writes the 0.x defaults (`resolution` [1280, 720], `inspect_resolution` [640, 360],
  `tick_rate` 60) into a manifest that omitted them, so the game keeps its size, physics
  and trace hashes; the project's changelog names every pinned field.
- The Go API follows the Go 1 compatibility promise within 1.x for exported identifiers of
  the root package, `gmath`, `gfx`, `sprite`, `scene`, `sim` and `asset`; `internal/` and the
  tool's JSON reports may grow fields.

## 14. CI

`ci.yml` on push and PR, on `ubuntu-latest` and `windows-latest` with Go stable and
oldstable: `gofmt -l`, `go vet`, empty `go.sum`, `go test ./...` (unit, golden, scenarios,
the fused multiply-add gate), the rasterizer benchmark with an allocation check,
`veduta init tmp && cd tmp && veduta test` (template smoke test), the check that
`cmd/veduta` never links `platform`, `install.sh` from both channels (Linux), and
cross-compiles for windows/amd64, darwin/arm64, linux/amd64 and linux/arm64.

A separate job runs `go test ./...` for `GOARCH=arm64` under `qemu-aarch64-static`, so the
goldens recorded on amd64 are checked on the console's architecture. Trace hashes and
sheets of `testdata/` scenarios are compared against the same committed files everywhere.
There is no display job: the player has no display server to talk to, and the framebuffer
path is tested against a fake sysfs and, where `/dev/uinput` is writable, a virtual gamepad.

## 15. Acceptance criteria for v1.0.0

1. On a fresh Ubuntu 24.04 VPS: `install.sh` → `veduta init demo` → `cd demo` →
   `veduta test` passes in under 5 minutes total, no network after install except GitHub.
2. `claude` started in `demo/` lists all tools in §11; `render`, `simulate`, `inspect`,
   `query`, `diff` return images inline, `render` at 320×240 by default.
3. `veduta release v0.1.0` from the template project produces a GitHub Release with
   `demo_v0.1.0_linux_arm64.tar.gz` and `demo_v0.1.0_linux_amd64.tar.gz`; each unpacks to a
   `demo/` folder with `card.json`, and the arm64 binary answers `-headless render` under
   qemu-aarch64. On the console (checked by a person): the game starts from the dashboard,
   the D-pad moves, A jumps, gems are collected, Select+Start returns to the dashboard.
4. Two runs of the same scenario produce identical trace hashes and identical PNGs; the
   same holds between the `ubuntu-latest`, `windows-latest` and linux/arm64 CI jobs, and
   `TestEngineHasNoFusedMultiplyAdd` finds no fused multiply-add in the engine.
5. `veduta inspect model hero` on a model with one part's normals flipped reports
   `MESH_FLIPPED_NORMALS` with the part index and produces a `normals` sheet where the
   part is visibly wrong.
6. `veduta fuzz --games 200 --ticks 200` on the demo finds no invariant violation; a
   deliberately broken build (player speed × 100) yields a minimized `within_bounds` repro.
7. `veduta update` on an older tool version installs the latest release and verifies it;
   `veduta upgrade` of a v0.2.0 project that omits `tick_rate` writes `"tick_rate": 60`.
8. Rasterizer benchmark: 320×240, 10k textured triangles, 0 allocs/op (gate). Target, checked
   on hardware by a person: a Raspberry Pi Zero 2 W holds 20 Hz (50 ms per tick for update,
   render and present) on a level of about 1200 submitted triangles.
9. `go.sum` contains no third-party modules.
10. `veduta run` on a machine without a framebuffer refuses with a message naming
    `VEDUTA_FB`; `go list -deps ./cmd/veduta` contains no `platform`;
    `CGO_ENABLED=0 GOOS=windows go build ./cmd/veduta` succeeds.
11. A scene with an orthographic camera, thin quads with hitboxes and layers renders in
    layer order, emits a collision between two coplanar quads, and answers `query --at`.

## 16. Order of work

Each step ends with a commit, a passing `go test ./...` and, for format or behaviour changes,
a note in `CHANGELOG.md`.

0. arm64 determinism: round every fused product, `internal/fused` gate, audits of other
   architecture differences.
1. CI runs the whole suite on linux/arm64 under qemu-user.
2. Generated projects build for the board and ship `card.json`.
3. Defaults of the panel: 320×240, 20 Hz; the demo loses first person and gains the pad.
4. `veduta upgrade` pins the v0.x defaults.
5. Inspection and MCP images follow the panel (4:3, 320×240 / 640×480).
6. One player backend: X11 and Win32 removed; keyboard table completeness; CI follows.
7. The tool tells the truth about the console: `run`, `doctor`, `release`, `status`.
8. 2D: `hitbox`, `layer`, orthographic sorting, `Camera2D`, `docs/2d.md`.
9. This specification, the README, the template's prose, the benchmark at 320×240.
10. PROGRESS and CHANGELOG; `v1.0.0-rc.1` cut by hand and rehearsed; `veduta release v1.0.0`.

## 17. What only hardware can verify

- The SPI panel's real refresh at 20 Hz and its RGB565 byte order on the ILI9341 driver.
- The button order of the real Rii GP100 (the pad table is provisional until then).
- Whether a Raspberry Pi Zero 2 W holds the 50 ms tick budget.

---

## Appendix A — `CLAUDE.md` shipped by the template (game projects)

```markdown
# Game project on Veduta

You are building a game with the Veduta engine. There is no display: verify everything
with the `veduta` MCP tools (or the CLI). Never try to open a window.

Loop: edit → `build` → `cook` → `simulate` / `render` / `inspect` → read the report →
fix → `diff` to confirm only the intended change → `test` → commit.

Rules
- Read reports before images. Open an image only to confirm a suspected issue.
- Assets are JSON sources under assets/. Never edit files under assets/.cooked.
- Every feature gets a scenario in tests/scenarios. Every bug fix gets a regression scenario.
- Keep the game deterministic: randomness only via ctx.RNG; no time.Now in game code.
- The game plays on a console: linux/arm64, a 320x240 panel at 20 Hz, a gamepad (D-pad =
  arrow keys, A = Space, B = Escape, Start = Enter). Drive the game with those keys; there
  is no mouse on the console.
- Round every float product that feeds + or -: `y += float32(v * ctx.DT)`. On arm64 the
  compiler fuses a*b+c into one rounding and the console's traces drift from the goldens.
  `doctor` lists the lines where it happened; gmath's own operations are already safe.
- Run `fuzz` before every release; add minimized repros as scenarios.
- Commit small, message in imperative mood. Tag releases with `release` only when `test` is green.
- Do not add third-party Go modules.
- When something in the engine blocks you, write it down in ENGINE-NOTES.md with a repro,
  do not work around it silently.
```
