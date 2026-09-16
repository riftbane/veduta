package lua

import (
	"math"
	"strings"

	"github.com/riftbane/veduta/gmath"
)

// The operations of the language with their slow paths: coercions, metamethods and errors.
// The compiler inlines the common cases (two integers, two floats, a table without a
// metatable) and calls these for the rest.

type arithOp uint8

const (
	opAdd arithOp = iota
	opSub
	opMul
	opDiv
	opMod
	opPow
	opIDiv
	opBand
	opBor
	opBxor
	opShl
	opShr
	opUnm
	opBnot
)

var arithEvents = [...]string{"__add", "__sub", "__mul", "__div", "__mod", "__pow", "__idiv",
	"__band", "__bor", "__bxor", "__shl", "__shr", "__unm", "__bnot"}

func (op arithOp) bitwise() bool { return op >= opBand }

// arith applies op to a and b (b is a again for the unary operators). ad and bd describe the
// operands for an error message.
func (vm *VM) arith(op arithOp, a, b Value, ad, bd string) Value {
	if op.bitwise() {
		// Unlike arithmetic, bitwise operators do not convert strings.
		ia, oka := numToInteger(a)
		ib, okb := numToInteger(b)
		if oka && okb {
			return Int(intArith(op, ia, ib))
		}
	} else if na, ok := toNumber(a); ok {
		if nb, ok := toNumber(b); ok {
			return vm.numArith(op, na, nb)
		}
	}
	if h := vm.metaField(a, arithEvents[op]); !h.IsNil() {
		return vm.call1(h, a, b)
	}
	if h := vm.metaField(b, arithEvents[op]); !h.IsNil() {
		return vm.call1(h, a, b)
	}
	if op.bitwise() {
		if a.IsNumber() && b.IsNumber() {
			vm.Errorf("number has no integer representation")
		}
		bad, desc := b, bd
		if !a.IsNumber() {
			bad, desc = a, ad
		}
		vm.typeError(bad, "perform bitwise operation on", desc)
	}
	bad, desc := b, bd
	if _, ok := toNumber(a); !ok {
		bad, desc = a, ad
	}
	if bad.k == kindString {
		vm.typeError(bad, "perform arithmetic on", desc) // a string that is not a number
	}
	vm.typeError(bad, "perform arithmetic on", desc)
	return Nil
}

// numArith applies op to two numbers.
func (vm *VM) numArith(op arithOp, a, b Value) Value {
	if a.k == kindInt && b.k == kindInt {
		x, y := a.i(), b.i()
		switch op {
		case opAdd:
			return Int(x + y)
		case opSub:
			return Int(x - y)
		case opMul:
			return Int(x * y)
		case opMod:
			if y == 0 {
				vm.Errorf("attempt to perform 'n%%0'")
			}
			return Int(intMod(x, y))
		case opIDiv:
			if y == 0 {
				vm.Errorf("attempt to divide by zero")
			}
			return Int(intIDiv(x, y))
		case opUnm:
			return Int(-x)
		}
	}
	x, _ := a.Float()
	y, _ := b.Float()
	return Float(floatArith(op, x, y))
}

func floatArith(op arithOp, x, y float64) float64 {
	switch op {
	case opAdd:
		return x + y
	case opSub:
		return x - y
	case opMul:
		return float64(x * y)
	case opDiv:
		return x / y
	case opMod:
		return floatMod(x, y)
	case opPow:
		return gmath.Pow64(x, y)
	case opIDiv:
		return math.Floor(x / y)
	case opUnm:
		return -x
	}
	return 0
}

func intArith(op arithOp, x, y int64) int64 {
	switch op {
	case opBand:
		return x & y
	case opBor:
		return x | y
	case opBxor:
		return x ^ y
	case opShl:
		return shiftLeft(x, y)
	case opShr:
		return shiftLeft(x, -y)
	case opBnot:
		return ^x
	}
	return 0
}

// numToInteger converts a number (not a string) to an integer.
func numToInteger(v Value) (int64, bool) {
	switch v.k {
	case kindInt:
		return v.i(), true
	case kindFloat:
		return floatToInt(v.f())
	}
	return 0, false
}

// intMod is Lua's integer %: the result has the sign of the divisor.
func intMod(m, n int64) int64 {
	if n == -1 {
		return 0 // avoids overflowing on the smallest integer
	}
	r := m % n
	if r != 0 && (r^n) < 0 {
		r += n
	}
	return r
}

// intIDiv is Lua's integer //: the quotient rounded towards minus infinity.
func intIDiv(m, n int64) int64 {
	if n == -1 {
		return -m // wraps for the smallest integer, as in Lua
	}
	q := m / n
	if (m^n) < 0 && m%n != 0 {
		q--
	}
	return q
}

// floatMod is Lua's float %.
func floatMod(a, b float64) float64 {
	m := math.Mod(a, b)
	if m > 0 && b < 0 || m < 0 && b != m && b > 0 {
		m += b
	}
	return m
}

// shiftLeft shifts x left by y bits, right (logically) when y is negative.
func shiftLeft(x, y int64) int64 {
	switch {
	case y <= -64 || y >= 64:
		return 0
	case y < 0:
		return int64(uint64(x) >> uint(-y))
	}
	return int64(uint64(x) << uint(y))
}

// call1 calls f and returns its first result.
func (vm *VM) call1(f Value, args ...Value) Value {
	base := vm.top
	vm.ensure(base + len(args))
	copy(vm.stack[base:], args)
	vm.top = base + len(args)
	res := vm.call(f, vm.stack[base:vm.top], "")
	vm.top = base
	if len(res) == 0 {
		return Nil
	}
	return res[0]
}

// equal is ==, with __eq for two tables or two userdata.
func (vm *VM) equal(a, b Value) bool {
	if RawEqual(a, b) {
		return true
	}
	if a.k != b.k || a.k != kindTable && a.k != kindUserdata {
		return false
	}
	h := vm.metaField(a, "__eq")
	if h.IsNil() {
		h = vm.metaField(b, "__eq")
	}
	return !h.IsNil() && vm.call1(h, a, b).Truthy()
}

// lessThan is <.
func (vm *VM) lessThan(a, b Value) bool {
	if a.IsNumber() && b.IsNumber() {
		return numLess(a, b)
	}
	if a.k == kindString && b.k == kindString {
		return a.s() < b.s()
	}
	return vm.compareMeta("__lt", a, b)
}

// lessEqual is <=.
func (vm *VM) lessEqual(a, b Value) bool {
	if a.IsNumber() && b.IsNumber() {
		return numLessEqual(a, b)
	}
	if a.k == kindString && b.k == kindString {
		return a.s() <= b.s()
	}
	return vm.compareMeta("__le", a, b)
}

func (vm *VM) compareMeta(event string, a, b Value) bool {
	h := vm.metaField(a, event)
	if h.IsNil() {
		h = vm.metaField(b, event)
	}
	if h.IsNil() {
		ta, tb := vm.typeName(a), vm.typeName(b)
		if ta == tb {
			vm.Errorf("attempt to compare two %s values", ta)
		}
		vm.Errorf("attempt to compare %s with %s", ta, tb)
	}
	return vm.call1(h, a, b).Truthy()
}

// intFitsFloat reports whether i converts to a float exactly.
func intFitsFloat(i int64) bool { return uint64(i)+(1<<53) <= 2<<53 }

func numLess(a, b Value) bool {
	switch {
	case a.k == kindInt && b.k == kindInt:
		return a.i() < b.i()
	case a.k == kindFloat && b.k == kindFloat:
		return a.f() < b.f()
	case a.k == kindInt:
		i, f := a.i(), b.f()
		if intFitsFloat(i) || math.IsNaN(f) {
			return float64(i) < f
		}
		if f >= 0x1p63 {
			return true
		}
		if f < -0x1p63 {
			return false
		}
		return i < int64(math.Ceil(f))
	}
	f, i := a.f(), b.i()
	if intFitsFloat(i) || math.IsNaN(f) {
		return f < float64(i)
	}
	if f >= 0x1p63 {
		return false
	}
	if f < -0x1p63 {
		return true
	}
	return int64(math.Floor(f)) < i
}

func numLessEqual(a, b Value) bool {
	switch {
	case a.k == kindInt && b.k == kindInt:
		return a.i() <= b.i()
	case a.k == kindFloat && b.k == kindFloat:
		return a.f() <= b.f()
	case a.k == kindInt:
		i, f := a.i(), b.f()
		if intFitsFloat(i) || math.IsNaN(f) {
			return float64(i) <= f
		}
		if f >= 0x1p63 {
			return true
		}
		if f < -0x1p63 {
			return false
		}
		return i <= int64(math.Floor(f))
	}
	f, i := a.f(), b.i()
	if intFitsFloat(i) || math.IsNaN(f) {
		return f <= float64(i)
	}
	if f >= 0x1p63 {
		return false
	}
	if f < -0x1p63 {
		return true
	}
	return int64(math.Ceil(f)) <= i
}

// length is #v.
func (vm *VM) length(v Value, desc string) Value {
	switch v.k {
	case kindString:
		return Int(int64(len(v.s())))
	case kindTable:
		t := v.p.(*Table)
		if t.meta == nil {
			return Int(int64(t.Len()))
		}
		if h := t.meta.GetString("__len"); !h.IsNil() {
			return vm.call1(h, v)
		}
		return Int(int64(t.Len()))
	}
	if h := vm.metaField(v, "__len"); !h.IsNil() {
		return vm.call1(h, v)
	}
	vm.typeError(v, "get length of", desc)
	return Nil
}

// concat is a .. b .. …, evaluated right to left as Lua does.
func (vm *VM) concat(vals []Value, descs []string) Value {
	plain := true
	for _, v := range vals {
		if v.k != kindString && v.k != kindInt && v.k != kindFloat {
			plain = false
			break
		}
	}
	if plain {
		var b strings.Builder
		for _, v := range vals {
			b.WriteString(v.String())
		}
		return String(b.String())
	}
	acc, accDesc := vals[len(vals)-1], descs[len(vals)-1]
	for i := len(vals) - 2; i >= 0; i-- {
		acc = vm.concat2(vals[i], acc, descs[i], accDesc)
		accDesc = ""
	}
	return acc
}

func (vm *VM) concat2(a, b Value, ad, bd string) Value {
	sa := a.k == kindString || a.k == kindInt || a.k == kindFloat
	sb := b.k == kindString || b.k == kindInt || b.k == kindFloat
	if sa && sb {
		return String(a.String() + b.String())
	}
	h := vm.metaField(a, "__concat")
	if h.IsNil() {
		h = vm.metaField(b, "__concat")
	}
	if h.IsNil() {
		if !sa {
			vm.typeError(a, "concatenate", ad)
		}
		vm.typeError(b, "concatenate", bd)
	}
	return vm.call1(h, a, b)
}

// index is o[k] with __index.
func (vm *VM) index(o, k Value, desc string) Value {
	for loop := 0; loop < 2000; loop++ {
		var h Value
		if t, ok := o.p.(*Table); ok && o.k == kindTable {
			v := t.Get(k)
			if !v.IsNil() || t.meta == nil {
				return v
			}
			h = t.meta.GetString("__index")
			if h.IsNil() {
				return Nil
			}
		} else {
			h = vm.metaField(o, "__index")
			if h.IsNil() {
				if s, ok := k.Str(); ok && desc == "" {
					desc = "field '" + s + "'"
				}
				vm.typeError(o, "index", desc)
			}
		}
		if h.k == kindFunction {
			return vm.call1(h, o, k)
		}
		o, desc = h, ""
	}
	vm.Errorf("'__index' chain too long; possible loop")
	return Nil
}

// setIndex is o[k] = v with __newindex.
func (vm *VM) setIndex(o, k, v Value, desc string) {
	for loop := 0; loop < 2000; loop++ {
		var h Value
		if t, ok := o.p.(*Table); ok && o.k == kindTable {
			if t.meta == nil || !t.Get(k).IsNil() {
				vm.rawSet(t, k, v)
				return
			}
			h = t.meta.GetString("__newindex")
			if h.IsNil() {
				vm.rawSet(t, k, v)
				return
			}
		} else {
			h = vm.metaField(o, "__newindex")
			if h.IsNil() {
				vm.typeError(o, "index", desc)
			}
		}
		if h.k == kindFunction {
			base := vm.top
			vm.ensure(base + 3)
			vm.stack[base], vm.stack[base+1], vm.stack[base+2] = o, k, v
			vm.top = base + 3
			vm.call(h, vm.stack[base:base+3], "")
			vm.top = base
			return
		}
		o, desc = h, ""
	}
	vm.Errorf("'__newindex' chain too long; possible loop")
}

// rawSet is t[k] = v, with Lua errors for a nil or NaN key.
func (vm *VM) rawSet(t *Table, k, v Value) {
	if msg := keyError(k); msg != "" {
		vm.Errorf("%s", msg)
	}
	t.Set(k, v)
}

// tostring converts v to a string as tostring does, with __tostring and __name.
func (vm *VM) tostring(v Value) Value {
	if h := vm.metaField(v, "__tostring"); !h.IsNil() {
		r := vm.call1(h, v)
		if r.k != kindString {
			if r.IsNumber() {
				return String(r.String())
			}
			vm.Errorf("'__tostring' must return a string")
		}
		return r
	}
	switch v.k {
	case kindString:
		return v
	case kindTable, kindUserdata:
		if m := vm.metatable(v); m != nil {
			if n, ok := m.GetString("__name").Str(); ok {
				s := v.String()
				return String(n + s[strings.IndexByte(s, ':'):])
			}
		}
	}
	return String(v.String())
}
