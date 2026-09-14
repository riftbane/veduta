# Veduta — engine repository instructions

You are developing Veduta: a headless, deterministic game engine and asset toolchain in
pure Go whose primary user is an AI agent on a server with no display, and whose games play
on a small linux/arm64 console (320×240 panel, 20 Hz, gamepad). The specification is
`SPEC-v1.0.0.md`; it is the source of truth (`SPEC-v0.1.0.md` is the record of the first
release and is never edited). When this file and the spec disagree, the spec wins. When
the spec is silent, choose the simplest option that keeps determinism and headless
operation, and record the decision in `CHANGELOG.md` under "Decisions".

## Environment

- Linux VPS, no display, no GPU. You cannot open windows. Do not try.
- Go toolchain and git are installed. `gh` may not be: releases are published by CI.
- Everything you build must be verifiable from this machine with the engine's own tools.

## Hard rules

1. Standard library only. `go.sum` must never list a third-party module.
2. `CGO_ENABLED=0` for every build. No `import "C"`.
3. Nothing under `sim/`, `scene/`, `gfx/`, `asset/` reads the clock, environment, or
   filesystem at tick time. Randomness only through `sim.RNG`.
3b. Every float product that feeds + or − is wrapped in a conversion to its own type
   (`float32(a*b) + c`), or arm64 fuses it and the goldens stop reproducing on the console.
   `go test ./internal/fused` fails on any engine line that fused.
4. No map iteration whose order can affect output. Sort first.
5. The `veduta` tool never contains game logic; game-dependent operations run in the
   game binary via `-headless`.
6. Rasterizer inner loops allocate nothing. Benchmarks with `-benchmem` are gates.
7. Source formats are strict: unknown JSON fields are errors.
8. Every format, tool, and CLI flag described in the spec exists with that exact name.

## Workflow

- Work step by step in the order of spec §16. Do not start a step before the previous
  one has a passing `go test ./...` and a commit. Changes that touch arithmetic also pass
  `GOARCH=arm64 go test -exec qemu-aarch64-static ./...` (CI runs it).
- Before writing code for a phase, write the tests that define "done" for it (golden
  images, scenario files, benchmark thresholds), then implement until they pass.
- Verify visually with your own tools as soon as they exist: after phase 1, render to PNG
  and look at it; after phase 4, use `inspect` on every asset in `testdata/`; after
  phase 6, connect Claude Code to `veduta mcp` in this repository and exercise every tool.
- Golden images are regenerated only with `--update-golden`, and only after you have
  looked at the new image and written why it changed in the commit message.
- Commit after each coherent step. Imperative mood, one topic per commit, body explains
  why. Never commit `out/`, `bin/`, or cooked assets.
- Keep `CHANGELOG.md` current under an `Unreleased` heading. Tag `v0.1.0` only when every
  item in spec §15 is checked in the changelog with the command used to verify it.

## Code conventions

- Package names are short, lowercase, no underscores. One responsibility per package.
- Exported API is documented. Formats are documented in `docs/` in the same words the
  `docs` MCP tool returns.
- Errors are values with context: `fmt.Errorf("cook %s: %w", path, err)`. Tools return
  structured errors (`{file,line,col,msg}`) whenever a location is known.
- Float32 for storage and rasterization, float64 allowed for setup math. Use `gmath`'s
  deterministic trig, never `math.Sin` in `sim`.
- Coordinate system: right-handed, Y up, −Z forward, column-major matrices. Degrees in
  files, radians in code.
- Tests live next to the code; integration tests and fixtures in `testdata/`.
- `gofmt` and `go vet` clean at every commit.

## Performance targets (spec §15.8)

320×240, 10k textured lit triangles: 0 allocs/op in the triangle loop is the gate. The
target is the console: a Raspberry Pi Zero 2 W holding 20 Hz (50 ms per tick for update,
render and present) on a level of about 1200 submitted triangles. Profile with
`go test -cpuprofile` before optimizing; do not optimize without a benchmark that shows the
gain.

## Reporting

At the end of each phase, append a short section to `PROGRESS.md`:
what was built, how it was verified (commands + one image path), what is deferred, and
any spec ambiguity you resolved. This file is for the human who reviews the work
asynchronously; keep it factual.

## When stuck

- If two spec requirements conflict, implement the one that preserves determinism and
  write the conflict in `CHANGELOG.md` → Decisions.
- If a platform detail (framebuffer, evdev, uinput) is unclear, write a minimal probe
  program under `internal/probe/` (git-ignored) rather than guessing in the real code.
- Do not stop the phase to ask questions. Make the decision, document it, continue.
