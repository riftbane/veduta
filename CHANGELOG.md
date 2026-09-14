# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## Unreleased

### Added

- A player backend for machines with no display server, which is what a console is: the
  panel's own framebuffer. It is found by reading sysfs rather than by assuming a device
  number, frames are packed to RGB565 honouring a padded stride, and an integer render
  scale lets a slower board draw a quarter of the pixels and still fill the glass. Which
  backend runs is decided when the player starts, not when it is built — `VEDUTA_BACKEND`
  forces one, and by default X11 is used when a display server is there and the panel when
  it is not — so one binary serves a desktop and an appliance. `VEDUTA_FB`, `VEDUTA_PAD`
  and `VEDUTA_SCALE` override what is found.
- Gamepad input, read from evdev and translated into W3C key codes in the platform layer,
  so `sim.Input`, scenarios, traces and goldens are untouched and a session recorded on a
  console replays like any other. A D-pad is understood as a hat, a stick or four buttons;
  auto-repeat is dropped; what is held is released when the kernel admits it lost events
  or when the pad is unplugged mid-game, and the pad is looked for again afterwards.
  Select and Start together close the window, which is the way back on a device with no
  keyboard.
- `title` and `icon` in the project manifest: the name a player sees and the picture shown
  beside it, neither of which the identifier in `name` can carry (`docs/project.md`).

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
