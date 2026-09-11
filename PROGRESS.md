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

## Phase 4 — inspection (2026-09-11)

- Built: `inspect` — model inspector (13 MESH_* codes, 7 sheet kinds), texture inspector
  (6 TEX_* codes + TEX_LAYERS_NOT_CHECKED, 6 sheet kinds incl. `on_model:<model>`), scene
  inspector (7 SCENE_* codes, camera/top/ids sheets with legend), image diff, frame
  bundles and queries. Reference: `docs/inspect.md`.
- Verified:
  - `go test ./inspect/` — every code triggered by a crafted fixture and absent on the
    template assets; determinism (two runs, identical JSON and PNG bytes).
  - §15.5: `TestModelFlippedNormals` — hero with `"flip_normals": true` on the cylinder
    reports `MESH_FLIPPED_NORMALS` on part 0 and the normals sheet has 4229 hatched
    pixels inside that part (0 for the unmodified hero). Image:
    `testdata/golden/inspect_model_hero_flipped_normals.png`.
  - Sheets looked at: `inspect_scene_main_summary.png`, `inspect_texture_grass_summary.png`,
    `inspect_model_hero_summary.png`.

## Phases 5–6 — the veduta tool and the MCP server (2026-09-11)

- Built: `cmd/veduta` with every command of §10, `internal/cli` (one Session method per
  operation, shared by the CLI and MCP), `veduta init` from the embedded template,
  `veduta test` (go test + scenarios + goldens in `tests/golden`), `veduta fuzz`, and the
  MCP server (`mcp/`, JSON-RPC 2.0 over stdio) with the 13 tools of §11.
- Verified:
  - `go test ./internal/cli/` — `TestInitAndCommands` (init → build --vet → test →
    located compile errors → exit codes), `TestMCPEndToEnd` (a full MCP session over
    pipes: initialize, tools/list with strict schemas, every tool; render, simulate,
    query and diff return PNG images inline), `TestReleaseProjectFlow` (release against
    a local bare remote).
  - By hand from a built binary: `veduta init demo --engine-dir <repo> && cd demo &&
    veduta test` → PASS in 7.8 s.
  - §15.6: `veduta fuzz --games 200 --ticks 600 --seed 1` on the demo → 0 violations in
    15.4 s; with `PlayerSpeed = 400.0` (×100) → 50/50 games violate `within_bounds`,
    minimized repro `tests/scenarios/fuzz_703219f1.scenario.json` (18 ticks, one input
    event: KeyS held from tick 3), which `veduta simulate` then reports as `fail`.
- Deferred to phase 8: running the MCP server inside Claude Code on this repository
  (`.mcp.json`), which needs the released tool on PATH.

## Phase 7 — player windows (2026-09-11)

- Built: `platform` — X11 client in pure Go over the unix socket (Xauthority, setup,
  BIG-REQUESTS, PutImage strips, keysym → W3C table, auto-repeat filtering, sticky X
  errors) and Win32 via `syscall` (one window procedure callback, StretchDIBits, scan
  code → W3C table, repeat filtering, SC_KEYMENU swallowed, DPI awareness); the player
  loop in `player.go` (60 Hz, one frame per tick, framebuffer reused).
- Verified here: `go test ./platform/` against an in-process fake X server (request
  sequences, strip sizes with and without BIG-REQUESTS, event decoding, auto-repeat,
  errors, no allocation in Present); both key tables produce all 99 `asset.KeyCodes`;
  `CGO_ENABLED=0 GOOS=windows go vet ./...` and `go test -c` for windows; `go list -deps
  ./cmd/veduta` does not contain `platform`.
- Verified by CI: `TestDisplaySmoke` under Xvfb (ubuntu-latest) and on windows-latest
  (opens a 320×240 window, presents 30 frames, polls, checks size, closes).
- **A human must test on a real desktop** (this machine has no display):
  - Linux (Xorg and XWayland): the demo opens a 1280×720 window titled with the game
    name; colors correct; resizing follows; the close button exits with status 0; holding
    W moves smoothly and releasing stops at once; Alt-Tab while holding a key leaves
    nothing stuck; left/middle/right clicks at the right place; numpad, AltGr, Super,
    F-keys, PageUp/PageDown and punctuation produce the right codes; `ssh -X` works;
    without a matching Xauthority entry the error is clear, not a hang.
  - Windows 10/11: 1280×720 client area, sharp at 125%/150% scaling, 60 fps; WASD
    movement and gem collection; NumLock on/off gives the same Numpad codes; left/right
    modifiers distinguished; AltGr gives a single AltRight; é, € and emoji arrive as
    Text; Alt or F10 does not freeze the game; Alt+F4 and the close button exit cleanly;
    Alt-Tab releases keys; dragging a button outside the window still delivers ButtonUp;
    resize/maximize/minimize/restore; moving between monitors of different DPI; with a
    Japanese IME active WASD still moves; works over Remote Desktop.
  - Event logging aid on either OS: `VEDUTA_DISPLAY_TEST=1 go test -count=1 -v -run
    TestDisplaySmoke ./platform/` (on Windows `VEDUTA_DISPLAY_TEST_SECONDS=60` keeps the
    window open and logs every event).
- CI result (runs 34644648386, 34645104184): `TestDisplaySmoke` passes under Xvfb on
  ubuntu-latest and on windows-latest (Go stable and oldstable).

## Phase 8 — distribution and release (2026-09-11)

- Built: `install.sh`, `veduta update`/`upgrade`/`release`, the tool and template release
  workflows, the README, `.mcp.json` for this repository (`veduta --project template mcp`),
  `scripts/acceptance/fresh_vps.sh`.
- MCP on this repository: a headless Claude Code session with `.mcp.json` listed the 13
  tools and called them all. It found that `simulate` refused a bare scenario name (fixed:
  names resolve under `tests/scenarios`); `doctor` ignored `--project` (fixed).
- Release rehearsal with `v0.1.0-rc.1`, tagged by hand: `release.yml` published the three
  archives and `checksums.txt`; `install.sh` on a fresh Ubuntu 24.04 container took 42 s
  from nothing to a passing `veduta test`; `veduta update` took a v0.0.9 build to the rc;
  the demo project, pushed to https://github.com/riftbane/veduta-demo, released
  `v0.1.0-rc.1` with `veduta release` and its workflow published the Linux and Windows
  archives. Found and fixed on the way: two Windows-only test bugs (a JSON-escaped temp
  path, `%APPDATA%`), fresh projects had no `CHANGELOG.md` so `veduta release` refused
  them, a duplicated release step label.
- §15 walk: see CHANGELOG → Acceptance, one exact command per criterion.
- **For the human** (needs real desktops): download the demo's v0.1.0 archives from
  https://github.com/riftbane/veduta-demo/releases, run them on Windows 10/11 and on a
  Linux desktop and go through the phase 7 checklist (1280×720 window, 60 fps, WASD, gem
  collection, clean close).
