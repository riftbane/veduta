# Entities

Everything in a game is an **entity**: the hero, a wall, a coin, an invisible trigger, the
level builder. An entity has a position, rotation and scale, an optional model and material
(what it looks like), tags, a kind (what it does) and a state (its own values).

Entities come from the [scene file](Scenes) when a scene loads, or from `scene.spawn`
while the game runs. A Lua value of an entity is always the same value, so `a == b` tells
whether two variables hold the same entity.

## Fields

| Field | Access | Meaning |
|-------|--------|---------|
| `id` | read | a number, 1, 2, 3… in load and spawn order |
| `name` | read | unique in the scene |
| `kind` | read | the name of its behaviour |
| `alive` | read | `false` once despawned |
| `x`, `y`, `z` | read/write | position, relative to the parent if it has one |
| `visible` | read/write | drawn or not; an invisible entity still updates and collides |
| `model`, `material` | read/write | asset names, or `nil` |
| `layer` | read/write | the first key of the draw order (see [2D Games](2D-Games#depth-and-layers)) |
| `state` | read/write | a table of your own values |

Setting any other field is an error. Switching `material` every few ticks is how a sprite
is animated:

```lua
kinds.torch = {
  update = function(e)
    if engine.tick % 4 == 0 then
      e.material = (e.material == "torch_1") and "torch_2" or "torch_1"
    end
  end,
}
```

## Methods

| Method | Does |
|--------|------|
| `e:position()` | returns x, y, z |
| `e:set_position(x, y, z)` | |
| `e:move(dx, dy, dz)` | adds to the position |
| `e:world_position()` | x, y, z in the world, with the parents' transforms applied |
| `e:rotation()`, `e:set_rotation(x, y, z)` | Euler angles in degrees |
| `e:scale()`, `e:set_scale(x, y, z)` | |
| `e:has_tag(t)`, `e:add_tag(t)`, `e:remove_tag(t)`, `e:tags()` | tags |
| `e:overlapping([tag])` | the live entities whose bounds overlap this one's, in id order; only those with `tag` when given |
| `e:bounds()` | min x, y, z, max x, y, z in the world, or `nil` for an entity with no model and no hitbox |
| `e:despawn()` | removes the entity at the end of the tick |

Rotations are Euler angles in degrees, applied roll about Z first, then pitch about X, then
yaw about Y (R = Ry · Rx · Rz), as in scene files.

## State

`e.state` holds the values that belong to one entity: its health, its direction, a timer.
Entities of a Lua kind start with an empty table.

```lua
kinds.enemy = {
  init = function(e)
    e.state.health = 3
    e.state.dir = 1
  end,
  update = function(e)
    for _, shot in ipairs(e:overlapping("shot")) do
      shot:despawn()
      e.state.health = e.state.health - 1
    end
    if e.state.health <= 0 then
      trace("enemy_down", {name = e.name})
      e:despawn()
    end
  end,
}
```

The trace records the state with every tick as `state.<key>`, so a [scenario](Testing) can
check it: `{"entity": "boss", "path": "state.health", "op": "==", "value": 0}`. Keep it to
numbers, strings, booleans and tables of them: other values (functions, entities) are
recorded as a description only.

## Spawning

```lua
local coin = scene.spawn{
  kind = "coin",
  name = "coin_7",
  model = "quad",
  material = "coin",
  position = {3, -2, 1},
  rotation = {0, 0, 45},
  scale = {0.5, 0.5, 1},
  tags = {"coin", "pickup"},
  visible = true,
  layer = 0,
  state = {value = 5},
}
```

Every field is optional. `kind` defaults to `static` (no behaviour). Without a `name` the
entity is named after its kind and id (`coin_12`); a name already taken gets `#id` added.
`state` is merged into the state the entity starts with. The new entity's kind `init` runs
at once; its `update` starts on the next tick.

Spawning is allowed from `game.init`, `game.update` and any kind's `init` or `update`,
including while a scene loads, which is how [Your First Game](Your-First-Game) builds its
level from a text map.

Entities spawned from Lua cannot yet have a parent or a hitbox: put entities that need them
in the scene file (see [Scenes](Scenes#entities)).

## Finding entities

| Function | Returns |
|----------|---------|
| `scene.find(name)` | the entity, or `nil` |
| `scene.tagged(tag)` | a list of the live entities with that tag, in id order |
| `scene.entities()` | every live entity, in id order |

Lists are ordinary Lua tables, safe to keep for the tick. Across ticks, check `e.alive`
before using an entity you kept: a despawned entity stays a valid value, but it is no longer
in the scene.

## Collisions

`e:overlapping(tag)` compares axis-aligned bounding boxes: the model's bounds, or the
entity's hitbox when the scene file gives one. It uses the bounds computed at the end of
the **previous** tick, so an entity moved this tick is seen where it was.

- Boxes that only touch do not overlap.
- A sprite made from the `quad` model is only 0.02 units deep: two sprites overlap only when
  their z are close. Keep actors that must touch at the same z, or give them hitboxes with
  depth. See [2D Games](2D-Games#collisions).
- For walls and floors, testing a proposed position against a map before moving, as
  [Your First Game](Your-First-Game#5-the-hero) does, is exact and cheaper than moving and
  undoing.

The engine also records a `collision` event in the trace when two entities start
overlapping (two `static` entities never do), which scenarios can count.
