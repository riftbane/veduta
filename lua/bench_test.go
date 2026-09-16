package lua

import "testing"

func benchScript(b *testing.B, src string) {
	vm := New(Options{})
	f, err := vm.Load("bench.lua", src)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := vm.Call(FunctionValue(f)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkFib: calls and integer arithmetic.
func BenchmarkFib(b *testing.B) {
	benchScript(b, "local function fib(n) if n < 2 then return n end return fib(n-1) + fib(n-2) end return fib(20)")
}

// BenchmarkLoop: a numeric loop with float arithmetic.
func BenchmarkLoop(b *testing.B) {
	benchScript(b, "local s = 0.0 for i = 1, 10000 do s = s + i * 0.5 end return s")
}

// BenchmarkEntities: what a game's update does per tick: a hundred entity tables with
// methods, fields read and written.
func BenchmarkEntities(b *testing.B) {
	benchScript(b, `
		if not Entity then
			Entity = {}
			Entity.__index = Entity
			function Entity.new(x) return setmetatable({x = x, y = 0, vx = 1, vy = 0.5}, Entity) end
			function Entity:update(dt)
				self.x = self.x + self.vx * dt
				self.y = self.y + self.vy * dt
				if self.x > 100 then self.vx = -self.vx end
			end
			entities = {}
			for i = 1, 100 do entities[i] = Entity.new(i) end
		end
		for _, e in ipairs(entities) do e:update(0.05) end
	`)
}

// BenchmarkTableBuild: allocating and filling tables.
func BenchmarkTableBuild(b *testing.B) {
	benchScript(b, "local t = {} for i = 1, 1000 do t[i] = {id = i, name = 'x'} end return #t")
}
