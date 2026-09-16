-- Language core: values, operators, control flow, functions, closures, tables, metatables.
-- Every check is an assert; the Go test runs the file and fails on the first error.

local function eq(a, b, what)
  if a ~= b then
    error((what or "value") .. ": got " .. tostring(a) .. ", want " .. tostring(b), 2)
  end
end

-- numbers
eq(1 + 2, 3)
eq(7 // 2, 3)
eq(-7 // 2, -4)
eq(7 % 3, 1)
eq(-7 % 3, 2)
eq(7 % -3, -2)
eq(7.5 // 2, 3.0)
eq(-7.5 % 2, 0.5)
eq(5.5 % -2, -0.5)
eq(2 ^ 10, 1024.0)
eq(10 / 4, 2.5)
eq(3 / 1, 3.0)
eq(1e2, 100.0)
eq(0x10, 16)
eq(0xff, 255)
eq(0x1p4, 16.0)
eq(0xA.8p0, 10.5)
eq(9223372036854775807 + 1, -9223372036854775808)
eq((-9223372036854775807 - 1) // -1, -9223372036854775807 - 1)
eq(0xffffffffffffffff, -1)
eq(1 == 1.0, true)
eq(1 < 1.5, true)
eq(2^53 == 2^53 + 1, true)
eq(9007199254740993 < 9007199254740992.0, false)
eq(9007199254740993 > 9007199254740992.0, true)
eq(-9223372036854775808 < -9.3e18, false)
eq(tostring(1e15), "1e+15")
eq(tostring(2^63), "9.2233720368548e+18")
eq(tostring(3.0), "3.0")
eq(tostring(-0.0), "-0.0")
eq(tostring(1/0), "inf")
eq(tostring(-1/0), "-inf")
eq(tostring(0/0), "nan")
eq(tostring(0.1), "0.1")
eq(tostring(100), "100")
eq(tonumber("0x10"), 16)
eq(tonumber("  12  "), 12)
eq(tonumber("1e1"), 10.0)
eq(tonumber("1e"), nil)
eq(tonumber("z", 36), 35)
eq(tonumber("ff", 16), 255)
eq(tonumber("8", 8), nil)
eq(tonumber(""), nil)
eq(tonumber("0x"), nil)
eq("10" + 1, 11)
eq("3.5" * 2, 7.0)
eq(10 .. "", "10")
eq(1.5 .. "|", "1.5|")

-- bitwise
eq(5 & 3, 1)
eq(5 | 3, 7)
eq(5 ~ 3, 6)
eq(~0, -1)
eq(1 << 63, -9223372036854775808)
eq(1 << 64, 0)
eq(-1 >> 1, 9223372036854775807)
eq(2.0 | 1, 3)
eq("3" & 1, 1)
eq(1 << -1, 0)
eq(4 >> -1, 8)

-- strings and comparisons
eq("a" < "b", true)
eq("abc" < "abd", true)
eq("Z" < "a", true)
eq("" < "a", true)
eq(#"hello", 5)
eq("a\tb\\n", "a\9b\\n")
eq("\65\066\x43\u{44}", "ABCD")
eq("x\z
      y", "xy")
eq([[
line]], "line")
eq([==[a]]b]==], "a]]b")

-- logic
eq(nil or 1, 1)
eq(false and error("no"), false)
eq(1 and 2, 2)
eq(not nil, true)
eq(not 0, false)

-- control flow
local sum = 0
for i = 1, 10 do sum = sum + i end
eq(sum, 55)
sum = 0
for i = 10, 1, -2 do sum = sum + i end
eq(sum, 30)
sum = 0
for i = 1, 3 do for j = 1, 3 do if j == 2 then break end sum = sum + 1 end end
eq(sum, 3)
local n = 0
for x = 1.0, 2.0, 0.25 do n = n + 1 end
eq(n, 5)
n = 0
for i = 9223372036854775806, 9223372036854775807 do n = n + 1 end
eq(n, 2)
n = 0
for i = 1, 0 do n = n + 1 end
eq(n, 0)
n = 0
for i = 1, 3.5 do n = n + 1 end
eq(n, 3)
local i = 0
while true do i = i + 1 if i == 5 then break end end
eq(i, 5)
repeat local k = i; i = i - 1 until k == 1
eq(i, 0)
local grade
local score = 75
if score > 90 then grade = "A" elseif score > 70 then grade = "B" else grade = "C" end
eq(grade, "B")

-- functions and varargs
local function va(...) return select("#", ...), ... end
eq(va(), 0)
eq(select("#", va(1, nil, 3)), 4)
eq(select(2, "a", "b", "c"), "b")
eq(select(-1, "a", "b", "c"), "c")
local function three() return 1, 2, 3 end
local t = {three()}
eq(#t, 3)
t = {three(), three()}
eq(#t, 4)
t = {(three())}
eq(#t, 1)
local a, b, c, d = three()
eq(c, 3); eq(d, nil)
local function pack(...) return {n = select("#", ...), ...} end
local p = pack(nil, nil)
eq(p.n, 2)

-- closures
local function counter()
  local c = 0
  return function() c = c + 1; return c end
end
local c1, c2 = counter(), counter()
c1(); c1()
eq(c1(), 3)
eq(c2(), 1)
local fns = {}
for i = 1, 3 do fns[i] = function() return i end end
eq(fns[1]() + fns[2]() + fns[3](), 6)
local shared = {}
do
  local x = 0
  shared.inc = function() x = x + 1 end
  shared.get = function() return x end
end
shared.inc(); shared.inc()
eq(shared.get(), 2)
local function fib(n) if n < 2 then return n end return fib(n - 1) + fib(n - 2) end
eq(fib(20), 6765)
local function outer()
  local v = 1
  local function mid()
    local function inner() v = v + 10; return v end
    return inner
  end
  return mid()
end
eq(outer()(), 11)

-- tables
t = {10, 20, 30, x = 1, ["y z"] = 2, [1.0 + 1] = "two"}
eq(t[2], "two")
eq(t.x, 1)
eq(t["y z"], 2)
eq(#t, 3)
t[4] = 40
eq(#t, 4)
t[#t] = nil
eq(#t, 3)
local keys = {}
for k in pairs({a = 1, b = 2, c = 3}) do keys[#keys + 1] = k end
eq(keys[1] .. keys[2] .. keys[3], "abc")
local ordered = {}
local src = {}
src.zeta, src.alpha, src[5], src.mid = 1, 2, 3, 4
for k, v in pairs(src) do ordered[#ordered + 1] = tostring(k) .. "=" .. v end
eq(ordered[1] .. " " .. ordered[2] .. " " .. ordered[3] .. " " .. ordered[4], "zeta=1 alpha=2 5=3 mid=4")
local arr = {}
for i = 1, 5 do arr[i] = i end
local cnt = 0
for k, v in pairs(arr) do arr[k] = nil; cnt = cnt + 1 end
eq(cnt, 5)
eq(next(arr), nil)
local ip = 0
for i, v in ipairs({1, 2, nil, 4}) do ip = i end
eq(ip, 2)
eq(rawlen({1, 2}), 2)
eq(rawequal(t, t), true)
local nested = {a = {b = {c = "deep"}}}
eq(nested.a.b.c, "deep")
local ok, err = pcall(function() local q = {}; q[nil] = 1 end)
eq(ok, false)
eq(err, "core.lua:208: index is nil")

-- metatables
local V = {}
V.__index = V
V.__add = function(a, b) return setmetatable({x = a.x + b.x}, V) end
V.__eq = function(a, b) return a.x == b.x end
V.__lt = function(a, b) return a.x < b.x end
V.__le = function(a, b) return a.x <= b.x end
V.__len = function() return 42 end
V.__call = function(self, y) return self.x + y end
V.__tostring = function(self) return "V(" .. self.x .. ")" end
V.__concat = function(a, b) return "cat" end
V.__unm = function(a) return setmetatable({x = -a.x}, V) end
function V.new(x) return setmetatable({x = x}, V) end
function V:double() return self.x * 2 end
local v1, v2 = V.new(1), V.new(2)
eq((v1 + v2).x, 3)
eq(v1 == V.new(1), true)
eq(v1 < v2, true)
eq(v2 <= v1, false)
eq(#v1, 42)
eq(v1(5), 6)
eq(tostring(v2), "V(2)")
eq(v1 .. "s", "cat")
eq((-v2).x, -2)
eq(v2:double(), 4)
local defaults = setmetatable({}, {__index = function(t, k) return k .. "!" end})
eq(defaults.hi, "hi!")
local log = {}
local proxy = setmetatable({}, {__newindex = function(t, k, v) rawset(t, k, v * 2) end})
proxy.a = 5
eq(proxy.a, 10)
local chain = setmetatable({}, {__index = setmetatable({}, {__index = {deep = 1}})})
eq(chain.deep, 1)
local prot = setmetatable({}, {__metatable = "locked"})
eq(getmetatable(prot), "locked")
eq(pcall(setmetatable, prot, {}), false)
local named = setmetatable({}, {__name = "Thing"})
eq(type(tostring(named)), "string")

-- errors
local ok2, e2 = pcall(error, {code = 7})
eq(ok2, false)
eq(e2.code, 7)
local ok3, e3 = pcall(error, "plain", 0)
eq(e3, "plain")
local ok4, e4 = pcall(function() error("where") end)
eq(e4, "core.lua:257: where")
local ok5, e5 = pcall(function() local x = nil; return x.field end)
eq(ok5, false)
local ok6, e6 = xpcall(function() error("boom") end, function(m) return "handled: " .. m end)
eq(e6, "handled: core.lua:261: boom")
eq(select("#", pcall(function() return 1, 2 end)), 3)

-- integer and float keys agree
local mix = {}
mix[1] = "int"
eq(mix[1.0], "int")
mix[2.5] = "float"
eq(mix[2.5], "float")

return "ok"
