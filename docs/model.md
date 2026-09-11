# Model format — `model/1`

A model is a mesh described as a list of primitive **parts** (boxes, cylinders, spheres,
planes, extrusions, lathed profiles and mirror copies). The compiler turns the parts into
one triangle mesh with normals, UVs, one material slot per part and a pivot. Parts are
joined by concatenation only (no boolean operations): overlapping parts simply overlap.

File: `assets/models/<name>.model.json`. The model name is the file name without
`.model.json`; it must be 1–64 characters of `a-z`, `0-9`, `_`, `-`, starting with a
letter or digit.

Units and axes: lengths are meters, angles are degrees. Coordinates are right-handed,
+Y up, +X right, +Z towards the viewer of a camera looking down −Z.

## Minimal example

```json
{
  "veduta": "model/1",
  "parts": [
    { "shape": "box", "size": [1, 1, 1], "position": [0, 0.5, 0], "material": "crate_wood" }
  ]
}
```

## Top-level fields

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `veduta` | string | required | Must be `"model/1"`. |
| `name` | string | file name | Optional. When present it must equal the file name's model name. |
| `units` | string | `"m"` | Only `"m"` (meters) exists. |
| `pivot` | string | `"origin"` | `origin`, `center` or `bottom-center` (see [Pivot](#pivot)). |
| `smooth_angle_deg` | number | `30` | In [0, 180]. Faces meeting at an angle ≤ this are shaded smoothly (see [Normals](#normals)). |
| `symmetry` | string | `""` | `""` (none), `x`, `y` or `z`: asks `inspect` to check that the model is mirror-symmetric across the plane perpendicular to that axis (issue `MESH_ASYMMETRIC`). It does not change the geometry. |
| `triangle_budget` | integer | `20000` | In [1, 1000000]. `inspect` reports `MESH_TRIANGLE_BUDGET` above it. The compiler never refuses a model for exceeding it. |
| `parts` | array | required | At least one part object (see below). |

## Parts

Every part is an object with a `shape` field. Each shape accepts only its own fields plus
the common fields; **any other field is an error, even when it is `null`, `0` or
`false`**. Part indices (used by `mirror` and in error messages and reports) start at 0.

### Common fields (every shape except `mirror`)

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `position` | [x, y, z] | [0, 0, 0] | Translation in meters. |
| `rotation_deg` | [x, y, z] | [0, 0, 0] | Euler angles in degrees, applied Z first, then X, then Y (R = Ry·Rx·Rz), the same convention as scene entities. |
| `scale` | [x, y, z] | [1, 1, 1] | Per-axis scale; every component must be non-zero. Negative components mirror the part; the compiler keeps its faces pointing outwards. |
| `material` | string | none | Material name (`assets/materials/<name>.mat.json`). A part without a material is drawn with the entity's material (or the default white material). |
| `uv` | string | per shape | `box`, `planar`, `cylindrical` or `spherical` (see [UV mapping](#uv-mapping)). Defaults: box → `box`, plane → `planar`, cylinder → `cylindrical`, sphere → `spherical`, extrude → `box`, lathe → `cylindrical`. |
| `flip_normals` | boolean | `false` | Reverses the winding and the normals so the part faces inwards (for example the inside of a room or a sky dome). |

A part is built in its local space (the shape centered at the local origin as described
below), then scaled, rotated, and translated: `p' = R·(scale ⊙ p) + position`.

### `box`

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `size` | [x, y, z] | required | Full extents in meters, each > 0. |

A box centered at the origin spanning ±size/2. 12 triangles, 24 vertices (4 per face),
always sharp edges unless `smooth_angle_deg` ≥ 90.

### `cylinder`

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `radius` | number | required | > 0. |
| `height` | number | required | > 0. |
| `segments` | integer | `16` | 3–256 sides around the Y axis. |

Axis along Y from y = −height/2 to +height/2, closed by flat caps. The first side edge
is at +Z. 4 × segments triangles. At the default 30° smoothing a cylinder with 12 or
more segments has a smooth side (adjacent sides meet at 360°/segments); caps stay sharp.

### `sphere`

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `radius` | number | required | > 0. |
| `segments` | integer | `16` | 3–256 divisions around the Y axis. |
| `rings` | integer | `8` | 2–128 latitude bands from pole to pole. |

A UV sphere centered at the origin with poles at (0, ±radius, 0). 2 × segments ×
(rings − 1) triangles (triangle fans at the poles, quads elsewhere). Smooth when
360°/segments and 180°/rings are both ≤ `smooth_angle_deg` (true for the defaults).

### `plane`

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `size` | [x, z] | required | Extents along X and Z, each > 0. |

A rectangle in the XZ plane (y = 0) centered at the origin, facing +Y. It is
**single-sided**: with the default back-face culling it is invisible from below. Use a
material with `"cull": "none"` or a second, flipped plane for two sides. 2 triangles.

### `extrude`

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `profile` | [[x, y], …] | required | A polygon in the XY plane, 3–1024 points. |
| `depth` | number | required | > 0. |

The profile is extruded along Z from z = −depth/2 to z = +depth/2, with a flat cap at
each end (the front cap faces +Z) and one rectangular wall per profile edge. Profile
coordinates are used as given (not re-centered). Rules for the profile:

- it must be a **simple polygon**: edges may not cross or touch except consecutive
  edges at their shared point, and an edge may not fold back onto the previous one;
- consecutive points must differ, and the last point must not repeat the first (the
  polygon is closed automatically);
- concave shapes and collinear points are fine; either winding (clockwise or
  counter-clockwise) is accepted — the compiler orients it so faces point outwards.

Caps are triangulated by ear clipping. 2 × (points − 2) + 2 × points triangles.

### `lathe`

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `profile` | [[r, y], …] | required | 2–1024 points; r ≥ 0 is the distance from the Y axis, y the height. |
| `segments` | integer | `16` | 3–256 divisions around the Y axis. |

The profile is revolved around the Y axis (the first division is at +Z). There are **no
implicit caps**: the profile is the whole surface. To make a closed solid, start and end
the profile on the axis (r = 0), for example `[[0, 0], [0.5, 0], [0.4, 1], [0, 1.2]]`,
or end it on its first point to make a ring (`[[1, 0], [1.5, 0], [1.5, 1], [1, 1], [1, 0]]`).

- Points with r = 0 collapse to a single point on the axis (triangle fans, no
  degenerate triangles); a profile segment lying on the axis adds nothing.
- Consecutive points must differ; at least one point must have r > 0.
- Which side the surface faces: the right-hand side of the direction of travel when the
  profile is drawn with r to the right and y up. List an open profile **bottom to top**
  to face away from the axis. A closed profile (both ends on the axis, or last point
  equal to the first) is oriented automatically, so its point order does not matter.
- Triangles per profile segment: 2 × segments when both ends have r > 0, segments when
  one end is on the axis, 0 when both are.

### `mirror`

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `axis` | string | required | `x`, `y` or `z`. |
| `of` | integer | required | Index of an **earlier** part (any shape, including another mirror). |
| `material` | string | source's | Overrides the material of the copy. |

Copies the final geometry of part `of` (after its own scale, rotation and translation)
and reflects it across the model-space plane through the origin perpendicular to `axis`
(`"x"` reflects across the plane x = 0, before the pivot is applied). The winding is
reversed so the copy faces outwards like its source. UVs are copied, so a texture appears
mirrored. The copy inherits the source's `uv` mode and `flip_normals` (a copy of a
flipped part is flipped too). No other fields are allowed: `position`, `rotation_deg`,
`scale`, `uv` and `flip_normals` are errors on a mirror. To mirror a part in place,
build it on one side of the plane and mirror it: both halves meet exactly.

## Winding and orientation

Every triangle is counter-clockwise seen from the side it faces: its outward normal is
cross(b − a, c − a). All closed shapes (box, cylinder, sphere, extrude, closed lathe)
are watertight and face outwards, whatever the sign of `scale`, unless `flip_normals`
is set.

## Normals

Normals are computed per part from its final triangles; parts never smooth into each
other. Around each vertex position, triangles that share an edge there and whose face
normals differ by at most `smooth_angle_deg` (+0.001° tolerance) form a smoothing group
(groups chain through such edges). A corner's normal is the average of its group's face
normals, each weighted by the triangle's angle at that corner. So with the default 30°:
a 16×8 sphere and a cylinder side with ≥ 12 segments are smooth, box edges and cylinder
caps are sharp. `smooth_angle_deg: 0` gives flat shading; 180 smooths everything except
faces that fold back onto each other. Vertices are shared within a part only when
position, normal and UV are all identical; otherwise they are split.

## UV mapping

UVs are in **meters**, computed from the part's geometry after `scale` but before
rotation and translation: 1 UV unit = 1 m, so a tiling texture repeats every meter,
stays attached to the part when it moves or turns, and scaling a part covers more
texture instead of stretching it. **v grows downwards** (like image rows), so textures
appear upright on vertical faces.

Notation: p = (x, y, z) a corner in that scaled local space; `min`, `max` the part's
bounding box in the same space; θ = atan2(x, z), the angle around +Y (0 at +Z, growing
towards +X, ±180° at −Z); ρ = √(x² + z²), the distance from the Y axis; r = |p|, the
distance from the part origin; φ = acos(y / r), the angle from +Y.

**`box`** — each triangle is projected along the axis of its normal's largest component
(ties prefer Y, then Z, then X):

| Face normal | u | v |
|-------------|---|---|
| +X | max.z − z | max.y − y |
| −X | z − min.z | max.y − y |
| +Y | x − min.x | z − min.z |
| −Y | x − min.x | max.z − z |
| +Z | x − min.x | max.y − y |
| −Z | max.x − x | max.y − y |

Every face's top-left corner, seen from outside, is at (0, 0) and u grows to the right
(top faces are seen from above with −Z up, bottom faces from below with +Z up). Example:
a box of size [2, 1, 1] has u in [0, 2] and v in [0, 1] on its front (+Z) face, (0, 0) at
its top-left corner (−1, 0.5, 0.5).

**`planar`** — the whole part is projected along the axis of its smallest bounding-box
extent (ties prefer Y, then Z, then X) with the +Y, +Z or +X row of the table above. A
`plane` therefore uses u = x − min.x, v = z − min.z.

**`cylindrical`** — triangles whose normal is mostly vertical (largest component Y, e.g.
caps) use the ±Y rows of the box table; all others use u = (θ + 180°)·ρ with θ in
radians, v = max.y − y. On a cylinder of radius R, u runs from 0 at the back (−Z) around
to 2πR, and is πR at the front (+Z).

**`spherical`** — u = (θ + 180°)·r, v = φ·r (θ, φ in radians). On a sphere of radius R,
v is 0 at the top pole and πR at the bottom pole, and u runs from 0 to 2πR.

For `cylindrical` and `spherical`, θ is unwrapped per triangle so no triangle straddles
the seam, which lies exactly at −Z; corners on the Y axis (poles, cone tips) take the
angle of their triangle's center. A texture repeating every meter only matches across
the seam when the circumference is a whole number of meters, so keep the −Z side at the
back of the model.

## Pivot

After all parts are built the compiler takes the bounding box of every vertex and moves
the whole mesh so that the pivot point lands on the origin:

| `pivot` | Pivot point |
|---------|-------------|
| `origin` | (0, 0, 0): the mesh is not moved. |
| `center` | the bounding box center. |
| `bottom-center` | (center x, min y, center z): the model stands on y = 0, centered on the Y axis. |

The applied translation (minus the pivot point) is reported as `pivot_offset`. Mirror
planes are evaluated before the pivot is applied.

## What the compiler produces

- one mesh: vertices (position, normal, UV), indices (triangles), and one index range
  per source part, in part order;
- the materials in order of first use (a part without a material uses an empty slot);
- per part: shape, index range, material, UV mode, `flip_normals`, and for mirrors the
  mirrored part;
- the bounding box after the pivot, `pivot_offset`, and the settings `inspect` uses
  (`smooth_angle_deg`, `symmetry`, `triangle_budget`).

Compilation is deterministic: the same source always produces the same bytes.

## Errors

Decoding is strict. Unknown fields (typos), wrong JSON types and duplicate keys are
errors. Validation then reports **every** problem at once, each with the file, line,
column and JSON path of the offending value. For example, `crate.model.json` containing

```json
{
  "veduta": "model/1",
  "parts": [
    { "shape": "box", "size": [1, 1, 1], "radius": 0.5 },
    { "shape": "cylinder", "radius": 0.2, "height": 1, "segments": 2 },
    { "shape": "mirror", "axis": "x", "of": 7 }
  ]
}
```

reports

```
crate.model.json:4:52: parts[0].radius: not used by shape box
crate.model.json:5:68: parts[1].segments: 2 out of range [3, 256]
crate.model.json:6:45: parts[2].of: 7 is not the index of an earlier part (want 0..1)
```

Checks: required fields present; numbers finite; sizes, radii, heights and depths > 0;
counts in range (an explicit `0` is out of range, not "default"); scale components
non-zero; enum values from the lists above; material names valid; fields that the shape
does not use absent; profiles valid as described per shape; `of` refers to an earlier
part; `name`, when present, equal to the file name.

## Limits

| Item | Range |
|------|-------|
| `segments` (cylinder, sphere, lathe) | 3–256, default 16 |
| `rings` (sphere) | 2–128, default 8 |
| profile points (extrude, lathe) | extrude 3–1024, lathe 2–1024 |
| `smooth_angle_deg` | 0–180, default 30 |
| `triangle_budget` | 1–1000000, default 20000 |

## Full example

The crate of the specification, with a material on the box only:

```json
{
  "veduta": "model/1",
  "name": "crate",
  "units": "m",
  "pivot": "bottom-center",
  "smooth_angle_deg": 30,
  "parts": [
    { "shape": "box", "size": [1, 1, 1], "position": [0, 0.5, 0], "material": "crate_wood" },
    { "shape": "cylinder", "radius": 0.1, "height": 1.2, "segments": 12, "position": [0.6, 0.6, 0] },
    { "shape": "sphere", "radius": 0.3, "segments": 16, "rings": 8 },
    { "shape": "plane", "size": [10, 10] },
    { "shape": "extrude", "profile": [[0,0],[1,0],[1,2],[0,2]], "depth": 0.5 },
    { "shape": "lathe", "profile": [[0,0],[0.5,0],[0.4,1],[0,1.2]], "segments": 16 },
    { "shape": "mirror", "axis": "x", "of": 1 }
  ]
}
```

A symmetric figure built from one side and mirrored:

```json
{
  "veduta": "model/1",
  "name": "robot",
  "pivot": "bottom-center",
  "symmetry": "x",
  "parts": [
    { "shape": "box", "size": [0.5, 0.6, 0.4], "position": [0, 0.3, 0], "material": "body" },
    { "shape": "cylinder", "radius": 0.07, "height": 0.7, "segments": 12,
      "position": [0.45, 0.55, 0], "rotation_deg": [0, 0, -50], "material": "arm" },
    { "shape": "mirror", "axis": "x", "of": 1 },
    { "shape": "sphere", "radius": 0.12, "segments": 12, "rings": 6,
      "position": [0.72, 0.8, 0.1], "material": "hand" },
    { "shape": "mirror", "axis": "x", "of": 3 },
    { "shape": "lathe", "profile": [[0, 0.6], [0.18, 0.6], [0.16, 0.8], [0, 0.85]], "segments": 16,
      "material": "head" }
  ]
}
```
