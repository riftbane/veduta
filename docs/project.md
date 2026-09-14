# Project manifest — `veduta.json`

Every game project has a `veduta.json` at its root. It names the game, pins the engine
version, and sets the defaults the tools use: window and inspection sizes, tick rate,
default scene and seed, where assets live, project-wide invariants and world bounds.
`veduta init` writes it; `veduta upgrade` updates `engine` (see [Upgrading](#upgrading)).

## Format

- One JSON object. Decoding is strict: unknown fields, duplicate keys, wrong types and
  trailing data are errors, located by `file:line:col` and JSON path; all problems are
  reported together.
- Only `veduta`, `name` and `engine` are required. Every other field may be omitted; a
  field set to its zero value (`0`, `""`, missing) also takes the default.
- Paths are relative to the project root, use forward slashes, and must stay inside the
  project: no leading `/`, no drive letter, no `..`, `.` or empty segments, no trailing
  slash.

## Fields

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `veduta` | string | required | Must be exactly `"project/1"`. |
| `name` | string | required | The game's name: 1–64 characters of `a-z`, `0-9`, `_`, `-`, starting with a letter or digit. Used for release archive names (`<name>_v1.2.3_linux_amd64.tar.gz`). |
| `title` | string | the `name` | The name a player sees, on a console dashboard for instance: 1–64 printable characters, spaces and accents allowed (`"Cave of Gems"`). Unlike `name` it never appears in a file name, and it may repeat between games. |
| `icon` | string | none | Optional PNG shown beside the title, relative to the project root and outside the assets directory (`"icon.png"`), so cooking never sees it. A console that finds none draws a placeholder. |
| `engine` | string | required | Veduta version the project targets, `vMAJOR.MINOR.PATCH` with optional `-prerelease` and `+build` (for example `"v0.1.0"`, `"v0.2.0-rc.1"`). `doctor` and `status` warn when it differs from the tool's version by a minor version or more. |
| `entry` | string | `"./cmd/game"` | Go package of the game binary, as given to `go build`: `"."` or a relative path starting with `./`. |
| `resolution` | `[width, height]` | `[320, 240]` | The frame the game is designed for, in pixels, each 1 to 8192: the default render size and the size a player asks for. On the console the frame actually drawn is the panel's size divided by `VEDUTA_SCALE` (320×240 on the reference panel), so design for that. |
| `inspect_resolution` | `[width, height]` | `[320, 240]` | Default size of inspection and simulation images; each 1 to 8192. |
| `tick_rate` | integer | `20` | Simulation ticks per second, 1 to 1000. The player renders exactly one frame per tick, and the console's panel shows 20 frames a second, so a higher rate only spends time on frames nobody sees. `ctx.DT` is `1 / tick_rate`, so changing it changes every trace hash. |
| `default_scene` | string | `"main"` | Scene used when a command does not name one (`assets/scenes/<name>.scene.json`). |
| `default_seed` | integer | `1` | Seed used when a command does not give one. `0` also means the default, so the default seed can never be 0 (scenarios can use seed 0). |
| `assets` | string | `"assets"` | Directory of asset sources. |
| `cooked` | string | `"assets/.cooked"` | Directory of compiled `.vda` files written by `veduta cook`. Must differ from `assets`. Never edit or commit it. |
| `invariants` | array of strings | `[]` | Invariants checked in every simulation, scenario and fuzz run, in addition to a scenario's own. Same specs as in scenarios: `finite_positions`, `within_bounds`, `entity_count_max:N` (N ≥ 1), `no_overlap:tagA,tagB`, or the name of a game-registered invariant. No duplicates. |
| `bounds` | `[[min_x, min_y, min_z], [max_x, max_y, max_z]]` | `[[-100, -50, -100], [100, 100, 100]]` | World bounds in meters, used by the `within_bounds` invariant and by `inspect scene` (`SCENE_ENTITY_OUTSIDE_BOUNDS`). Each max must be greater than the matching min. |

Invariant specs are stored canonically: `entity_count_max:0500` becomes
`entity_count_max:500`.

## Errors (examples)

```
veduta.json:4:13: engine: "0.1.0" is not a version like v0.1.0 (vMAJOR.MINOR.PATCH, optional -prerelease and +build)
veduta.json:7:17: tick_rate: -5 out of range [1, 1000]
veduta.json:14:33: bounds[1][1]: max y (-60) must be greater than min y (-50)
```

## Upgrading

`veduta upgrade` sets `engine` to the tool's version, moves `go.mod` to the same engine
version and adds a line to the project's `CHANGELOG.md`. Upgrading from a v0.x engine to
v1.0.0 or later also pins the v0.x defaults: each of `resolution`, `inspect_resolution` and
`tick_rate` that `veduta.json` leaves out or sets to its zero value is written into it as
`[1280, 720]`, `[640, 360]` and `60`, so the game keeps its size and physics under the new
defaults. The changelog line names the pinned fields, as does the `migrations` list of the
JSON report (`pin "tick_rate": 60 in veduta.json (the default before v1.0.0)`). Only those
fields are written: every other byte of the file, its formatting and its key order stay as
they were. A project already on v1.0.0 or later, or moving between two v0.x versions, pins
nothing.

## Full example

```json
{
  "veduta": "project/1",
  "name": "mygame",
  "engine": "v0.1.0",
  "entry": "./cmd/game",
  "resolution": [320, 240],
  "inspect_resolution": [320, 240],
  "tick_rate": 20,
  "default_scene": "main",
  "default_seed": 1,
  "assets": "assets",
  "cooked": "assets/.cooked",
  "invariants": ["finite_positions", "within_bounds"],
  "bounds": [[-100, -50, -100], [100, 100, 100]]
}
```

Minimal manifest (every other field takes its default):

```json
{ "veduta": "project/1", "name": "mygame", "engine": "v0.1.0" }
```
