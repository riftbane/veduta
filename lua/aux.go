package lua

import "fmt"

// Helpers for Go functions: argument checks that raise the errors Lua's own library raises.

// Arg returns argument i (0-based), or nil when the call has fewer.
func Arg(args []Value, i int) Value {
	if i < len(args) {
		return args[i]
	}
	return Nil
}

// ArgError raises "bad argument #i to 'fname' (msg)"; i is 0-based. As in Lua, the name is
// the one the caller used, qualified ("string.rep") when a Go function such as pcall made
// the call, and a method's self is not counted.
func (vm *VM) ArgError(i int, fname, msg string) {
	if vm.goActive > 1 && vm.goFunc != nil {
		fname = vm.goFunc.name
	}
	if vm.goMethod && vm.goActive == 1 {
		if i == 0 {
			vm.Errorf("calling '%s' on bad self (%s)", fname, msg)
		}
		i--
	}
	vm.Errorf("bad argument #%d to '%s' (%s)", i+1, fname, msg)
}

func (vm *VM) typeArgError(args []Value, i int, fname, want string) {
	got := "no value"
	if i < len(args) {
		got = vm.typeName(args[i])
	}
	vm.ArgError(i, fname, fmt.Sprintf("%s expected, got %s", want, got))
}

// CheckAny raises an error when argument i is missing.
func (vm *VM) CheckAny(args []Value, i int, fname string) Value {
	if i >= len(args) {
		vm.ArgError(i, fname, "value expected")
	}
	return args[i]
}

// CheckTable returns argument i, which must be a table.
func (vm *VM) CheckTable(args []Value, i int, fname string) *Table {
	if t := Arg(args, i).Table(); t != nil {
		return t
	}
	vm.typeArgError(args, i, fname, "table")
	return nil
}

// CheckNumber returns argument i as a number (strings convert).
func (vm *VM) CheckNumber(args []Value, i int, fname string) Value {
	if n, ok := toNumber(Arg(args, i)); ok {
		return n
	}
	vm.typeArgError(args, i, fname, "number")
	return Nil
}

// CheckFloat returns argument i as a float.
func (vm *VM) CheckFloat(args []Value, i int, fname string) float64 {
	f, _ := vm.CheckNumber(args, i, fname).Float()
	return f
}

// CheckInt returns argument i as an integer: an integer, a float with an integral value, or
// a string that converts to one.
func (vm *VM) CheckInt(args []Value, i int, fname string) int64 {
	v := Arg(args, i)
	if n, ok := toInteger(v); ok {
		return n
	}
	if _, ok := toNumber(v); ok {
		vm.ArgError(i, fname, "number has no integer representation")
	}
	vm.typeArgError(args, i, fname, "number")
	return 0
}

// OptInt returns argument i as an integer, or def when it is missing or nil.
func (vm *VM) OptInt(args []Value, i int, fname string, def int64) int64 {
	if Arg(args, i).IsNil() {
		return def
	}
	return vm.CheckInt(args, i, fname)
}

// CheckString returns argument i as a string (numbers convert).
func (vm *VM) CheckString(args []Value, i int, fname string) string {
	v := Arg(args, i)
	switch v.k {
	case kindString:
		return v.s()
	case kindInt, kindFloat:
		return v.String()
	}
	vm.typeArgError(args, i, fname, "string")
	return ""
}

// OptString returns argument i as a string, or def when it is missing or nil.
func (vm *VM) OptString(args []Value, i int, fname string, def string) string {
	if Arg(args, i).IsNil() {
		return def
	}
	return vm.CheckString(args, i, fname)
}

// CheckFunction returns argument i, which must be a function.
func (vm *VM) CheckFunction(args []Value, i int, fname string) Value {
	v := Arg(args, i)
	if v.k != kindFunction {
		vm.typeArgError(args, i, fname, "function")
	}
	return v
}

// ToString converts v to a string as tostring does, with __tostring.
func (vm *VM) ToString(v Value) string { return vm.tostring(v).s() }

// Index returns o[k] with metamethods.
func (vm *VM) Index(o, k Value) Value { return vm.index(o, k, "") }

// SetIndex sets o[k] = v with metamethods.
func (vm *VM) SetIndex(o, k, v Value) { vm.setIndex(o, k, v, "") }

// Equal reports whether a == b, with metamethods.
func (vm *VM) Equal(a, b Value) bool { return vm.equal(a, b) }

// Less reports whether a < b, with metamethods.
func (vm *VM) Less(a, b Value) bool { return vm.lessThan(a, b) }
