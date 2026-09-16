-- The game: an empty scene and its name on the screen, the starting point of a new
-- project. `veduta docs lua` (the MCP docs tool, topic lua) describes the API.

function game.init()
end

function game.update()
end

function game.draw()
  local title = engine.title
  hud.text((engine.width - 8 * #title) // 2, engine.height // 2 - 4, title, "#f4f0e0")
end
