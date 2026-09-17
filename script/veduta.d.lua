---@meta veduta
---@diagnostic disable: missing-fields
-- The Veduta Lua API, for editors (Lua Language Server): completion, hover text and type
-- checks. It describes; the engine runs. The `lua` docs topic is the reference.
-- API level 1.

---@alias veduta.Button "up"|"down"|"left"|"right"|"a"|"b"|"select"|"cancel"

---@alias veduta.Color string|integer A color: "#rrggbb", "#rrggbbaa" or 0xrrggbb.

---An entity of the scene. The same entity is always the same value, so `a == b` compares
---entities. Setting a field not listed here is an error: keep your own values in `state`.
---@class veduta.Entity
---@field id integer (read only)
---@field name string (read only)
---@field kind string (read only)
---@field alive boolean false once despawned (read only)
---@field x number position, relative to the parent
---@field y number
---@field z number
---@field visible boolean
---@field model string? a model asset's name
---@field material string? a material asset's name
---@field layer integer the first key of the draw order
---@field parent veduta.Entity? the parent; set it to an entity, an entity's name or nil
---@field hitbox number[][]? {{min x, y, z}, {max x, y, z}} in the entity's own space, or nil
---@field state table your own values; the trace records them as state.<key>
local Entity = {}

---The position, relative to the parent.
---@return number x, number y, number z
function Entity:position() end

---@param x number
---@param y number
---@param z number
function Entity:set_position(x, y, z) end

---Adds to the position.
---@param dx number
---@param dy number
---@param dz number
function Entity:move(dx, dy, dz) end

---The position in the world.
---@return number x, number y, number z
function Entity:world_position() end

---Euler angles in degrees.
---@return number x, number y, number z
function Entity:rotation() end

---@param x number degrees
---@param y number degrees
---@param z number degrees
function Entity:set_rotation(x, y, z) end

---@return number x, number y, number z
function Entity:scale() end

---@param x number
---@param y number
---@param z number
function Entity:set_scale(x, y, z) end

---@param tag string
---@return boolean
function Entity:has_tag(tag) end

---@param tag string
function Entity:add_tag(tag) end

---@param tag string
function Entity:remove_tag(tag) end

---@return string[]
function Entity:tags() end

---The live entities whose bounds overlap this one's (last tick's), in id order.
---@param tag? string only those with this tag
---@return veduta.Entity[]
function Entity:overlapping(tag) end

---The bounds in the world, or nil for an entity without bounds.
---@return number? min_x, number min_y, number min_z, number max_x, number max_y, number max_z
function Entity:bounds() end

---Removed at the end of the tick.
function Entity:despawn() end

---The live entities whose parent is this one, in id order.
---@return veduta.Entity[]
function Entity:children() end

---A kind: the behaviour of the entities whose `kind` names it.
---@class veduta.Kind
---@field init? fun(e: veduta.Entity) when the entity is loaded or spawned
---@field update? fun(e: veduta.Entity) once per tick, in entity id order, after game.update

---The game's callbacks; all are optional.
---@class veduta.Game
---@field init? fun() once, after the first scene is loaded
---@field update? fun() once per tick, before the entities
---@field draw? fun() once per rendered frame, after the scene: the hud

---@type veduta.Game
game = {}

---Kinds by name: `kinds.coin = { init = ..., update = ... }`.
---@type table<string, veduta.Kind>
kinds = {}

---@class veduta.Engine
---@field tick integer the tick (0 in game.init)
---@field dt number seconds per tick
---@field width integer the frame's width
---@field height integer the frame's height
---@field headless boolean true when no player shows the game
---@field name string the project's name
---@field title string the project's title
---@field api integer the runtime's Lua API level

---@type veduta.Engine
engine = {}

---The console's eight buttons. Home leaves the game and never reaches it.
input = {}

---@param button veduta.Button
---@return boolean held
function input.down(button) end

---@param button veduta.Button
---@return boolean went_down_this_tick
function input.pressed(button) end

---@param button veduta.Button
---@return boolean went_up_this_tick
function input.released(button) end

---The D-pad: x is -1 left, 0 or 1 right; y is -1 down, 0 or 1 up.
---@return integer x, integer y
function input.dpad() end

---The scene. It does not exist while the main script runs: use it from game.init on.
scene = {}

---@return string
function scene.name() end

---@param name string
---@return veduta.Entity?
function scene.find(name) end

---@param tag string
---@return veduta.Entity[]
function scene.tagged(tag) end

---Every live entity, in id order.
---@return veduta.Entity[]
function scene.entities() end

---@class veduta.Spawn
---@field kind? string default "static"
---@field name? string
---@field model? string
---@field material? string
---@field position? number[] {x, y, z}
---@field rotation? number[] {x, y, z} in degrees
---@field scale? number[] {x, y, z}
---@field tags? string[]
---@field visible? boolean
---@field layer? integer
---@field parent? veduta.Entity|string an entity or an entity's name
---@field hitbox? number[][] {{min x, y, z}, {max x, y, z}}
---@field state? table merged into the kind's

---Adds an entity.
---@param spec veduta.Spawn
---@return veduta.Entity
function scene.spawn(spec) end

---Adds the entities of a prefab, the min corner of its footprint at (x, y, z), turned by
---rotation (0, 90, 180 or 270) about +Y, named "<prefix>_<entity>". The table lists them in
---prefab order and holds each under its name in the prefab.
---@param name string
---@param x number
---@param y number
---@param z number
---@param rotation? integer degrees: 0, 90, 180 or 270
---@param prefix? string default: the prefab's name
---@return table<integer|string, veduta.Entity>
function scene.spawn_prefab(name, x, y, z, rotation, prefix) end

---Replaces the scene, as a reset.
---@param name string
function scene.load(name) end

---@class veduta.Camera
---@field position number[] {x, y, z}
---@field target number[] {x, y, z}
---@field ortho boolean
---@field fov number degrees
---@field size number visible height of an orthographic camera
---@field near number
---@field far number

camera = {}

---@return veduta.Camera
function camera.get() end

---Changes the fields given; the others stay.
---@param fields veduta.Camera|table
function camera.set(fields) end

---The orthographic camera of a 2D game, looking at (x, y) and showing `height` units.
---@param x number
---@param y number
---@param height number
function camera.follow2d(x, y, height) end

world = {}

---Replaces the scene with a world streamed around a cell.
---@param name string
---@param cx integer
---@param cz integer
function world.load(name, cx, cz) end

---The loaded world, or nil.
---@return string?
function world.name() end

---Where the chunks follow; call it every tick.
---@param x number
---@param y number
---@param z number
function world.focus(x, y, z) end

---The ground's height.
---@param x number
---@param z number
---@return number
function world.height(x, z) end

---The water level there, or nil.
---@param x number
---@param z number
---@return number?
function world.water(x, z) end

---Drawing over the frame, only inside game.draw. Coordinates are pixels from the top left.
hud = {}

---The built-in 8×8 font.
---@param x number
---@param y number
---@param text any written as tostring would
---@param color? veduta.Color default white
---@param scale? integer
---@return integer width drawn
function hud.text(x, y, text, color, scale) end

---A filled rectangle.
---@param x number
---@param y number
---@param w number
---@param h number
---@param color? veduta.Color default white
function hud.rect(x, y, w, h, color) end

---@class veduta.ImageOptions
---@field src? integer[] {x, y, w, h}: the part of the texture, in texels (default: all of it)
---@field w? number width drawn, in pixels (default: the part's width)
---@field h? number height drawn, in pixels (default: the part's height)
---@field color? veduta.Color multiplies the texels (default white: unchanged)
---@field flip_x? boolean mirrored left to right
---@field flip_y? boolean mirrored top to bottom

---A texture, or a part of it (an icon of a sheet), at (x, y); texels stay sharp.
---@param texture string a texture asset's name
---@param x number
---@param y number
---@param options? veduta.ImageOptions
function hud.image(texture, x, y, options) end

---@class veduta.PanelOptions
---@field src? integer[] {x, y, w, h}: the part of the texture, in texels
---@field color? veduta.Color

---A panel that stretches to any size (nine-slice): the corners keep their size, the edges
---and the middle stretch.
---@param texture string
---@param x number
---@param y number
---@param w number
---@param h number
---@param border integer|integer[] the corners' width in texels, or {left, top, right, bottom}
---@param options? veduta.PanelOptions
function hud.panel(texture, x, y, w, h, border, options) end

---A texture's width and height in texels.
---@param texture string
---@return integer w, integer h
function hud.image_size(texture) end

---A mesh being built; mesh.set makes it a model.
---@class veduta.Mesh
local Mesh = {}

---What follows is drawn with this material; without one, with the entity's.
---@param material? string
function Mesh:part(material) end

---A quad, its corners counter-clockwise seen from the side that shows.
function Mesh:quad(x1, y1, z1, x2, y2, z2, x3, y3, z3, x4, y4, z4) end

---A triangle, its corners counter-clockwise seen from the side that shows.
function Mesh:triangle(x1, y1, z1, x2, y2, z2, x3, y3, z3) end

---The box from (x, y, z), w × h × d.
---@param faces? string the faces to add, run together: "+x-x+y-y+z-z" (default all)
function Mesh:box(x, y, z, w, h, d, faces) end

---@return integer
function Mesh:triangles() end

---A grid of blocks: ids from 0 (empty) to 255.
---@class veduta.Volume
local Volume = {}

---@return integer id
function Volume:get(x, y, z) end

---@param id integer 0 (empty) to 255
function Volume:set(x, y, z, id) end

---Every block of the box between two cells.
function Volume:fill(x1, y1, z1, x2, y2, z2, id) end

---@return integer x, integer y, integer z
function Volume:size() end

---Models built while the game runs.
mesh = {}

---@return veduta.Mesh
function mesh.new() end

---Makes the mesh the model name (it contains ':', as "game:chunk"), a copy.
---@param name string
---@param m veduta.Mesh
function mesh.set(name, m) end

---@param name string
function mesh.remove(name) end

---The faces between a block and an empty cell or the edge, one part per block id.
---@param v veduta.Volume
---@param materials table<integer, string> block id → material name
---@param size? number a block's size (default 1)
---@return veduta.Mesh
function mesh.voxels(v, materials, size) end

volume = {}

---@param x integer
---@param y integer
---@param z integer
---@return veduta.Volume
function volume.new(x, y, z) end

---Adds an event to the tick's trace; scenarios count them.
---@param name string
---@param fields? table<string, any>
function trace(name, fields) end

---A check scenarios and veduta.json can list by name; the predicate returns true while it
---holds.
---@param name string
---@param predicate fun(): boolean
function invariant(name, predicate) end

---Runs module.lua (dots are directories, relative to the main script) once and returns
---what it returned.
---@param module string
---@return any
function require(module) end
