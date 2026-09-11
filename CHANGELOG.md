# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## Unreleased

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
- **`.vda` hashes.** `META.source_hash` is the SHA-256 computed by `cook` over a version
  line, the compiler version, then the source and each dependency as
  `<tag> <path> <length>\n<bytes>`; a missing dependency hashes as `missing <path>`.
- **Sprites** are unlit, drawn with `gfx.State2D`, counter-clockwise on screen (front
  facing), and only rendered in `color` mode (debug modes show the 3D scene alone).
- **Wireframe mode** is a hidden-line wireframe (dark fill + one-pixel edges computed from
  the edge functions); edges created by clipping are not drawn.
