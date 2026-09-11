# Scene — `assets/scenes/<name>.scene.json`

A scene is the starting state of a level: one camera, one directional light, a background
color and an ordered list of entities. Each entity has a transform, an optional model and
material, tags, an optional parent, and a `kind` that selects its Go behaviour.

## File and name

- Location: `assets/scenes/<name>.scene.json`. The scene's name is the file name without
  `.scene.json` (`main.scene.json` → `main`); it is what `render --scene`, scenarios
  (`"scene": "main"`) and `default_scene` in `veduta.json` refer to.
- Names (scene, entity, kind, model, material, tag) are 1–64 characters of `a-z`, `0-9`,
  `_` and `-`, starting with a letter or a digit.
- The file is one JSON object. Decoding is strict: unknown fields, duplicate keys, wrong
  types and trailing data are errors. Every error carries `file:line:col` and the JSON path
  (for example `entities[2].scale[1]`), and all problems in a file are reported together.

## Units and conventions

- Distances are meters. Coordinates are right-handed with +Y up and −Z forward (a camera
  at `[0, 5, 10]` looking at the origin looks toward −Z).
- Vectors are JSON arrays of exactly 3 finite numbers `[x, y, z]`.
- Angles are degrees. `rotation_deg: [x, y, z]` is applied as R = Ry · Rx · Rz: first
  roll about Z, then pitch about X, then yaw about Y. Positive angles rotate
  counter-clockwise when looking down the axis toward the origin.
- Colors are `"#RRGGBB"` or `"#RRGGBBAA"` (hex, either case).

## Top-level fields

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `veduta` | string | required | Must be exactly `"scene/1"`. |
| `camera` | object | required | The view the scene is rendered from. See Camera. |
| `light` | object | see Light | The directional light and ambient term. May be omitted entirely. |
| `background` | color | `"#202830"` | Clear color behind everything. An alpha byte is kept (it becomes the alpha of empty pixels). |
| `entities` | array of objects | `[]` | The entities, in order. Entity ids are assigned 1, 2, 3, … in this order when the scene is loaded; entities spawned later get the following ids. |

## Camera

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `type` | string | `"perspective"` | `perspective` or `orthographic`. |
| `fov_deg` | number | `60` | Vertical field of view in degrees, strictly between 1 and 179. Perspective only: an error on an orthographic camera. The horizontal extent follows from the output aspect ratio. |
| `size` | number | none | Visible height in meters (> 0). Required for `orthographic`, an error on `perspective`. The visible width is size × aspect ratio. |
| `near` | number | `0.1` | Near clip distance in meters, > 0. |
| `far` | number | `200` | Far clip distance in meters, > `near`. |
| `position` | vector | required | Eye position. |
| `look_at` | vector | required | Point the camera looks at; must differ from `position`. |

The camera's up direction is +Y. When the camera looks straight up or down (the view
direction is within about 0.8° of the Y axis), up becomes −Z instead, so the top of the
image points toward −Z (north on a top-down view).

## Light

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `direction` | vector | `[-0.4, -1, -0.3]` | Direction the light travels (from the light toward the scene), world space. Any non-zero length; the renderer normalizes it. `[0, -1, 0]` is light straight from above. |
| `color` | color | `"#ffffff"` | Light color, `#RRGGBB` only (no alpha). |
| `ambient` | color | `"#404040"` | Ambient color added to every lit surface, `#RRGGBB` only. |

Shading is Lambert, evaluated per vertex: light = ambient + color × max(0, n · −d̂) where n
is the vertex normal and d̂ the normalized direction; the surface color is
albedo × light × texel, each channel clamped to [0, 1]. Materials with `"unlit": true`
ignore the light (color = albedo × texel). When `light` is omitted
all three defaults apply; inside `light`, each field is optional.

## Entities

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `name` | string | required | Unique within the scene. Scenarios, traces and `parent` refer to entities by name. |
| `kind` | string | required | Behaviour selector. Built-in kinds: `static` (no behaviour: scenery), `camera`, `light`. Any other valid name must be registered by the game in Go with `veduta.RegisterKind`; the scene compiler accepts it, and an unregistered kind is reported when the game loads the scene. |
| `model` | string | none | Name of a model asset (`assets/models/<name>.model.json`). Without a model the entity is invisible in renders but still exists (triggers, spawn points, logic). |
| `material` | string | none | Name of a material asset (`assets/materials/<name>.mat.json`). It is used by the model parts that do not name a material of their own; parts with their own material keep it. Parts left without any material use the default material (white, opaque, lit). |
| `position` | vector | `[0, 0, 0]` | Translation in meters. |
| `rotation_deg` | vector | `[0, 0, 0]` | Rotation in degrees (order above). Any finite value. |
| `scale` | vector | `[1, 1, 1]` | Scale per axis. Every component must be non-zero; a negative component mirrors along that axis. |
| `tags` | array of strings | `[]` | Labels used by game code, invariants (`no_overlap:gem,wall`) and inspection (entities tagged `important` must be on screen). Valid names, no duplicates within an entity. |
| `parent` | string | none | Name of another entity of this scene. The entity's `position`, `rotation_deg` and `scale` are then relative to the parent (world = parent world × local, applied scale, then rotation, then translation). The parent may appear before or after the child in the list. An entity cannot be its own parent and parent chains must not form cycles. |
| `visible` | boolean | `true` | `false` keeps the entity in the simulation but does not draw it. |

`model` and `material` are references by name: the scene compiles even if the assets do
not exist yet; `inspect scene` reports `SCENE_MISSING_ASSET` for missing ones.

## Compiled form

The compiler produces `asset.Scene`: `Name`, `Camera` (`Ortho`, `FovDeg` — 0 for
orthographic, `Size` — 0 for perspective, `Near`, `Far`, `Position`, `LookAt`), `Light`
(`Dir` as written; `Color` and `Ambient` as linear RGB in [0, 1], each channel = byte / 255),
`Background` (packed `0xAARRGGBB`) and `Entities` in file order with every default filled
in (`Parent` is the parent's name, `Tags` nil when empty). The binary layout in `.vda`
files is in `docs/vda.md` (chunk `SCEN`).

## Errors (examples)

```
main.scene.json:4:31: camera.look_at: must differ from camera.position {0 5 10}
main.scene.json:9:62: entities[1].scale[1]: must be non-zero
main.scene.json:12:45: entities[3].name: duplicate entity name "gem" (first used by entities[2])
main.scene.json:14:20: entities[4].parent: parent cycle: arm -> hand -> arm
```

## Full example

```json
{
  "veduta": "scene/1",
  "camera": { "type": "perspective", "fov_deg": 60, "near": 0.1, "far": 200,
              "position": [0, 5, 10], "look_at": [0, 0, 0] },
  "light":  { "direction": [-0.4, -1, -0.3], "color": "#ffffff", "ambient": "#404040" },
  "background": "#202830",
  "entities": [
    { "name": "ground", "kind": "static", "model": "ground", "material": "grass" },
    { "name": "player", "kind": "player", "model": "hero", "material": "hero",
      "position": [0, 0, 0], "rotation_deg": [0, 180, 0], "tags": ["player", "important"] },
    { "name": "hat", "kind": "static", "model": "hat", "parent": "player",
      "position": [0, 1.8, 0], "scale": [1.1, 1, 1.1] },
    { "name": "gem_1", "kind": "collectible", "model": "gem", "material": "gem",
      "position": [3, 0.5, -2], "tags": ["gem"] },
    { "name": "spawn_point", "kind": "static", "position": [0, 0, 8], "visible": false }
  ]
}
```

Orthographic top-down camera (only the camera shown; `entities` may be empty):

```json
{
  "veduta": "scene/1",
  "camera": { "type": "orthographic", "size": 20, "position": [0, 30, 0], "look_at": [0, 0, 0] },
  "entities": []
}
```
