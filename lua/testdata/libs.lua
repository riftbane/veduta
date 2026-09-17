-- Standard libraries, compared line by line with the output of PUC Lua 5.4.6 (libs.out).
-- Only what both must agree on: no table addresses, no pairs order, no random draws.

local function show(label, ...)
  local parts = {}
  for i = 1, select("#", ...) do
    local v = select(i, ...)
    local s = tostring(v)
    if math.type(v) == "float" then s = s .. "f" end
    parts[#parts + 1] = s
  end
  print(label, table.concat(parts, " | "))
end

local function try(label, f, ...)
  local res = table.pack(pcall(f, ...))
  if res[1] then
    show(label, table.unpack(res, 2, res.n))
  else
    print(label, "error: " .. tostring(res[2]))
  end
end

-- string basics
show("len", #"hello", string.len(""), ("abc"):len())
show("sub", ("hello"):sub(2, 4), ("hello"):sub(-3), ("hello"):sub(0), ("hello"):sub(4, 2), ("hello"):sub(-100, 100))
show("upper", ("Hello World 123"):upper(), ("ÀBC"):lower())
show("rep", ("ab"):rep(3), ("ab"):rep(3, ","), ("x"):rep(0), ("x"):rep(-1))
show("reverse", ("hello"):reverse(), (""):reverse())
show("byte", ("ABC"):byte(), ("ABC"):byte(1, -1), ("ABC"):byte(10))
show("char", string.char(72, 105), string.char())
try("char bad", string.char, 256)

-- find and match
show("find plain", ("hello world"):find("o w"), ("a.b"):find(".", 1, true), ("abc"):find("x"))
show("find pattern", ("hello world"):find("o%s+w"), ("hello"):find("l+"), ("hello"):find("^h"), ("hello"):find("^e"))
show("find init", ("aaa"):find("a", 2), ("aaa"):find("a", -1), ("aaa"):find("a", 10), ("aaa"):find("", 4))
show("find captures", ("key=value"):find("(%w+)=(%w+)"))
show("match", ("key=value"):match("(%w+)=(%w+)"), ("2024-01-15"):match("(%d+)-(%d+)-(%d+)"))
show("match whole", ("hello 123"):match("%d+"), ("hello"):match(".-l"), ("hello"):match(".*l"))
show("match position", ("hello"):match("()ll()"))
show("match anchors", ("hello"):match("^hel"), ("hello"):match("llo$"), ("hello"):match("^llo"))
show("match classes", ("a1 B2_c3!"):match("%a%d"), ("  x"):match("^%s*(.-)%s*$"), ("x\t"):match("%c"))
show("match sets", ("hello123"):match("[a-z]+"), ("hello123"):match("[^a-z]+"), ("a-b"):match("[%-]"), ("]"):match("[]]"))
show("match optional", ("color colour"):match("colou?r"), ("ac"):match("ab?c"))
show("match balance", ("f(a(b)c) d"):match("%b()"), ("{x{y}}"):match("%b{}"))
show("match frontier", ("THE (quick) fox"):match("%f[%a]%a+"), ("hello world"):match("%f[%w]%w+$"))
show("match backref", ('say "hi" or \'yo\''):match("([\"'])(.-)%1"))
show("match empty", ("abc"):match(""), ("abc"):match("()"))
try("match bad", string.match, "x", "[a")
try("match bad2", string.match, "x", "%")
try("match bad3", string.match, "x", "(()")

-- gmatch
local words = {}
for w in ("one two  three"):gmatch("%a+") do words[#words + 1] = w end
show("gmatch", table.concat(words, ","))
local pairs_found = {}
for k, v in ("a=1, b=2"):gmatch("(%w+)=(%w+)") do pairs_found[#pairs_found + 1] = k .. v end
show("gmatch captures", table.concat(pairs_found, ","))
local empties = 0
for _ in ("abc"):gmatch("x*") do empties = empties + 1 end
show("gmatch empty", empties)

-- gsub
show("gsub string", ("hello world"):gsub("o", "0"))
show("gsub limit", ("aaa"):gsub("a", "b", 2))
show("gsub captures", ("hello world"):gsub("(%w+)", "<%1>"))
show("gsub percent", ("50"):gsub("%d+", "%0%%"))
show("gsub table", ("$name is $age"):gsub("%$(%w+)", {name = "Ada", age = 36}))
show("gsub function", ("1 2 3"):gsub("%d", function(d) return d * 2 end))
show("gsub keep", ("abc"):gsub("%w", function(c) if c == "b" then return nil end return c:upper() end))
show("gsub anchor", ("aaa"):gsub("^a", "b"))
show("gsub empty", ("abc"):gsub("", "-"))
try("gsub bad repl", string.gsub, "abc", "b", "%2")
try("gsub bad value", string.gsub, "abc", "b", function() return {} end)

-- format
show("format d", string.format("%d %5d %-5d| %05d %+d", 42, 42, 42, 42, 42))
show("format x", string.format("%x %X %#x %o", 255, 255, 255, 8))
show("format neg hex", string.format("%x", -1))
show("format f", string.format("%f %.2f %10.3f %-10.1f|", 3.14159, 3.14159, 3.14159, 3.14159))
show("format e", string.format("%e %.3E", 12345.678, 0.00012345))
show("format g", string.format("%g %g %g %g %.3g", 100000, 1000000, 0.0001, 0.00001, 3.14159))
show("format g2", string.format("%g %g %g", 1e20, 2^53, 1/3))
show("format s", string.format("[%s] [%10s] [%-10s] [%.2s]", "hi", "hi", "hi", "hello"))
show("format q", string.format("%q", 'a "quoted"\nline\0end'))
show("format q num", string.format("%q %q %q", 42, math.mininteger, 1/0))
show("format c", string.format("%c%c%c", 76, 117, 97))
show("format pct", string.format("100%%"))
show("format float int", string.format("%d", 3.0))
try("format bad int", string.format, "%d", 3.5)
try("format bad conv", string.format, "%y", 1)
try("format no value", string.format, "%d")
show("format a", string.format("%a", 1.0), string.format("%a", 0.5))
show("format inf", string.format("%f %e %g", 1/0, -1/0, 1/0))
show("format tostring", string.format("%s %s %s", 1, 1.5, true))

-- tostring / tonumber edge cases
show("tostring num", tostring(1e100), tostring(-1e-100), tostring(123456789012), tostring(2^63), tostring(0.1 + 0.2))
show("tonumber", tonumber("0x1p4"), tonumber("1e+2"), tonumber(".5"), tonumber("5."), tonumber("0x.8"))
show("tonumber bad", tonumber("1 2"), tonumber("abc"), tonumber("1e1e1"), tonumber(nil))
show("tonumber base", tonumber("777", 8), tonumber("zz", 36), tonumber("-ff", 16), tonumber("10", 2))

-- table
local t = {1, 2, 3}
table.insert(t, 4)
table.insert(t, 1, 0)
show("insert", table.concat(t, ","))
show("remove", table.remove(t), table.remove(t, 1), table.concat(t, ","))
show("remove empty", table.remove({}), #t)
try("insert bad pos", table.insert, {1, 2}, 5, 0)
try("insert args", table.insert, {}, 1, 2, 3)
show("concat", table.concat({1, 2.5, "x"}, "-"), table.concat({}, ","), table.concat({1, 2, 3}, ",", 2, 3))
try("concat bad", table.concat, {1, {}, 3})
show("unpack", table.unpack({1, 2, 3}), table.unpack({1, 2, 3}, 2), table.unpack({1, 2, 3}, 2, 5))
local packed = table.pack(1, nil, 3)
show("pack", packed.n, packed[1], packed[2], packed[3])
show("move", table.concat(table.move({1, 2, 3, 4, 5}, 2, 4, 1), ","))
show("move other", table.concat(table.move({1, 2, 3}, 1, 3, 2, {9}), ","))
local s = {5, 2, 8, 1, 9, 3}
table.sort(s)
show("sort", table.concat(s, ","))
table.sort(s, function(a, b) return a > b end)
show("sort desc", table.concat(s, ","))
local names = {"pear", "Apple", "fig", "banana"}
table.sort(names)
show("sort strings", table.concat(names, ","))
local recs = {{n = 3}, {n = 1}, {n = 2}}
table.sort(recs, function(a, b) return a.n < b.n end)
show("sort records", recs[1].n, recs[2].n, recs[3].n)
try("sort mixed", table.sort, {1, "x"})

-- math
show("math floor", math.floor(3.7), math.floor(-3.7), math.floor(5), math.floor(2^70))
show("math ceil", math.ceil(3.2), math.ceil(-3.2), math.ceil(1e300))
show("math abs", math.abs(-5), math.abs(-5.5), math.abs(math.mininteger))
show("math max", math.max(1, 5, 3), math.max(1, 5.0), math.min(2, 1.5), math.max(3))
show("math fmod", math.fmod(7, 3), math.fmod(-7, 3), math.fmod(7, -3), math.fmod(7.5, 2), math.fmod(-7, math.huge))
try("math fmod zero", math.fmod, 1, 0)
show("math modf", math.modf(3.7), math.modf(-3.7), math.modf(5), math.modf(math.huge))
show("math tointeger", math.tointeger(3.0), math.tointeger(3.5), math.tointeger("8"), math.tointeger({}))
show("math type", math.type(1), math.type(1.0), math.type("1"))
show("math ult", math.ult(1, -1), math.ult(-1, 1))
show("math sqrt", math.sqrt(16), math.sqrt(2))
show("math deg rad", math.deg(math.pi), math.rad(180), math.deg(1), math.rad(-90), math.deg(0))
try("math deg string", math.deg, "x")
show("math exp log", math.exp(0), math.log(1), math.log(8, 2), math.log(100, 10), math.log(1000, 10))
show("math trig", math.sin(0), math.cos(0), math.atan(1, 1) * 4 == math.pi, math.atan(0, -1) == math.pi)
show("math pow", 2^0.5 == math.sqrt(2), 10^2, (-8)^(1/3) ~= (-8)^(1/3))
show("math consts", math.pi, math.huge, math.maxinteger, math.mininteger)
local r = math.random(1, 6)
show("random range", r >= 1 and r <= 6, math.type(r))
local f = math.random()
show("random float", f >= 0 and f < 1)
try("random empty", math.random, 3, 1)

-- utf8
show("utf8 char", utf8.char(72, 228, 8364, 128512))
show("utf8 len", utf8.len("häl€"), utf8.len("abc", 2), utf8.len("\xff"))
show("utf8 codepoint", utf8.codepoint("h€", 1, -1))
show("utf8 offset", utf8.offset("a€b", 3), utf8.offset("a€b", -1), utf8.offset("a€b", 0, 3))
local cps = {}
for p, c in utf8.codes("a€b") do cps[#cps + 1] = p .. ":" .. c end
show("utf8 codes", table.concat(cps, " "))

-- metamethods through the libraries
local obj = setmetatable({}, {__tostring = function() return "OBJ" end, __len = function() return 3 end,
  __index = function(_, i) return i * 10 end})
show("tostring meta", tostring(obj), string.format("%s", obj))
show("concat meta", table.concat(obj, ","))
show("unpack meta", table.unpack(obj))

-- select and varargs
show("select", select("#"), select("#", nil, nil), select(2, "a", "b", "c"))
show("select neg", select(-2, "a", "b", "c"))

-- where errors point, and the names they use
try("err from lua", function() string.char(256) end)
try("err method", function() return ("x"):rep() end)
try("err self", function() return string.rep() end)
try("err global", function() setmetatable(1) end)
try("err direct", setmetatable, 1)
try("err level2", function() local function f() error("lvl", 2) end f() end)
try("err concat", function() return table.concat({{}}) end)
try("err index", function() local s = "x"; return s.y.z end)
try("err call field", function() return math.nope() end)
