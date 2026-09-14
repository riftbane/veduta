# Progress log

Factual log for the human reviewer. One section per phase of `SPEC-v0.1.0.md` §16 and per
release line after it (`SPEC-v1.0.0.md` §16 for v1.0.0).

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
  - First person (added after v0.1.0), on either OS: press `KeyF` in the demo. The camera
    goes to the hero's eyes, the cursor disappears and the view turns with the mouse; it
    keeps turning however far you push the mouse in one direction (the cursor is locked
    inside the window), WASD moves relative to the view, `KeyF` gives the cursor back.
    Check that no movement is lost or doubled when the view turns quickly, that Alt-Tab
    away and back leaves the cursor and the view sane, and that resizing while locked
    keeps the pointer centered. CI only opens, locks and closes a window; the feel of the
    mouse is what needs a person.
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
- Tagged `v0.1.0` with `veduta release v0.1.0` (release commit d17245c); `release.yml`
  published https://github.com/riftbane/veduta/releases/tag/v0.1.0 (linux/amd64,
  linux/arm64, windows/amd64, `checksums.txt`), CI green on the tag. Re-checked against
  the final release: `scripts/acceptance/fresh_vps.sh` in docker → PASS in 43 s total;
  `veduta update` took a v0.0.9 build to v0.1.0 (checksum verified); the demo ran
  `veduta upgrade && veduta release v0.1.0` and published
  https://github.com/riftbane/veduta-demo/releases/tag/v0.1.0 with
  `demo_v0.1.0_linux_amd64.tar.gz` and `demo_v0.1.0_windows_amd64.zip` (checksums OK,
  the Linux binary simulates headless from the extracted folder). That upgrade exposed a
  missing blank line in the changelog entry, fixed after the tag (Unreleased).
- **For the human** (needs real desktops, first): download the demo's v0.1.0 archives from
  https://github.com/riftbane/veduta-demo/releases, run them on Windows 10/11 and on a
  Linux desktop and go through the phase 7 checklist (1280×720 window, 60 fps, WASD, gem
  collection, clean close).

## After v0.1.0 (2026-09-12)

- **First person.** `Input.MouseDelta` (the movement of one tick, derived from the
  positions a window or a scenario already gave, so no format changed),
  `scene.Camera.LookFrom`, `Context.LockPointer` with the cursor hidden and recentred by
  both player windows, and a demo that switches view with `KeyF`. Verified here by
  `go test ./...`, the new `look` scenario and its golden contact sheet
  (`testdata/golden/scenario_look_sheet.png`, looked at), and in CI by the display smoke
  tests, which now lock and unlock a real window under Xvfb and on windows-latest.
- **Beta release channel.** `veduta update --channel beta` and `install.sh --channel beta`
  follow release candidates; `channel` in `~/.config/veduta/config.json` records the
  choice (`docs/config.md`); tags with a suffix are published as pre-releases; pre-release
  suffixes are ordered as SemVer does. Verified here: `go test ./internal/update/
  ./internal/cli/`; `install.sh` resolving both channels against the real GitHub API (beta
  answers `v0.1.0`, not the older `v0.1.0-rc.1`, which is the invariant that makes opting
  in safe) and installing what it resolved; `veduta release v0.2.0-rc.1 --dry-run` refused
  with exit 2. CI installs from both channels and runs the binary it got.
- **Before any candidate is cut** (the ordering this forces): a configuration naming a
  channel is unreadable to an older tool, because unknown keys are errors. So this work
  must first ship in a stable release (v0.2.0) that users reach through the stable
  channel; only then tag `v0.2.1-rc.1` by hand and walk the rehearsal — `veduta update
  --check --channel beta` (saves nothing), `veduta update --channel beta`, `veduta
  doctor`, `veduta update --channel stable --force` — recording the output here.

## v1.0.0 — the console (2026-09-14)

The engine now makes games for the Veduta console (linux/arm64, 320×240 panel at 20 Hz,
gamepad). `SPEC-v1.0.0.md` supersedes the v0.1.0 specification; `CHANGELOG.md` (Unreleased)
lists every change and decision. The phase 7 desktop checklist above is void: the X11 and
Win32 windows are gone.

- **Step 0, arm64 determinism.** The goldens did not reproduce on arm64: the compiler fuses
  `a*b + c` into one FMADD with a single rounding. 133 fused engine lines were found by
  disassembling arm64 builds of the tool and the demo, and rounded with same-type
  conversions (amd64 results unchanged: no golden moved, benchmark unchanged). Two audits
  removed `math.Log`/`math.Cbrt` from inspect and clamped out-of-range float→int
  conversions. `internal/fused` reads arm64 binaries and is now a test gate
  (`go test ./internal/fused`), covering the tool, the demo and every exported function of
  the public packages. Verified: `CGO_ENABLED=0 GOARCH=arm64 go test -exec qemu-aarch64-static ./...`
  green against the amd64 goldens, locally and in the new CI job.
- **Steps 1–3.** CI runs the whole suite for linux/arm64 under qemu-user. `veduta init`
  writes `card.json` and a release workflow for linux/arm64 and linux/amd64. Defaults moved
  to 320×240 / 20 Hz; the demo lost first person (look scenario and its goldens deleted),
  gained the arrows and a `pad` scenario, and its scenarios were re-timed to the same
  seconds. Blast radius, looked at: `scenario_{collect,idle,move}` hashes and sheets
  (640×246→640×324 and 640×364→640×482, as predicted), three `inspect_scene_*` top views.
  Re-timing check: walking ends at the same place at 20 Hz as at 60 Hz; the jump apex is
  0.77 m instead of 0.85 m (Euler step). Image: `testdata/golden/scenario_collect_sheet.png`.
- **Steps 4–9** (four agents in isolated worktrees, each reviewed adversarially and fixed):
  `veduta upgrade` pins the v0.x defaults; MCP and inspection images follow the panel (4:3,
  320×240 default, 640×480 max; six `inspect_scene_*` goldens regenerated and looked at, e.g.
  `testdata/golden/inspect_scene_main_summary.png`); X11 and Win32 removed (5,510 lines),
  keypad codes added, a real `/dev/uinput` gamepad test, which found two input bugs no fake
  had shown (reads never delivered, pads never found); `run`, `doctor`, `release` and
  `status` check the project against the console; 2D: `hitbox`, `layer`, view-axis blended
  sort, `Camera2D`, `docs/2d.md`, the `testdata/twod` fixture; benchmark at 320×240; the
  v1.0.0 specification, README and template prose.
- **Pre-release review.** Six reviewers over the whole diff, every finding checked by an
  independent skeptic: 49 confirmed, fixed in four worktrees and checked again (details in
  CHANGELOG → Fixed). The serious ones were all on the console path: the exit chord on
  joystick-style pads, a Raspberry Pi's 16-bit HDMI framebuffer beside the panel, sticks
  with 0..255 axes, keys reaching the text console, and doctor missing fusions.
- **Found during the acceptance walk:** the MCP walk through Claude Code questioned the
  distance `query` gave at the hero (6.4 m for 13.3 m). The rasterizer weighed vertices with
  edge functions still carrying the fill rule's bias, so triangles of a pixel or less were
  drawn nearer and darker; present since v0.1.0. Fixed with a test of sub-pixel triangles;
  28 golden images changed by a few pixels along silhouettes (looked at, e.g.
  `testdata/golden/model_lathe.png`). The candidate `v1.0.0-rc.1` predates this fix.
- **Verified here:**
  - `go test ./...`, `go vet ./...` (linux and windows), `gofmt -l .`, builds for
    linux/amd64, linux/arm64, linux/arm, windows/amd64, darwin/arm64; the arm64 suite under
    qemu-user.
  - `veduta init demo --engine-dir . && cd demo && veduta test`: 4 scenarios pass in 2.3 s.
  - `veduta fuzz --games 200 --ticks 200 --seed 1` on the demo: 0 violations in 5.7 s; with
    `PlayerSpeed = 400.0`: every game violates `within_bounds`, minimized repro
    `tests/scenarios/fuzz_cd96a0cd.scenario.json` (6 ticks, one input), which `simulate`
    reports as `fail`.
  - Rasterizer: `BenchmarkDraw10kTriangles320x240` 2.9–3.8 ms/frame, 0 allocs/op on this VPS;
    31 ms/frame under qemu-aarch64 (emulation, an order of magnitude only, not a board).
  - Emulated console, end to end: Debian 13 arm64 under `qemu-system-aarch64` (TCG, 4
    cores) with `virtio-gpu-pci` at 1280×960 and `virtio-keyboard-pci`; the demo built for
    arm64 ran at boot with `VEDUTA_BACKEND=fbdev VEDUTA_SCALE=4` on `virtio_gpudrmfb`
    (32 bpp). Keys sent through the QEMU monitor: holding the up arrow walked the hero to a
    gem (SCORE 1), right and Space moved and jumped, R reset, Ctrl+Q closed the player with
    exit status 0. Screendumps looked at (about 15 ticks a second under emulation). This
    ran before the review fixes that grab devices; the uinput test covers the grab since.
- **For a person, on hardware** (nothing here emulates it):
  - The SPI panel: the game fills the ILI9341 at 320×240, colours right (RGB565 byte
    order), about 20 frames a second, no tearing that makes it unplayable.
  - The Rii GP100: which physical buttons arrive as A, B, X, Y, Select, Start
    (`platform/padmap_linux.go` is provisional); the D-pad moves the hero; Select+Start
    returns to the dashboard; unplugging and replugging the pad mid-game.
  - A Raspberry Pi Zero 2 W holds 20 Hz on the demo (50 ms per tick for update, render and
    present); if not, `VEDUTA_SCALE=2` and fewer triangles.
  - With HDMI connected as well as the panel, the game still picks the panel.
- **Deferred:** the demo platformer in `veduta-demo`; hiding the console cursor (an image
  setting); a hardware-verified pad table.


## Unreleased — the console's final controls (2026-09-15)

- **Built:** `Input.Stick` and the scenario `stick` field; the player reads a pad's
  `ABS_X`/`ABS_Y` as the stick (arrows too, unless the D-pad is `BTN_DPAD_*` buttons), Home
  (`BTN_MODE`) as an exit, W/A/S/D as the arrows as well as their own codes, and mice and
  tablets as the stick (`platform/stick_linux.go`). The template demo walks with the stick
  (`stick` scenario); fuzz players move it. vedutaos: `vedutaos qemu` adds `usb-tablet`.
- **Verified:**
  - `go test ./...` and `GOARCH=arm64 go test -exec qemu-aarch64-static ./...`, both green;
    `go vet`, `gofmt -l`. The uinput tests ran with real kernel devices here (not skipped):
    a console pad (stick without arrows, D-pad buttons, X, Y, Home, unplug), a mouse and a
    tablet.
  - Template: the four existing trace hashes are identical before and after the change;
    the stick scenario fails on the old game and passes on the new one. New golden looked
    at: `testdata/golden/scenario_stick_sheet.png`.
  - QEMU end to end: `vedutaos qemu --fresh --game ../veduta/template` (Debian image from a
    mirror, SHA-512 checked by hand: the redirector's mirror had an expired certificate);
    tablet positions sent with QMP `input-send-event` walked the hero right, stopped it at
    the middle and walked it up (screendumps looked at). HMP `mouse_move` cannot drive a
    tablet (relative events only).
- **Not verified:** Home and WASD in QEMU (covered by decoder and uinput tests); a relative
  mouse on a real machine; the handheld's real controls.
- **Observed, not investigated:** after `system_powerdown` of that machine, the next boot
  stopped in the initramfs ("Target filesystem doesn't have requested /sbin/init"); a
  `--fresh` machine booted normally.
- **Deferred:** engine release v1.1.0; vedutaos moving to it (its release clones the engine
  by tag, so no pseudo-version).
- **Resolved:** the spec is extended, not edited; D-pad vs stick decided by `BTN_DPAD_*`;
  mouse held where left; Home as a one-button chord (CHANGELOG → Decisions).
