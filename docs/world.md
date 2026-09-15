# World — `assets/worlds/<name>.world.json`

A world is a map too large to write by hand: a seeded generator paints biomes over an
integer grid of cells, scatters one-cell prefabs, places sites (villages, cities, ruins)
on a jittered grid, and puts the landmarks you name (`places`) exactly where you say. The
game streams the chunks around a focus (the hero, usually), so the world is as large as
`extent` allows (up to ±8192 m from the origin, 268 million cells at the defaults) while
only a few chunks exist at a time. Since v1.2.0.

You author rules and a few named places in cells; the engine computes every meter, checks
every footprint, and tells you what is where (`world_query`), shows you the map
(`world_map`) and finds a valid cell for a landmark (`world_place`). Same seed, same world,
on the authoring machine and on the console.

## File and name

- Location: `assets/worlds/<name>.world.json`. The name is the file name without
  `.world.json`; `render --world`, `simulate --world`, scenarios (`"world": …`) and
  `ctx.LoadWorld` refer to it.
- Decoding is strict (unknown fields, duplicate keys, wrong types and trailing data are
  errors located by `file:line:col` and JSON path; all problems are reported together).
  Prefab references are checked when the prefab exists: a prefab too large for its cell,
  its site spacing or the world is an error; a missing prefab is a cook warning and an
  `inspect world` error.

## Cells and chunks

- A **cell** is `cell` meters square; cell `[x, z]` covers x in `[x·cell, (x+1)·cell)`,
  z in `[z·cell, (z+1)·cell)` at y = 0. +X is east, −Z is north (up on a top-down render).
- A **chunk** is `chunk` × `chunk` cells; chunk `[cx, cz]` starts at cell
  `[cx·chunk, cz·chunk]`.
- The world spans chunks `-extent` to `extent-1` on each axis, so cells `-extent·chunk`
  to `extent·chunk-1`. `extent × chunk × cell` must not exceed 8192 m: farther out,
  float32 positions lose the precision a 320×240 frame needs.
- Around the focus, the game keeps the `(2·view+1)²` chunks loaded, and unloads a chunk
  only when it is `view+1` chunks away (so a hero walking along a border does not reload
  chunks every tick).

## Top-level fields

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `veduta` | string | required | Must be exactly `"world/1"`. |
| `seed` | integer | `0` | Seed of the generator (biomes, scatter, sites). It is part of the world: the simulation seed does not change the map. |
| `cell` | number | `1` | Meters per cell, in (0, 64]. |
| `chunk` | integer | `16` | Cells per chunk side, 4 to 64. |
| `extent` | integer | `512` | Chunks from the origin in each direction, 1 to 4096 (see the 8192 m limit). |
| `view` | integer | `1` | Chunks loaded around the focus in each direction, 1 to 4. `1` keeps 3×3 chunks. |
| `biome_scale` | integer | `64` | Cells per period of the biome noise, 4 to 4096: larger values make larger biomes. |
| `camera` | object | required | As in a scene, **relative to the start cell**: `position` and `look_at` are added to the centre of the start cell. |
| `light` | object | scene default | As in a scene. |
| `background` | color | `"#202830"` | As in a scene. |
| `biomes` | array of objects | required, at least one | See Biomes. |
| `scatter` | array of objects | `[]` | See Scatter. |
| `sites` | array of objects | `[]` | See Sites. |
| `places` | array of objects | `[]` | See Places. |
| `entities` | array of objects | `[]` | Persistent entities (the hero, a HUD anchor), exactly as in a scene, never streamed. Positions are meters **relative to the start cell**. Names must not start with `chunk_`, `site_` or `place_`. |

The **start cell** is the `at` of `render`, `simulate`, a scenario or `ctx.LoadWorld`
(`[0, 0]` by default): the camera and the persistent entities are placed relative to
its centre, and the focus begins there. `render --world w --at 120,-40` therefore shows the
world around cell `[120, -40]` with the hero standing there.

### Biomes

Biomes are bands of one smooth noise field (integer arithmetic, so every machine agrees):
the range of the noise is split among the biomes in file order in proportion to their
weights, so with weights 3 and 1 the first biome covers three quarters of the ground and
adjacent biomes in the list are adjacent on the map.

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `name` | string | required | Unique biome name, referred to by `scatter`, `sites` and prefab rules. |
| `ground` | string | required | Material painted on the biome's cells. Its texture must be `tiling`: the ground is one mesh per chunk with one tile per cell. |
| `weight` | integer | `1` | Share of the noise range, 1 to 1000. |

### Scatter

Each rule puts a **one-cell prefab** (footprint at most `cell` × `cell`) on a share of
the cells of its biomes, decided cell by cell from the seed. Cells occupied by a site or a
place, or too close to one for the prefab's `min_distance`, are skipped.

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `prefab` | string | required | The prefab. |
| `biomes` | array of strings | the prefab's rule | Biomes to scatter on; the prefab's own `rules.biomes` also apply. |
| `density` | number | required | Share of the cells that get the prefab, more than 0 and at most 1. |

### Sites

Each rule cuts the world into `spacing` × `spacing` cell regions and puts, in a share of
them, one of its prefabs at a seeded offset that keeps the footprint inside the region.
Sites of one rule therefore never touch and keep their `min_distance`; a site that would
break a rule against a place or a site of an earlier rule is dropped. Sites are not
rotated.

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `tag` | string | required | Tag of the sites of this rule, for `min_distance` rules (`city`, `ruin`). |
| `prefabs` | array of strings | required | Prefabs to choose from, by seed. Each must fit: `spacing` ≥ its footprint in cells (the larger side) + the largest `min_distance` of the rule's prefabs, in cells. |
| `biomes` | array of strings | the prefab's rule | Biomes the region's candidate cell must be in. |
| `spacing` | integer | required | Cells per region, 2 to 4096. |
| `chance` | number | `1` | Share of the regions that get a site, more than 0 and at most 1. |

### Places

Explicit landmarks. A place always wins: sites and scatter inside its footprint or its
`min_distance` are dropped. `world_place` writes them (it validates or finds the cell);
writing one by hand is allowed, and `inspect world` reports one that overlaps, stands in a
wrong biome or breaks a distance rule.

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `name` | string | required | Unique; generated entities are `place_<name>_<entity>`. Must not start with `chunk_`, `site_` or `place_`. |
| `prefab` | string | required | The prefab. |
| `cell` | `[x, z]` | required | Min corner of the (rotated) footprint, inside the world along with the whole footprint. |
| `rotation` | integer | `0` | 0, 90, 180 or 270 degrees about +Y; 90 and 270 swap the footprint's width and depth. |

## Generated entities

Every entity of a chunk has a deterministic name, the same on every machine and in every
run, so scenarios and traces can refer to it:

| Entity | Name |
|--------|------|
| Ground of chunk `[cx, cz]` | `chunk_<cx>_<cz>_ground` (one static entity per chunk, tagged `ground`, no collision box) |
| Scatter at cell `[x, z]` | `chunk_<cx>_<cz>_c<i>_<entity>`, `i` the cell's index in the chunk (row-major) |
| Site of rule `r` in region `[rx, rz]` | `site_<r>_<rx>_<rz>_<entity>` |
| Place | `place_<name>_<entity>` |

A negative coordinate is written with `n`: chunk `[-3, 12]` is `chunk_n3_12`. A structure
(site or place) is spawned whole as soon as any chunk its footprint touches is loaded and
despawned when none is. Streamed spawns do not emit `spawn`/`despawn` events; the trace
gets one `chunk_load` (`{x, z, entities, first_id}`) and `chunk_unload` (`{x, z}`) event per
chunk, and `world_load` (`{world, at}`) when the world is loaded.

## Runtime

```go
func (g *Game) Init(ctx *veduta.Context) error {
	// A game started with --world already has it loaded; a scene game can switch:
	if ctx.World() == nil {
		return ctx.LoadWorld("overworld", [2]int32{0, 0})
	}
	return nil
}

func (g *Game) Update(ctx *veduta.Context, in veduta.Input) {
	hero := ctx.Scene.Find("player")
	if w := ctx.World(); w != nil && hero != nil {
		w.Focus(hero.WorldPosition()) // chunks follow the hero from the next tick
		ctx.Scene.Camera = scene.Camera2D(gmath.V2(hero.WorldPosition().X, hero.WorldPosition().Z), 12)
	}
}
```

`ctx.World()` is nil until a world is loaded. `Focus` sets the point the window is kept
around; `CellOf(pos)` returns the cell of a position; `Loaded()` lists the loaded chunks.
While a world is loaded the `within_bounds` invariant uses the world's extent (and the
project's Y range) instead of the project's `bounds`. Snapshots carry the loaded window,
so `Restore` continues the same streaming.

## Inspection and tools

`inspect world <name>` reports, ranked by severity:

| Code | Severity | Meaning |
|------|----------|---------|
| `WORLD_MISSING_PREFAB` | error | A scatter, site or place names a prefab that does not exist. |
| `WORLD_MISSING_ASSET` | error | A ground material, or a model or material of a persistent entity, does not exist. |
| `WORLD_GROUND_NOT_TILING` | error | A biome's ground material has no texture or one that is not `tiling`. |
| `WORLD_PLACE_OVERLAP` | error | Two places' footprints overlap. |
| `WORLD_PLACE_OUTSIDE` | error | A place's footprint leaves the world. |
| `WORLD_PLACE_BIOME` | warning | A place stands on a biome its prefab (or the biome list) does not allow. |
| `WORLD_PLACE_TOO_CLOSE` | warning | Two places are closer than a `min_distance` rule allows. |
| `WORLD_CHUNK_BUDGET` | warning | The densest sampled chunk, scaled to what the camera sees, exceeds the console's triangle budget. |
| `WORLD_VIEW_SHORT` | warning | The camera sees farther than `view` chunks, so unloaded ground is visible. |

Sheet: `map`, the top-down map around the origin (biome colors, footprints, names).

MCP tools (CLI `veduta world …`), all cell-based:

| Tool | Input | Output |
|------|-------|--------|
| `world_map` | `{ world, center?: [x, z], radius?: cells }` | Biome shares, places and sites in the region with their cell rectangles, counts; one image (biomes, footprints, names). |
| `world_query` | `{ world, cell: [x, z] }` | Biome, ground, chunk, what occupies the cell (place, site or scatter, with its rectangle), nearest place and site per tag with distances. |
| `world_place` | `{ world, prefab, name, cell?: [x, z], near?: [x, z], within?: cells, rotation?, dry_run? }` | With `cell`: validates it. Without: searches outward from `near` (default `[0, 0]`, rings up to `within`, default 64 cells, +X first then clockwise) for the first cell where every rule holds. A valid place is appended to the file's `places` (other bytes untouched) and returned; an invalid one is refused with every reason and nothing is written. |
| `world_remove` | `{ world, name }` | Removes the place from the file. |
| `render`, `simulate` | `{ world, at?: [x, z], … }` | As for a scene, starting at cell `at`. |

## Authoring loop

1. Write the prefabs (`prefab` topic); `inspect prefab <name>` until clean.
2. Write the world's biomes, scatter and sites; `cook`; `world_map` and look.
3. Add landmarks with `world_place` (let it choose the cell with `near`); `world_query`
   to check a cell you care about.
4. `inspect world <name>` until it reports no error; heed `WORLD_CHUNK_BUDGET`.
5. `render { world, at }` at a landmark and look; `simulate { world, at, ticks }` walking
   across a chunk border, expecting `chunk_load` events and no invariant violation.
6. Keep that simulation as a scenario:

```json
{
  "veduta": "scenario/1",
  "world": "overworld",
  "at": [0, 0],
  "seed": 1,
  "ticks": 200,
  "inputs": [ { "tick": 0, "press": ["ArrowRight"] } ],
  "expect": [ { "tick": 200, "trace": "chunk_load", "count_min": 12 } ],
  "invariants": ["no_overlap:player,obstacle", "entity_count_max:2000"]
}
```

## Example

```json
{
  "veduta": "world/1",
  "seed": 7,
  "cell": 1,
  "chunk": 16,
  "extent": 512,
  "view": 1,
  "biome_scale": 64,
  "camera": { "type": "orthographic", "size": 12, "position": [0, 30, 0], "look_at": [0, 0, 0] },
  "light": { "direction": [-0.4, -1, -0.3], "color": "#ffffff", "ambient": "#404040" },
  "background": "#202830",
  "biomes":  [ { "name": "plain",  "ground": "grass", "weight": 3 },
               { "name": "forest", "ground": "moss",  "weight": 2 } ],
  "scatter": [ { "prefab": "tree", "biomes": ["forest"], "density": 0.06 } ],
  "sites":   [ { "tag": "village", "prefabs": ["village"], "biomes": ["plain"], "spacing": 48, "chance": 0.5 } ],
  "places":  [ { "name": "capital", "prefab": "city", "cell": [120, -40], "rotation": 90 } ],
  "entities": [ { "name": "player", "kind": "player", "model": "hero", "material": "hero", "tags": ["player"] } ]
}
```
