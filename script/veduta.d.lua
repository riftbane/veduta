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
---@field state? table merged into the kind's

---Adds an entity.
---@param spec veduta.Spawn
---@return veduta.Entity
function scene.spawn(spec) end

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
