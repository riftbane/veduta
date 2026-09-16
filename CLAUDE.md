# Veduta

**Top priority: be laconic.** Overrides everything below.

Headless deterministic game engine + asset toolchain, pure Go. User: AI agent on a
display-less server. Target: linux/arm64 console, 320×240, 20 Hz, gamepad.
Spec `SPEC-v1.0.0.md` wins over this file (`SPEC-v0.1.0.md`: frozen). Spec silent →
simplest deterministic headless option, logged in `CHANGELOG.md` → Decisions.
v2 in progress: `V2.md` is the plan and wins over the spec where they conflict (8-button
input, Lua games, Windows simulator). Tick its boxes as steps land.

## Environment
- Linux VPS, no display/GPU: never open windows.
- Go, git present; `gh` maybe not (CI publishes releases).
- Everything verifiable here with the engine's own tools.

## Hard rules
1. Stdlib only; `go.sum` has no third-party module.
2. `CGO_ENABLED=0`, no `import "C"`.
3. `sim/`, `scene/`, `gfx/`, `asset/`: no clock, env or filesystem at tick time; randomness only `sim.RNG`.
3b. Float product feeding +/− is wrapped: `float32(a*b) + c` (else arm64 fuses, goldens break). `go test ./internal/fused` checks.
4. No output-affecting map iteration; sort first.
5. `veduta` tool has no game logic; game-dependent ops run in the game binary via `-headless`.
6. Rasterizer inner loops: 0 allocs; `-benchmem` benchmarks are gates.
7. Source formats strict: unknown JSON fields are errors.
8. Every spec format, tool, CLI flag exists with its exact name.

## Workflow
- Follow spec §16 in order; next step only after `go test ./...` passes and a commit.
  Arithmetic changes also pass `GOARCH=arm64 go test -exec qemu-aarch64-static ./...`.
- Tests defining "done" (goldens, scenarios, benchmark thresholds) before code.
- Verify with own tools: render PNG and look; `inspect` every `testdata/` asset; exercise every `veduta mcp` tool.
- Goldens: only `--update-golden`, after viewing; commit message says why.
- Commit per coherent step: imperative, one topic, body = why. Never commit `out/`, `bin/`, cooked assets.
- `CHANGELOG.md` current under `Unreleased`. Tag a release only when every §15 item is checked there with its verifying command.

## Code
- Packages: short, lowercase, one responsibility.
- Exported API documented; formats in `docs/`, same text as the `docs` MCP tool.
- Errors with context: `fmt.Errorf("cook %s: %w", path, err)`; structured `{file,line,col,msg}` when location known.
- float32 storage/raster, float64 setup math; `gmath` trig, never `math.Sin` in `sim`.
- Right-handed, Y up, −Z forward, column-major. Degrees in files, radians in code.
- Tests beside code; integration + fixtures in `testdata/`.
- `gofmt`, `go vet` clean every commit.

## Performance (§15.8)
- Gate: 320×240, 10k textured lit triangles, 0 allocs/op in triangle loop.
- Target: Pi Zero 2 W at 20 Hz (50 ms update+render+present), ~1200 triangles.
- Profile (`-cpuprofile`) first; optimize only with a benchmark proving the gain.

## Reporting
End of phase → `PROGRESS.md`: built, verified (commands + one image path), deferred, ambiguities resolved. Factual.

## When stuck
- Spec conflict → keep determinism, log in `CHANGELOG.md` → Decisions.
- Unclear platform detail (fb, evdev, uinput) → probe in `internal/probe/` (git-ignored), don't guess.
- Don't ask; decide, document, continue.
