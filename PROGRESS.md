# Progress log

Factual log for the human reviewer. One section per phase of `SPEC-v0.1.0.md` §16.

## Phase 0 — repository skeleton (2026-09-11)

- Built: `go.mod` (`github.com/riftbane/veduta`, `go 1.25`), `LICENSE` (MIT), README stub,
  `CHANGELOG.md` with an Unreleased section, `.gitignore`, `.gitattributes`,
  `.github/workflows/ci.yml`, `.github/workflows/release.yml`.
- Environment: Go 1.27.1 installed from go.dev (sha256 verified) into `/usr/local/go`.
- Verified: `go vet ./...`, `gofmt -l .` on the empty module; CI run on push.

## Phase 1 — gmath, gfx, software rasterizer, golden cube (2026-09-11)

- Built: `gmath` (vectors, column-major matrices, quaternions, AABB/Rect, deterministic
  trig with FMA-proof rounding), `gfx` (handle-based Backend, DrawList, PipelineState,
  Framebuffer with color/depth/ids/normals, PNG, mips, id/heat palettes), `gfx/soft`
  (tile-parallel rasterizer: six-plane homogeneous clipping, 28.4 edge functions,
  top-left rule, perspective-correct attributes, per-triangle mips, nearest/bilinear,
  Lambert + ambient, opaque/alpha/add, cutout, 9 debug modes, debug lines), `sprite`
  (HUD batcher, 9-slice, text) with an original CC0 8×8 font, `internal/golden`.
- Verified:
  - `go test ./...` — golden lit textured cube `testdata/golden/soft_cube_lit_textured.png`
    plus one golden per debug mode, all inspected by eye; fill rule watertight on a
    jittered grid; identical output for 1/2/3/8 workers; zero allocations per frame.
  - `go test ./gfx/soft -run '^$' -bench . -benchmem`: 10k textured lit triangles at
    1280×720 ≈ 16–18 ms/frame on this 4-core VPS, 0 allocs/op (the 8 ms target is not met
    yet; see deferred).
  - Sprite text rendered through gfx/soft matches the font atlas pixel for pixel.
  - Two agent reviews (gmath, sprite) found and fixed: strict JSON null vectors, `Wrap`
    returning m, `QuatAxisAngle` with a zero axis, `Sin(-0)`, huge trig arguments,
    `AABB.Transform` missing corners by one ulp, sprite `Begin` escaping to the heap,
    sprite winding.
- Deferred: rasterizer performance work towards the 8 ms target (a profiling review is
  in progress); frame bundles and inspection sheets arrive in phase 4.
- Spec ambiguities resolved: see CHANGELOG → Decisions (clip conventions, Euler order,
  ID-buffer rule, font, golden update switch).
- Follow-up (same day): a four-lens agent review of gfx/soft found and fixed inverted
  perspective depth mode, holes from float32 near-plane clipping, a GC race in the worker
  pool, inconsistent debug-mode id/depth writes, per-fan mip seams, mirrored transforms
  rendering inside-out; normals mode now hatches back faces and away-facing normals.
  Performance patches brought the benchmark to ≈ 13.5 ms/frame (from ≈ 17), bit-identical.

## Phase 2 — asset formats, compilers, .vda, cook (2026-09-11)

- Built: strict decoding with `{file,line,col,msg}` errors for every format; compilers
  for materials, scenes, scenarios, the manifest (`asset`), models (`asset/model`) and
  textures (`asset/texture`); the `.vda` container with bit-exact codecs; incremental
  `cook` with input hashes in `META`, pruning, reference warnings, and `Load` for games.
- Verified:
  - `go test ./asset/...` — located-error tables for every format, codec round trips
    over randomized assets, CRC/truncation detection, watertight closed model shapes with
    positive volume, smoothing at the 30° boundary, seamless tiling noise, blend-mode
    bytes, cook incremental/force/dry-run/prune/dependency behaviour.
  - Goldens checked by eye: `testdata/golden/model_crate.png` (and every primitive),
    `testdata/golden/texture_*.png`, `testdata/golden/asset_scene_materials.png`.
- Resolved: smoothing rule, UV conventions, lathe orientation, source-hash definition —
  see CHANGELOG → Decisions.
- Note: the agents building phase 2 were interrupted by a usage limit; the unfinished
  core tests (a hanging codec test, miscounted columns) were fixed by hand.

## Phase 3 — scene, sim, headless game, trace, invariants, scenarios (2026-09-11)

- Built: `scene` (entities, hierarchy, AABBs, camera presets, drawing), `sim` (RNG,
  Input, input scripts, canonical trace + hash, contacts, invariants, expectations),
  the public API and `veduta.Run` with `-headless render|simulate|query|snapshot|describe`,
  and the demo game in `template/` with three scenarios.
- Verified:
  - `go test .` — same seed ⇒ same trace, different seed ⇒ different trace; snapshot at
    tick 60 + restore reproduces ticks 61–120 byte for byte; invariant violations and
    KeyR reset; exit codes and structured errors; render bundles + query.
  - `go test . -run TestTemplateScenarios` runs `template/tests/scenarios/*` end to end
    (real files → cook → engine → simulate) twice each, compares trace hashes and contact
    sheets with `testdata/golden/scenario_*` — the cross-OS determinism check of CI.
  - Contact sheet looked at: `testdata/golden/scenario_collect_sheet.png` (gem collected
    by tick 60, second gem by 140, reset at 150, trajectory tile).
- Deferred: the player window (phase 7).
