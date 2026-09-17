# 2D Games

A 2D game in Veduta is a 3D scene seen straight on: an orthographic camera looks down −Z,
the game is played in the XY plane (+X right, +Y up), and sprites are thin quads facing the
camera. Everything else (entities, kinds, collisions, tests) works as in any game.

## The recipe

1. **Camera**: orthographic, looking down −Z. In the scene file:

   ```json
   {
     "veduta": "scene/1",
     "camera": { "type": "orthographic", "size": 12, "position": [0, 0, 100], "look_at": [0, 0, 0] },
     "background": "#101018",
     "entities": []
   }
   ```

   and from a script, `camera.follow2d(x, y, 12)`. See [Camera](Camera).

2. **Sprites**: the `quad` model every project has, scaled with the entity's `scale`
   (`{2, 1, 1}` is 2 units wide), and one material per image:

   ```json
   { "veduta": "material/1", "texture": "coin", "unlit": true, "alpha": "cutout", "filter": "nearest" }
   ```

3. **Planes**: give every plane of the game its own z. For example the background at 0,
   tiles and actors at 1, a foreground at 2.

4. **Hitboxes** for everything that collides, in the scene file (see
   [Collisions](#collisions)).

## Units and pixels

The panel is 320 × 240. With a camera `size` of 12, one unit is 20 pixels, so a 16 × 16
texture on a quad of scale 0.8 is drawn at exactly 16 × 16 pixels. Choose sizes where
units are whole numbers of pixels (size 12 → 20 px, 15 → 16 px, 24 → 10 px) and sprite
scales that match their textures, and pixel art stays crisp.

## Depth and layers

What covers what is decided by z, then by `layer`:

- Opaque and cutout sprites write depth: among them, the one with the larger z (nearer the
  camera) covers the others, whatever their order or layer.
- Two opaque sprites at the **same** z are not ordered by anything you should rely on: put
  the one that must win at a larger z.
- `layer` (−1000 to 1000, default 0) is the first key of the draw order: lower layers are
  drawn first.
- A translucent sprite (`"alpha": "blend"`: water, fog, glass) writes no depth. It covers
  another sprite only when it is nearer **and** on a layer at least as high. Give
  translucent sprites a larger z than what they cover and a layer at least as high.

## Animation

One material per frame, switched on a timer:

```lua
local WALK = {"hero_walk_1", "hero_walk_2", "hero_walk_3", "hero_walk_2"}

kinds.hero = {
  update = function(e)
    local dx = input.dpad()
    if dx ~= 0 then
      e.x = e.x + dx * 4 * engine.dt
      e.material = WALK[(engine.tick // 3) % #WALK + 1]   -- a frame every 3 ticks
      e:set_scale(dx * 0.8, 0.8, 1)                        -- a negative x mirrors: face left
    else
      e.material = "hero_idle"
    end
  end,
}
```

A negative scale mirrors the sprite, so one set of frames serves both directions.

## Collisions

`e:overlapping(tag)` compares the entities' boxes. A `quad` is 1 × 1 × 0.02: flat along Z.

- Sprites at the same z overlap as expected. Sprites at different z do not, even when
  they cover each other on screen.
- A **hitbox** gives an entity a collision box of its own, in its local space, that
  replaces the model's bounds: make it deeper along Z to meet sprites on nearby planes,
  and smaller than the drawing for fair play (the transparent corners of a round coin
  should not collect it).
- An entity with a hitbox and no model is an invisible trigger.
- Boxes that only touch do not overlap.

Hitboxes are set in the scene file:

```json
{
  "veduta": "scene/1",
  "camera": { "type": "orthographic", "size": 12, "position": [0, 0, 100], "look_at": [0, 0, 0] },
  "background": "#101018",
  "entities": [
    { "name": "sky", "kind": "static", "model": "quad", "material": "sky", "scale": [16, 12, 1] },
    { "name": "ground", "kind": "static", "model": "quad", "material": "ground",
      "position": [0, -5, 1], "scale": [16, 2, 1], "hitbox": [[-0.5, -0.5, -0.5], [0.5, 0.5, 0.5]], "tags": ["solid"] },
    { "name": "hero", "kind": "hero", "model": "quad", "material": "hero",
      "position": [-5, -3.5, 1], "hitbox": [[-0.4, -0.5, -0.5], [0.4, 0.5, 0.5]], "tags": ["hero"] },
    { "name": "coin_1", "kind": "coin", "model": "quad", "material": "coin",
      "position": [3, -3.5, 1], "scale": [0.5, 0.5, 1], "hitbox": [[-0.3, -0.3, -0.5], [0.3, 0.3, 0.5]], "tags": ["coin"] },
    { "name": "exit", "kind": "static", "position": [7, -3.5, 1], "hitbox": [[-0.5, -1, -0.5], [0.5, 1, 0.5]], "tags": ["exit"] }
  ]
}
```

Entities spawned from Lua have no hitbox yet: a spawned sprite collides by its quad, so
spawn it at the same z as what it must touch.

### Platforms and gravity

A simple platformer step: gravity pulls, A jumps when standing, and the hero never ends a
tick inside the ground.

```lua
local GRAVITY, JUMP, RUN = -30, 11, 5
local FLOOR = -3.5   -- the hero's y when standing on the ground above

kinds.hero = {
  init = function(e)
    e.state.vy = 0
  end,
  update = function(e)
    local dx = input.dpad()
    e.x = e.x + dx * RUN * engine.dt

    local standing = e.y <= FLOOR
    if standing and input.pressed("a") then
      e.state.vy = JUMP
    end
    e.state.vy = e.state.vy + GRAVITY * engine.dt
    e.y = e.y + e.state.vy * engine.dt
    if e.y < FLOOR then
      e.y, e.state.vy = FLOOR, 0
    end

    for _, coin in ipairs(e:overlapping("coin")) do
      trace("coin_collected", {coin = coin.name})
      coin:despawn()
    end
    camera.follow2d(e.x, 0, 12)
  end,
}
```

For levels with many platforms, keep the platforms in a table or a text map and test the
hero's box against them before moving, as [Your First Game](Your-First-Game) does with
walls.

## The traps

Three mistakes break a 2D game without any error:

1. **A `plane` part faces +Y**, like a floor: a camera looking down −Z sees it edge-on and
   draws nothing. Use the `quad` model for sprites.
2. **Flat sprites at different z never collide.** Keep colliding sprites at the same z or
   give them hitboxes with depth.
3. **A sprite material needs `unlit`, `cutout` and `nearest`.** Lit, it comes out dark;
   opaque, its transparent pixels are a black rectangle; bilinear, pixel art blurs and
   gets dark fringes.

`veduta inspect scene NAME` warns about sprites that may z-fight (opaque sprites
overlapping at the same z).
