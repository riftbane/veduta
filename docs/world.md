# World — `assets/worlds/<name>.world.json`

A world is a map too large to write by hand: a seeded generator paints biomes over an
integer grid of cells, raises rolling ground and the hills, plains, lakes and seas you
name (`terrain`, `features`), scatters one-cell prefabs, places sites (villages, cities,
ruins) on a jittered grid, and puts the landmarks you name (`places`) exactly where you
say. The
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
  z in `[z·cell, (z+1)·cell)`. +X is east, −Z is north (up on a top-down render).
- A **vertex** `[x, z]` is the min corner of cell `[x, z]`: the ground has one height per
  vertex (whole millimeters, computed with integers so every machine agrees) and each cell
  is two triangles between its four corners.
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
| `terrain` | object | flat | See Terrain. Since v1.3.0. |
| `features` | array of objects | `[]` | Hills, plains, lakes and seas; see Features. Since v1.3.0. |
| `vegetation` | array of objects | `[]` | Trees, grass and flowers, everywhere or in round areas; see Vegetation. Since v1.3.0. |
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

### Terrain

The ground's shape. Without `terrain` (or with `relief` 0 and no features) the ground is
flat at y = 0.

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `relief` | number | `0` | Meters, 0 to 256: seeded rolling ground between −relief and +relief. |
| `relief_scale` | integer | `48` | Cells per period of the relief noise, 4 to 4096: larger values make wider hills. |
| `sea_level` | number | none | Meters, −1024 to 1024: wherever the ground lies below it, it is sea. Without it only lakes and seas hold water. |
| `water` | string | built-in | Material of water surfaces. The built-in water (`world:water`) is an opaque lake blue. |
| `lod_distance` | number | 2 × `chunk` × `cell` | Meters from which a chunk's ground is drawn at its first coarser level; each next level from twice as far (see Rendering). |

### Features

Named changes to the ground, applied **in file order** on top of the relief: a later
feature works on the ground the earlier ones left. `world_terrain` adds them (it reports
the heights before and after and what now stands in water); `world_remove` removes them.

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `name` | string | required | Unique among places, features and vegetation rules; not starting with `chunk_`, `site_` or `place_`. |
| `kind` | string | required | `hill`, `plain`, `lake` or `sea`. |
| `cell` | `[x, z]` | required | The centre: a vertex inside the world. |
| `radius` | integer | required | Cells from the centre to the edge, 1 to 16384. |
| `height` | number | see below | `hill`: meters added at the top, −256 to 256, required (negative digs a hollow). `plain`: the level, default the ground at the centre. `lake`, `sea`: the water level, default the ground at the centre (lake) or `sea_level`, else 0 (sea). |
| `depth` | number | lake 2, sea 8 | `lake`, `sea`: meters from the water down to the bottom at the centre, (0, 256]. |
| `falloff` | integer | see below | `hill`, `plain`: cells inside the edge over which the feature fades into the ground around it, 0 to `radius` (hill default `radius`: a dome; plain default `radius`/3; 0 cuts a step). `lake`, `sea`: width of the shore outside the rim over which the ground returns to its own height (lake default `radius`/4 in 2..16, sea `radius`/8 in 4..32). |
| `roughness` | number | hill 0.2, plain 0.1, lake and sea 0.3 | 0 to 1: how far the edge wanders from a circle (the radius varies by up to ± roughness/2, smoothly). |

- A **hill** adds `height` at the centre, fading to nothing at the edge.
- A **plain** levels the ground to its level within `radius − falloff`, blending out to
  the edge: flatten a hillside before placing a town, or cut a mesa into a hill.
- A **lake** or a **sea** is a bowl: water at its level out to the (wandering) radius, the
  bottom `depth` below it at the centre, a dry rim 2 cells wide just above the water
  (5 cm), then the shore back to the ground. A sea is a big lake: centre it off the coast
  with a radius that reaches the shore (`cell: [-600, 0], radius: 500` makes the west
  a sea from about x = −100 on).
- With `sea_level`, any ground below it is sea too, lakes and seas included.

**Pads.** Every site and place stands on a pad: the vertices of its footprint are levelled
to the ground at the footprint's centre (features and relief included), blending into the
ground over 2 cells around it. Its entities' `y` is measured from the pad; a scattered
prefab's from the ground at the centre of its cell.

**Water.** A site whose footprint has a vertex under water is dropped; scatter skips cells
whose centre is under water; a place in water is reported (`WORLD_PLACE_WATER`). Water is
drawn as flat quads at its level over every cell with a corner under it: the ground hides
the part that lies above it. Nothing collides with water: a game asks `WaterAt`.

### Vegetation

Named rules that plant the world, everywhere or in a round area. `world_vegetation` adds
them (it reports how many plants the area gets and what they cost to draw);
`world_remove` removes them.

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `name` | string | required | Unique among places, features and vegetation rules; not starting with `chunk_`, `site_` or `place_`. |
| `prefab` | string | one of the two | A **one-cell prefab** (a tree, a rock): entities that collide, exactly like `scatter`. |
| `model` | string | one of the two | A **flora model** (grass, flowers, pebbles): drawn as part of its chunk, no entity, no collision, not in the trace. |
| `density` | number | required | Share of the cells that get one, more than 0 and at most 1. |
| `biomes` | array of strings | any | Biomes to plant on (a prefab's own `rules.biomes` also apply). |
| `cell` | `[x, z]` | none | With `radius`: the centre of the area (a vertex inside the world). |
| `radius` | integer | none | With `cell`: cells from the centre, 1 to 16384. The edge wanders by ±15% of it. |
| `scale` | `[min, max]` | `[0.8, 1.2]` | `model` only: each plant is scaled by a seeded factor between them (0 < min ≤ max ≤ 16). |

- A **prefab** rule competes for cells with the scatter rules and the prefab rules before
  it: the first rule that fires owns the cell (scatter rules first, then vegetation rules
  in file order). It keeps its prefab's `min_distance` from sites and places.
- A **model** rule plants at most one plant per cell, somewhere in the middle 60% of the
  cell, turned by a quarter turn and mirrored by the seed, standing on the ground. Any
  number of model rules may plant the same cell (grass and flowers together). Flora skips
  water and the footprints of sites and places.
- A flora model is an ordinary model (`model` topic): keep it to a handful of triangles
  (a `lathe` cone of 3–4 segments is a tuft of grass) and give it a `draw_distance`, and a
  `lod` level or two: flora is thinned with distance (next section).

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
| Ground of chunk `[cx, cz]` | `chunk_<cx>_<cz>_ground` (one static entity per chunk, tagged `ground`, no collision box; it draws the chunk's water too) |
| Flora of model `m` on chunk `[cx, cz]` | `chunk_<cx>_<cz>_flora_<m>` (one static entity per flora model present, tagged `flora`, no collision box) |
| Scatter or vegetation prefab at cell `[x, z]` | `chunk_<cx>_<cz>_c<i>_<entity>`, `i` the cell's index in the chunk (row-major) |
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
around; `CellOf(pos)` returns the cell of a position; `Loaded()` lists the loaded chunks;
`HeightAt(pos)` returns the height of the ground under a position (exactly on the
triangles of the chunk's finest drawn level, so a hero set to it stands on the ground) and `WaterAt(pos)` the water level
over it and whether the ground there is under water:

```go
if w := ctx.World(); w != nil {
	p := hero.Transform.Position
	next := p.Add(step)
	if _, wet := w.WaterAt(next); !wet { // keep out of lakes and seas
		next.Y = w.HeightAt(next)
		hero.Transform.Position = next
	}
}
```
While a world is loaded the `within_bounds` invariant uses the world's extent (and the
project's Y range) instead of the project's `bounds`. Snapshots carry the loaded window,
so `Restore` continues the same streaming.

## Rendering

A console draws about 1200 triangles a frame at 20 Hz, so a world is drawn with two
savings, both automatic:

- **Limited view.** An entity whose drawn bounds lie wholly outside the camera's view
  volume costs nothing, and nothing beyond the camera's `far` plane is drawn: a chunk
  behind the camera or past `far` is skipped whole. Keep `far` (perspective) or `size`
  (orthographic) within the loaded chunks (`WORLD_VIEW_SHORT`).
- **Fewer triangles far away.** Each chunk's ground has levels of detail on grids of 1, 2,
  4, 8 and 16 cells. The finest level a chunk is drawn with is the coarsest grid that
  passes within 6 cm of every vertex (and keeps its biome edges: flat ground at 1 cell
  where biomes meet, uneven ground at up to 2), so gentle ground costs few triangles even
  up close; `HeightAt` follows that grid. Each coarser level is drawn from `lod_distance`
  × 2^(k−1) meters on (k = 1 for the first coarser level); flat runs merge into one quad
  at every level. Neighbouring chunks drawn with different grids would leave cracks along
  their shared edge, so each level hangs a skirt below every edge that is not straight,
  as deep as the edge's height range. Flora follows its model's `lod` and `draw_distance`:
  at the model's level k a chunk draws one plant in 2^k (a seeded half, then a quarter)
  with that level's geometry, and none beyond the draw distance. Trees and every other
  entity get the same savings from their own model's `lod` and `draw_distance` (`model`
  topic).

Distances are measured from the camera to the nearest point of an entity's bounds, as
seen through a 60° lens (an orthographic camera counts 0.866 × `size` for everything).
`render` reports `draw`: the entities considered, culled, too far, and drawn at a lower
level.

## Inspection and tools

`inspect world <name>` reports, ranked by severity:

| Code | Severity | Meaning |
|------|----------|---------|
| `WORLD_MISSING_PREFAB` | error | A scatter, site, place or vegetation rule names a prefab that does not exist. |
| `WORLD_MISSING_ASSET` | error | A ground or water material, a vegetation model, or a model or material of a persistent entity, does not exist. |
| `WORLD_GROUND_NOT_TILING` | error | A biome's ground material has no texture or one that is not `tiling`. |
| `WORLD_PLACE_OVERLAP` | error | Two places' footprints overlap. |
| `WORLD_PLACE_OUTSIDE` | error | A place's footprint leaves the world. |
| `WORLD_PLACE_BIOME` | warning | A place stands on a biome its prefab (or the biome list) does not allow. |
| `WORLD_PLACE_WATER` | warning | A vertex of a place's footprint lies under water. |
| `WORLD_PLACE_TOO_CLOSE` | warning | Two places are closer than a `min_distance` rule allows. |
| `WORLD_CHUNK_BUDGET` | warning | The densest sampled chunk (ground at its finest level, flora, scatter, structures), scaled to what the camera sees, exceeds the console's triangle budget. |
| `WORLD_VIEW_SHORT` | warning | The camera sees farther than `view` chunks, so unloaded ground is visible. |

Sheet: `map`, the top-down map around the origin (biome colours shaded by the relief,
water, features and vegetation areas as labelled circles, footprints, names).

MCP tools (CLI `veduta world …`), all cell-based:

| Tool | Input | Output |
|------|-------|--------|
| `world_map` | `{ world, center?: [x, z], radius?: cells }` | Biome shares, `ground` (lowest and highest cell, water cells), the `features` touching the region with their resolved `level`, every `vegetation` rule with its `plants` in the region, places and sites with their cell rectangles, counts; one image (relief, water, circles, footprints, names). |
| `world_query` | `{ world, cell: [x, z] }` | Biome, ground material, chunk, `height` at the cell's centre, `water` level when it is under water, the `features` shaping it, the `vegetation` rules with a plant on it, what occupies the cell (place, site or scatter, with its rectangle), nearest place and site per tag with distances. |
| `world_place` | `{ world, prefab, name, cell?: [x, z], near?: [x, z], within?: cells, rotation?, dry_run? }` | With `cell`: validates it. Without: searches outward from `near` (default `[0, 0]`, rings up to `within`, default 64 cells, +X first then clockwise) for the first cell where every rule holds. A valid place is appended to the file's `places` (other bytes untouched) and returned; an invalid one is refused with every reason and nothing is written. |
| `world_terrain` | `{ world, name, kind, cell, radius, height?, depth?, falloff?, roughness?, dry_run? }` | Appends a hill, plain, lake or sea to `features` when the world still compiles (else refused with the compiler's reasons, nothing written). Reports the feature as resolved (its `level`), the `chunks` it changes, the ground `before` and `after` (min, max, centre, water cells) within 96 cells of its centre, `places_in_water`, `sites_removed` and `sites_added`; one map image. |
| `world_vegetation` | `{ world, name, prefab? \| model?, density, biomes?, cell?, radius?, scale?, dry_run? }` | Appends a vegetation rule when valid. Reports the `plants` in its area (or within 64 cells of the origin for a world-wide rule), their `triangles`, the most in one chunk (`max_chunk_triangles`), the flora model's `draw_distance`, and `warnings` (no draw distance, a heavy flora model, a chunk over half the budget, no plant at all); one map image. |
| `world_remove` | `{ world, name }` | Removes the place, feature or vegetation rule of that name from the file (an emptied array stays as `[]`). |
| `render`, `simulate` | `{ world, at?: [x, z], … }` | As for a scene, starting at cell `at`. |

## Authoring loop

1. Write the prefabs (`prefab` topic) and the flora models (a few triangles each, with a
   `draw_distance`); `inspect prefab <name>` and `inspect model <name>` until clean.
2. Write the world's biomes, `terrain` (a gentle `relief`), scatter and sites; `cook`;
   `world_map` and look.
3. Shape the land with `world_terrain`: hills and hollows, a `plain` where a town will
   stand, lakes, a sea off a coast. Read `after` and `places_in_water`; `world_map` again.
4. Plant it with `world_vegetation`: groves of trees (`prefab`), grass on a biome, flowers
   in a meadow (`model`, `cell`, `radius`). Heed its `warnings`.
5. Add landmarks with `world_place` (let it choose the cell with `near`); `world_query`
   to check a cell you care about (its height, water, plants).
6. `inspect world <name>` until it reports no error; heed `WORLD_CHUNK_BUDGET`.
7. `render { world, at }` at a landmark and look (`draw` in the report says what the view
   limit and the levels of detail saved); `simulate { world, at, ticks }` walking across a
   chunk border, expecting `chunk_load` events and no invariant violation.
8. Keep that simulation as a scenario:

```json
{
  "veduta": "scenario/1",
  "world": "overworld",
  "at": [0, 0],
  "seed": 1,
  "ticks": 200,
  "inputs": [ { "tick": 0, "press": ["right"] } ],
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
  "terrain": { "relief": 1.5, "relief_scale": 48, "lod_distance": 24 },
  "features": [ { "name": "north_hills", "kind": "hill", "cell": [10, -60], "radius": 24, "height": 9 },
                { "name": "town_ground", "kind": "plain", "cell": [126, -34], "radius": 14 },
                { "name": "mirror_lake", "kind": "lake", "cell": [-30, 20], "radius": 9, "depth": 2.5 },
                { "name": "west_sea", "kind": "sea", "cell": [-700, 0], "radius": 560, "height": -0.5 } ],
  "vegetation": [ { "name": "grass", "model": "grass_tuft", "density": 0.3, "biomes": ["plain"] },
                  { "name": "oak_grove", "prefab": "oak", "density": 0.25, "cell": [40, 10], "radius": 12 },
                  { "name": "poppies", "model": "poppy", "density": 0.5, "cell": [-12, 8], "radius": 6, "scale": [0.7, 1.3] } ],
  "scatter": [ { "prefab": "tree", "biomes": ["forest"], "density": 0.06 } ],
  "sites":   [ { "tag": "village", "prefabs": ["village"], "biomes": ["plain"], "spacing": 48, "chance": 0.5 } ],
  "places":  [ { "name": "capital", "prefab": "city", "cell": [120, -40], "rotation": 90 } ],
  "entities": [ { "name": "player", "kind": "player", "model": "hero", "material": "hero", "tags": ["player"] } ]
}
```
