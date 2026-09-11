# Veduta — Specification v0.1.0

> **veduta** (it.) — a highly detailed, faithful painting of a view.
>
> Veduta is a headless, deterministic game engine and asset toolchain written in pure Go,
> designed to be *looked at by machines*. Its primary user is an AI agent working on a
> server with no display. Every feature exists to let that agent see, measure, and fix
> what it is building. Humans download the resulting game builds and play them.

Status: draft for the first public release. Module path: `github.com/riftbane/veduta`.

---

## 1. Principles

1. **Numbers before pixels.** Every visual check has a numeric counterpart. Reports are
   ranked by severity; images exist to confirm, not to discover.
2. **Declarative sources.** The agent edits text (JSON) that describes models, textures,
   materials, scenes and test scenarios. The toolchain compiles them. Most anomalies become
   impossible by construction; the rest are localized to a parameter.
3. **Headless everywhere except the player build.** Nothing in the toolchain opens a window.
   The only window in the system is the game binary the human downloads.
4. **Determinism.** Same seed + same inputs → same trace, same frames. This is what makes
   scenario tests, fuzzing and golden images possible.
5. **Standard library only.** No third-party Go modules. `cgo` is forbidden on Linux and
   Windows. (macOS will require cgo for windowing and is deferred; when added it must live
   behind a `darwin` build tag only.)
6. **Small outputs.** Inspection images default to 640×360 and contact sheets. Token cost
   is a design constraint.

## 2. Scope of v0.1.0

### In scope

- `gmath`: vectors, matrices, quaternions, rects, deterministic helpers.
- Software rasterizer: perspective-correct textured triangles, z-buffer, homogeneous
  clipping, backface culling, one directional light + ambient (Lambert), alpha blending,
  per-pixel entity ID buffer, tile-parallel rendering.
- 2D sprite/HUD layer on the same rasterizer (orthographic, depth off, blend on).
- Scene: entities with transform, model, material, tags, `kind` → Go behaviour.
- Simulation: fixed 60 Hz tick, seeded RNG, per-tick input events, structured trace,
  invariants, snapshot/restore, scenario tests.
- Asset sources (JSON): model (primitive composition), texture (layer program), material,
  scene, scenario. Compiler to a chunked binary container (`.vda`).
- Inspection: `model`, `texture`, `scene` reports + contact sheets; `diff` between two
  images; `query` on ID buffer.
- Platform layer for the player build: Linux (X11 over unix socket) and Windows
  (user32/gdi32 via `syscall`). Keyboard + mouse.
- `veduta` CLI with all commands in §10, including the MCP server (`veduta mcp`).
- Project template (`veduta init`) with Claude Code configuration, `CLAUDE.md`, and a
  GitHub Actions release workflow for the game.
- Distribution: `install.sh`, GitHub Releases with checksums, `veduta update` (tool),
  `veduta upgrade` (project), update policy.
- Demo game shipped by the template: a playable 3D scene with a 2D HUD.

### Non-goals (deferred)

GPU backend, macOS, skeletal animation and skinning, audio, physics beyond AABB, CSG
booleans, networking, scripting language, shadows, post-processing, Streamable HTTP MCP
transport, Windows installer for the tool.

## 3. Constraints

- Go 1.25 or newer. `go.mod` declares the minimum; CI tests the two latest releases.
- `CGO_ENABLED=0` for every build in v0.1.0. `go vet ./...` and `gofmt -l .` clean.
- `GOAMD64=v1` in all release builds (avoids FMA fusion differences).
- Determinism guarantee: identical trace hash and identical frames for the same
  (`GOOS`, `GOARCH`). Cross-architecture identity is a goal, verified by CI on
  linux/amd64 and windows/amd64, not a hard requirement for arm64.
- No wall-clock reads inside `sim` or anything it calls. `time.Now` is allowed only in the
  player loop and CLI timing.
- No allocations in the rasterizer inner loops (verified by benchmarks with `-benchmem`).

## 4. Repository layout

```
veduta/
  go.mod                      module github.com/riftbane/veduta
  LICENSE                     MIT
  README.md
  CHANGELOG.md
  SPEC-v0.1.0.md              this file (moves to docs/spec/ after release)
  CLAUDE.md                   agent instructions for developing the engine itself
  install.sh
  .github/workflows/ci.yml
  .github/workflows/release.yml

  veduta.go                   Run(Game), Context, Input, RegisterKind — the public API
  gmath/
  gfx/                        backend interface, DrawList, PipelineState, Texture, Mesh
  gfx/soft/                   software rasterizer (the only backend in v0.1.0)
  sprite/                     2D batcher on top of gfx
  scene/                      entities, transforms, camera, scene loading
  sim/                        fixed-step loop, RNG, input script, trace, invariants, snapshot
  asset/                      source formats, compiler, .vda container, loaders
  inspect/                    reports, contact sheets, diff, query
  platform/                   window + input for the player build
    window_linux.go           X11, pure Go
    window_windows.go         user32/gdi32 via syscall, pure Go
    window_other.go           stub that errors at runtime (all other GOOS)
  mcp/                        JSON-RPC 2.0 over stdio, tool registry
  internal/cli/               command implementations shared by cmd/veduta
  internal/update/            release lookup, download, checksum, atomic replace
  cmd/veduta/                 the tool binary
  template/                   files copied by `veduta init` (embedded with embed.FS)
  testdata/                   golden images, sample assets, scenarios
  docs/                       format references generated from Go types (v0.1: hand-written)
```

## 5. Architecture

Three roles, one module:

| Role | What it is | Where it runs |
|------|-----------|---------------|
| **Library** | `github.com/riftbane/veduta` and subpackages, imported by games | inside the game binary |
| **Tool** | `veduta` binary: CLI + MCP server, asset compiler, inspectors | VPS (headless) |
| **Game** | A Go program calling `veduta.Run(game)` | player machine (window) or VPS (`-headless`) |

Key design decision: **the tool never contains game logic.** Every operation that needs the
game (render, simulate, query) is performed by the game binary in headless mode. The tool
builds the game (`go build ./cmd/game`), runs it with `-headless <subcommand>`, and wraps
the result. Consequences:

- The MCP server process stays alive while the agent edits code; compile errors are
  returned as tool results.
- The CLI and the MCP server expose the same operations; the MCP layer is a typed wrapper.
- A game binary can always reproduce any report on its own, without the tool.

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
    RNG     *sim.RNG                        // seeded, deterministic
    // Trace emits a structured event into the current tick's trace.
    Trace(event string, fields map[string]any)
    // Invariant registers a named predicate evaluated after every tick.
    Invariant(name string, pred func() bool)
    // Snapshot/Restore of user state: games register a codec once in Init.
    RegisterState(codec StateCodec)
}
```

`Input` is a value type: pressed/held/released key sets (W3C `KeyboardEvent.code` names:
`KeyW`, `Space`, `ArrowLeft`…), mouse position, mouse buttons, and a `Text` string for
typed characters. It is identical whether it comes from a real window or a scenario script.

### 5.2 Manifest `veduta.json`

```json
{
  "veduta": "project/1",
  "name": "mygame",
  "engine": "v0.1.0",
  "entry": "./cmd/game",
  "resolution": [1280, 720],
  "inspect_resolution": [640, 360],
  "tick_rate": 60,
  "default_scene": "main",
  "default_seed": 1,
  "assets": "assets",
  "cooked": "assets/.cooked",
  "invariants": ["finite_positions", "within_bounds"],
  "bounds": [[-100, -50, -100], [100, 100, 100]]
}
```

## 6. Engine modules

### 6.1 `gmath`
`Vec2/3/4`, `Mat3/4` (column-major, right-handed, Y up, -Z forward), `Quat`, `Rect`,
`AABB`. Deterministic `Sin/Cos/Atan2` implemented in Go (no assembly paths). Float32 for
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
  tile via a worker pool. Deterministic output by construction (tiles never overlap).
- Debug render modes: `color`, `wireframe`, `normals`, `depth`, `ids`, `silhouette`,
  `overdraw`, `uv_checker`, `collision`.

### 6.3 `sprite`
Batches textured quads into the same `DrawList` with an orthographic camera and a
`PipelineState{Depth:false, Blend:alpha}`. Supports 9-slice and a built-in bitmap font
(embedded PNG atlas generated at build time from a public-domain 8×8 font) for HUD text.

### 6.4 `scene`
`Entity{ID uint32, Name string, Kind string, Transform, Model, Material, Tags, AABB,
Visible, State any}`. Parent/child transforms. Scene files load into this structure;
`kind` instantiates the registered Go behaviour. Built-in kinds: `static`, `camera`,
`light`.

### 6.5 `sim`
- `Loop`: fixed `tick_rate` (60). No interpolation in v0.1.0; the player renders once per
  tick and sleeps to hold 60 fps.
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

### 6.6 `platform`
- Linux: X11 protocol over the unix socket, pure Go. Handshake, `CreateWindow`,
  `MapWindow`, `PutImage` in horizontal strips under the max request size (use BIG-REQUESTS
  when advertised), `KeyPress/Release`, `MotionNotify`, `ButtonPress/Release`,
  `ConfigureNotify`, `WM_DELETE_WINDOW`. Keysym → W3C code table for a US layout.
- Windows: `RegisterClassExW`, `CreateWindowExW`, message loop with `syscall.NewCallback`
  window procedure, `StretchDIBits` with a top-down 32-bit `BITMAPINFO`. `WM_KEYDOWN/UP`,
  `WM_MOUSEMOVE`, `WM_LBUTTONDOWN`…, `WM_SIZE`, `WM_CLOSE`. Scan code → W3C code table.
- Both: `runtime.LockOSThread` in `init`, window created and pumped on the main goroutine.
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
6. CI runs every scenario in `testdata/` on linux/amd64 and windows/amd64 and compares
   trace hashes and rendered frames byte-for-byte.

## 8. Asset sources

All source files are JSON with a `"veduta": "<type>/<version>"` header. Unknown fields are
errors (strict decoding), so typos surface immediately. Paths are relative to `assets/`.
Colors are `#RRGGBB` or `#RRGGBBAA`. Angles are degrees in source files, radians in code.

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
`alpha` ∈ `opaque|blend|cutout` (`cutoff` default 0.5). `texture` is optional.

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
      "position": [3, 0.5, -2], "tags": ["gem"] }
  ]
}
```

### 8.5 Scenario — `tests/scenarios/<name>.scenario.json`

```json
{
  "veduta": "scenario/1",
  "scene": "main",
  "seed": 42,
  "ticks": 300,
  "inputs": [
    { "tick": 10, "press": ["KeyW"] },
    { "tick": 70, "release": ["KeyW"] },
    { "tick": 80, "press": ["Space"] }
  ],
  "expect": [
    { "tick": 120, "entity": "player", "path": "position.z", "op": "<", "value": -1 },
    { "tick": 300, "trace": "gem_collected", "count_min": 1 }
  ],
  "invariants": ["finite_positions", "within_bounds", "entity_count_max:500"],
  "screenshots": [0, 60, 120, 300]
}
```

`path` grammar: dotted access on the entity summary (`position.x`, `tags`, `visible`,
`state.<field>` for exported game state). `op` ∈ `< <= == != >= > contains`.

### 8.6 Compiled container — `.vda`

Little-endian, chunked, inspired by PNG: magic `VDA1`, then chunks `(type[4], length u32,
payload, crc32)`. Chunk types in v0.1.0: `MESH`, `TEXR` (BGRA8 + mip levels), `MATL`,
`SCEN`, `META` (source path, source hash, compiler version). Unknown chunks are skipped.
`veduta cook` compiles only sources whose hash changed (hash stored in `META`).

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
`SCENE_UNLIT`, `SCENE_ZFIGHT_RISK` (coplanar faces).
Sheets: `camera`, `top`, `ids` (entity id false-color with legend).

### 9.4 Diff
`diff a.png b.png` → `{"changed_pixels": n, "changed_ratio": r, "bbox": [...], "max_delta": d}`
plus a sheet: `a | b | heat` side by side.

### 9.5 Query
`query --frame f.vda-frame --at x,y` → entity at pixel, depth, normal.
`query --frame f --coverage` → per-entity pixel counts, screen-space bounding boxes,
occlusion ratio (visible pixels / projected pixels).

`render` can write a **frame bundle** (`.vframe`: color + depth + ids + camera) so
`query` and `diff` operate on it without re-rendering.

## 10. `veduta` CLI

| Command | Purpose |
|---------|---------|
| `veduta init [dir] --name N [--module M]` | create a game project from the embedded template |
| `veduta doctor` | check Go version, git, project manifest, engine/binary version match, update status |
| `veduta build` | `go build` the game (`CGO_ENABLED=0`), cook assets first if stale |
| `veduta run` | run the player binary (refuses if no display; never used on the VPS) |
| `veduta cook [--force]` | compile changed asset sources to `.vda` |
| `veduta render --scene S [--tick T] [--seed N] [--camera preset] [--mode M] [--out f.png] [--bundle]` | render one frame headless |
| `veduta simulate --scenario F` or `--scene S --ticks N --seed N [--input script.json]` | run ticks, write trace, screenshots contact sheet, verdict |
| `veduta inspect model|texture|scene NAME [--focus ISSUE] [--sheets list]` | report + sheets |
| `veduta query --frame F --at x,y|--coverage` | ID-buffer queries |
| `veduta diff A B [--out f.png]` | image diff |
| `veduta test [--update-golden]` | `go test ./...` + all scenarios + golden images |
| `veduta fuzz --scene S --games N --ticks T --seed N` | random input games, invariant violations, minimized repro written to `tests/scenarios/fuzz_<hash>.scenario.json` |
| `veduta release vX.Y.Z` | checklist (clean tree, test, cook, smoke render) → CHANGELOG entry → tag → push; CI publishes |
| `veduta mcp` | serve the MCP server on stdio |
| `veduta update [--check]` | update the tool binary from GitHub Releases |
| `veduta upgrade` | set the project's `go.mod` and `veduta.json` engine version to the binary's version, `go mod tidy`, run migrations |
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

## 11. MCP server tools

Names and input schemas (JSON Schema, `additionalProperties: false`). Every tool returns a
`text` block with the report JSON and, when applicable, `image` blocks.

| Tool | Input | Output |
|------|-------|--------|
| `status` | `{}` | versions, project, cooked/stale assets, last build result, update availability |
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
| `docs` | `{ topic }` | format reference text for `model|texture|material|scene|scenario|api` |

Rules: image blocks are ≤ 640×360 unless `width/height` are given (max 1280×720).
`simulate` never returns more than one image; ask for `render` at a specific tick for
detail. `trace` output is paginated (max 200 ticks per call).

Client configuration produced by `veduta init` (`.mcp.json` at the project root):

```json
{ "mcpServers": { "veduta": { "type": "stdio", "command": "veduta", "args": ["mcp"] } } }
```
Equivalent: `claude mcp add --scope project veduta -- veduta mcp`.

## 12. Project template (`veduta init`)

```
mygame/
  veduta.json
  go.mod                      require github.com/riftbane/veduta vX.Y.Z
  cmd/game/main.go            func main() { veduta.Run(&game.Game{}) }
  game/game.go                Game impl; registers kinds, invariants, state codec
  game/kinds.go               player, collectible behaviours
  assets/models/*.model.json  ground, hero (capsule-ish from cylinder+spheres), gem
  assets/textures/*.tex.json
  assets/materials/*.mat.json
  assets/scenes/main.scene.json
  tests/scenarios/*.scenario.json   move, collect, idle
  .mcp.json
  CLAUDE.md                   agent instructions for building a game (Appendix A)
  .gitignore                  out/ assets/.cooked/ bin/
  .github/workflows/release.yml     builds linux/amd64 + windows/amd64 on tag, uploads zips
  README.md
```

Demo game: WASD moves the hero on a ground plane, gems are collected on AABB contact,
HUD shows score and tick, `KeyR` resets. Three scenarios pass out of the box.

## 13. Distribution

### 13.1 Releases
Tag `vX.Y.Z` → `release.yml` builds `cmd/veduta` for `linux/amd64`, `linux/arm64`,
`windows/amd64` with `-trimpath -ldflags "-s -w -X main.version=$TAG"`, `CGO_ENABLED=0`,
`GOAMD64=v1`; archives `veduta_<ver>_<os>_<arch>.tar.gz` (`.zip` on Windows); writes
`checksums.txt` (sha256); publishes with `gh release create "$TAG" --generate-notes dist/*`
(`gh` is preinstalled on GitHub-hosted runners; no third-party actions besides
`actions/checkout` and `actions/setup-go`).

### 13.2 `install.sh`
POSIX `sh`, usable as `curl -fsSL https://raw.githubusercontent.com/riftbane/veduta/main/install.sh | sh`.

1. Detect OS/arch (`uname -s`, `uname -m`); refuse unsupported combos with a clear message.
2. Resolve version: `$VEDUTA_VERSION` if set, else the latest release via the GitHub API.
3. Download archive + `checksums.txt`; verify with `sha256sum` or `shasum -a 256`; abort on
   mismatch.
4. Install to `$VEDUTA_HOME/bin` (default `~/.local/bin`); `--prefix DIR` overrides.
   Print a PATH hint if the directory is not on `PATH`.
5. Check `go` ≥ 1.25. If missing and `--with-go` (default when non-interactive), download
   the official tarball from `go.dev/dl` into `~/.local/go` and print the PATH line.
6. Check `git`; print instructions if missing.
7. Run `veduta doctor` and print next steps: `veduta init mygame && cd mygame && claude`.

Flags: `--prefix`, `--version`, `--with-go`, `--no-go`, `--yes`. No `sudo` unless `--prefix`
points to a root-owned dir. Idempotent.

### 13.3 Update policy
- Config file `~/.config/veduta/config.json`:
  `{ "auto_update": "check" | "auto" | "off", "check_interval_hours": 24 }`; default `check`.
- `check`: `veduta mcp` and `veduta doctor` look up the latest release at most once per
  interval (cache in `~/.cache/veduta/update.json`, offline-safe, 3 s timeout) and report
  availability in `status`.
- `auto`: `veduta mcp` applies the update **before** serving (never mid-session), then
  execs the new binary. Also applied by `veduta doctor`.
- `veduta update`: download, verify checksum, write to a temp file next to the binary,
  atomic rename. Windows tool binary is out of scope (§2).
- The engine library version is pinned by the game's `go.mod`; updating the tool never
  changes a project. `veduta upgrade` does, explicitly, and records the change in the
  project's `CHANGELOG.md`. `doctor`/`status` warn when tool and project versions differ
  by a minor or more.

### 13.4 Compatibility
Semantic versioning. Within `0.x`, minor versions may break formats; every source format
carries `"veduta": "<type>/<n>"` and `veduta upgrade` runs migrations `n → n+1` (none in
v0.1.0). Patch releases never change formats or trace hashes.

## 14. CI

`ci.yml` on push and PR: `gofmt -l`, `go vet`, `go test ./...` (unit, golden, scenarios),
`go test -bench . -benchmem -run ^$ ./gfx/soft` with an allocation check,
`CGO_ENABLED=0 GOOS=windows go build ./...`, `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./...`,
then `veduta init tmp && cd tmp && veduta test` (template smoke test), on `ubuntu-latest`
and `windows-latest`. Trace hashes of `testdata/` scenarios are compared across the two.

## 15. Acceptance criteria for v0.1.0

1. On a fresh Ubuntu 24.04 VPS: `install.sh` → `veduta init demo` → `cd demo` →
   `veduta test` passes in under 5 minutes total, no network after install except GitHub.
2. `claude` started in `demo/` lists all tools in §11; `render`, `simulate`, `inspect`,
   `query`, `diff` return images inline.
3. `veduta release v0.1.0` from the template project produces a GitHub Release with
   `demo_v0.1.0_windows_amd64.zip` and `demo_v0.1.0_linux_amd64.tar.gz`; both run,
   open a 1280×720 window at 60 fps, respond to WASD, collect gems, close cleanly.
4. Two runs of the same scenario produce identical trace hashes and identical PNGs; the
   same holds between `ubuntu-latest` and `windows-latest` CI jobs.
5. `veduta inspect model hero` on a model with one part's normals flipped reports
   `MESH_FLIPPED_NORMALS` with the part index and produces a `normals` sheet where the
   part is visibly wrong.
6. `veduta fuzz --games 200 --ticks 600` on the demo finds no invariant violation; a
   deliberately broken build (player speed × 100) yields a minimized `within_bounds` repro.
7. `veduta update` on an older tool version installs the latest release and verifies it.
8. Rasterizer benchmark: `1280×720`, 10k textured triangles, ≤ 8 ms per frame on 4 cores
   of a typical VPS (target, not gate), 0 allocs/op in the triangle loop (gate).
9. `go.sum` contains no third-party modules.

## 16. Order of work

Each phase ends with a commit, a passing `veduta test`, and a note in `CHANGELOG.md`.

1. `gmath`, `gfx` types, `soft` rasterizer to PNG, golden test of a lit textured cube.
2. `asset`: model/texture/material compilers, `.vda`, `cook`.
3. `scene`, `sim`, `veduta.Run` headless mode, trace, invariants, scenarios.
4. `inspect`: reports, sheets, diff, query, frame bundles.
5. `cmd/veduta` CLI, `init` template with demo game, `test`, `fuzz`.
6. `mcp` server; verify with Claude Code on the repo itself.
7. `platform` Linux X11, then Windows; player loop; cross-compile checks.
8. `install.sh`, `update`, `upgrade`, `release`, workflows, README, tag `v0.1.0`.

Phases 1–6 are fully verifiable on the VPS with the engine's own tools. Phase 7 is
verified by compiling on CI and by the human running the demo build.

## 17. Open questions (decide during v0.1.0, record in CHANGELOG)

- Float32 vs float64 in `sim` (default float32 for positions; revisit if precision issues
  appear in scenarios longer than 10 minutes).
- Whether `simulate` should also emit a top-down trajectory sheet by default (cheap,
  probably yes).
- Bitmap font source (must be public domain; embed the PNG and its license note).

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
- Run `fuzz` before every release; add minimized repros as scenarios.
- Commit small, message in imperative mood. Tag releases with `release` only when `test` is green.
- Do not add third-party Go modules.
- When something in the engine blocks you, write it down in ENGINE-NOTES.md with a repro,
  do not work around it silently.
```
