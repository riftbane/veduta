# Veduta

> **veduta** (it.) — a highly detailed, faithful painting of a view.

Veduta is a headless, deterministic game engine and asset toolchain written in pure Go,
designed to be *looked at by machines*. Its primary user is an AI agent working on a
server with no display: every feature exists to let that agent see, measure and fix what
it is building. People play the result on the Veduta console — a Raspberry Pi Zero 2 W or
Pi 5 with a 320×240 panel refreshed at 20 Hz and a gamepad, which lists the games copied
onto its card ([vedutaos](https://github.com/riftbane/vedutaos)).

- **Numbers before pixels.** Every inspection produces a JSON report ranked by severity;
  small images (320×240, contact sheets) only confirm.
- **Declarative sources.** Models, textures, materials, scenes and test scenarios are
  strict JSON with located errors (`file:line:col`).
- **Headless everywhere except the console.** The toolchain never opens a window; the
  player draws on the console's framebuffer and reads its pad.
- **Deterministic across architectures.** Same seed + same input ⇒ same trace hash and
  identical frames on the PC that authors a game (linux/amd64, windows/amd64) and on the
  console that plays it (linux/arm64).
- **Standard library only**, `CGO_ENABLED=0`; the tool is released for Linux and Windows
  and builds from source on macOS, games are released for linux/arm64.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/riftbane/veduta/main/install.sh | sh
```

The installer downloads the `veduta` tool from GitHub Releases (SHA-256 verified) into
`~/.local/bin`, installs Go ≥ 1.25 into `~/.local/go` when it is missing (`--with-go`,
default when non-interactive; `--no-go` to skip), checks git and runs `veduta doctor`.
Flags: `--prefix DIR`, `--version vX.Y.Z`, `--channel stable|beta`, `--with-go`, `--no-go`,
`--yes`; `$VEDUTA_VERSION` and `$VEDUTA_CHANNEL` set `--version` and `--channel` without
flags, `$VEDUTA_HOME` sets the prefix (`$VEDUTA_HOME/bin`). The beta channel installs
release candidates as well as releases; to keep following it afterwards, run
`veduta update --channel beta` once.

Releases carry the tool for linux/amd64, linux/arm64 and windows/amd64 (on Windows, unzip
`veduta_<tag>_windows_amd64.zip` from GitHub Releases). On macOS, build it from source with
Go ≥ 1.25, naming the version so `veduta upgrade` and the update checks know it (a macOS
build cannot update itself, since no macOS archive is published):

```sh
go install -ldflags "-X main.version=vX.Y.Z" github.com/riftbane/veduta/cmd/veduta@vX.Y.Z
```

## Quick start

```sh
veduta init mygame && cd mygame
veduta test          # the project's scenarios
claude               # Claude Code connects to `veduta mcp` through .mcp.json
```

A game is written in Lua: `main.lua` and the `lua` docs topic ([docs/lua.md](docs/lua.md));
the tool runs it itself, with no Go and no build. `veduta init --go` creates a Go game
instead. The template is an empty game: an empty scene with the game's name on it, the
`quad` model and `sprite` material a 2D game builds on, and one scenario. `veduta release v0.1.0` tags the game; its GitHub Actions workflow publishes a linux/arm64
archive that unpacks to a folder the console lists by its `card.json`, and a linux/amd64
one for any other Linux machine with a framebuffer. A 2D game starts from the `2d` docs
topic ([docs/2d.md](docs/2d.md)).

## Commands

| Command | Purpose |
|---------|---------|
| `veduta init [dir] --name N [--go [--module M]]` | create a game project from the embedded template: Lua, or Go with `--go` |
| `veduta doctor` | check Go, git, the manifest, engine/tool versions, assets, updates, and the game against the console (arm64 build, fused multiply-adds in the game's code, release target, `card.json`) |
| `veduta build [--vet]` | cook stale assets, `go build` the game; errors as `{file,line,col,msg}` |
| `veduta run` | run the player on this machine's framebuffer; refuses where there is none (no 16 or 32 bpp `/sys/class/graphics/fbN`, no `VEDUTA_FB`). A desktop session's DRM framebuffer counts as one: play from a text console (a VT) |
| `veduta cook [--force]` | compile changed asset sources to `.vda` |
| `veduta render --scene S [--tick T] [--seed N] [--camera P] [--mode M] [--out f.png] [--bundle]` | render one frame headless |
| `veduta simulate --scenario F` / `--scene S --ticks N --seed N [--input F]` | trace, verdict, expectations, invariants, one contact sheet |
| `veduta bench --scenario F [--cpus N]` / `--scene S --ticks N` | update, render and frame milliseconds per tick against the tick budget, triangles per frame |
| `veduta inspect model\|texture\|scene\|prefab\|world NAME [--focus ISSUE] [--sheets list]` | report + sheets |
| `veduta world map\|query\|place\|terrain\|vegetation\|remove NAME [flags]` | describe a generated world ([docs/world.md](docs/world.md)), shape its ground (hills, plains, lakes, seas), plant it (trees, grass, flowers) and put landmarks where its rules allow |
| `veduta query --frame F --at x,y` / `--coverage` | ID-buffer questions on a frame bundle |
| `veduta diff A B [--out f.png]` | image diff with an a\|b\|heat sheet |
| `veduta test [--update-golden]` | `go test ./...` + every scenario + golden hashes and sheets |
| `veduta fuzz --scene S --games N --ticks T --seed N` | random games; minimized repro of the first violation |
| `veduta release vX.Y.Z [--dry-run]` | checklist (tests, cook, smoke render, console build) → CHANGELOG → tag → push (CI publishes) |
| `veduta mcp` | MCP server on stdio for AI agents |
| `veduta update [--check] [--force] [--channel stable\|beta]` | update the tool from GitHub Releases; beta also offers release candidates |
| `veduta upgrade` | move the project to the tool's engine version |
| `veduta version` | version, commit, build date |

Every command that writes files writes under `out/` unless `--out` is given, and prints
its report as JSON with `--json`.

## MCP tools

`veduta mcp` speaks JSON-RPC 2.0 over stdio (protocol 2025-06-18, also 2025-03-26 and
2024-11-05). Tools: `status`, `build`, `cook`, `render`, `simulate`, `trace`, `inspect`,
`world_map`, `world_query`, `world_place`, `world_terrain`, `world_vegetation`, `world_remove`, `bench`,
`query`, `diff`, `test`, `fuzz`, `release`, `docs`. Reports come back as JSON text;
`render`, `simulate`, `inspect`, `query`, `diff` and the `world_*` map tools also return PNG images inline
sized after the 320×240 console panel: renders are 320×240 by default and at most
640×480 (a single side gets the other at 4:3); sheets are at most 640×720, and
`simulate` returns exactly one contact sheet.
`veduta init` writes `.mcp.json`; the equivalent command is
`claude mcp add --scope project veduta -- veduta mcp`.

## Writing games

A game is a Go program: `func main() { veduta.Run(&game.Game{}) }`. Entity behaviours are
registered by kind with `veduta.RegisterKind`; randomness comes only from `ctx.RNG`. The
same binary runs the player on the console or, with `-headless`, the `render`, `simulate`,
`query`, `snapshot` and `describe` subcommands the tool delegates to — the tool never
contains game logic.

Format references (also served by the MCP `docs` tool): [model](docs/model.md),
[prefab](docs/prefab.md), [world](docs/world.md),
[texture](docs/texture.md), [material](docs/material.md), [scene](docs/scene.md),
[scenario](docs/scenario.md), [game API](docs/api.md), [project manifest](docs/project.md),
[.vda container](docs/vda.md), [inspection](docs/inspect.md),
[tool configuration](docs/config.md), [2D games](docs/2d.md).

## Repository layout

```
veduta.go, engine.go, headless.go   public API, engine loop, headless subcommands
gmath/                             vectors, matrices, quaternions, deterministic trig
gfx/, gfx/soft/                    backend interface and the tile-parallel software rasterizer
sprite/                            2D/HUD batcher and the built-in 8×8 font
scene/, sim/                       entities and cameras; RNG, input, trace, invariants
asset/, asset/model, asset/texture, asset/cook   source formats, compilers, .vda, cooking
inspect/                           reports, sheets, diff, frame bundles and queries
platform/                          the console player: framebuffer and evdev pad/keyboard (Linux)
mcp/, internal/cli, cmd/veduta     the MCP server and the veduta tool
internal/fused                     finds fused multiply-adds in linux/arm64 binaries
lua/                               the Lua 5.4 interpreter games are written for
script/                            script games: Lua kinds and callbacks on the engine
template/                          the project created by veduta init (empty game, Lua or Go)
internal/testgame/                 the engine's test game: scenarios, goldens, benchmarks
testdata/                          golden images, trace hashes and fixtures
```

## Determinism

Trace hashes and frames are identical on linux/amd64, windows/amd64 and linux/arm64. CI
runs every test and golden on `ubuntu-latest`, `windows-latest` and linux/arm64 under
qemu-user against the same committed files. Release builds use `CGO_ENABLED=0 GOAMD64=v1`.
Trigonometry is implemented in Go, and every float product that feeds an addition is
rounded explicitly, because arm64 would otherwise fuse `a*b + c` into one instruction with
one rounding; `go test ./internal/fused` fails on any engine line where that happened, and
`veduta doctor` lists the lines of a game's own code (see the determinism rules in
[docs/api.md](docs/api.md)).

## Updates

`~/.config/veduta/config.json`: `{"auto_update": "check" | "auto" | "off",
"check_interval_hours": 24, "channel": "stable" | "beta"}` (defaults `check`, 24,
`stable`). `veduta doctor` and the MCP `status` tool report available updates on the
configured channel (checked at most once per interval, 3 s timeout, offline safe); in
`auto` mode `veduta mcp` updates itself before serving.

The beta channel offers release candidates as well as releases, so it always answers with
whichever is newer: `veduta update --channel beta` installs one and follows beta from then
on, `veduta update --check --channel beta` only previews it, and
`veduta update --channel stable --force` goes back (`--force` because that is a
downgrade). See [configuration](docs/config.md). Updating the tool never changes a
project; `veduta upgrade` does, explicitly. Upgrading from a v0.x engine to v1.0.0 or
later also pins the v0.x defaults: each of `resolution`, `inspect_resolution` and
`tick_rate` that `veduta.json` leaves out or sets to its zero value is written into it as
`[1280, 720]`, `[640, 360]` and `60`, so the game keeps its size and physics under the new
defaults (see [the project manifest](docs/project.md#upgrading)).

## Status

v1.0.0 is the first stable release; see [CHANGELOG.md](CHANGELOG.md), the specification
[SPEC-v1.0.0.md](SPEC-v1.0.0.md) (which supersedes [SPEC-v0.1.0.md](SPEC-v0.1.0.md)) and
the progress log [PROGRESS.md](PROGRESS.md). From v1.0.0 the source formats are stable
within 1.x (SPEC §13.4). Deferred: GPU backend, desktop player windows, audio, skeletal
animation, physics beyond AABB overlap, networking.

## License

MIT — see [LICENSE](LICENSE). The built-in font is an original design dedicated to the
public domain (CC0 1.0).
