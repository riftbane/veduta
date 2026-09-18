# Prefab — `assets/prefabs/<name>.vprefab`

A prefab is a group of entities placed as one structure in a world (`world` topic): a
tree, a house, a whole village. A world places prefabs, never single entities, so a city
is one placement with one footprint and the generator keeps footprints apart. Since v1.2.0.

## File and name

- Location: `assets/prefabs/<name>.vprefab`. The prefab's name is the file name
  without `.vprefab`; worlds refer to it by that name. A folder under
  `assets/prefabs/` works too; names must be unique across the folders.
- Names are 1–64 characters of `a-z`, `0-9`, `_` and `-`, starting with a letter or a
  digit. Decoding is strict: unknown fields, duplicate keys, wrong types and trailing data
  are errors located by `file:line:col` and JSON path; all problems are reported together.

## Fields

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `veduta` | string | required | Must be exactly `"prefab/1"`. |
| `footprint` | `[width, depth]` | required | The ground the prefab occupies, in meters along +X (width) and +Z (depth), each in (0, 1024]. The world rounds it up to whole cells. |
| `tags` | array of strings | `[]` | Tags of the structure as a whole, matched by other prefabs' `min_distance` rules. Valid names, no duplicates. |
| `rules` | object | none | Placement rules; see below. |
| `entities` | array of objects | `[]` | The entities, exactly as in a scene (`scene` topic: `name`, `kind`, `model`, `material`, `position`, `rotation_deg`, `scale`, `tags`, `parent`, `visible`, `hitbox`, `layer`), except that `kind` cannot be `camera` or `light`. Names are unique within the prefab; `parent` names another entity of the prefab. |

### Rules

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `biomes` | array of strings | any | Biomes of the world the prefab may stand in. |
| `min_distance` | object `{ tag: meters }` | `{}` | Free ground the prefab needs between its footprint and any structure carrying `tag` (a site tag or a prefab tag), in meters, 0 to 4096, rounded up to whole cells. The rule is checked both ways: a house asking for 6 m from `city` is also kept 6 m from cities placed later. |

## Coordinates

The prefab's origin is the min corner of its footprint on the ground: the footprint spans
x in `[0, width]`, z in `[0, depth]`, y = 0 is the ground. Entity positions are meters from
that corner, so a 3×2 house has its walls around `[1.5, 0, 1]`. When the world places the
prefab at cell `[cx, cz]` with rotation `r` (0, 90, 180 or 270 degrees about +Y), the
entities are turned by `r` about the centre of the footprint, which for 90 and 270 becomes
`[depth, width]`, and then moved to the cell; the origin is the min corner of the rotated
footprint.

## Inspection

`inspect prefab <name>` reports:

- `PREFAB_MISSING_ASSET` (error): a model or material of an entity does not exist.
- `PREFAB_FOOTPRINT_SMALL` (warning): an entity's bounds reach outside the footprint on the
  ground (x or z), so it will overlap the neighbours the generator keeps clear.
- `PREFAB_OVERLAP` (warning): two entities of the prefab overlap.
- Unknown entity kinds are reported as information: the tool has no game, and a kind is
  checked when the game loads the world.

Sheets: `summary` (iso and top views).

## Example

```json
{
  "veduta": "prefab/1",
  "footprint": [3, 2],
  "tags": ["house", "building"],
  "rules": { "biomes": ["plain", "forest"], "min_distance": { "house": 1, "city": 6 } },
  "entities": [
    { "name": "walls", "kind": "static", "model": "house", "material": "wood", "position": [1.5, 0, 1] },
    { "name": "roof", "kind": "static", "model": "roof", "material": "tile", "parent": "walls", "position": [0, 2, 0] },
    { "name": "door", "kind": "static", "position": [1.5, 0, 2], "hitbox": [[-0.5, 0, -0.1], [0.5, 2, 0.1]], "tags": ["door"] }
  ]
}
```

A one-cell prefab for `scatter` (a tree on a 1 m cell):

```json
{
  "veduta": "prefab/1",
  "footprint": [1, 1],
  "tags": ["tree"],
  "rules": { "biomes": ["forest"] },
  "entities": [ { "name": "trunk", "kind": "static", "model": "tree", "material": "bark", "position": [0.5, 0, 0.5], "tags": ["obstacle"] } ]
}
```
