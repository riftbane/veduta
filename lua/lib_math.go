package lua

import (
	"math"

	"github.com/riftbane/veduta/v2/gmath"
)

// The math library, on gmath's deterministic functions. math.random draws from a Random
// the host provides (the engine's seeded generator), or from the VM's own xoshiro256**.

// Random is a source of random numbers: sim.RNG is one.
type Random interface {
	Uint64() uint64
	Seed(seed uint64)
}

// xoshiro is the VM's own generator, used until the host sets one.
type xoshiro struct{ s [4]uint64 }

func (x *xoshiro) Uint64() uint64 {
	s := &x.s
	result := rotl(s[1]*5, 7) * 9
	t := s[1] << 17
	s[2] ^= s[0]
	s[3] ^= s[1]
	s[1] ^= s[2]
	s[0] ^= s[3]
	s[2] ^= t
	s[3] = rotl(s[3], 45)
	return result
}

func (x *xoshiro) Seed(seed uint64) {
	for i := range x.s {
		seed += 0x9e3779b97f4a7c15
		z := seed
		z = (z ^ z>>30) * 0xbf58476d1ce4e5b9
		z = (z ^ z>>27) * 0x94d049bb133111eb
		x.s[i] = z ^ z>>31
	}
}

func rotl(x uint64, n uint) uint64 { return x<<n | x>>(64-n) }

// SetRandom makes math.random draw from r.
func (vm *VM) SetRandom(r Random) { vm.random = r }

func openMath(vm *VM) {
	x := &xoshiro{}
	x.Seed(0)
	vm.random = x
	lib := NewTable(0, 32)
	setFuncs(lib, "math.", map[string]GoFunction{
		"abs": mathAbs, "acos": mathUnary(gmath.Acos64), "asin": mathUnary(gmath.Asin64), "atan": mathAtan,
		"ceil": mathCeil, "cos": mathUnary(gmath.Cos64), "exp": mathUnary(gmath.Exp64), "floor": mathFloor,
		"fmod": mathFmod, "log": mathLog, "max": mathMax, "min": mathMin, "modf": mathModf,
		"random": mathRandom, "randomseed": mathRandomseed, "sin": mathUnary(gmath.Sin64),
		"sqrt": mathUnary(math.Sqrt), "tan": mathUnary(gmath.Tan64), "tointeger": mathToInteger,
		"type": mathType, "ult": mathUlt, "deg": mathUnary(mathDeg), "rad": mathUnary(mathRad),
	})
	lib.SetString("pi", Float(math.Pi))
	lib.SetString("huge", Float(math.Inf(1)))
	lib.SetString("maxinteger", Int(math.MaxInt64))
	lib.SetString("mininteger", Int(math.MinInt64))
	vm.globals.SetString("math", TableValue(lib))
}

// piFloat is π as a float64 variable: C Lua divides by the double π at run time, and so must
// these, where a constant expression would divide by Go's exact π and round differently.
var piFloat = math.Pi

func mathDeg(x float64) float64 { return x * (180 / piFloat) }
func mathRad(x float64) float64 { return x * (piFloat / 180) }

func mathUnary(f func(float64) float64) GoFunction {
	return func(vm *VM, args []Value) []Value {
		return vm.ret1(Float(f(vm.CheckFloat(args, 0, "math function"))))
	}
}

func mathAbs(vm *VM, args []Value) []Value {
	n := vm.CheckNumber(args, 0, "abs")
	if n.k == kindInt {
		if i := n.i(); i < 0 {
			return vm.ret1(Int(-i))
		}
		return vm.ret1(n)
	}
	return vm.ret1(Float(math.Abs(n.f())))
}

func mathAtan(vm *VM, args []Value) []Value {
	y := vm.CheckFloat(args, 0, "atan")
	x := 1.0
	if !Arg(args, 1).IsNil() {
		x = vm.CheckFloat(args, 1, "atan")
	}
	return vm.ret1(Float(gmath.Atan264(y, x)))
}

// floatOrInt returns f as an integer when it has an integral value in range.
func floatOrInt(f float64) Value {
	if i, ok := floatToInt(f); ok {
		return Int(i)
	}
	return Float(f)
}

func mathFloor(vm *VM, args []Value) []Value {
	n := vm.CheckNumber(args, 0, "floor")
	if n.k == kindInt {
		return vm.ret1(n)
	}
	return vm.ret1(floatOrInt(math.Floor(n.f())))
}

func mathCeil(vm *VM, args []Value) []Value {
	n := vm.CheckNumber(args, 0, "ceil")
	if n.k == kindInt {
		return vm.ret1(n)
	}
	return vm.ret1(floatOrInt(math.Ceil(n.f())))
}

func mathFmod(vm *VM, args []Value) []Value {
	a := vm.CheckNumber(args, 0, "fmod")
	b := vm.CheckNumber(args, 1, "fmod")
	if a.k == kindInt && b.k == kindInt {
		d := b.i()
		switch {
		case d == 0:
			vm.ArgError(1, "fmod", "zero")
		case d == -1:
			return vm.ret1(Int(0))
		}
		return vm.ret1(Int(a.i() % d))
	}
	x, _ := a.Float()
	y, _ := b.Float()
	return vm.ret1(Float(math.Mod(x, y)))
}

func mathModf(vm *VM, args []Value) []Value {
	n := vm.CheckNumber(args, 0, "modf")
	if n.k == kindInt {
		return vm.Ret(n, Float(0))
	}
	f := n.f()
	ip := math.Floor(f)
	if f < 0 {
		ip = math.Ceil(f)
	}
	frac := 0.0
	if ip != f {
		frac = f - ip
	}
	return vm.Ret(floatOrInt(ip), Float(frac))
}

func mathLog(vm *VM, args []Value) []Value {
	x := vm.CheckFloat(args, 0, "log")
	if Arg(args, 1).IsNil() {
		return vm.ret1(Float(gmath.Log64(x)))
	}
	base := vm.CheckFloat(args, 1, "log")
	if base == 2 {
		if frac, exp := math.Frexp(x); frac == 0.5 {
			return vm.ret1(Float(float64(exp - 1))) // an exact power of two
		}
	}
	r := gmath.Log64(x) / gmath.Log64(base)
	if rounded := math.Round(r); rounded != r && math.Abs(rounded-r) < 1e-9 && gmath.Pow64(base, rounded) == x {
		r = rounded // log(1000, 10) is 3, not 2.9999999999999996
	}
	return vm.ret1(Float(r))
}

func mathMax(vm *VM, args []Value) []Value {
	best := vm.CheckNumber(args, 0, "max")
	for i := 1; i < len(args); i++ {
		if v := vm.CheckNumber(args, i, "max"); numLess(best, v) {
			best = v
		}
	}
	return vm.ret1(best)
}

func mathMin(vm *VM, args []Value) []Value {
	best := vm.CheckNumber(args, 0, "min")
	for i := 1; i < len(args); i++ {
		if v := vm.CheckNumber(args, i, "min"); numLess(v, best) {
			best = v
		}
	}
	return vm.ret1(best)
}

func mathToInteger(vm *VM, args []Value) []Value {
	vm.CheckAny(args, 0, "tointeger")
	if i, ok := toInteger(args[0]); ok {
		return vm.ret1(Int(i))
	}
	return vm.ret1(Nil)
}

func mathType(vm *VM, args []Value) []Value {
	switch vm.CheckAny(args, 0, "type").k {
	case kindInt:
		return vm.ret1(String("integer"))
	case kindFloat:
		return vm.ret1(String("float"))
	}
	return vm.ret1(Nil)
}

func mathUlt(vm *VM, args []Value) []Value {
	return vm.ret1(Bool(uint64(vm.CheckInt(args, 0, "ult")) < uint64(vm.CheckInt(args, 1, "ult"))))
}

func mathRandom(vm *VM, args []Value) []Value {
	rv := vm.random.Uint64()
	var low, up int64
	switch len(args) {
	case 0:
		return vm.ret1(Float(float64(rv>>11) * 0x1p-53))
	case 1:
		low, up = 1, vm.CheckInt(args, 0, "random")
		if up == 0 {
			return vm.ret1(Int(int64(rv))) // math.random(0): all bits random
		}
	case 2:
		low, up = vm.CheckInt(args, 0, "random"), vm.CheckInt(args, 1, "random")
	default:
		vm.Errorf("wrong number of arguments")
	}
	if low > up {
		vm.ArgError(0, "random", "interval is empty")
	}
	return vm.ret1(Int(low + int64(project(rv, uint64(up)-uint64(low), vm.random))))
}

// project maps a random number into [0, n] without bias, as Lua does.
func project(ran, n uint64, r Random) uint64 {
	if n&(n+1) == 0 {
		return ran & n
	}
	lim := n
	for _, s := range []uint{1, 2, 4, 8, 16, 32} {
		lim |= lim >> s
	}
	for ran &= lim; ran > n; ran = r.Uint64() & lim {
	}
	return ran
}

func mathRandomseed(vm *VM, args []Value) []Value {
	var seed uint64
	if len(args) > 0 {
		n := vm.CheckNumber(args, 0, "randomseed")
		if n.k == kindInt {
			seed = uint64(n.i())
		} else {
			seed = math.Float64bits(n.f())
		}
	}
	vm.random.Seed(seed)
	return nil
}
