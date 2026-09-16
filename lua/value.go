// Package lua is a Lua 5.4 interpreter in pure Go: the language games are written in on
// the Veduta console.
//
// Source is compiled into a tree of Go closures, which run on a value stack owned by the
// VM. The interpreter is deterministic on every architecture: integers wrap as in Lua,
// float arithmetic never fuses a multiply-add (each operation is its own function), table
// traversal follows insertion order, and the math library is built on gmath rather than
// on the math package, whose functions differ between amd64 and arm64.
//
// Differences from Lua 5.4: no goto, no to-be-closed variables, no weak tables and no
// finalizers (the Go collector owns memory); no io, os, debug or package libraries.
package lua

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Type is the type of a Lua value, as the type function names it.
type Type uint8

// The Lua types.
const (
	TypeNil Type = iota
	TypeBoolean
	TypeNumber
	TypeString
	TypeTable
	TypeFunction
	TypeUserdata
)

var typeNames = [...]string{"nil", "boolean", "number", "string", "table", "function", "userdata"}

// String returns the type's name.
func (t Type) String() string { return typeNames[t] }

// kind is a value's representation: numbers are integers or floats.
type kind uint8

const (
	kindNil kind = iota
	kindBool
	kindInt
	kindFloat
	kindString
	kindTable
	kindFunction
	kindUserdata
)

// Value is a Lua value. The zero Value is nil. Values are comparable with ==, which is Lua's
// raw equality except that an integer and a float of the same value are different Values
// (use RawEqual).
type Value struct {
	p any    // string, *Table, *Function, *Userdata
	n uint64 // boolean (0 or 1), int64 bits or float64 bits
	k kind
}

// Nil is the nil value.
var Nil = Value{}

// True and False are the boolean values.
var (
	True  = Value{k: kindBool, n: 1}
	False = Value{k: kindBool}
)

// Bool returns b as a Lua boolean.
func Bool(b bool) Value {
	if b {
		return True
	}
	return False
}

// Int returns an integer.
func Int(i int64) Value { return Value{k: kindInt, n: uint64(i)} }

// Float returns a float.
func Float(f float64) Value { return Value{k: kindFloat, n: math.Float64bits(f)} }

// String returns a string.
func String(s string) Value { return Value{k: kindString, p: s} }

// TableValue returns t as a value.
func TableValue(t *Table) Value {
	if t == nil {
		return Nil
	}
	return Value{k: kindTable, p: t}
}

// FunctionValue returns f as a value.
func FunctionValue(f *Function) Value {
	if f == nil {
		return Nil
	}
	return Value{k: kindFunction, p: f}
}

// UserdataValue returns u as a value.
func UserdataValue(u *Userdata) Value {
	if u == nil {
		return Nil
	}
	return Value{k: kindUserdata, p: u}
}

// Type returns the value's type.
func (v Value) Type() Type {
	switch v.k {
	case kindNil:
		return TypeNil
	case kindBool:
		return TypeBoolean
	case kindInt, kindFloat:
		return TypeNumber
	case kindString:
		return TypeString
	case kindTable:
		return TypeTable
	case kindFunction:
		return TypeFunction
	}
	return TypeUserdata
}

// IsNil reports whether v is nil.
func (v Value) IsNil() bool { return v.k == kindNil }

// Truthy reports whether v counts as true: everything but nil and false.
func (v Value) Truthy() bool { return v.k > kindBool || v.k == kindBool && v.n != 0 }

// IsInt reports whether v is an integer (not a float with an integral value).
func (v Value) IsInt() bool { return v.k == kindInt }

// IsFloat reports whether v is a float.
func (v Value) IsFloat() bool { return v.k == kindFloat }

// IsNumber reports whether v is a number.
func (v Value) IsNumber() bool { return v.k == kindInt || v.k == kindFloat }

// Int returns v's integer and whether v is an integer.
func (v Value) Int() (int64, bool) { return int64(v.n), v.k == kindInt }

// Float returns v as a float and whether v is a number.
func (v Value) Float() (float64, bool) {
	switch v.k {
	case kindInt:
		return float64(int64(v.n)), true
	case kindFloat:
		return math.Float64frombits(v.n), true
	}
	return 0, false
}

// Bool returns v's boolean and whether v is a boolean.
func (v Value) Bool() (bool, bool) { return v.n != 0, v.k == kindBool }

// Str returns v's string and whether v is a string.
func (v Value) Str() (string, bool) {
	s, ok := v.p.(string)
	return s, ok && v.k == kindString
}

// Table returns v's table, or nil.
func (v Value) Table() *Table {
	t, _ := v.p.(*Table)
	return t
}

// Function returns v's function, or nil.
func (v Value) Function() *Function {
	f, _ := v.p.(*Function)
	return f
}

// Userdata returns v's userdata, or nil.
func (v Value) Userdata() *Userdata {
	u, _ := v.p.(*Userdata)
	return u
}

func (v Value) i() int64   { return int64(v.n) }
func (v Value) f() float64 { return math.Float64frombits(v.n) }
func (v Value) s() string  { return v.p.(string) }

// String formats v as tostring does without metamethods: numbers as Lua prints them,
// strings unquoted, other values by type and identity.
func (v Value) String() string {
	switch v.k {
	case kindNil:
		return "nil"
	case kindBool:
		if v.n != 0 {
			return "true"
		}
		return "false"
	case kindInt:
		return strconv.FormatInt(v.i(), 10)
	case kindFloat:
		return formatFloat(v.f())
	case kindString:
		return v.s()
	case kindTable:
		return fmt.Sprintf("table: %p", v.p)
	case kindFunction:
		f := v.p.(*Function)
		if f.gofn != nil {
			return fmt.Sprintf("builtin: %p", f)
		}
		return fmt.Sprintf("function: %p", f)
	}
	return fmt.Sprintf("userdata: %p", v.p)
}

// formatFloat formats f as Lua's %.14g, with ".0" added to integral values so that a float
// never reads as an integer.
func formatFloat(f float64) string {
	switch {
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	case f != f:
		return "nan" // the sign of a NaN depends on the processor: never show it
	}
	s := strconv.FormatFloat(f, 'g', 14, 64)
	if !strings.ContainsAny(s, ".en") {
		s += ".0"
	}
	return s
}

// RawEqual reports whether a and b are equal without metamethods: an integer equals a float
// of the same value.
func RawEqual(a, b Value) bool {
	if a.k == b.k {
		switch a.k {
		case kindFloat:
			return a.f() == b.f()
		case kindString:
			return a.s() == b.s()
		}
		return a == b
	}
	switch {
	case a.k == kindInt && b.k == kindFloat:
		return floatEqualsInt(b.f(), a.i())
	case a.k == kindFloat && b.k == kindInt:
		return floatEqualsInt(a.f(), b.i())
	}
	return false
}

func floatEqualsInt(f float64, i int64) bool {
	j, ok := floatToInt(f)
	return ok && i == j
}

// floatToInt returns f as an integer when it is integral and in range.
func floatToInt(f float64) (int64, bool) {
	if f >= -0x1p63 && f < 0x1p63 && f == math.Floor(f) {
		return int64(f), true
	}
	return 0, false
}

// normKey turns a float key with an integral value into an integer key, as Lua does.
func normKey(k Value) Value {
	if k.k == kindFloat {
		if i, ok := floatToInt(k.f()); ok {
			return Int(i)
		}
	}
	return k
}

// toNumber converts v to a number: numbers as they are, strings by Lua's syntax.
func toNumber(v Value) (Value, bool) {
	switch v.k {
	case kindInt, kindFloat:
		return v, true
	case kindString:
		return parseNumber(v.s())
	}
	return Nil, false
}

// toInteger converts v to an integer: integers, floats with an integral value, and strings
// that convert to either.
func toInteger(v Value) (int64, bool) {
	switch v.k {
	case kindInt:
		return v.i(), true
	case kindFloat:
		return floatToInt(v.f())
	case kindString:
		if n, ok := parseNumber(v.s()); ok {
			return toInteger(n)
		}
	}
	return 0, false
}

// parseNumber reads a number the way the Lua lexer does, with surrounding whitespace: a
// decimal or hexadecimal integer (a decimal one too large becomes a float, a hexadecimal
// one wraps around), or a float, decimal or hexadecimal, with an optional exponent.
func parseNumber(s string) (Value, bool) {
	s = strings.TrimSpace(s)
	neg := false
	body := s
	if body != "" && (body[0] == '-' || body[0] == '+') {
		neg = body[0] == '-'
		body = body[1:]
	}
	if body == "" {
		return Nil, false
	}
	if len(body) > 2 && body[0] == '0' && (body[1] == 'x' || body[1] == 'X') {
		return parseHex(body[2:], neg)
	}
	isFloat := false
	for i := 0; i < len(body); i++ {
		c := body[i]
		switch {
		case c >= '0' && c <= '9':
		case c == '.' || c == 'e' || c == 'E':
			isFloat = true
		case (c == '+' || c == '-') && i > 0 && (body[i-1] == 'e' || body[i-1] == 'E'):
		default:
			return Nil, false // rejects inf, nan, underscores and the like
		}
	}
	if !isFloat {
		if u, err := strconv.ParseUint(body, 10, 64); err == nil {
			if neg {
				return Int(int64(-u)), u <= 1<<63
			}
			if u < 1<<63 {
				return Int(int64(u)), true
			}
		}
	}
	f, err := strconv.ParseFloat(body, 64)
	if err != nil {
		if ne, ok := err.(*strconv.NumError); !ok || ne.Err != strconv.ErrRange {
			return Nil, false
		}
	}
	if neg {
		f = -f
	}
	return Float(f), true
}

// parseHex reads the digits of a hexadecimal number after 0x: an integer wraps around, a
// float (with a point or a binary exponent) is rounded correctly.
func parseHex(s string, neg bool) (Value, bool) {
	digits, isFloat, dot := 0, false, false
	i := 0
	for ; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
			digits++
		case c == '.' && !dot:
			dot, isFloat = true, true
		default:
			goto exponent
		}
	}
exponent:
	if digits == 0 {
		return Nil, false
	}
	if i < len(s) {
		if s[i] != 'p' && s[i] != 'P' {
			return Nil, false
		}
		if _, err := strconv.Atoi(s[i+1:]); err != nil {
			return Nil, false
		}
		isFloat = true
	}
	if isFloat {
		text := "0x" + s
		if i == len(s) {
			text += "p0"
		}
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			if ne, ok := err.(*strconv.NumError); !ok || ne.Err != strconv.ErrRange {
				return Nil, false
			}
		}
		if neg {
			f = -f
		}
		return Float(f), true
	}
	var mant uint64
	for j := 0; j < len(s); j++ {
		c := s[j]
		switch {
		case c >= '0' && c <= '9':
			mant = mant<<4 | uint64(c-'0')
		case c >= 'a' && c <= 'f':
			mant = mant<<4 | uint64(c-'a'+10)
		default:
			mant = mant<<4 | uint64(c-'A'+10)
		}
	}
	if neg {
		mant = -mant
	}
	return Int(int64(mant)), true
}
