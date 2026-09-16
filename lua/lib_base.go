package lua

import (
	"io"
	"sort"
	"strconv"
	"strings"
)

// The base library: the global functions every Lua program has. Missing on purpose: load,
// loadstring, dofile and require (code comes from the game's files, loaded by the host),
// and the garbage collector controls (collectgarbage only answers).

// nextFunction and ipairsIterator are shared by every VM, so a for loop can recognise them
// and iterate a table without going through calls.
var (
	nextFunction   = NewFunction("next", baseNext)
	ipairsIterator = NewFunction("ipairs_iterator", ipairsAux)
)

// setFuncs sets Go functions in a table in name order, so that pairs visits a library the
// same way on every run.
func setFuncs(t *Table, prefix string, funcs map[string]GoFunction) {
	names := make([]string, 0, len(funcs))
	for name := range funcs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		t.SetString(name, FunctionValue(NewFunction(prefix+name, funcs[name])))
	}
}

func openBase(vm *VM) {
	g := vm.globals
	setFuncs(g, "", map[string]GoFunction{
		"assert":         baseAssert,
		"collectgarbage": baseCollectGarbage,
		"error":          baseError,
		"getmetatable":   baseGetMetatable,
		"ipairs":         baseIpairs,
		"pairs":          basePairs,
		"pcall":          basePcall,
		"print":          basePrint,
		"rawequal":       baseRawEqual,
		"rawget":         baseRawGet,
		"rawlen":         baseRawLen,
		"rawset":         baseRawSet,
		"select":         baseSelect,
		"setmetatable":   baseSetMetatable,
		"tonumber":       baseTonumber,
		"tostring":       baseTostring,
		"type":           baseType,
		"xpcall":         baseXpcall,
	})
	g.SetString("next", FunctionValue(nextFunction))
	g.SetString("_G", TableValue(g))
	g.SetString("_VERSION", String("Lua 5.4"))
}

func baseAssert(vm *VM, args []Value) []Value {
	if vm.CheckAny(args, 0, "assert").Truthy() {
		return args
	}
	if len(args) < 2 {
		panic(vm.newError(String(vm.where(1) + "assertion failed!")))
	}
	panic(vm.newError(args[1]))
}

func baseCollectGarbage(vm *VM, args []Value) []Value {
	if opt := vm.OptString(args, 0, "collectgarbage", "collect"); opt == "count" {
		return vm.Ret(Float(0), Int(0))
	}
	return vm.ret1(Int(0))
}

func baseError(vm *VM, args []Value) []Value {
	v := Arg(args, 0)
	level := vm.OptInt(args, 1, "error", 1)
	if s, ok := v.Str(); ok && level > 0 {
		v = String(vm.where(int(level)) + s)
	}
	panic(vm.newError(v))
}

func baseGetMetatable(vm *VM, args []Value) []Value {
	m := vm.metatable(vm.CheckAny(args, 0, "getmetatable"))
	if m == nil {
		return vm.ret1(Nil)
	}
	if p := m.GetString("__metatable"); !p.IsNil() {
		return vm.ret1(p)
	}
	return vm.ret1(TableValue(m))
}

func baseSetMetatable(vm *VM, args []Value) []Value {
	t := vm.CheckTable(args, 0, "setmetatable")
	mv := Arg(args, 1)
	if mv.k != kindNil && mv.k != kindTable {
		vm.typeArgError(args, 1, "setmetatable", "nil or table")
	}
	if t.meta != nil && !t.meta.GetString("__metatable").IsNil() {
		vm.Errorf("cannot change a protected metatable")
	}
	t.meta = mv.Table()
	return vm.ret1(args[0])
}

func baseNext(vm *VM, args []Value) []Value {
	t := vm.CheckTable(args, 0, "next")
	k, v, ok, err := t.Next(Arg(args, 1))
	if err != nil {
		vm.Errorf("%s", err.(*Error).Value.s())
	}
	if !ok {
		return vm.ret1(Nil)
	}
	return vm.Ret(k, v)
}

func basePairs(vm *VM, args []Value) []Value {
	v := vm.CheckAny(args, 0, "pairs")
	if h := vm.metaField(v, "__pairs"); !h.IsNil() {
		res := vm.call(h, args[:1], "")
		out := [3]Value{}
		copy(out[:], res)
		return vm.Ret(out[:]...)
	}
	if v.k != kindTable {
		vm.typeArgError(args, 0, "pairs", "table")
	}
	return vm.Ret(FunctionValue(nextFunction), v, Nil)
}

func baseIpairs(vm *VM, args []Value) []Value {
	return vm.Ret(FunctionValue(ipairsIterator), vm.CheckAny(args, 0, "ipairs"), Int(0))
}

func ipairsAux(vm *VM, args []Value) []Value {
	i := vm.CheckInt(args, 1, "ipairs") + 1
	t := Arg(args, 0)
	var v Value
	if tb := t.Table(); tb != nil && tb.meta == nil {
		v = tb.GetInt(i)
	} else {
		v = vm.index(t, Int(i), "")
	}
	if v.IsNil() {
		return vm.ret1(Nil)
	}
	return vm.Ret(Int(i), v)
}

// protectedCall calls f and reports a Lua error instead of raising it.
func (vm *VM) protectedCall(f Value, args []Value) (res []Value, errv Value, ok bool) {
	depth, top, btop, gs := vm.depth, vm.top, vm.btop, vm.saveGo()
	defer func() {
		if r := recover(); r != nil {
			e, isErr := r.(*Error)
			if !isErr {
				panic(r) // a budget exhausted, or a bug: not for Lua to catch
			}
			vm.unwind(depth, top, btop)
			vm.restoreGo(gs)
			res, errv, ok = nil, e.Value, false
		}
	}()
	return vm.call(f, args, ""), Nil, true
}

func basePcall(vm *VM, args []Value) []Value {
	f := vm.CheckAny(args, 0, "pcall")
	res, errv, ok := vm.protectedCall(f, args[1:])
	if !ok {
		return vm.Ret(False, errv)
	}
	out := make([]Value, len(res)+1)
	out[0] = True
	copy(out[1:], res)
	return out
}

func baseXpcall(vm *VM, args []Value) []Value {
	f := vm.CheckAny(args, 0, "xpcall")
	h := vm.CheckFunction(args, 1, "xpcall")
	res, errv, ok := vm.protectedCall(f, args[2:])
	if !ok {
		return vm.Ret(False, vm.call1(h, errv))
	}
	out := make([]Value, len(res)+1)
	out[0] = True
	copy(out[1:], res)
	return out
}

func basePrint(vm *VM, args []Value) []Value {
	var b strings.Builder
	for i, v := range args {
		if i > 0 {
			b.WriteByte('\t')
		}
		b.WriteString(vm.tostring(v).s())
	}
	b.WriteByte('\n')
	io.WriteString(vm.stdout, b.String())
	return nil
}

func baseRawEqual(vm *VM, args []Value) []Value {
	return vm.ret1(Bool(RawEqual(vm.CheckAny(args, 0, "rawequal"), vm.CheckAny(args, 1, "rawequal"))))
}

func baseRawGet(vm *VM, args []Value) []Value {
	return vm.ret1(vm.CheckTable(args, 0, "rawget").Get(vm.CheckAny(args, 1, "rawget")))
}

func baseRawSet(vm *VM, args []Value) []Value {
	t := vm.CheckTable(args, 0, "rawset")
	vm.rawSet(t, vm.CheckAny(args, 1, "rawset"), vm.CheckAny(args, 2, "rawset"))
	return vm.ret1(args[0])
}

func baseRawLen(vm *VM, args []Value) []Value {
	v := Arg(args, 0)
	switch v.k {
	case kindTable:
		return vm.ret1(Int(int64(v.p.(*Table).Len())))
	case kindString:
		return vm.ret1(Int(int64(len(v.s()))))
	}
	vm.ArgError(0, "rawlen", "table or string expected")
	return nil
}

func baseSelect(vm *VM, args []Value) []Value {
	if s, ok := Arg(args, 0).Str(); ok && s == "#" {
		return vm.ret1(Int(int64(len(args) - 1)))
	}
	n := vm.CheckInt(args, 0, "select")
	switch {
	case n < 0:
		n += int64(len(args))
		if n < 1 {
			vm.ArgError(0, "select", "index out of range")
		}
	case n == 0:
		vm.ArgError(0, "select", "index out of range")
	}
	if n >= int64(len(args)) {
		return nil
	}
	return args[n:]
}

func baseTonumber(vm *VM, args []Value) []Value {
	v := vm.CheckAny(args, 0, "tonumber")
	if Arg(args, 1).IsNil() {
		if n, ok := toNumber(v); ok {
			return vm.ret1(n)
		}
		return vm.ret1(Nil)
	}
	base := vm.CheckInt(args, 1, "tonumber")
	s, ok := v.Str()
	if !ok {
		vm.typeArgError(args, 0, "tonumber", "string")
	}
	if base < 2 || base > 36 {
		vm.ArgError(1, "tonumber", "base out of range")
	}
	s = strings.ToLower(strings.TrimSpace(s))
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	if s == "" {
		return vm.ret1(Nil)
	}
	var n uint64
	for i := 0; i < len(s); i++ {
		d, err := strconv.ParseUint(s[i:i+1], 36, 8)
		if err != nil || int64(d) >= base {
			return vm.ret1(Nil)
		}
		n = n*uint64(base) + d
	}
	if neg {
		n = -n
	}
	return vm.ret1(Int(int64(n)))
}

func baseTostring(vm *VM, args []Value) []Value {
	return vm.ret1(vm.tostring(vm.CheckAny(args, 0, "tostring")))
}

func baseType(vm *VM, args []Value) []Value {
	return vm.ret1(String(vm.CheckAny(args, 0, "type").Type().String()))
}
