-- A module: required as "lib.movement".
local M = {}

M.SPEED = 4

function M.step(e, dt)
  local dx, dy = input.dpad()
  if dx ~= 0 and dy ~= 0 then
    dx, dy = dx * 0.7071, dy * 0.7071
  end
  e:move(dx * M.SPEED * dt, dy * M.SPEED * dt, 0)
  return dx ~= 0 or dy ~= 0
end

return M
