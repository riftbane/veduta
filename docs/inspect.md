# Inspection (`inspect`)

`veduta inspect model|texture|scene NAME [--focus CODE] [--sheets list]` (MCP tool
`inspect` with `kind`, `name`, `focus`, `sheets`) returns a **report** and writes
**sheets** (PNG, at most 640 px wide) under `out/<name>.<kind>.png`. Read the report
first; open a sheet only to confirm a suspected issue.

```json
{
  "subject": "model:hero",
  "summary": { "errors": 1, "warnings": 0, "info": 0 },
  "issues": [
    { "severity": "error", "code": "MESH_FLIPPED_NORMALS", "count": 64,
      "where": { "part": 0, "shape": "cylinder", "triangles": [0, 1, 2, 3, 4, 5, 6, 7] },
      "hint": "Part 0 (cylinder) has normals pointing inward. Set \"flip_normals\": false or check winding." }
  ],
  "metrics": { "triangles": 588, "vertices": 322, "aabb": [[-0.3, 0, -0.425], [0.3, 1.4, 0.3]] },
  "sheets": ["out/hero.summary.png"]
}
```

- Issues are sorted by severity (error, warning, info), then code. Each has a `count`,
  a `where` that locates it in the source (part index, triangles, layer, entity, pixels…)
  and a `hint` naming the source field to change.
- `--focus CODE` keeps only issues with that code (the summary still counts all).
- `--sheets`: comma-separated kinds; default one `summary` sheet; `all` for every kind;
  `none` for the report only. Unknown kinds are errors that list the valid ones.
- Numbers in metrics are rounded to 4 decimals; vectors are `[x, y, z]`; non-finite
  values are the strings `"NaN"`, `"+Inf"`, `"-Inf"`.

## Model (`inspect model NAME`)

Positions are welded within 1e-6 × the model's AABB diagonal before topology checks.
Degenerate triangles are left out of every other check. `where.part` is the index in
`parts` (with `shape`, and `of` for mirrors). "Eligible" parts can show a texture: no part
material, or a material with a texture.

| Code | Severity | Meaning | What to change |
|---|---|---|---|
| `MESH_DEGENERATE_TRIANGLE` | warning | Triangles with an index out of range, non-finite, repeated or welded corners, or 2·area ≤ 1e-6 × longest edge² (`where.reasons` counts each cause). | Near-zero `size`, `radius`, `height`, `depth` or `scale`; repeated or collinear `profile` points. |
| `MESH_DUPLICATE_VERTEX` | warning / info | Vertices identical in position, normal and UV. Warning when they span parts (the parts coincide and z-fight); info within a part. | Move or remove the duplicated part (`position`). |
| `MESH_NONMANIFOLD_EDGE` | warning | An edge shared by more than 2 triangles of a part. | The part's `profile` (touching or repeated points). |
| `MESH_OPEN_BOUNDARY` | warning / info | Hole loops (boundary edges grouped into loops; `where` gives loop sizes and centres). Info for `plane` and open `lathe` parts. | Close the lathe `profile` (start and end at r = 0); check sizes of closed shapes. |
| `MESH_FLIPPED_NORMALS` | error | A closed part whose signed volume is negative (inside out), or triangles whose vertex normals point against their winding. | `"flip_normals"` (on the source part of a mirror), or lower `smooth_angle_deg`. |
| `MESH_MIXED_WINDING` | error | Triangles wound against their neighbours (a shared edge runs the same way in both). | The part's geometry fields and `flip_normals`. |
| `MESH_UV_OUT_OF_RANGE` | warning / info | UVs (meters) outside [0, 1]. Warning when the part's texture does not tile; info otherwise (merged across parts). | `"tiling": true` in the texture, or a smaller part. |
| `MESH_UV_OVERLAP` | warning / info | Triangles of one part share UV cells (grid of 64 cells along the longer UV extent, reported above 1% of covered cells). Warning only with a non-tiling texture. | `"tiling": true` or another `uv` mode. |
| `MESH_TEXEL_DENSITY_UNEVEN` | warning / info | `texel_density_cv` > 0.5; `where` names the worst part. | That part's `uv` mode. |
| `MESH_PIVOT_OFF` | warning | The origin is not where `pivot` says; with `origin`, the origin lies outside the geometry. | `pivot`, or the parts' `position`. |
| `MESH_SCALE_SUSPICIOUS` | warning | Largest extent outside 0.05–100 m. | `size`, `radius`, `height`, `profile`, `scale` (units are meters). |
| `MESH_ASYMMETRIC` | warning | Only when `symmetry` is set: fewer than 98% of vertices mirror onto the surface (within 0.1% of the diagonal) across the source plane `<axis> = 0`. `where` lists the parts involved. | Centre those parts, use a `mirror` part, or remove `symmetry`. |
| `MESH_TRIANGLE_BUDGET` | warning | More triangles than `triangle_budget`; `where` names the largest part. | Its `segments` / `rings`, or `triangle_budget`. |

Metrics: `triangles`, `vertices`, `parts`, `materials`, `aabb`, `size`, `pivot`,
`pivot_offset`, `surface_area`, `volume` (closed parts), `watertight`,
`texel_density_cv` (area-weighted coefficient of variation of UV area per m² over
eligible parts), `symmetry_x` and `symmetry_<axis>`, `triangle_budget`, `part_stats` (per
part: triangles, closed, area, signed volume — negative means inside out).

Sheets:
- `summary` (default): iso lit, front, right, normals, wireframe, UV checker.
- `turntable`: 8 views every 45° at 20° pitch, plus top and bottom.
- `silhouette`: front, right, top, iso.
- `normals`: front, right, back, left. Back faces are hatched magenta and normals pointing
  away from the viewer hatched orange, so flipped parts stand out.
- `wireframe`: iso, iso back, front, top. `uv_checker`: the same views with the UV checker.
- `sections`: three cuts through the model centre across X, Y and Z, in normals mode, so
  exposed interiors show as hatched back faces.

Hole edges are drawn red and non-manifold edges yellow.

## Texture (`inspect texture NAME`)

Measured on the compiled texture (level 0 unless stated); given its source, the texture is
also re-rendered once without each layer. "Texel difference" is the largest of |ΔR|, |ΔG|,
|ΔB|, |ΔA| (0–255); "luminance" is 0.299 R + 0.587 G + 0.114 B (0–255).

| Code | Severity | Meaning | What to change |
|---|---|---|---|
| `TEX_NOT_POWER_OF_TWO` | warning when `tiling` or mipmapped, else info | Width or height is not a power of two. | `"size"` (the report suggests the nearest powers of two). |
| `TEX_SEAM` | warning (tiling only) | Mean difference across the left-right (or top-bottom) wrap is ≥ 8, > 2 × the inside neighbour difference + 4, and > 1.25 × the strongest line inside. `where.layer` names the layer causing it when removing that layer removes at least half. | That layer (gradient: remove it; image: `path`; rect: `xy`/`size`; circle: `center`/`radius`; stripes: `width`/`angle_deg`), or `"tiling": false`. |
| `TEX_LOW_CONTRAST` | warning | Luminance std < 4 and alpha range < 16: reads as one flat color. | Layer `color`/`colors`/`from`/`to`/`opacity`, or drop the texture and use the material `albedo`. |
| `TEX_ALPHA_UNUSED` | warning / info | Warning: texels with alpha < 255 but every material using the texture is `opaque`. Info: a `blend` material's texture × albedo alpha is 255 everywhere, or no texel falls below a `cutout` material's `cutoff`. | Material `"alpha"` (and `"cutoff"`), or the texture's layer alphas. |
| `TEX_MIP_ILLEGIBLE` | warning | With ≥ 3 levels and level 2 ≥ 4×4: luminance std at mip 2 < 0.35 × level 0. `where.layer` names the layer causing it. | Checker `cells`, noise `scale`/`octaves`, stripes `width`, image `path`; for pixel art `"mipmaps": false` with material `"filter": "nearest"`. |
| `TEX_LAYER_NO_EFFECT` | warning | Leaving out `layers[i]` changes no texel by more than 1/255 (covered by a later opaque layer, opacity 0, or transparent colors). | Remove or reorder `layers[i]`, or its `opacity`, `blend` or colors. |
| `TEX_LAYERS_NOT_CHECKED` | info | Layer analysis did not run (no source, over budget, or stale cooked texture). | Inspect with the source, or cook again. |

Metrics: `size`, `levels`, `tiling`, `wrap`, `layers`, `mean_color`, `luminance_mean`,
`luminance_std`, `luminance_range` (99th − 1st percentile), `alpha_min`, `alpha_max`,
`nonopaque_texels`, `seam_delta_lr`, `seam_delta_tb`, `inside_delta`, `inside_delta_x`,
`inside_delta_y`, `mip2_contrast_ratio` (null without 3 levels), `materials` (materials
using the texture).

Sheets: `summary` (default: single + tiled 2×2 when tiling + channels + mips), `single`,
`tiled_2x2`, `channels` (R, G, B, A as gray with value ranges), `mips` (every level
upscaled with nearest neighbour to one size, labeled `L<n> WxH`), `on_model:<model>` (the
texture on every part of the model, lit, iso view). `all` writes every sheet except
`on_model`. Transparent texels are shown over a gray checker.

## Scene (`inspect scene NAME`)

The scene is loaded with the library's model bounds. The project supplies the bounds and
the analysis resolution (`inspect_resolution`, 640×360 by default). Pixel counts come
from one render of the scene camera in which every drawn part, alpha-blended ones
included, writes the entity-id buffer. Entity index `i` is the position in `entities`.
At most 16 issues are listed per code; one more issue with `where.omitted` counts the
rest.

| Code | Severity | Meaning | What to change |
|------|----------|---------|----------------|
| `SCENE_MISSING_ASSET` | error | An entity's `model` or `material`, a model's `parts[k].material`, or a used material's `texture` is not in the library. One issue per missing name; count = entities affected. | Create the named asset file, or fix the field (a near match is suggested). |
| `SCENE_ENTITY_OUTSIDE_BOUNDS` | warning | An entity's world position or AABB is not inside the project bounds (boundary included). | `entities[i].position`, or `bounds` in `veduta.json`. |
| `SCENE_OVERLAP` | warning | The AABBs of two `static` entities intersect by more than 1 mm on every axis. Touching boxes and parent/descendant pairs do not count. | Move one entity by the suggested amount along the axis of least depth (`entities[i].position`), or scale it. |
| `SCENE_CAMERA_SEES_NOTHING` | error | No pixel of the camera render belongs to an entity. | `camera.look_at`, `camera.position`, `camera.near` or `camera.far` as the hint says, or add visible entities with a model. |
| `SCENE_ENTITY_OFFSCREEN` | warning | An entity tagged `important` has no visible pixel. `reason`: `occluded` (occluders listed), `outside_view`, `no_pixels`, `hidden`, `no_model`, `missing_model`, `not_measured`. | `camera.position` / `camera.look_at`, the occluder's or the entity's `position`, `visible`, `scale`, or its `tags`. |
| `SCENE_UNLIT` | warning / info | Warning: `light.color` and `light.ambient` are both near black with lit materials drawn, or entity pixels average below 32/255 luminance. Info: every drawn material is unlit. | `light.color`, `light.ambient`, `light.direction` (y must be negative), a material's `albedo`, or `unlit`. |
| `SCENE_ZFIGHT_RISK` | warning | Faces of two different entities lie in the same plane (within 1 mm, normals within about 0.8°), face the same way (or one is double-sided) and overlap by more than 0.1% of the smaller triangle. | Move the smaller entity at least 0.01 m along the normal (`entities[i].position`), remove the covered face, or set `cull` to `"back"`. |

Metrics: `entities`, `drawn_entities`, `visible_entities`, `important_entities`,
`important`, `triangles`, `bounds`, `project_bounds`, `camera`, `light`, `background`,
`resolution`, `background_ratio`, `mean_luminance`, `entity_pixels` (per drawn entity:
`id`, `name`, `pixels`, `bbox`; first 64), `lit_parts`, `unlit_parts`, `overlap_pairs`,
`zfight_pairs`, `zfight_tests`, `zfight_truncated`.

Sheets are framed 4:3 like the console panel: single views are 640×480 (`ids` adds its
legend below) and summary tiles 317×238. The camera view keeps the scene camera's
vertical `fov_deg`, so its horizontal field follows the aspect, and the top view's
frustum is drawn at that same aspect. Issues and pixel metrics come from the render at
`inspect_resolution` instead, so with a wider analysis resolution an entity near a side
edge can count as visible although the camera view and the frustum leave it out.

- `summary` (default): camera, top, ids and legend in one 640×482 grid.
- `camera`: the scene camera with issue overlays (red overlap boxes and out-of-bounds
  entities, orange boxes of hidden important entities, magenta z-fight outlines).
- `top`: orthographic top view with -Z up, every entity AABB and name, the camera frustum
  and look-at point in yellow, the project bounds in blue, and the same overlays.
- `ids`: entity-id false colors with a legend of every visible entity.

## Diff and query

- `veduta diff A B [--threshold N]` (PNG or `.vframe`): `changed_pixels`, `changed_ratio`,
  `bbox`, `max_delta`, `mean_delta` and an `a | b | heat` sheet.
- `veduta render --bundle` writes a frame bundle (`.vframe`: color, depth, entity ids,
  normals, camera, per-entity projected coverage). `veduta query --frame F --at x,y`
  answers entity, depth, eye distance, world position and normal at a pixel;
  `--coverage` gives per-entity visible pixels, screen bounds, projected pixels and the
  occlusion ratio (visible / projected).
