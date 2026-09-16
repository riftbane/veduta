-- The script package's test game: a hero the D-pad moves, coins it collects, a reset on
-- Select, and every part of the API a test reaches.
local movement = require("lib.movement")

local score = 0

function game.init()
  invariant("score_non_negative", function() return score >= 0 end)
  trace("started", {coins = #scene.tagged("coin"), scene = scene.name()})
end

function game.update()
  if input.pressed("select") then
    score = 0
    scene.load("main")
    trace("reset", {})
    return
  end
  if input.pressed("b") then
    local c = scene.spawn{kind = "coin", model = "quad", material = "coin",
      position = {-3, -3, 0}, scale = {0.5, 0.5, 1}, tags = {"coin"}, state = {bonus = true}}
    trace("spawned", {name = c.name, bonus = c.state.bonus})
  end
end

function game.draw()
  hud.rect(0, 0, engine.width, 12, "#00000080")
  hud.text(4, 2, string.format("SCORE %d  TICK %d", score, engine.tick), "#ffffff")
end

kinds.hero = {
  init = function(e)
    e.state.steps = 0
  end,
  update = function(e)
    if movement.step(e, engine.dt) then
      e.state.steps = e.state.steps + 1
    end
    if input.pressed("a") then
      trace("jump", {x = e.x, y = e.y})
    end
    for _, coin in ipairs(e:overlapping("coin")) do
      score = score + 1
      trace("coin_collected", {coin = coin.name, score = score})
      coin:despawn()
    end
    e.state.score = score
  end,
}

kinds.coin = {
  init = function(e)
    e.state.value = 1
    e.state.spin = 0
  end,
  update = function(e)
    e.state.spin = (e.state.spin + 90 * engine.dt) % 360
    e:set_rotation(0, 0, e.state.spin)
  end,
}
