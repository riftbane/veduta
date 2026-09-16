# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## Unreleased

### Added

- Levels of detail and a draw distance for models (`docs/model.md`): `lod` lists up to
  four levels, nearest first, each drawn from its `distance` on, either the same model
  rebuilt with `segments` and `rings` halved once more per level or another `model`;
  beyond `draw_distance` the model is not drawn. Distances are measured per entity, from
  the camera to the nearest point of its drawn bounds, as through a 60° lens (an
  orthographic camera counts 0.866 × `size`). `inspect model` reports `lod_triangles`,
  `draw_distance`, `MESH_LOD_NO_GAIN` and `MESH_LOD_MODEL_MISSING`.
- Terrain for worlds (`docs/world.md`): `terrain` (seeded `relief`, `relief_scale`,
  `sea_level`, a `water` material, `lod_distance`) and named `features` applied in file
  order: `hill`, `plain`, `lake` and `sea`, each around a vertex with a `radius`, optional
  `height`, `depth`, `falloff` and `roughness`. Heights are whole millimeters per vertex,
  computed with integers; sites and places stand on pads levelled to the ground at their
  centre; sites avoid water, scatter skips it, a place in water is `WORLD_PLACE_WATER`.
  Each chunk's ground is drawn at up to five levels of detail (every 1, 2, 4, 8, 16
  cells) with skirts closing the cracks between levels, and its water as flat quads
  (built-in material `world:water`). `World.HeightAt` and `World.WaterAt` (also on
  `world.Gen`) give the ground and the water under a position.
- Vegetation for worlds (`docs/world.md`): named `vegetation` rules plant a one-cell
  `prefab` (trees: entities, like scatter) or a flora `model` (grass, flowers: drawn with
  the chunk as one `chunk_<cx>_<cz>_flora_<model>` entity per model, no collision, not
  in the trace) at a `density`, optionally on `biomes` and inside a round area (`cell`,
  `radius`), flora with a seeded `scale`. Flora follows its model's `lod` (level k keeps
  one plant in 2^k) and `draw_distance`. Each chunk's ground is drawn with the coarsest
  grid within 6 cm of its vertices (gentle ground costs a quarter of the triangles or
  less), and `HeightAt` follows it.
- Tools to shape and plant a world (`docs/world.md`): MCP `world_terrain` and CLI `veduta
  world terrain` add a hill, plain, lake or sea (refused with the compiler's reasons when
  invalid) and report the ground before and after, the chunks changed, places now in
  water and sites removed; `world_vegetation` / `world vegetation` add a tree or flora
  rule and report its plants, triangles and budget warnings; both write only the new
  array element and take `dry_run`. `world_remove` removes places, features and
  vegetation rules by name. `world_map` reports the ground, features and plants and
  shades its image by the relief with water and labelled circles; `world_query` reports
  the height, water, features and plants of a cell. Inspection adds `WORLD_PLACE_WATER`
  and checks water materials and vegetation models and prefabs.
- `scene.Draw` skips entities whose drawn bounds lie wholly outside the camera's view
  volume (1 mm margin, so no pixel changes) and chooses levels of detail;
  `DrawOptions.Stats` counts culled, distant and reduced entities, and `render` reports
  them as `draw`.

- The template's `overworld` has relief, a hill (`lookout`), a lake (`pond`), a sea far
  south, grass on the plain, a flower meadow and a grove; `grass` and `flower` flora
  models and a `tree` with a level of detail and a draw distance. Its hero walks on the
  terrain (`World.HeightAt`) and stops at the shore (`World.WaterAt`); a new `shore`
  scenario checks it and the `world` scenario checks the climb.

### Changed

- `veduta.Version` is `v1.3.0` (snapshots of v1.2.0 do not restore).
- `asset.CompilerVersion` is `veduta-asset/0.4.0`: `MESH` chunks end with the draw
  distance and the levels (`docs/vda.md`), so every asset is recompiled once.

### Decisions

- **Terrain, vegetation and the rendering savings extend `SPEC-v1.0.0.md` at the user's
  request (2026-09-16).** The spec is not edited; this entry, `docs/world.md`,
  `docs/model.md`, `docs/inspect.md` and `docs/vda.md` are the record. The MCP tools
  `world_terrain` and `world_vegetation` mirror `veduta world terrain|vegetation`.
- **Two savings, both automatic: the view limit and levels of detail.** Culling is by
  drawn bounds against the view volume with a 1 mm margin, so it never changes a pixel;
  the camera's `far` is the view limit. Levels of detail are per model (`lod`,
  `draw_distance`) and per chunk (ground grids, flora thinning), chosen per entity per
  frame from the camera, so rendering stays a pure function of the state.
- **Distances are measured as through a 60° lens.** Orthographic cameras have no
  distance, and a narrow lens magnifies: scaling by tan(fov/2)/tan(30°), and using
  0.866 × `size` for orthographic cameras, keeps "farther means fewer triangles" true to
  what reaches the screen.
- **Automatic levels halve segments.** Models are primitives with `segments` and
  `rings`, so a level needs no second source; a level may name another model when that
  is not enough. Boxes keep their triangles (`MESH_LOD_NO_GAIN` says so).
- **Heights are integer millimeters per vertex, features apply in file order.** As with
  biomes, integer arithmetic keeps every machine and any chunk order in agreement; file
  order makes "flatten this hill" (a plain after a hill) expressible. Lakes and seas are
  bowls with a dry rim 5 cm above the water so the water plane never z-fights with the
  ground and never floats over a downhill shore.
- **Skirts close the cracks between chunk levels.** Stitching edges would make a chunk's
  mesh depend on its neighbours' levels; a skirt as deep as the edge's height range
  closes any crack from the higher side, costs nothing on straight edges, and keeps every
  chunk's meshes a function of the chunk alone.
- **A chunk's finest grid is the coarsest within 6 cm.** Gentle relief then costs a
  quarter of the triangles near the camera; flat biome edges keep 1-cell precision,
  uneven ground accepts 2-cell biome steps. `HeightAt` follows the same triangles, so
  heroes stand exactly on what is drawn.
- **Flora is baked, not spawned.** The trace summarizes every entity every tick; a meadow
  as entities would bury it. Flora is one model per chunk and flora model, without
  collision, thinned by rank (a seeded half per level) and cut at the model's draw
  distance. Trees stay prefabs: they collide.
- **Structures stand on pads.** Sites and places level their footprint to the ground at
  its centre and blend over 2 cells, so houses never float on slopes; sites are dropped
  from water (checked on the ground before pads, so there is no cycle).
- **One namespace for places, features and vegetation rules.** `world_remove` takes a
  name; duplicates across the three lists are compile errors.

## v1.2.0 — 2026-09-15

### Added

- Two source formats for generated maps (`docs/prefab.md`, `docs/world.md`):
  `assets/prefabs/<name>.prefab.json` (`prefab/1`), a group of entities with a footprint in
  meters, tags and placement rules (`biomes`, `min_distance`), and
  `assets/worlds/<name>.world.json` (`world/1`), a seeded world of integer cells and chunks
  with biome noise, `scatter` and `sites` rules, explicit `places` and persistent
  `entities`. Both cook to `.vda` (`PRFB`, `WRLD`); a world depends on its prefabs, so a
  changed prefab recooks the world, and prefabs known at cook time are checked against
  their cells, site spacing and the world's extent (`extent × chunk × cell ≤ 8192 m`).
- Scenarios may name a `world` and a start cell `at` instead of a `scene`.
- Package `world`: the generator behind a world. Integer-only biome noise cut at
  quantiles so biome shares follow their weights; scatter decided cell by cell; sites on
  a jittered region grid that keeps their `min_distance`, yielding to places and to
  earlier rules; every chunk generated on its own, in any order, with one greedy-merged
  ground mesh per chunk; `Validate`, `Solve` (rings outward, east first, clockwise),
  `Displaced`, `Region`, `Query` and `Check` for the tools.
- Worlds run: `Context.LoadWorld(name, at)` and `Context.World()` (`Focus`, `CellOf`,
  `Loaded`), the chunks within `view` of the focus loaded at the start of every tick and
  unloaded one chunk farther out, structures spawned whole, one `chunk_load` /
  `chunk_unload` trace event per chunk (streamed entities emit no `spawn`/`despawn`),
  `world_load` on load; snapshots carry the window, so `Restore` streams on; while a
  world is loaded `within_bounds` uses its extent. `render`, `simulate` and `snapshot`
  take `--world W --at x,z`; the manifest's `default_world` starts the player in a world;
  `describe` lists worlds and prefabs. The template ships `overworld` with tree, gem,
  house and village prefabs and a `world` scenario walking across chunk borders.
- `gfx.Backend.UpdateMesh` and `scene.Resources.AddModel`/`Remove`: chunk ground meshes
  are uploaded when a frame is rendered and their handles reused.
- Tools for worlds (`docs/world.md`, `docs/inspect.md`): `inspect prefab` and `inspect
  world` (codes `PREFAB_*`, `WORLD_*`, a map sheet); `veduta world map|query|place|remove`
  and the MCP tools `world_map`, `world_query`, `world_place` and `world_remove`.
  `world_place` validates a cell or searches outward from one, writes only a valid place
  into the world file (every other byte untouched) and reports the sites it displaces;
  `render`, `simulate` and `fuzz` take `world` and `at`. The `docs` tool serves `prefab`
  and `world`.

### Changed

- `asset.CompilerVersion` is `veduta-asset/0.3.0`: every cooked asset is recompiled once.
- The player prepares its run without recording a trace: no tick spends time summarizing
  every entity (a streamed world has hundreds), and nothing read the trace.
- `veduta.Version` (what `game -version` and `describe` print, and what snapshots are
  checked against) is `v1.2.0`; it had stayed at `v1.0.0` through v1.1.x.

### Decisions

- **Worlds extend `SPEC-v1.0.0.md` at the user's request (2026-09-15).** The spec file is
  not edited (as for v1.1.0); this entry, `docs/prefab.md`, `docs/world.md` and
  `docs/inspect.md` are the record. Prefabs and worlds are cooked kinds and inspect kinds
  beyond §11's list; the MCP tools `world_map`, `world_query`, `world_place` and
  `world_remove` mirror `veduta world`.
- **Integer cells, prefabs and a solver instead of hand-written coordinates.** An agent
  writing thousands of positions overlaps structures and misses biomes; so a world is
  rules plus named places, places go through `world_place`, which refuses an invalid
  cell and can search for a valid one, and every generated position is the engine's.
- **A world reaches at most 8192 m from the origin.** A float32 position loses about a
  millimetre of precision there (`P·V·M` cancels in eye space); farther out a 320×240
  frame would jitter. No floating origin: 16384 × 16384 cells at the defaults is 68
  minutes of walking at 4 m/s. Only the x/z plane is generated.
- **Generation is integer-only and neighbour-free.** The biome noise is Q16 value noise
  cut at quantiles of a fixed sample; scatter is a hash per cell; sites live inside
  regions sized for their footprint and largest `min_distance`, so no rounding differs
  between architectures and any chunk generates alone, whatever was generated before.
  Sites yield to places and to earlier rules; scatter yields to both.
- **Streamed spawns are silent.** A chunk of hundreds of entities would emit hundreds of
  `spawn` events per border crossing; the trace gets one `chunk_load` per chunk (with the
  first id and the count) and one `chunk_unload`. Overlaps present when a chunk loads are
  not collisions, as at scene load. Chunks unload one chunk beyond `view` (hysteresis).
- **`within_bounds` follows the loaded world.** The project's `bounds` would fail every
  world game at the first border; while a world is loaded the invariant uses the world's
  extent along x and z and the project's range along y.
- **The camera and the persistent entities of a world are relative to the start cell.**
  `render --world W --at x,z` shows the world there with the hero standing there.
- **Prefab footprints and rules are in meters.** A prefab knows no cell size; the world
  rounds both up to whole cells, so `inspect prefab` can check the footprint.
- **`world_place` edits the file in place.** Only the `places` array changes (an element
  appended or removed in the file's own indentation); the result is parsed again before
  it is written, so a wrong edit is an error, never a broken file.

## v1.1.1 — 2026-09-15

### Fixed

- `veduta init` from a build without a version (`go run ./cmd/veduta`, `go install` with no
  `-ldflags`) created a project on engine v1.0.0 while the template it copied reads the
  stick of v1.1.0, so the project did not build. The template's `veduta.json` now names the
  engine its code is written against, and `veduta release` moves it to each engine release
  along with the changelog; a test holds the two together.

## v1.1.0 — 2026-09-15

The console's final controls: a D-pad, an analog stick, A, B, X, Y, Select, Start and Home,
with a keyboard and a mouse standing in for them until the handheld exists. A minor
release: every v1.0.0 file keeps its meaning, and a v1.0.0 project traces the same.

### Added

- The console's analog stick: `Input.Stick` (`gmath.Vec2`, each axis −1…1, +X right, +Y
  up, zero at rest), `sim.InputState.SetStick`, and the optional `stick` field
  (`{ "x": …, "y": … }`) of scenario and input-script events, introduced in this release
  (older tools reject a file that uses it). Traces of games that do not read the stick are
  unchanged.
- The player reads the console's final controls, and a keyboard and mouse stand in for them
  until they exist (`docs/api.md`, "The console's controls"): a pad's `ABS_X`/`ABS_Y` move
  `Input.Stick` (15% dead zone, `+Y` up); Home (`BTN_MODE`) closes the player on its own, as
  Select+Start does; a keyboard's W, A, S and D press the arrows as well as their own codes;
  a mouse moves the stick and stays where it is left (400 counts to the end, any button
  recentres it); an absolute pointer such as QEMU's `usb-tablet` is the stick directly.
  Mice and tablets are found through `/sys/class/input` after pads and keyboards.
- The template demo walks with the stick as fast as it is pushed, and a fifth scenario,
  `stick`, covers it; the trace hashes of the other four are unchanged. `fuzz` players move
  the stick too, from a random stream of their own, so the keys and mouse of a seed are
  those of v1.0.0.

### Changed

- On a pad whose D-pad is four `BTN_DPAD_*` buttons, `ABS_X`/`ABS_Y` only move the stick and
  no longer press the arrows. Every other pad keeps getting the arrows from them.

### Decisions

- **The console's controls extend `SPEC-v1.0.0.md` §5.1 and §6.6 at the user's request
  (2026-09-14).** The final handheld has a D-pad, an analog stick, A/B/X/Y, Select, Start and
  Home. Buttons stay key codes, as the spec's design wants, so no game, scenario or golden
  changes; only the stick, which a key cannot carry, is new API (`Input.Stick`). The spec
  file is not edited, as `SPEC-v0.1.0.md` was not for v0.2.0; this entry and `docs/` are the
  record.
- **The stick presses the arrows unless the D-pad is buttons.** A game on the console must
  tell the D-pad from the stick, but many cheap pads (perhaps the test pad) report their
  D-pad on `ABS_X`/`ABS_Y` with a hat capability they do not use, so hats cannot decide.
  `BTN_DPAD_*` can: the handheld's D-pad must report them (natural for `gpio-keys`).
- **Mouse as a held stick, not a velocity.** QEMU's `usb-tablet` reports a position, which
  can only be read as "where the stick is"; a relative mouse is read the same way, so both
  behave alike, and a click recentres because a mouse has no spring. Several devices add up,
  clamped, as held keys are counted across devices.
- **Home is a one-button exit chord** (`{BTN_MODE, BTN_MODE}`), so dropped events, the
  release wait on close and "no game can swallow it" apply to it unchanged. Its keyboard
  stand-in stays Ctrl+Q; the `Home` key remains a key a game may read.
- **W, A, S, D press both codes.** Replacing `KeyW` with `ArrowUp` would break games that
  read WASD; pressing both is what "held while anything holds it" already allows, and a
  game summing WASD and the arrows before normalising (the template) is unaffected.

### Acceptance (§15)

Every criterion of `SPEC-v1.0.0.md` §15, run again against the candidate `v1.1.0-rc.1`
(commit a125a48, tagged by hand and published as a pre-release by `release.yml`).

1. **Fresh VPS → `veduta test` in under 5 minutes.**
   `docker run --rm -v "$PWD/scripts/acceptance:/a:ro" ubuntu:24.04 sh /a/fresh_vps.sh v1.1.0-rc.1`:
   install 16 s, `veduta init demo && cd demo && veduta test` 35 s, 51 s in total; tool and
   project engine are v1.1.0-rc.1, the five scenarios pass.
2. **`claude` in a project lists every §11 tool; visual tools return images.** With the
   candidate on PATH in a `veduta init` project, `claude -p '<list tools, status, render,
   simulate stick, inspect hero, query, diff>' --mcp-config .mcp.json --strict-mcp-config
   --allowedTools 'mcp__veduta__*'` listed build, cook, diff, docs, fuzz, inspect, query,
   release, render, simulate, status, test and trace; status reported the console target and
   engine v1.1.0-rc.1, render 320×240, simulate `stick` passed with one image, inspect `hero`
   0 issues and a sheet, query at 160,120 named `player`, diff 0 changed pixels.
   `go test ./internal/cli -run TestMCPEndToEnd` passes.
3. **A game release is a card for the console.** In a `veduta init` project, the `build
   archives` step of its `release.yml`, run as written with `GITHUB_REF_NAME=v0.1.0`:
   both archives and `checksums.txt`; the arm64 archive unpacks to `demo/` with the binary,
   `veduta.json`, `README.md`, `assets/` and a `card.json` at version `v0.1.0`, and
   `qemu-aarch64-static ./demo -project . -headless render --scene main` renders 320×240.
   Emulated end to end: `vedutaos qemu --fresh --game <engine>/template` booted the dashboard,
   Enter started the demo, and tablet positions sent with QMP walked the hero right, stopped
   it and walked it up.
4. **Identical traces and frames across runs and architectures.** CI runs 34902871695
   (main) and 34934352193 (tag) on a125a48: ubuntu-latest and windows-latest (Go stable and
   oldstable) and linux/arm64 under qemu-user pass the same goldens, with
   `TestEngineHasNoFusedMultiplyAdd`. Locally `GOARCH=arm64 go test -exec qemu-aarch64-static ./...`
   passes. A project made by the v1.0.0 tool and upgraded to the candidate gives the same
   four trace hashes as its v1.0.0 build.
5. **Flipped normals are reported and visible.** `go test ./inspect -run TestModelFlippedNormals`.
6. **Fuzzing.** `veduta fuzz --games 200 --ticks 200 --seed 1` on the demo: 0 violations in
   6.3 s. With `PlayerSpeed = 400.0`: every game violates `within_bounds`; minimized repro
   `tests/scenarios/fuzz_cd96a0cd.scenario.json` (6 ticks, one input), the same file as for
   v1.0.0, which `veduta simulate` reports as `fail`.
7. **Update and upgrade.** The v1.0.0 tool from GitHub Releases with a fresh configuration:
   `veduta update --check --channel beta` → "v1.1.0-rc.1 is available", then
   `veduta update --channel beta` → "updated v1.0.0 → v1.1.0-rc.1 … verified"; the
   configuration follows beta. `veduta upgrade` of a v1.0.0 project moved `go.mod` and
   `veduta.json` to the candidate and its tests pass. Pinning:
   `go test ./internal/cli -run TestUpgradePinsTheV0Defaults`.
8. **Rasterizer.** `go test -bench . -benchmem -run '^$' ./gfx/soft`:
   `BenchmarkDraw10kTriangles320x240` 6.1 ms/frame, 0 allocs/op, on a loaded VPS where
   v1.0.0 measured 5.6 ms in the same minute (the rasterizer is unchanged).
9. **No third-party modules.** There is no `go.sum`.
10. **The tool and the console.** `veduta run` on this VPS refuses with "no framebuffer on this
    machine … Set VEDUTA_FB …"; `go list -deps ./cmd/veduta` holds no `platform`;
    `CGO_ENABLED=0 GOOS=windows go build ./cmd/veduta` succeeds.
11. **2D.** `go test . -run TestTwoDFixture`.

## v1.0.0 — 2026-09-14

The first stable release, and a breaking one: Veduta now makes games for the Veduta
console — a linux/arm64 board with a 320×240 panel refreshed at 20 Hz and a gamepad — and
the machines that author them no longer open a window. `SPEC-v1.0.0.md` describes the
result and supersedes `SPEC-v0.1.0.md`, which stays as the record of v0.1.0.

### Upgrading from v0.x

- Run `veduta upgrade`. A project that left `resolution`, `inspect_resolution` or
  `tick_rate` to their defaults gets the old values written into `veduta.json`
  (`[1280, 720]`, `[640, 360]`, `60`), so its size, physics and trace hashes do not move;
  the project's changelog names every pinned field. Then decide: a game meant for the
  console should move to `[320, 240]` and `20` deliberately, re-time its scenarios and
  regenerate its goldens (`veduta doctor` warns until it does).
- The game no longer opens a window on Windows or a desktop Linux session: see it with
  `render`, `simulate` and `inspect`, and play it on the console (or at a Linux text
  console with a framebuffer). `VEDUTA_BACKEND=x11` is an error and `platform.BackendX11`
  is gone.
- Add `linux/arm64` to the project's release workflow and a `card.json` beside
  `veduta.json` (compare with what `veduta init` writes); `veduta release` refuses a
  project whose release the console cannot install.
- Run `veduta doctor`: it lists every line of the game's own code that compiles to a fused
  multiply-add on arm64. Wrap each product that feeds `+` or `-` in a conversion to its own
  type (`y += float32(v * ctx.DT)`), or the console's traces drift from the goldens.
- A scene that uses `hitbox` or `layer` cannot be read by a v0.x tool, and cooked assets
  are recompiled once (the asset compiler version moved to `veduta-asset/0.2.0`).
- MCP `render` images are 320×240 by default and at most 640×480 (were 640×360 and
  1280×720); inspection views are framed 4:3.
- A tool configured with `"auto_update": "auto"` moves to v1.0.0 by itself the next time
  `veduta mcp` starts. To stay on v0.x until a project is upgraded, set `auto_update` to
  `check`, or reinstall with `install.sh --version v0.2.0`.

### Added

- The console player: frames are drawn on the panel's framebuffer, found by reading sysfs
  rather than by assuming a device number (a 16-bit framebuffer is the panel, a 32-bit one
  is used when there is none), packed to RGB565 or XRGB honouring a padded stride, and
  written without allocating. `VEDUTA_SCALE` divides the panel so a slower board draws a
  quarter of the pixels and still fills the glass; `VEDUTA_FB` names a framebuffer.
- Gamepads and keyboards, read from evdev and translated into W3C key codes in the platform
  layer, so `sim.Input`, scenarios, traces and goldens are untouched and a session played on
  the console replays like any other. The D-pad is understood as a hat, a stick or four
  buttons (arrows), A is `Space`, B `Escape`, X `KeyF`, Y `KeyR`, L1 `KeyQ`, R1 `KeyE`,
  Select `Tab`, Start `Enter`; keyboards report keys by physical position, keypad
  included. Auto-repeat is dropped; what is held is released when the kernel admits it lost
  events or a device is unplugged, and devices are looked for again every second, a pad
  plugged in while a keyboard is being read included. Select and Start together, or Ctrl+Q
  on a keyboard, close the player: on a device with no keyboard it is the way back to the
  dashboard. `VEDUTA_PAD` names a device.
- `title` and `icon` in the project manifest: the name a player sees and the picture shown
  beside it, neither of which the identifier in `name` can carry (`docs/project.md`).
- `card.json`, the frozen `card/1` description the console lists, written by `veduta init`;
  a new project's release workflow builds linux/arm64 and linux/amd64 archives that each
  unpack to a card folder, with the tag stamped into the card as its version.
- `internal/fused`, which reads a linux/arm64 binary, finds fused multiply-add instructions
  by their encoding and maps them to source lines. `TestEngineHasNoFusedMultiplyAdd` builds
  the tool and the template game for arm64 and fails on any engine line that fused.
- A CI job that runs the whole suite for linux/arm64 under `qemu-aarch64-static`, so the
  goldens recorded on amd64 are checked on the console's architecture.
- `veduta doctor` checks the project against the console: where it plays (and whether this
  machine has a framebuffer), a `tick_rate` above 20 or a `resolution` that is not 4:3, the
  linux/arm64 build (a failure is located), every line of the game's code that fused into a
  multiply-add, a release workflow without linux/arm64, and a `card.json` that is missing
  or disagrees with `veduta.json`. Findings that do not stop the game are warnings, marked
  `warn`. The MCP `status` tool reports the target.
- 2D in the scene format: an entity's optional `hitbox` (a local-space box that replaces
  the model's bounds for collisions, `ctx.Overlapping`, `no_overlap` and the trace's
  `aabb`; an entity with a hitbox and no model is a trigger zone) and `layer` (−1000…1000,
  the first key of the draw order); `scene.Camera2D(center, height)`; the `2d` docs topic
  with the recipe and its three silent traps; a `quad` model and a `sprite` material in the
  template; a 2D fixture project under `testdata/twod` run through `render --bundle`,
  `query --at` and `simulate`.
- The demo reads the arrows as well as WASD, so it plays on the pad, and its new `pad`
  scenario walks, collects and jumps with only the codes the pad produces.
- Tests of the console's input path that need no hardware: `TestEvdevUinput` creates a real
  gamepad through `/dev/uinput` (skipped without access) and reads it through sysfs and
  `/dev/input`; `TestEvdevKeymapCoversKeyCodes` requires the kernel key table to produce
  every W3C code the engine knows and nothing else.

### Changed

- The manifest defaults follow the panel: `resolution` and `inspect_resolution` are
  `[320, 240]` and `tick_rate` is `20` (were `[1280, 720]`, `[640, 360]` and `60`). `ctx.DT`
  is 1/20 by default, so the trace hash of every project relying on the default changes. A
  player asked for no size opens at 320×240, and `simulate --ticks` and `fuzz --ticks`
  default to 200 (ten seconds, as 600 were). The demo's scenarios are re-timed to the same
  seconds.
- MCP images are sized after the panel: `render` returns 320×240 unless asked, at most
  640×480, and one side alone gets the other at 4:3; the tool's schema text is generated
  from those limits. Contact sheets and inspection sheets are fitted inside 640×720, and a
  query image keeps its crosshair sharp at every frame size.
- `inspect scene` views are framed 4:3 (640×480 single views, 317×238 summary tiles), a
  width given alone sets the height to 3/4 of it in every inspector, and the top view draws
  the camera frustum at the aspect of the camera view on the same sheet. Z-fighting is
  judged from the models that are drawn, not from hitboxes (`SCENE_OVERLAP` still compares
  AABBs, the hitbox where one is set), and translucent sprites stacked by layer are
  reported by neither.
- `veduta run` refuses on a machine with no framebuffer (and off Linux), naming
  `VEDUTA_FB` and `VEDUTA_SCALE`, instead of deciding with `$DISPLAY`.
- `veduta release` refuses a project that does not build for linux/arm64 or whose workflow
  publishes no linux/arm64 archive.
- `veduta upgrade` from a v0.x engine to v1 pins the v0.x defaults (see Upgrading), edits
  `veduta.json` as text so every other byte stays, and writes it only after `go get` and
  `go mod tidy` succeed.
- Blended parts are sorted back to front by depth along the camera's view axis, not by
  distance from the eye, which was wrong under an orthographic camera.
- `ctx.Width` and `ctx.Height` are set before `Init` and every `Update`, to the project's
  resolution in every mode, so the HUD can be laid out during `Update`; `Draw` still sees
  the frame it draws.
- The trajectory tile of `simulate` is drawn in the XY plane when the scene camera is
  orthographic and looks along −Z, so a 2D game's paths no longer collapse onto a line.
- Models whose vertices leave the float32 range after sizes, positions and scales are
  applied are refused with an error on the part.
- The rasterizer benchmark draws its 10k triangles into a 320×240 frame
  (`BenchmarkDraw10kTriangles320x240`); 0 allocs/op remains the gate.
- The template's README, `CLAUDE.md` and the API reference speak of the console: the pad's
  bindings, the exit chord, the target, and the rule for rounding products.

### Removed

- The X11 player window (Linux) and the Win32 player window (Windows): eight source files
  and their tests, 5,510 lines. `platform.BackendX11`, the display smoke tests and their
  CI steps went with them; the platform package's `runtime.LockOSThread`, needed only by
  Win32's message pump, is gone too.
- The demo's first-person view (`KeyF`, mouse look, crosshair), its `look` scenario and that
  scenario's two goldens. `Input.MouseDelta`, `Camera.LookFrom` and `Context.LockPointer`
  stay in the API; no player backend produces mouse movement or locks a pointer.

### Fixed

- Triangles of a pixel or less were drawn nearer and darker than they are: the rasterizer
  weighed vertices with edge functions that still carried the top-left fill rule's bias,
  so the weights summed to less than one. `query` reported 6.4 m at the demo hero's centre
  pixel, which is 13.3 m away; depth tests at silhouettes were wrong and small geometry had
  dark speckles. Older than v1.0.0. 28 golden images change by a few pixels each along
  silhouettes and thin rims; no trace hash changes.
- The goldens did not reproduce on linux/arm64: the compiler fused products into
  multiply-adds that round once where amd64 rounds twice. Every product that feeds an
  addition or subtraction in the engine and the demo is now rounded explicitly (133 fused
  engine lines before, 0 after), and the whole suite passes under qemu-aarch64 against the
  amd64 goldens. `inspect` no longer uses `math.Log` and `math.Cbrt`, which differ between
  architectures, and the debug line drawer, the scene sheets' labels and `simulate`'s tile
  height clamp values whose float-to-integer conversion would differ.
- The console never saw a press on a real pad or keyboard: the evdev reader set a read
  deadline already in the past, which the Go runtime refuses without reading. It now reads
  the non-blocking descriptor directly until `EAGAIN`.
- A gamepad was not found unless `VEDUTA_PAD` named it: sysfs capability bitmaps were
  parsed with a word width guessed from the text, while the kernel prints words unpadded.
- A pad plugged in while a keyboard was already being read was never opened, and only the
  left Ctrl made Ctrl+Q close the player.
- `ctx.HUD` panicked when called during `Init` or `Update`.
- `veduta upgrade` from a release candidate to its release (v1.0.0-rc.1 to v1.0.0) left
  `go.mod` on the candidate, and a manifest key spelled in another case was not upgraded.
- Found by the pre-release review of the console path, before any board ran it:
  - Select+Start did not close the player on a joystick-style pad (buttons from
    `BTN_TRIGGER`); the exit chords are now read from the button table, so correcting the
    table against the real pad moves them too.
  - A Raspberry Pi's HDMI framebuffer is 16-bit, like the panel, so the player refused to
    choose between them; the board's own framebuffers (`vc4drmfb`, `BCM2708 FB`,
    `simpledrmdrmfb`) are now passed over and then the smallest wins.
  - A stick on a 0..255 or −128..127 axis was dead, and a push to 1 read as the opposite
    direction; sticks are read against the range the device reports (`EVIOCGABS`).
  - At a text console every key also reached the console and the shell behind it (echoed
    over the frames, run as commands after quitting). The player now takes its devices for
    itself while it polls (`EVIOCGRAB`), gives them back after 2 s without a poll so a hung
    game cannot lock the keyboard, waits for the closing chord to be let go, and flushes the
    terminal's unread input on close.
  - Releasing one of two things holding the same key (the hat and the stick, a keyboard's
    Space and the pad's A) released the key; a key is now held while anything holds it.
    After dropped events a half-pressed exit chord is forgotten, so its other half alone no
    longer quits.
  - A player with no readable input said nothing; it now says why on stderr, once per
    change, with a hint when the input group is missing.
  - `veduta doctor` missed fused multiply-adds when `GOFLAGS=-trimpath` was set, when the
    project was reached through a symlink, and when a game's product fused inside an
    inlined engine helper; it now builds with `-trimpath`, matches module paths, lists such
    places as `gmath/vec.go:105 inlined in demo/game.updatePlayer`, and gives every place in
    the `sites` of its JSON report. The engine gate is likewise independent of paths, and
    also scans a program that refers to every exported function and method of the public
    engine packages, so API that no linked binary calls is checked.
  - The console check of `doctor` and `release` was fooled by a comment naming linux/arm64,
    by arm64 built for another system and by a test-only CI job, and missed matrix
    workflows; it now reads the release workflows (triggered by a tag or a release) with
    comments cut.
  - The game release workflow stamped `card.json` by matching a line, which broke a
    one-line card and duplicated an existing `version`; it now edits the card as JSON with
    `jq`, stops on an invalid card before building, ships the manifest's `icon` as
    `icon.png`, and builds with Go `stable`. `veduta release` refuses a project without a
    valid `card.json`, checks the workflow and the card before the tests, and prints the
    reason and the fix when it refuses.
  - `inspect scene` counted translucent overlays as occluders of important entities, and its
    `SCENE_OVERLAP` hint moved by the overlap depth, which does not separate a hitbox from a
    thin quad; the hint now moves by the separating distance.
  - A project and a tool a minor version apart failed `doctor` although the specification
    calls it a warning, and a tool updated automatically from v0.2.0 would have applied the
    console's rules to projects still on a v0.x engine; such projects are now told to
    upgrade instead of refused, and `veduta run` checks for a display for them as before.

### Decisions

- **`SPEC-v1.0.0.md` supersedes `SPEC-v0.1.0.md`.** The old file records what a released
  product promised and what its §15 acceptance walk certified; amending it in place would
  falsify that record. The successor is the source of truth, repeats every section so it
  reads on its own, and says in its first lines that it supersedes the old one.
- **The console is the only player target.** Windows and desktop Linux became authoring
  machines. Keeping X11 and Win32 would have kept three quarters of `platform/` alive for a
  target the product no longer has, with checks (Xvfb, windows-latest) that prove nothing
  about the panel. The loss is real — a developer can no longer play on their own PC — and
  is paid with `render`, `simulate`, a QEMU arm64 machine with a framebuffer, or the console.
- **20 Hz, 320×240.** They feed `ctx.DT` and the frame size, and so every trace hash and
  scenario expectation; they are set once, in the release that may break formats, rather
  than later. A tick count keeps meaning ticks, so the template's scenarios were re-timed
  (divided by three) to keep describing the same seconds of play.
- **Rounding by conversion, checked by disassembly.** The Go specification makes an
  explicit conversion a rounding point the compiler may not fuse across, so that is the
  rule, applied to every product that feeds `+` or `-` (a division by a power-of-two
  constant counts: the compiler turns it into a product). It is enforced by reading the
  built arm64 binary for FMADD/FMSUB/FNMADD/FNMSUB instructions (`internal/fused`), not by
  the compiler's unsupported `-d=fmahash` debug flag, which a game built with plain
  `go build` would not get. A fusion with a power-of-two constant is harmless in value but
  still flagged, so the rule has no exceptions to remember. `gmath` rounds inside its own
  operations, so `pos.Add(vel.Scale(dt))` is safe as written.
- **Other architecture differences are refused, not tolerated.** Out-of-range float to
  integer conversions are clamped in floating point with the result amd64 already gave;
  a model that overflows float32 is an error, because a NaN made from infinities carries
  a sign that differs between amd64 and arm64 and would change the cooked bytes.
- **Pinning on upgrade is decided by the major version.** A project moving from a 0.x engine
  to 1.x or later is pinned; release candidates of 1.0.0 already carry the new defaults and
  count as 1.x, so v0.2.0 → v1.0.0-rc.1 pins and v1.0.0-rc.1 → v1.0.0 does not. A field is
  pinned exactly when `CompileProject` would fill it in (absent, `null` or zero), the frozen
  v0 values live in one table that never reads `DefaultProject`, and the manifest is edited
  as text and re-parsed before it is written.
- **`VEDUTA_BACKEND=x11` gets its own error** naming the removal, so an old service file
  says what happened instead of "unknown value"; `auto` and `fbdev` both mean the
  framebuffer. The mouse, text, resize and focus event kinds and `SetPointerLock` stay in
  the API, documented as never produced, so games and scenarios that use them still build.
- **The keyboard table maps only codes the engine knows.** The keypad now reports `Numpad*`;
  kernel keys whose W3C name is not in `asset.KeyCodes` (NumLock, PrintScreen, Pause, …)
  stay unmapped, since no scenario could name them.
- **Images.** `render` defaults to 320×240 and stops at 640×480, two panel pixels per
  image pixel. Sheets get their own 640×720 box, because 4:3 tiles stack taller and a
  480-pixel cap would shrink the common 2×2 sheet (640×482); simulate sheets stay 640 wide
  since 320-wide tiles are illegible.
- **Layer orders drawing, depth still decides among opaque parts.** The rasterizer's depth
  test is strict, so a layer never lifts an opaque or cutout sprite over a nearer one;
  blended parts write no depth, so for them the layer decides. The docs tell games to order
  sprites with z and translucent overlays with layer as well. `Hitbox` is a pointer, because
  a zero box would give every spawn template an AABB.
- **`Camera2D` is a helper, not a preset**, because a new preset name would change the
  `describe` report and the documented preset lists. It sits at z = 100 looking at z = 0 with
  the scene's default near and far, so z from −100 to 99.9 is visible.
- **`ctx.Width`/`Height` in `Update` are the project resolution in every mode**, so a trace
  never depends on `render --width`, the screenshot tile or the panel's scale.
- **`card.json` is static and the release stamps it.** The console's format is frozen and
  optional in every field; the workflow edits the card as JSON with `jq` (preinstalled on
  the runners) once, before any target is built: `version` becomes the tag, and when the
  manifest names an icon the file ships as `icon.png` and the card names it. An invalid
  card stops the job, and `veduta release` refuses it before tagging. `doctor` warns when
  the card and the manifest disagree.
- **The player takes its input for itself.** Grabbing every device it reads keeps keys away
  from a text console and from anything else reading the pad during a game; a 2 s watchdog
  gives the devices back to a player that stopped polling, so a hung game never takes
  Ctrl+C, console switching and SysRq with it. The console's cursor is not hidden: an escape
  sequence could not be undone for a player killed outright; a console image sets
  `vt.global_cursor_default=0` instead.
- **Workflow detection is a text heuristic without an override.** It reads the workflows
  that run on a tag or a release, comments cut, and accepts `linux/arm64` or GOOS linux with
  GOARCH arm64 (directly or from a matrix). A workflow can still mislead it; the release
  archive itself is the proof.
- **An engine release waits for arm64.** `veduta release` in the engine repository runs the
  suite under `qemu-aarch64-static` when it is installed, and `release.yml` runs it again
  before building archives, so a commit whose goldens fail on the console is never
  published.
- **Pre-console projects are not held to console rules.** A v1 tool may reach a v0.x project
  by an automatic update, and updating the tool must not change what a project can do:
  until the project is upgraded, `release` passes the console step with a note and `run`
  checks for a display as v0.x did.
- **`doctor` warns, `release` refuses.** A desktop-shaped manifest or a fused line is a
  choice the author may be making on purpose, so it does not fail `doctor`; a release the
  console cannot install serves nobody, so `release` stops.
- **The benchmark keeps 10k triangles at 320×240**, several times what a level for a Pi Zero
  2 W should submit, so it measures the rasterizer under load; the 50 ms tick on the board
  is a target only hardware can check.

### Acceptance (§15)

Every criterion of `SPEC-v1.0.0.md` §15 with the command that verified it. Items that need
a published release ran against the candidate `v1.0.0-rc.1` (commit c26b3aa), tagged by
hand and published as a pre-release by `release.yml`; the only change after it is the
rasterizer weight fix above, which touches drawing only and passed the same suites.

1. **Fresh VPS → `veduta test` in under 5 minutes.**
   `docker run --rm -v "$PWD/scripts/acceptance/fresh_vps.sh:/fresh_vps.sh:ro" ubuntu:24.04 sh /fresh_vps.sh v1.0.0-rc.1`:
   install 10 s (Go included), `veduta init demo && cd demo && veduta test` 21 s, 31 s in
   total; the tool and the project's engine are v1.0.0-rc.1, the four scenarios pass.
2. **`claude` in a project lists every §11 tool; visual tools return images.** With the
   candidate on PATH, `claude -p '<status, render, simulate, inspect, render --bundle,
   query, diff, list tools>' --mcp-config .mcp.json --strict-mcp-config --allowedTools 'mcp__veduta__*'`
   listed build, cook, diff, docs, fuzz, inspect, query, release, render, simulate, status,
   test and trace; status reported the console target, render returned a 320×240 image,
   simulate `collect` passed with one sheet, inspect `hero` returned no issues and a sheet,
   query named the player, diff returned 0 changed pixels. It also questioned the distance
   `query` reported, which led to the rasterizer fix. `go test ./internal/cli -run TestMCPEndToEnd`
   exercises every tool.
3. **A game release is a card for the console.** In a project from `veduta init`, the
   `build archives` step of its `release.yml`, run as written with `GITHUB_REF_NAME=v0.1.0`
   and a card that already had a version: `demo_v0.1.0_linux_arm64.tar.gz`,
   `demo_v0.1.0_linux_amd64.tar.gz` and `checksums.txt`; the arm64 archive unpacks to
   `demo/` with the binary, `veduta.json`, `README.md`, `assets/` and a `card.json` whose
   version is `v0.1.0`, and `qemu-aarch64-static ./demo -project . -headless render --scene main`
   renders 320×240. Publishing on GitHub is `gh release create`, unchanged since v0.1.0 and
   exercised by the engine's own releases. On the console (for a person, see PROGRESS.md):
   the dashboard starts the game, the D-pad moves, A jumps, Select+Start returns.
   Emulated end to end: Debian arm64 in `qemu-system-aarch64` with a virtio framebuffer
   and keyboard ran the demo with `VEDUTA_BACKEND=fbdev VEDUTA_SCALE=4`; arrows walked the
   hero to a gem, Space jumped, R reset, Ctrl+Q exited with status 0.
4. **Identical traces and frames across runs and architectures.** CI run 34879994794 on
   c26b3aa: ubuntu-latest and windows-latest (Go stable and oldstable) and linux/arm64 under
   qemu-user all pass the same goldens; `TestEngineHasNoFusedMultiplyAdd` passes. Locally,
   `CGO_ENABLED=0 GOARCH=arm64 go test -exec qemu-aarch64-static ./...` passes. A v0.1.0
   project (github.com/riftbane/veduta-demo) upgraded to the candidate produces
   byte-identical traces and contact sheets to its v0.1.0 build for all three scenarios.
5. **Flipped normals are reported and visible.** `go test ./inspect -run TestModelFlippedNormals`.
6. **Fuzzing.** `veduta fuzz --games 200 --ticks 200 --seed 1` on the demo: 0 violations in
   5.7 s. With `PlayerSpeed = 400.0`: every game violates `within_bounds`; minimized repro
   `tests/scenarios/fuzz_cd96a0cd.scenario.json` (6 ticks, one input), which `veduta simulate`
   reports as `fail`.
7. **Update and upgrade.** A v0.2.0 tool from GitHub Releases, with a fresh configuration:
   `veduta update --check --channel beta` → "v1.0.0-rc.1 is available", then
   `veduta update --channel beta` → "updated v0.2.0 → v1.0.0-rc.1 … verified", and the
   configuration follows beta. `veduta upgrade` of the v0.1.0 demo project moved `go.mod`
   and `veduta.json` to the candidate, pinned nothing (its manifest is explicit) and listed
   the console's missing workflow build and card; `doctor` then named the four unrounded
   lines of its old game code. Pinning: `go test ./internal/cli -run TestUpgradePinsTheV0Defaults`.
8. **Rasterizer.** `go test -bench . -benchmem -run '^$' ./gfx/soft`:
   `BenchmarkDraw10kTriangles320x240` 2.9 ms/frame, 0 allocs/op; 31 ms/frame under
   qemu-aarch64 (emulation). The Pi Zero 2 W budget is for a person with the board.
9. **No third-party modules.** `go.sum` is empty (CI step "go.sum has no third-party modules").
10. **The tool and the console.** On this VPS (no framebuffer) `veduta run` refuses with
    "no framebuffer on this machine … Set VEDUTA_FB …"; `go list -deps ./cmd/veduta` holds no
    `platform`; `CGO_ENABLED=0 GOOS=windows go build ./cmd/veduta` succeeds (CI cross-compiles).
11. **2D.** `go test . -run TestTwoDFixture` renders the `testdata/twod` scene in layer order,
    records the collision of two coplanar quads with hitboxes and answers `query --at`.

## v0.2.0 — 2026-09-12

### Added

- First-person games: `Input.MouseDelta` reports the cursor movement of each tick (derived
  from the positions a window or a scenario provides, so scenarios and traces are
  unchanged), and `scene.Camera.LookFrom(eye, yawDeg, pitchDeg)` builds an eye camera from
  the scene's own camera, keeping its projection. The demo toggles a first-person view with
  `KeyF`: the mouse looks around, WASD moves relative to the view and the hero's own model
  is hidden; the `look` scenario covers it headless. `Context.LockPointer` asks the player
  window to hide the cursor and keep it inside (X11 warps it back to the middle with an
  empty cursor, Win32 hides it and recenters it), so looking around never stops at the
  edge of the screen; headless runs ignore it.

- A beta release channel: `veduta update --channel beta` follows release candidates and
  `veduta update --channel stable --force` goes back, `install.sh --channel beta` (or
  `$VEDUTA_CHANNEL`) installs one, and `channel` in `~/.config/veduta/config.json` says
  which one the tool follows (`docs/config.md`). Tags with a pre-release suffix are now
  published as pre-releases, so a candidate never reaches anyone who did not ask for one.

### Fixed

- `veduta upgrade` right after a release wrote its changelog entry directly above the
  release heading, without a blank line.
- Pre-release suffixes were ordered as plain strings, so `v0.2.0-rc.10` sorted before
  `v0.2.0-rc.2`. They now follow SemVer §11.4, which also changes which tags
  `veduta release` accepts.

### Decisions

The specification of v0.1.0 is left as it shipped; these extend §13 and are recorded here.

- **Release channels.** `stable` takes the release GitHub marks as the latest one (never a
  pre-release); `beta` takes the newest of every published release, candidates included.
  Beta is therefore a superset that answers with a stable release whenever that is the
  newer one, so opting in can never hand back an older binary and a beta user is never
  stranded on an abandoned candidate. The channel is recorded in the update cache as well:
  an answer from the other channel is a miss, not a stale hit.
- **`--channel` is a subscription.** It is saved to the configuration, because a binary
  that went back to stable while the configuration still said beta would be pulled onto a
  candidate again by the next automatic update. Naming a channel saves it even when there
  is nothing to install; `--check` never writes, so it previews another channel without
  changing anything.
- **Downgrades stay explicit.** Leaving beta for an older stable release needs `--force`,
  the flag that already meant "reinstall even when up to date"; `auto` mode never
  downgrades and never crosses channels.
- **The channel key ships before the first candidate.** Configuration parsing rejects
  unknown keys, so a configuration naming a channel is unreadable to an older tool: the
  feature must be in a stable release everyone can reach before a beta tag is cut from
  that line, and going back below it means deleting the key.
- **Zero values take their default.** `"channel": ""` means stable and `"auto_update": ""`
  means check, following the convention of the source formats; it is also what lets a
  cache file written before channels existed still read as a stable answer. An unknown
  key, or a value that names nothing, stays an error.
- **A configuration that cannot be read is never rewritten.** `--channel` has to save the
  choice, so it refuses outright rather than replacing the file with defaults and silently
  dropping settings; without `--channel` the command still runs and reports the problem.
  The choice is saved before installing, so a failed install cannot lose it and a failed
  save cannot undo an install.
- **Pre-release tags are cut by hand.** `veduta release` refuses them, because its
  checklist moves the changelog's Unreleased section into the tag being released and a
  candidate would consume the section the release itself needs.
- **install.sh picks the highest version, not the first.** The release list does not
  arrive in version order, so the installer sorts with `awk`, ranking a release ahead of
  its own candidates. Between candidates of one version the comparison is textual — the
  only place where the installer and the tool can disagree, and the first `veduta update`
  reconciles it.

## v0.1.0 — 2026-09-11

### Added

- Repository skeleton: module `github.com/riftbane/veduta`, MIT license, CI and release
  workflows.
- `gmath`: vectors, matrices (column-major), quaternions, AABB, Rect; deterministic
  Sin/Cos/Tan/Atan/Atan2/Asin/Acos implemented in Go with explicit rounding (no FMA fusion),
  pinned by a golden hash.
- `gfx`: backend interface, DrawList, PipelineState, Framebuffer (BGRA8 color, float32
  depth, entity ids, optional normals), images/PNG, mip chains, id and heat palettes.
- `gfx/soft`: tile-parallel software rasterizer — homogeneous clipping against six planes,
  28.4 fixed-point edge functions with the top-left rule, perspective-correct attributes,
  per-triangle mip selection, nearest/bilinear sampling, Lambert + ambient, opaque/alpha/add
  blending, cutout, ID and normal buffers, debug modes (color, wireframe, normals, depth,
  ids, silhouette, overdraw, uv_checker, collision), debug lines. Zero allocations per frame.
- `sprite`: 2D batcher (rects, images, 9-slice, text) on the same DrawList with an
  orthographic overlay view, and a built-in 8×8 bitmap font.
- `internal/golden`: golden image/text comparison under `testdata/golden`.
- `asset`: every source format of §8 (model, texture, material, scene, scenario, project
  manifest) as strict JSON: unknown fields, duplicate keys, wrong types, trailing data
  and bad headers are errors located by file, line and column (`{file,line,col,msg}`),
  and every problem is reported at once. Compilers for materials, scenes, scenarios and
  the manifest; W3C key code list; the `.vda` chunked container (magic `VDA1`, CRC per
  chunk, canonical JSON `META`) with bit-exact codecs for models, textures, materials and
  scenes.
- `asset/model`: model compiler — box, cylinder, sphere, plane, extrude (ear clipping),
  lathe and mirror parts with transforms, UV mapping in meters, angle-based smoothing,
  pivots; watertight closed shapes with outward winding.
- Format references in `docs/` (model, material, scene, scenario, project, vda).
- `asset/texture`: texture layer programs (solid, noise, stripes, rect, circle, gradient,
  checker, image) with blend modes, opacity, seamless tiling noise and mip chains.
- `asset/cook`: incremental cooking into `.vda` (input hash in `META`, stale-only
  recompiles, pruning of orphans, dangling-reference warnings, dry run) and `Load`, which
  gives the game every asset from fresh cooked files or compiles stale sources in memory.
- `scene`: entities with hierarchical transforms, world AABBs, spawn/despawn with
  deterministic ids, camera presets (`scene`, `top`, `front`, `back`, `left`, `right`, `iso`,
  `orbit:<deg>`, camera entities) and drawing into a DrawList.
- `sim`: xoshiro256** RNG, per-tick Input (W3C key codes) and input scripts, canonical
  JSON trace with SHA-256 hash, AABB contact events, built-in and game invariants,
  scenario expectations.
- `veduta`: the public API (`Game`, `Behaviour`, `RegisterKind`, `Context`, `StateCodec`,
  `Run`, `RunArgs`) and the headless subcommands `render`, `simulate`, `query`, `snapshot`
  and `describe`; snapshots restore to an identical trace.
- `template/`: the demo game (WASD, jump, gems, KeyR reset, HUD) with its assets and the
  `idle`, `move` and `collect` scenarios, compiled inside the module so CI runs it; its
  trace hashes and contact sheets are goldens (`testdata/golden/scenario_*`).
- `inspect`: model, texture and scene inspectors (issue codes of §9.1–§9.3 with counts,
  locations and hints, metrics, sheets), image diff, frame bundles (`.vframe`) and
  ID-buffer queries with per-entity coverage and occlusion ratios.
- `veduta` tool (`cmd/veduta`, `internal/cli`): every command of §10 — `init`, `doctor`,
  `build`, `run`, `cook`, `render`, `simulate`, `inspect`, `query`, `diff`, `test`, `fuzz`,
  `release`, `mcp`, `update`, `upgrade`, `version` — with human or `--json` output. Game
  operations build the game and run it with `-headless`; compile errors come back as
  `{file,line,col,msg}`.
- `mcp`: JSON-RPC 2.0 MCP server over stdio with the tools of §11 (`status`, `build`,
  `cook`, `render`, `simulate`, `trace`, `inspect`, `query`, `diff`, `test`, `fuzz`,
  `release`, `docs`); images inline as PNG.
- `platform`: the player window — X11 protocol client in pure Go (Linux) and
  user32/gdi32 through `syscall` (Windows), keyboard (W3C codes), mouse, text, resize,
  close and focus events; the 60 Hz player loop.
- `internal/update`, `install.sh`: release lookup, SHA-256-verified download and atomic
  self-replacement; the POSIX installer of §13.2.

### Decisions

- **Module path.** `github.com/riftbane/veduta` (the spec's `OWNER` placeholder).
- **Go version.** `go.mod` declares `go 1.25` (spec minimum); CI tests the two latest Go
  releases via `setup-go`'s `stable` and `oldstable` aliases. Development happens on
  Go 1.27.1.
- **Line endings.** `.gitattributes` forces LF for text files so `gofmt -l` is clean on
  `windows-latest` runners (where git defaults to `core.autocrlf=true`) and golden files
  compare byte-for-byte.
- **Cross-OS determinism check (§7.6, §14).** Instead of shipping trace hashes between CI
  jobs with third-party artifact actions, the expected trace hashes and frames of every
  `testdata/` scenario are committed as golden files. Each OS job compares against the same
  golden files byte-for-byte, so linux/amd64 and windows/amd64 are identical transitively.
- **Archive names (§13.1).** `<ver>` in `veduta_<ver>_<os>_<arch>` is the tag including
  its `v` (`veduta_v0.1.0_linux_amd64.tar.gz`), matching the game archive names of §15.3
  (`demo_v0.1.0_windows_amd64.zip`). `install.sh` and `veduta update` use the same rule.
- **Bitmap font (§17 open question).** The built-in font is an original 8×8 design made
  for Veduta (`sprite/font8x8.txt`, generated into the embedded `sprite/font8x8.png` by
  `go generate ./sprite`), dedicated to the public domain under CC0 1.0. The license note
  is embedded next to the PNG (`sprite.DefaultFontLicense`).
- **Golden updates.** Golden files are rewritten only when `VEDUTA_UPDATE_GOLDEN=1` is set
  (what `veduta test --update-golden` will set) or with the test flag `-update-golden`; a
  package-specific flag alone would break `go test ./... -update-golden` in packages that
  do not define it. Golden images are compared by decoded pixels, not PNG bytes, so a
  different Go release's compressor cannot fail them.
- **Clip space and depth.** OpenGL conventions: clip z in [-w, w], window depth
  z*0.5+0.5 in [0, 1] with 0 = near, depth test "less". Front faces are counter-clockwise
  in NDC.
- **Euler angles.** `rotation_deg: [x, y, z]` means R = Ry·Rx·Rz (roll, then pitch, then
  yaw), as in most engines.
- **Mip selection.** Per triangle, from the ratio of texel area to pixel area
  (level = ⌊log₄ ratio⌋), as §6.2 asks; computed with exact comparisons, no logarithms.
- **ID buffer.** A fragment writes the entity id (and normal) exactly when it writes
  depth, and never from HUD overlay views — in every render mode. So `ID != 0` implies
  `Depth < 1`, queries see the 3D scene, and alpha-blended materials (which do not write
  depth) never own ID-buffer pixels; debug sheets agree with color-mode buffers.
- **Normals mode flags wrong geometry.** In `normals` mode culling is off; fragments of
  back-facing triangles are hatched magenta and fragments whose normal points away from
  the viewer are hatched orange, so inside-out parts and flipped normals are visibly wrong
  (§15.5).
- **Depth mode** maps linear eye depth of the frame's covered pixels to gray, nearest
  white, farthest dark gray (adaptive per frame for legibility; diff depth sheets only
  from the same camera).
- **Mirroring transforms.** A model matrix with a negative determinant flips the front-face
  test (like `glFrontFace`), so mirrored entities are not drawn inside-out.
- **Clipping precision.** Clip distances and intersections are computed in float64 and
  clipped vertices are clamped to the screen, so kilometre-long triangles crossing the
  near plane leave no holes; one mip level is chosen per source triangle (from the whole
  clipped polygon).
- **Rasterizer lifecycle.** Using a closed renderer returns an error; the worker-stopping
  cleanup is attached to a handle shared by copies and kept alive during Draw.
- **Source validation.** Vectors and colors are decoded as plain JSON values and validated
  by path so every error has a line and column. A field a shape or layer type does not
  use is an error even when it is `0`, `false` or `null`; an explicit `0` for a count
  (segments, rings, triangle budget) is out of range rather than "default".
- **Model smoothing (§8.1 `smooth_angle_deg`, default 30).** Around each position,
  triangles of the same part that share an edge there and whose normals are within the
  angle (+0.001°) are joined, transitively; a corner's normal is the angle-weighted average
  of its group. 12+-segment cylinders and 16×8 spheres come out smooth, box edges and caps
  sharp; parts never smooth into each other.
- **Model UVs** are in meters from the part's scaled geometry before rotation and
  translation (textures tile at one repeat per meter and follow the part); box/planar faces
  start at their top-left corner seen from outside; cylindrical/spherical seams sit at −Z.
  Planar projects along the part's thinnest axis.
- **Model parts.** `mirror` may mirror any earlier part (mirrors included), inheriting its
  material (overridable), `uv` and `flip_normals`; the mirror plane goes through the model
  origin before the pivot. Extrude profiles must be simple polygons; lathe open profiles
  run bottom to top (closed ones are oriented automatically). Units: meters only.
  `symmetry` (x|y|z) and `triangle_budget` (default 20000) are model fields that only
  drive inspection.
- **Scenario expectations** address the trace's entity summary: `position[.x|y|z]`,
  `rotation_deg[...]`, `scale[...]`, `aabb.min|max[...]`, `visible`, `tags`, `kind`,
  `model`, `material`, `parent`, `state.<field>...`; operators are type-checked against the
  path. Input events must press only released keys and release only held ones.
- **Defaults.** Scene camera: perspective, fov 60°, near 0.1, far 200; light direction
  [-0.4,-1,-0.3], color #ffffff, ambient #404040; background #202830. Material: albedo
  #ffffff, opaque, cutoff 0.5, cull back, filter bilinear. Manifest defaults per §5.2.
- **Texture compositing.** W3C source-over with blend modes: on an opaque canvas
  `d + (B(d,s) − d)·a`; over transparent pixels a layer keeps its own color. Working
  canvas float32, quantized once (×255, round half up, straight alpha). Coordinates: origin
  top-left, y down; 0° points right, 90° down; multiples of 90° are exact.
- **Texture noise** is value noise over SplitMix64 hashes of (seed, octave, cell),
  smoothstep-interpolated, sampled at pixel centers; octaves double the cells and halve
  the weight, normalized to [0, 1). `scale` counts cells across the width. With `tiling`,
  S = max(1, round(scale)), Sy = max(1, round(h·S/w)) and octave o wraps with period
  S·2^o × Sy·2^o. Only noise wraps: shapes, stripes, gradients, checkers and images are
  drawn once (the docs explain how to keep them seamless).
- **Texture shapes.** Rect corner radii clamp to half the smaller side; outlines are drawn
  inside (an outline covering the whole shape fills it); 4×4 supersampling. Gradients run
  from the first to the last pixel center. Checker: N×N cells over the whole texture.
  Image layers: PNG only, paths confined to the assets directory (also against symlinks,
  through `os.Root`), premultiplied bilinear resampling with area averaging when
  shrinking.
- **Mip chains** average 2×2 blocks with premultiplied alpha, so colors hidden in fully
  transparent texels never darken visible edges.
- **Tick timeline.** Tick 0 is the loaded scene after `Init` (recorded with `scene_load`);
  ticks 1…N each run `Game.Update`, then behaviours in entity id order, then despawns,
  transforms, contacts, invariants and the trace record. Script events at tick t are in
  the Input of tick t; events at tick 0 appear in tick 1. Entities spawned during a tick
  first update on the next one. `LoadScene` restarts ids at 1.
- **Trace.** One canonical JSON object per tick (sorted keys, no whitespace, float32
  shortest round-trip formatting, non-finite numbers as `"NaN"`/`"+Inf"`/`"-Inf"`);
  `TraceHash` is the SHA-256 of the file. Entity summaries carry id, name, kind, world
  position, local `rotation_deg` (decomposed from the quaternion), scale, visible, tags,
  model, material, parent name, world `aabb` and `state`.
- **Collisions.** A `collision` event is emitted when two AABBs start overlapping
  (touching is not overlapping; two `static` entities never collide; overlaps present at
  load are not events).
- **Invariants** are checked after every tick including tick 0; a violation is reported
  when an invariant starts failing (not every tick while it keeps failing) and `simulate`
  lists the first violation of each. A scenario's `invariants` list replaces the
  project's. Unregistered entity kinds are a load error.
- **Behaviours are stateless**; per-entity state lives in `Entity.State` (traced as
  `state.*`, snapshotted with gob). The game's own state goes through `StateCodec`.
- **Headless protocol.** Every subcommand prints one JSON line on stdout; exit codes are
  0 (ok), 1 (error, `{"ok":false,"error":...,"errors":[{file,line,col,msg}]}`), 2 (usage),
  3 (`simulate` ran and the verdict is `fail`). An `--input` file is a JSON array of input
  events or a scenario file.
- **§17 open questions.** Positions are float32 in `sim` (no precision issue seen in
  the 300-tick scenarios; revisit for runs over 10 minutes). `simulate` always adds a
  top-down trajectory tile to its single contact sheet. Font: see above.
- **Project loading.** Games load assets from fresh cooked files and compile stale
  sources in memory at startup, so a player archive only needs the binary, `veduta.json`
  and `assets/`; when `veduta.json` is not in the working directory, the directory of the
  executable is used.
- **Inspection thresholds** (documented in `docs/inspect.md`): positions welded within
  1e-6 × diagonal; flipped normals from negative signed volume of closed parts or vertex
  normals opposing winding (error), mixed winding (error), holes/non-manifold/degenerate
  (warning); symmetry score = share of vertices whose mirror image lies on the surface,
  threshold 0.98; texel density CV > 0.5; texture seams must beat 2× the inside difference
  + 4 and every internal line; mip 2 contrast ratio < 0.35; layers without effect found by
  re-rendering with each layer skipped. Extra info code `TEX_LAYERS_NOT_CHECKED` makes
  skipped layer analysis explicit. Scene checks use one render at `inspect_resolution`;
  `SCENE_OVERLAP` covers static entities only (1 mm tolerance); z-fight risk needs
  coplanar, same-facing (or double-sided) triangles of two entities; at most 16 issues per
  code plus one summary issue. Info-level findings of one kind are merged to keep reports
  short.
- **Player windows.** Keys are reported by physical position (US-layout W3C codes from
  the unshifted keysym / scan code), text separately. Auto-repeat is filtered (X11: a
  release followed within 1 ms by a press of the same key; Win32: lParam bit 30).
  Consecutive mouse moves and resizes are merged per Poll. X errors and lost connections
  are sticky. The keymap is read once at open. Windows: one window class per window, one
  window-procedure callback per process, Alt/F10 menu mode swallowed, per-monitor DPI
  awareness when available; the game pauses while the frame is dragged (Windows' modal
  loop) but keeps showing the last frame. The player renders at the client size, one
  frame per tick, and skips ahead after stalls longer than 5 ticks instead of racing.
- **Network use (§15.1).** Every `go` command the tool runs gets `GOPROXY=direct` and
  `GONOSUMDB=github.com/riftbane/veduta` unless the user set them, so after installation
  a project needs no network except GitHub (the engine module is fetched with git).
  `veduta init --engine-dir PATH` adds a `replace` to a local engine checkout (development
  and CI's template smoke test).
- **Tool/game split.** `render`, `simulate` and fuzz games run in the game binary;
  `inspect`, `query` and `diff` need no game code and run in the tool. `veduta test`
  keeps per-scenario goldens in the project's `tests/golden/` (`<name>.hash`,
  `<name>.png`); a missing golden is reported as `new`, not a failure.
- **Fuzzing.** Random players hold keys from a default set (WASD, arrows, Space, Enter,
  ShiftLeft, KeyE, KeyQ, KeyR; `--keys` overrides) for random durations and move/click the
  mouse; game i uses a seed derived from `--seed`. Games run in parallel through the
  game binary without sheets. The first violation is minimized by delta debugging over
  whole key holds and mouse events (press/release pairs stay valid), then written to
  `tests/scenarios/fuzz_<hash>.scenario.json`, which fails until the bug is fixed.
- **MCP server.** Protocol versions 2025-06-18 (preferred), 2025-03-26 and 2024-11-05;
  requests are handled in order; tool arguments are decoded strictly (the schemas say
  `additionalProperties: false`); tool failures are `isError` results carrying located
  errors or the build report. `query` returns the frame with a marker (or the ID buffer
  for coverage) so all five visual tools return an image. The server re-reads
  `veduta.json` on every call so edits are seen without a restart.
- **Releases.** `veduta release` works in a game project (tests, cook, smoke render) and
  in the engine repository (vet, tests, building and running `cmd/veduta`); it needs a
  clean tree on a branch with an `origin` remote, a version above every existing tag and
  a non-empty `## Unreleased` changelog section, which becomes `## vX.Y.Z — date`.
- **Project changelog.** `veduta init` also writes `CHANGELOG.md` (not in the §12 list)
  with an Unreleased entry, so `veduta release v0.1.0` works on a fresh project (§15.3)
  and `veduta upgrade` has a file to record engine changes in (§13.3).
- **Scenario names.** `simulate --scenario` (CLI and MCP) takes a scenario name
  (`collect` = `tests/scenarios/collect.scenario.json`) as well as a path, like `render`
  takes a scene name.
- **Release rehearsal.** Acceptance items that need a published release (§15.1 install,
  §15.3 demo release, §15.7 update) were first verified against `v0.1.0-rc.1`, tagged by
  hand and published by the same `release.yml`, before `veduta release v0.1.0`.
- **MCP on the engine repository.** `.mcp.json` here runs `veduta --project template mcp`,
  so Claude Code working on the engine drives the demo game in `template/` (phase 6).
- **Updates.** Tool self-update is Linux-only in v0.1.0 (Windows tool binaries are out of
  scope, §2); the new binary must run and report the expected version before the atomic
  rename. `auto` mode updates before `veduta mcp` serves and re-executes the new binary.
- **`.vda` hashes.** `META.source_hash` is the SHA-256 computed by `cook` over a version
  line, the compiler version, then the source and each dependency as
  `<tag> <path> <length>\n<bytes>`; a missing dependency hashes as `missing <path>`.
- **Sprites** are unlit, drawn with `gfx.State2D`, counter-clockwise on screen (front
  facing), and only rendered in `color` mode (debug modes show the 3D scene alone).
- **Wireframe mode** is a hidden-line wireframe (dark fill + one-pixel edges computed from
  the edge functions); edges created by clipping are not drawn.

### Acceptance (§15)

Every criterion of `SPEC-v0.1.0.md` §15 with the exact command that verified it. Items
needing a published release ran against the rehearsal release `v0.1.0-rc.1`, whose code
is this release's apart from the release step label.

1. **Fresh VPS → `veduta test` in under 5 minutes, only GitHub after install.**
   `docker run --rm -v "$PWD/scripts/acceptance/fresh_vps.sh:/fresh_vps.sh:ro" ubuntu:24.04 sh /fresh_vps.sh`
   (Ubuntu 24.04.4 plus the curl, git and ca-certificates a server image has; as a normal
   user: `curl -fsSL https://raw.githubusercontent.com/riftbane/veduta/main/install.sh | sh`,
   then proxy.golang.org, sum.golang.org, go.dev, golang.org, dl.google.com and
   storage.googleapis.com are blocked in `/etc/hosts`, then
   `veduta init demo && cd demo && veduta test`). PASS: install 11 s (Go included),
   init + test 31 s, 42 s in total.
2. **`claude` in `demo/` lists every §11 tool; visual tools return images inline.** In
   the demo project with the released tool: `claude mcp list` shows `veduta: veduta mcp`
   (project scope, awaiting the one-time approval) and
   `claude -p '<call render, simulate, inspect, query, diff>' --mcp-config .mcp.json --strict-mcp-config --allowedTools 'mcp__veduta__*'`
   listed build, cook, diff, docs, fuzz, inspect, query, release, render, simulate, status,
   test and trace, and received an image block from render, simulate, inspect, query and
   diff. Every tool is also exercised by `go test ./internal/cli -run TestMCPEndToEnd`.
3. **`veduta release` from the template project publishes the demo archives.**
   `veduta init veduta-demo --name demo --module github.com/riftbane/veduta-demo`, pushed
   to https://github.com/riftbane/veduta-demo, then `veduta release v0.1.0-rc.1`: every
   checklist step ok, tag pushed, the project's `release.yml` published
   `demo_v0.1.0-rc.1_linux_amd64.tar.gz`, `demo_v0.1.0-rc.1_windows_amd64.zip` and
   `checksums.txt`; `gh release download -R riftbane/veduta-demo v0.1.0-rc.1 && sha256sum -c checksums.txt`
   OK and the extracted Linux binary renders headless (`./demo -headless render --scene main`).
   The window opens, presents and closes on both OSes in CI
   (`VEDUTA_DISPLAY_TEST=1 go test ./platform -run TestDisplaySmoke` under Xvfb and on
   windows-latest); 60 fps, WASD and gem collection on a real desktop are for the human
   (checklist in `PROGRESS.md`, phase 7).
4. **Identical trace hashes and PNGs across runs and across OSes.**
   `go test . -run TestTemplateScenarios` runs every template scenario twice and compares
   the trace hashes and contact sheets with the committed goldens
   (`testdata/golden/scenario_*`); it passes in `ci.yml` on ubuntu-latest and
   windows-latest with Go stable and oldstable (runs 34644648386, 34645104184).
5. **Flipped normals are reported and visible.** In a demo copy with
   `"flip_normals": true` on the hero's part 0:
   `veduta --json inspect model hero --sheets normals` → 1 error `MESH_FLIPPED_NORMALS`
   with `where: {part: 0, shape: cylinder, reason: inside_out}` (64 triangles), and
   `out/hero.normals.png` hatches the cylinder magenta in all four views. Pinned by
   `go test ./inspect -run TestModelFlippedNormals` (4229 hatched pixels, 0 when unmodified).
6. **Fuzzing.** In the demo: `veduta fuzz --games 200 --ticks 600` → `0 violating` in
   14.3 s. With `PlayerSpeed = 400.0` in `game/kinds.go`: 200/200 games violate
   `within_bounds`; minimized repro `tests/scenarios/fuzz_77ab7408.scenario.json`
   (16 ticks, one event: KeyS pressed at tick 1), byte-identical on a second run;
   `veduta simulate --scenario tests/scenarios/fuzz_77ab7408.scenario.json` exits 3 (fail).
7. **`veduta update` from an older version.**
   `go build -ldflags "-X main.version=v0.0.9" -o old/veduta ./cmd/veduta`, then
   `old/veduta update --check && old/veduta update && old/veduta version` →
   `updated v0.0.9 → v0.1.0-rc.1 (veduta_v0.1.0-rc.1_linux_amd64.tar.gz verified, sha256 38b9c3b5…)`
   and the replaced binary reports `v0.1.0-rc.1 (commit adcebd5…)`.
8. **Rasterizer benchmark.** `go test ./gfx/soft -run '^$' -bench . -benchmem -count 3`
   → 0 allocs/op (gate, also the CI step "rasterizer benchmark (0 allocs/op gate)");
   13.5–15.1 ms/frame on this 4-core AMD EPYC VPS, so the 8 ms target is not met yet.
9. **No third-party modules.** `test ! -s go.sum && go list -m all` → no `go.sum`, only
   `github.com/riftbane/veduta` (CI step "go.sum has no third-party modules").
