package lua

import (
	"math"
	"strings"
)

// The table library.

func openTable(vm *VM) {
	lib := NewTable(0, 8)
	setFuncs(lib, "table.", map[string]GoFunction{
		"concat": tabConcat, "insert": tabInsert, "move": tabMove, "pack": tabPack,
		"remove": tabRemove, "sort": tabSort, "unpack": tabUnpack,
	})
	vm.globals.SetString("table", TableValue(lib))
}

// tableLen is #t for a table argument, with __len.
func tableLen(vm *VM, args []Value, fname string) int64 {
	t := Arg(args, 0)
	if t.k != kindTable {
		vm.typeArgError(args, 0, fname, "table")
	}
	n, ok := vm.length(t, "").Int()
	if !ok {
		vm.Errorf("object length is not an integer")
	}
	return n
}

// geti and seti read and write t[i], raw when the table has no metatable.
func geti(vm *VM, t Value, i int64) Value {
	if tb := t.Table(); tb != nil && tb.meta == nil {
		return tb.GetInt(i)
	}
	return vm.index(t, Int(i), "")
}

func seti(vm *VM, t Value, i int64, v Value) {
	if tb := t.Table(); tb != nil && tb.meta == nil {
		tb.SetInt(i, v)
		return
	}
	vm.setIndex(t, Int(i), v, "")
}

func tabInsert(vm *VM, args []Value) []Value {
	e := tableLen(vm, args, "insert") + 1
	t := args[0]
	switch len(args) {
	case 2:
		seti(vm, t, e, args[1])
	case 3:
		pos := vm.CheckInt(args, 1, "insert")
		if uint64(pos)-1 >= uint64(e) {
			vm.ArgError(1, "insert", "position out of bounds")
		}
		for i := e; i > pos; i-- {
			seti(vm, t, i, geti(vm, t, i-1))
		}
		seti(vm, t, pos, args[2])
	default:
		vm.Errorf("wrong number of arguments to 'insert'")
	}
	return nil
}

func tabRemove(vm *VM, args []Value) []Value {
	size := tableLen(vm, args, "remove")
	t := args[0]
	pos := vm.OptInt(args, 1, "remove", size)
	if pos != size && uint64(pos)-1 > uint64(size) {
		vm.ArgError(1, "remove", "position out of bounds")
	}
	v := geti(vm, t, pos)
	for ; pos < size; pos++ {
		seti(vm, t, pos, geti(vm, t, pos+1))
	}
	seti(vm, t, pos, Nil)
	return vm.ret1(v)
}

func tabConcat(vm *VM, args []Value) []Value {
	n := tableLen(vm, args, "concat")
	sep := vm.OptString(args, 1, "concat", "")
	i := vm.OptInt(args, 2, "concat", 1)
	j := vm.OptInt(args, 3, "concat", n)
	var b strings.Builder
	for k := i; k <= j; k++ {
		v := geti(vm, args[0], k)
		if v.k != kindString && v.k != kindInt && v.k != kindFloat {
			vm.Errorf("invalid value (%s) at index %d in table for 'concat'", vm.typeName(v), k)
		}
		b.WriteString(v.String())
		if k != j {
			b.WriteString(sep)
		}
		if b.Len() > maxStringSize {
			vm.Errorf("resulting string too large")
		}
		if k == j {
			break // j may be the largest integer
		}
	}
	return vm.ret1(String(b.String()))
}

func tabPack(vm *VM, args []Value) []Value {
	t := NewTable(len(args), 1)
	for i, v := range args {
		t.SetInt(int64(i)+1, v)
	}
	t.SetString("n", Int(int64(len(args))))
	return vm.ret1(TableValue(t))
}

func tabUnpack(vm *VM, args []Value) []Value {
	t := Arg(args, 0)
	i := vm.OptInt(args, 1, "unpack", 1)
	var j int64
	if Arg(args, 2).IsNil() {
		n, ok := vm.length(t, "").Int()
		if !ok {
			vm.Errorf("object length is not an integer")
		}
		j = n
	} else {
		j = vm.CheckInt(args, 2, "unpack")
	}
	if i > j {
		return nil
	}
	if uint64(j-i) >= 1_000_000 {
		vm.Errorf("too many results to unpack")
	}
	out := make([]Value, j-i+1)
	for k := range out {
		out[k] = geti(vm, t, i+int64(k))
	}
	return out
}

func tabMove(vm *VM, args []Value) []Value {
	a1 := Arg(args, 0)
	if a1.k != kindTable {
		vm.typeArgError(args, 0, "move", "table")
	}
	f := vm.CheckInt(args, 1, "move")
	e := vm.CheckInt(args, 2, "move")
	t := vm.CheckInt(args, 3, "move")
	a2 := a1
	if len(args) > 4 && !args[4].IsNil() {
		a2 = args[4]
		if a2.k != kindTable {
			vm.typeArgError(args, 4, "move", "table")
		}
	}
	if e >= f {
		if !(f > 0 || e < math.MaxInt64+f) {
			vm.ArgError(2, "move", "too many elements to move")
		}
		if t > math.MaxInt64-(e-f) {
			vm.ArgError(3, "move", "destination wrap around")
		}
		if t > e || t <= f || (len(args) > 4 && !RawEqual(a1, a2)) {
			for i := int64(0); i <= e-f; i++ {
				seti(vm, a2, t+i, geti(vm, a1, f+i))
			}
		} else {
			for i := e - f; i >= 0; i-- {
				seti(vm, a2, t+i, geti(vm, a1, f+i))
			}
		}
	}
	return vm.ret1(a2)
}

// tabSort sorts with a merge sort: the same order on every machine and with every Go
// version, which a library sort does not promise.
func tabSort(vm *VM, args []Value) []Value {
	n := tableLen(vm, args, "sort")
	if n > 1<<31 {
		vm.ArgError(0, "sort", "array too big")
	}
	t := args[0]
	var less func(a, b Value) bool
	if cmp := Arg(args, 1); !cmp.IsNil() {
		vm.CheckFunction(args, 1, "sort")
		less = func(a, b Value) bool { return vm.call1(cmp, a, b).Truthy() }
	} else {
		less = vm.lessThan
	}
	vals := make([]Value, n)
	for i := range vals {
		vals[i] = geti(vm, t, int64(i)+1)
	}
	tmp := make([]Value, n)
	for width := 1; width < len(vals); width *= 2 {
		for lo := 0; lo < len(vals); lo += 2 * width {
			mid, hi := min(lo+width, len(vals)), min(lo+2*width, len(vals))
			i, j, k := lo, mid, lo
			for i < mid && j < hi {
				if less(vals[j], vals[i]) {
					tmp[k] = vals[j]
					j++
				} else {
					tmp[k] = vals[i]
					i++
				}
				k++
				if vm.budget--; vm.budget < 0 {
					vm.step()
				}
			}
			k += copy(tmp[k:], vals[i:mid])
			copy(tmp[k:], vals[j:hi])
		}
		vals, tmp = tmp, vals
	}
	for i, v := range vals {
		seti(vm, t, int64(i)+1, v)
	}
	return nil
}
