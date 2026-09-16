package lua

import (
	"errors"
	"strings"
	"testing"
)

// run compiles and calls src in a fresh VM.
func run(t *testing.T, src string, args ...Value) ([]Value, error) {
	t.Helper()
	vm := New(Options{})
	openTestLibs(vm)
	f, err := vm.Load("test.lua", src)
	if err != nil {
		return nil, err
	}
	return vm.Call(FunctionValue(f), args...)
}

func TestRuntimeErrorMessages(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{"local x\nreturn x.y", "test.lua:2: attempt to index a nil value (local 'x')"},
		{"return undefined.y", "test.lua:1: attempt to index a nil value (global 'undefined')"},
		{"local t = {}\nreturn t.a.b", "test.lua:2: attempt to index a nil value (field 'a')"},
		{"return nofunc()", "test.lua:1: attempt to call a nil value (global 'nofunc')"},
		{"local t = {}\nt.m()", "test.lua:2: attempt to call a nil value (field 'm')"},
		{"local t = {}\nt:m()", "test.lua:2: attempt to call a nil value (method 'm')"},
		{"local x = {}\nreturn x + 1", "test.lua:2: attempt to perform arithmetic on a table value (local 'x')"},
		{"return 1 + nil", "test.lua:1: attempt to perform arithmetic on a nil value"},
		{`return "abc" + 1`, "test.lua:1: attempt to perform arithmetic on a string value (constant 'abc')"},
		{"return 1 // 0", "test.lua:1: attempt to perform 'n//0'"},
		{"return 1 % 0", "test.lua:1: attempt to perform 'n%0'"},
		{"return 1.5 | 1", "test.lua:1: number has no integer representation"},
		{"return {} < {}", "test.lua:1: attempt to compare two table values"},
		{"return 1 < 'x'", "test.lua:1: attempt to compare number with string"},
		{"return #nil", "test.lua:1: attempt to get length of a nil value"},
		{"local s\nreturn 'a' .. s", "test.lua:2: attempt to concatenate a nil value (local 's')"},
		{"local t = {}\nt[nil] = 1", "test.lua:2: index is nil"},
		{"local t = {}\nt[0/0] = 1", "test.lua:2: index is NaN"},
		{"for i = 1, 10, 0 do end", "test.lua:1: 'for' step is zero"},
		{"for i = 'a', 10 do end", "test.lua:1: 'for' initial value must be a number"},
		{"error('custom')", "test.lua:1: custom"},
		{"error('no position', 0)", "no position"},
		{"local function f() error('up', 2) end\nf()", "test.lua:2: up"},
		{"assert(false)", "test.lua:1: assertion failed!"},
		{"assert(nil, 'message')", "message"},
		{"return setmetatable(1, {})", "test.lua:1: bad argument #1 to 'setmetatable' (table expected, got number)"},
		{"return select(0)", "test.lua:1: bad argument #1 to 'select' (index out of range)"},
		{"local t = setmetatable({}, {__index = function(t, k) return t[k] end})\nreturn t.x", "stack overflow"},
	} {
		_, err := run(t, c.src)
		var le *Error
		if !errors.As(err, &le) {
			t.Errorf("%q: error %v, want a Lua error", c.src, err)
			continue
		}
		msg := le.Value.String()
		if !strings.Contains(msg, c.want) {
			t.Errorf("%q:\n got %q\nwant %q", c.src, msg, c.want)
		}
	}
}

func TestSyntaxErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{"x = ", "test.lua:1: unexpected symbol near <eof>"},
		{"local 1 = 2", "test.lua:1: <name> expected near '1'"},
		{"if x then", "test.lua:1: 'end' expected near <eof>"},
		{"while x do\n\nfoo()", "test.lua:3: 'end' expected (to close 'while' at line 1) near <eof>"},
		{"x = 'unfinished", "test.lua:1: unfinished string near <eof>"},
		{"x = 'broken\nline'", "test.lua:1: unfinished string near <string>"},
		{"goto done", "test.lua:1: goto is not supported"},
		{"::label::", "test.lua:1: labels are not supported"},
		{"local x <close> = nil", "to-be-closed variables are not supported"},
		{"local x <const> = 1\nx = 2", "test.lua:2: attempt to assign to const variable 'x'"},
		{"break", "test.lua:1: break outside a loop at line 1"},
		{"return 1 2", "test.lua:1: <eof> expected near '2'"},
		{"f() = 1", "test.lua:1: syntax error near '='"},
		{"return ...", ""},
		{"function f() return ... end", "cannot use '...' outside a vararg function"},
		{"x = 3e", "malformed number near '3e'"},
		{"x = '\\q'", "invalid escape sequence"},
	} {
		_, err := run(t, c.src)
		if c.want == "" {
			if err != nil {
				t.Errorf("%q: %v", c.src, err)
			}
			continue
		}
		var se *SyntaxError
		if !errors.As(err, &se) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q:\n got %v\nwant %q", c.src, err, c.want)
		}
	}
}

func TestTraceback(t *testing.T) {
	_, err := run(t, "local function inner()\n  error('deep')\nend\nlocal function outer()\n  inner()\nend\nouter()")
	want := "test.lua:2: deep\nstack traceback:\n\ttest.lua:2: in local function 'inner'\n\ttest.lua:5: in local function 'outer'\n\ttest.lua:7: in main chunk"
	if err == nil || err.Error() != want {
		t.Fatalf("error:\n%v\nwant:\n%s", err, want)
	}
}

// TestBudget: an endless loop stops with a BudgetError, which pcall cannot swallow, and
// the VM is usable afterwards.
func TestBudget(t *testing.T) {
	vm := New(Options{})
	vm.SetBudget(10000)
	for _, src := range []string{
		"while true do end",
		"repeat until false",
		"for i = 1, 1e18 do end",
		"local function f() return f() end return f()",
		"while true do pcall(function() while true do end end) end",
		"local t = setmetatable({}, {__index = function() while true do end end}) return t.x",
	} {
		f, err := vm.Load("loop.lua", src)
		if err != nil {
			t.Fatal(err)
		}
		_, err = vm.Call(FunctionValue(f))
		var be *BudgetError
		var le *Error
		switch {
		case errors.As(err, &be):
		case src == "local function f() return f() end return f()" && errors.As(err, &le) && strings.Contains(le.Value.String(), "stack overflow"):
		default:
			t.Errorf("%q: %v, want the budget exhausted", src, err)
		}
	}
	f, _ := vm.Load("ok.lua", "local s = 0 for i = 1, 100 do s = s + i end return s")
	if res, err := vm.Call(FunctionValue(f)); err != nil || res[0] != Int(5050) {
		t.Fatalf("after exhausted budgets: %v %v", res, err)
	}
	if vm.depth != 0 || vm.top != 0 || vm.btop != 0 {
		t.Fatalf("stacks not unwound: depth %d top %d btop %d", vm.depth, vm.top, vm.btop)
	}
}

func TestGoFunctions(t *testing.T) {
	vm := New(Options{})
	type point struct{ x, y float64 }
	meta := NewTable(0, 2)
	meta.SetString("__index", FunctionValue(NewFunction("point.__index", func(vm *VM, args []Value) []Value {
		p := args[0].Userdata().Data.(*point)
		switch vm.CheckString(args, 1, "index") {
		case "x":
			return vm.Ret(Float(p.x))
		case "y":
			return vm.Ret(Float(p.y))
		}
		return vm.Ret(Nil)
	})))
	meta.SetString("__name", String("point"))
	vm.SetGlobal("newpoint", FunctionValue(NewFunction("newpoint", func(vm *VM, args []Value) []Value {
		p := &point{vm.CheckFloat(args, 0, "newpoint"), vm.CheckFloat(args, 1, "newpoint")}
		return vm.Ret(UserdataValue(&Userdata{Data: p, Meta: meta}))
	})))
	// A Go function that calls back into Lua.
	vm.SetGlobal("twice", FunctionValue(NewFunction("twice", func(vm *VM, args []Value) []Value {
		f := vm.CheckFunction(args, 0, "twice")
		a, err := vm.Call(f, Int(1))
		if err != nil {
			panic(err)
		}
		b, err := vm.Call(f, a[0])
		if err != nil {
			panic(err)
		}
		return b
	})))
	f, err := vm.Load("go.lua", `
		local p = newpoint(3, 4)
		assert(p.x == 3 and p.y == 4)
		assert(twice(function(n) return n * 10 end) == 100)
		local ok, e = pcall(newpoint, "x")
		assert(not ok and e == "go.lua:5: bad argument #1 to 'newpoint' (number expected, got string)", e)
		return p.x + p.y
	`)
	if err != nil {
		t.Fatal(err)
	}
	openTestLibs(vm)
	res, err := vm.Call(FunctionValue(f))
	if err != nil || len(res) != 1 || res[0] != Float(7) {
		t.Fatalf("results %v, error %v", res, err)
	}
}

func TestTableOrderAndMigration(t *testing.T) {
	tb := NewTable(0, 0)
	for _, k := range []string{"d", "b", "a", "c"} {
		tb.SetString(k, True)
	}
	tb.SetInt(3, String("three"))
	tb.SetInt(2, String("two"))
	tb.SetInt(1, String("one")) // pulls 2 and 3 into the array part
	if tb.Len() != 3 {
		t.Fatalf("len %d", tb.Len())
	}
	tb.SetString("b", Nil)
	tb.SetString("e", True)
	var got []string
	tb.ForEach(func(k, v Value) bool {
		got = append(got, k.String())
		return true
	})
	if strings.Join(got, ",") != "1,2,3,d,a,c,e" {
		t.Fatalf("order %v", got)
	}
	// Removing and re-adding many keys compacts the hash part without breaking order.
	big := NewTable(0, 0)
	for i := 0; i < 1000; i++ {
		big.Set(Float(float64(i)+0.5), Int(int64(i)))
	}
	for i := 0; i < 1000; i += 2 {
		big.Set(Float(float64(i)+0.5), Nil)
	}
	for i := 0; i < 100; i++ {
		big.SetString(string(rune('A'+i%26))+string(rune('a'+i/26)), Int(int64(i)))
	}
	n, last := 0, -1.0
	k := Nil
	for {
		nk, v, ok, err := big.Next(k)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		if f, isF := nk.Float(); isF && nk.IsFloat() {
			if f <= last || int(f)%2 == 0 {
				t.Fatalf("float key %v out of order or removed (value %v)", f, v)
			}
			last = f
		}
		n++
		k = nk
	}
	if n != 600 {
		t.Fatalf("%d entries, want 600", n)
	}
	if _, _, _, err := big.Next(String("missing")); err == nil {
		t.Fatal("next of a missing key did not fail")
	}
}
