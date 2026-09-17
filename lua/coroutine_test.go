package lua

import (
	"errors"
	"runtime"
	"testing"
	"time"
)

func runChunk(t *testing.T, vm *VM, src string) []Value {
	t.Helper()
	f, err := vm.Load("test", src)
	if err != nil {
		t.Fatal(err)
	}
	res, err := vm.Call(FunctionValue(f))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// A coroutine yields from anywhere in its call stack, Go functions included, which PUC Lua
// refuses (a yield across a C call).
func TestCoroutineYieldAcrossGo(t *testing.T) {
	vm := New(Options{})
	defer vm.Close()
	res := runChunk(t, vm, `
		local sorter = coroutine.wrap(function()
			local t = {3, 1, 2}
			table.sort(t, function(a, b) coroutine.yield("compare"); return a < b end)
			return table.concat(t, ",")
		end)
		local steps, r = 0, sorter()
		while r == "compare" do steps = steps + 1; r = sorter() end
		return r, steps`)
	if s, _ := res[0].Str(); s != "1,2,3" || res[1].i() < 2 {
		t.Fatalf("sorted %v after %v comparisons", res[0], res[1])
	}
}

// The budget counts the steps of the coroutines a call resumes, and running out is not the
// coroutine's error to catch.
func TestCoroutineBudget(t *testing.T) {
	vm := New(Options{})
	defer vm.Close()
	vm.SetBudget(10_000)
	f, _ := vm.Load("test", `
		local co = coroutine.wrap(function() while true do coroutine.yield() end end)
		while true do co() end`)
	_, err := vm.Call(FunctionValue(f))
	var be *BudgetError
	if !errors.As(err, &be) {
		t.Fatalf("an endless resume loop: %v", err)
	}
	g, _ := vm.Load("test", `
		local co = coroutine.create(function() while true do end end)
		return coroutine.resume(co)`)
	if _, err := vm.Call(FunctionValue(g)); !errors.As(err, &be) {
		t.Fatalf("an endless loop in a coroutine: %v", err)
	}
	// The next call has its budget again.
	if res := runChunk(t, vm, `return coroutine.wrap(function() return 7 end)()`); res[0].i() != 7 {
		t.Fatal("the budget did not come back")
	}
}

// Close ends the goroutines of suspended coroutines; a coroutine nobody refers to is ended
// when collected.
func TestCoroutineGoroutines(t *testing.T) {
	// The runtime starts its finalizer goroutine with the first finalizer: count after it.
	warm := New(Options{})
	runChunk(t, warm, `coroutine.create(print)`)
	warm.Close()
	runtime.GC()
	time.Sleep(10 * time.Millisecond)
	before := runtime.NumGoroutine()
	vm := New(Options{})
	runChunk(t, vm, `
		keep = {}
		for i = 1, 50 do
			local co = coroutine.create(function() coroutine.yield() end)
			coroutine.resume(co)
			keep[i] = co
		end`)
	if n := runtime.NumGoroutine(); n < before+50 {
		t.Fatalf("%d goroutines, want at least %d", n, before+50)
	}
	vm.Close()
	waitGoroutines(t, before)

	vm = New(Options{})
	runChunk(t, vm, `
		for i = 1, 50 do
			local co = coroutine.create(function() coroutine.yield() end)
			coroutine.resume(co)
		end`)
	// The VM's result buffer may still hold the last one.
	waitGoroutines(t, before+1)
	vm.Close()
	waitGoroutines(t, before)
}

func waitGoroutines(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > want {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<16)
			n := runtime.Stack(buf, true)
			t.Fatalf("%d goroutines left, want %d:\n%s", runtime.NumGoroutine(), want, buf[:n])
		}
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
	}
}

// The same program gives the same result every run, coroutines or not.
func TestCoroutineDeterministic(t *testing.T) {
	src := `
		local out = {}
		local cos = {}
		for i = 1, 20 do
			cos[i] = coroutine.wrap(function()
				for k = 1, 5 do out[#out + 1] = i * 100 + k + math.random(1, 9); coroutine.yield() end
			end)
		end
		for round = 1, 5 do for i = 20, 1, -1 do cos[i]() end end
		return table.concat(out, ",")`
	var first string
	for run := 0; run < 5; run++ {
		vm := New(Options{})
		res := runChunk(t, vm, src)
		vm.Close()
		s, _ := res[0].Str()
		if run == 0 {
			first = s
		} else if s != first {
			t.Fatalf("run %d differs", run)
		}
	}
}
