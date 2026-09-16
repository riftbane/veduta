package lua

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// The string library, and the metatable that makes ("x"):upper() work. Missing: pack,
// unpack, packsize and dump.

// maxStringSize bounds the strings rep and format build.
const maxStringSize = 64 << 20

func openString(vm *VM) {
	lib := NewTable(0, 16)
	setFuncs(lib, "string.", map[string]GoFunction{
		"byte": strByte, "char": strChar, "find": strFind, "format": strFormat, "gmatch": strGmatch,
		"gsub": strGsub, "len": strLen, "lower": strLower, "match": strMatch, "rep": strRep,
		"reverse": strReverse, "sub": strSub, "upper": strUpper,
	})
	vm.globals.SetString("string", TableValue(lib))
	meta := NewTable(0, 1)
	meta.SetString("__index", TableValue(lib))
	vm.stringMeta = meta
}

// posRelative turns a possibly negative position into one counted from 1 (Lua's posrelatI).
func posRelative(pos int64, n int) int64 {
	switch {
	case pos > 0:
		return pos
	case pos == 0:
		return 1
	case pos < -int64(n):
		return 1
	}
	return int64(n) + pos + 1
}

// endPosition clips an end position to the string (Lua's getendpos).
func endPosition(pos int64, n int) int64 {
	switch {
	case pos > int64(n):
		return int64(n)
	case pos >= 0:
		return pos
	case pos < -int64(n):
		return 0
	}
	return int64(n) + pos + 1
}

func strLen(vm *VM, args []Value) []Value {
	return vm.ret1(Int(int64(len(vm.CheckString(args, 0, "len")))))
}

func strSub(vm *VM, args []Value) []Value {
	s := vm.CheckString(args, 0, "sub")
	start := posRelative(vm.CheckInt(args, 1, "sub"), len(s))
	end := endPosition(vm.OptInt(args, 2, "sub", -1), len(s))
	if start > end {
		return vm.ret1(String(""))
	}
	return vm.ret1(String(s[start-1 : end]))
}

func strUpper(vm *VM, args []Value) []Value {
	s := []byte(vm.CheckString(args, 0, "upper"))
	for i, c := range s {
		if isLower(c) {
			s[i] = c - ('a' - 'A')
		}
	}
	return vm.ret1(String(string(s)))
}

func strLower(vm *VM, args []Value) []Value {
	s := []byte(vm.CheckString(args, 0, "lower"))
	for i, c := range s {
		if isUpper(c) {
			s[i] = c + ('a' - 'A')
		}
	}
	return vm.ret1(String(string(s)))
}

func strReverse(vm *VM, args []Value) []Value {
	s := []byte(vm.CheckString(args, 0, "reverse"))
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
	return vm.ret1(String(string(s)))
}

func strRep(vm *VM, args []Value) []Value {
	s := vm.CheckString(args, 0, "rep")
	n := vm.CheckInt(args, 1, "rep")
	sep := vm.OptString(args, 2, "rep", "")
	if n <= 0 {
		return vm.ret1(String(""))
	}
	if int64(len(s)+len(sep)) > 0 && n > maxStringSize/int64(len(s)+len(sep)) {
		vm.Errorf("resulting string too large")
	}
	if sep == "" {
		return vm.ret1(String(strings.Repeat(s, int(n))))
	}
	var b strings.Builder
	for i := int64(0); i < n; i++ {
		if i > 0 {
			b.WriteString(sep)
		}
		b.WriteString(s)
	}
	return vm.ret1(String(b.String()))
}

func strByte(vm *VM, args []Value) []Value {
	s := vm.CheckString(args, 0, "byte")
	i := posRelative(vm.OptInt(args, 1, "byte", 1), len(s))
	j := endPosition(vm.OptInt(args, 2, "byte", i), len(s))
	if i > j {
		return nil
	}
	out := make([]Value, j-i+1)
	for k := range out {
		out[k] = Int(int64(s[i-1+int64(k)]))
	}
	return out
}

func strChar(vm *VM, args []Value) []Value {
	b := make([]byte, len(args))
	for i := range args {
		c := vm.CheckInt(args, i, "char")
		if uint64(c) > 255 {
			vm.ArgError(i, "char", "value out of range")
		}
		b[i] = byte(c)
	}
	return vm.ret1(String(string(b)))
}

func strFind(vm *VM, args []Value) []Value  { return strFindAux(vm, args, true) }
func strMatch(vm *VM, args []Value) []Value { return strFindAux(vm, args, false) }

func strFindAux(vm *VM, args []Value, find bool) []Value {
	fname := "match"
	if find {
		fname = "find"
	}
	s := vm.CheckString(args, 0, fname)
	p := vm.CheckString(args, 1, fname)
	init := posRelative(vm.OptInt(args, 2, fname, 1), len(s)) - 1
	if init > int64(len(s)) {
		return vm.ret1(Nil)
	}
	if find && (Arg(args, 3).Truthy() || !strings.ContainsAny(p, "^$*+?.([%-")) {
		if i := strings.Index(s[init:], p); i >= 0 {
			start := init + int64(i)
			return vm.Ret(Int(start+1), Int(start+int64(len(p))))
		}
		return vm.ret1(Nil)
	}
	anchor := strings.HasPrefix(p, "^")
	if anchor {
		p = p[1:]
	}
	ms := &matchState{vm: vm, src: s, pat: p}
	for s1 := int(init); ; s1++ {
		ms.reset()
		if e := ms.match(s1, 0); e != -1 {
			if find {
				caps := ms.captures(s1, e, false)
				return append([]Value{Int(int64(s1) + 1), Int(int64(e))}, caps...)
			}
			return ms.captures(s1, e, true)
		}
		if s1 >= len(s) || anchor {
			return vm.ret1(Nil)
		}
	}
}

func strGmatch(vm *VM, args []Value) []Value {
	s := vm.CheckString(args, 0, "gmatch")
	p := vm.CheckString(args, 1, "gmatch")
	pos := int(posRelative(vm.OptInt(args, 2, "gmatch", 1), len(s)) - 1)
	if pos > len(s) {
		pos = len(s) + 1
	}
	last := -1
	iter := NewFunction("gmatch_iterator", func(vm *VM, _ []Value) []Value {
		ms := &matchState{vm: vm, src: s, pat: p}
		for ; pos <= len(s); pos++ {
			ms.reset()
			if e := ms.match(pos, 0); e != -1 && e != last {
				start := pos
				pos, last = e, e
				return ms.captures(start, e, true)
			}
		}
		return vm.ret1(Nil)
	})
	return vm.ret1(FunctionValue(iter))
}

func strGsub(vm *VM, args []Value) []Value {
	src := vm.CheckString(args, 0, "gsub")
	p := vm.CheckString(args, 1, "gsub")
	repl := Arg(args, 2)
	switch repl.k {
	case kindString, kindInt, kindFloat, kindTable, kindFunction:
	default:
		vm.typeArgError(args, 2, "gsub", "string/function/table")
	}
	maxN := vm.OptInt(args, 3, "gsub", int64(len(src))+1)
	anchor := strings.HasPrefix(p, "^")
	if anchor {
		p = p[1:]
	}
	ms := &matchState{vm: vm, src: src, pat: p}
	var b strings.Builder
	n := int64(0)
	s, last := 0, -1
	changed := false
	for n < maxN {
		ms.reset()
		if e := ms.match(s, 0); e != -1 && e != last {
			n++
			changed = gsubValue(vm, ms, &b, s, e, repl) || changed
			s, last = e, e
		} else if s < len(src) {
			b.WriteByte(src[s])
			s++
		} else {
			break
		}
		if anchor {
			break
		}
	}
	if !changed {
		return vm.Ret(args[0], Int(n))
	}
	b.WriteString(src[s:])
	return vm.Ret(String(b.String()), Int(n))
}

// gsubValue appends the replacement of the match src[s:e] and reports whether it differs
// from the match.
func gsubValue(vm *VM, ms *matchState, b *strings.Builder, s, e int, repl Value) bool {
	var v Value
	switch repl.k {
	case kindFunction:
		caps := ms.captures(s, e, true)
		res := vm.call(repl, caps, "")
		if len(res) > 0 {
			v = res[0]
		}
	case kindTable:
		v = vm.index(repl, ms.oneCapture(0, s, e), "")
	default:
		r := repl.String()
		for i := 0; i < len(r); i++ {
			c := r[i]
			if c != '%' {
				b.WriteByte(c)
				continue
			}
			i++
			switch d := byte(0); {
			case i < len(r) && r[i] == '%':
				b.WriteByte('%')
			case i < len(r) && r[i] == '0':
				b.WriteString(ms.src[s:e])
			case i < len(r) && isDigit(r[i]):
				d = r[i]
				b.WriteString(ms.oneCapture(int(d-'1'), s, e).String())
			default:
				vm.Errorf("invalid use of '%%' in replacement string")
			}
		}
		return true
	}
	switch {
	case !v.Truthy():
		b.WriteString(ms.src[s:e])
		return false
	case v.k == kindString || v.k == kindInt || v.k == kindFloat:
		b.WriteString(v.String())
		return true
	}
	vm.Errorf("invalid replacement value (a %s)", v.Type())
	return false
}

// strFormat is string.format: the conversions of C's printf that Lua accepts, formatted
// the way C formats them.
func strFormat(vm *VM, args []Value) []Value {
	f := vm.CheckString(args, 0, "format")
	var b strings.Builder
	arg := 0
	for i := 0; i < len(f); i++ {
		c := f[i]
		if c != '%' {
			b.WriteByte(c)
			continue
		}
		i++
		if i >= len(f) {
			vm.Errorf("invalid conversion '%%' to 'format'")
		}
		if f[i] == '%' {
			b.WriteByte('%')
			continue
		}
		start := i
		for i < len(f) && strings.IndexByte("-+ #0", f[i]) >= 0 {
			i++
		}
		for n := 0; i < len(f) && isDigit(f[i]); n++ {
			if n == 2 {
				vm.Errorf("invalid conversion '%%%s' to 'format'", f[start:i+1])
			}
			i++
		}
		if i < len(f) && f[i] == '.' {
			i++
			for n := 0; i < len(f) && isDigit(f[i]); n++ {
				if n == 2 {
					vm.Errorf("invalid conversion '%%%s' to 'format'", f[start:i+1])
				}
				i++
			}
		}
		if i >= len(f) {
			vm.Errorf("invalid conversion '%%%s' to 'format'", f[start:])
		}
		spec := f[start:i] // flags, width, precision
		conv := f[i]
		arg++
		if conv != '%' && arg >= len(args) && conv != 'q' {
			vm.ArgError(arg, "format", "no value")
		}
		switch conv {
		case 'c':
			b.WriteString(fmt.Sprintf("%"+spec+"s", string([]byte{byte(vm.CheckInt(args, arg, "format"))})))
		case 'd', 'i':
			n := vm.CheckInt(args, arg, "format")
			b.WriteString(fmt.Sprintf("%"+spec+"d", n))
		case 'u':
			b.WriteString(fmt.Sprintf("%"+spec+"d", uint64(vm.CheckInt(args, arg, "format"))))
		case 'o', 'x', 'X':
			b.WriteString(fmt.Sprintf("%"+spec+string(conv), uint64(vm.CheckInt(args, arg, "format"))))
		case 'a', 'A':
			b.WriteString(formatHexFloat(vm.CheckFloat(args, arg, "format"), spec, conv == 'A'))
		case 'e', 'E', 'f', 'F', 'g', 'G':
			x := vm.CheckFloat(args, arg, "format")
			b.WriteString(formatCFloat(x, spec, conv))
		case 'q':
			if spec != "" {
				vm.Errorf("specifier '%%q' cannot have modifiers")
			}
			b.WriteString(quoteValue(vm, Arg(args, arg)))
		case 's':
			s := vm.tostring(vm.CheckAny(args, arg, "format")).s()
			if dot := strings.IndexByte(spec, '.'); dot >= 0 {
				// C counts bytes where Go counts runes: cut the string here.
				if n, _ := strconv.Atoi(spec[dot+1:]); n < len(s) {
					s = s[:n]
				}
				spec = spec[:dot]
			}
			b.WriteString(fmt.Sprintf("%"+spec+"s", s))
		default:
			vm.Errorf("invalid conversion '%%%s' to 'format'", f[start:i+1])
		}
		if b.Len() > maxStringSize {
			vm.Errorf("resulting string too large")
		}
	}
	return vm.ret1(String(b.String()))
}

// formatCFloat formats x as C's printf does for %e, %f and %g with the flags spec.
func formatCFloat(x float64, spec string, conv byte) string {
	if math.IsInf(x, 0) || math.IsNaN(x) {
		s := "inf"
		switch {
		case math.IsNaN(x):
			s = "nan"
		case x < 0:
			s = "-inf"
		case strings.Contains(spec, "+"):
			s = "+inf"
		}
		if conv == 'E' || conv == 'F' || conv == 'G' {
			s = strings.ToUpper(s)
		}
		return fmt.Sprintf("%"+strings.Trim(spec, "0#")+"s", s)
	}
	if !strings.Contains(spec, ".") {
		spec += ".6" // C's default precision; Go's is the shortest exact one
	}
	return fmt.Sprintf("%"+spec+string(conv), x)
}

// formatHexFloat formats x as C's %a.
func formatHexFloat(x float64, spec string, upper bool) string {
	prec := -1
	if i := strings.IndexByte(spec, '.'); i >= 0 {
		prec, _ = strconv.Atoi(spec[i+1:])
		spec = spec[:i]
	}
	s := strconv.FormatFloat(x, 'x', prec, 64) // 0x1.8p+01: C writes the exponent without padding
	if i := strings.IndexByte(s, 'p'); i >= 0 {
		exp, _ := strconv.Atoi(s[i+1:])
		s = fmt.Sprintf("%sp%+d", s[:i], exp)
	}
	if strings.Contains(spec, "+") && x >= 0 {
		s = "+" + s
	}
	if upper {
		s = strings.ToUpper(s)
	}
	return fmt.Sprintf("%"+strings.Trim(spec, "+")+"s", s)
}

// quoteValue is %q: a value written so that Lua reads it back.
func quoteValue(vm *VM, v Value) string {
	switch v.k {
	case kindString:
		var b strings.Builder
		b.WriteByte('"')
		s := v.s()
		for i := 0; i < len(s); i++ {
			c := s[i]
			switch {
			case c == '"' || c == '\\' || c == '\n':
				b.WriteByte('\\')
				b.WriteByte(c)
			case c == '\r':
				b.WriteString("\\r")
			case c == 0:
				if i+1 < len(s) && isDigit(s[i+1]) {
					b.WriteString("\\000")
				} else {
					b.WriteString("\\0")
				}
			case c < 32 || c == 127:
				if i+1 < len(s) && isDigit(s[i+1]) {
					fmt.Fprintf(&b, "\\%03d", c)
				} else {
					fmt.Fprintf(&b, "\\%d", c)
				}
			default:
				b.WriteByte(c)
			}
		}
		b.WriteByte('"')
		return b.String()
	case kindInt:
		if v.i() == math.MinInt64 {
			return "0x8000000000000000"
		}
		return v.String()
	case kindFloat:
		f := v.f()
		switch {
		case math.IsInf(f, 1):
			return "1e9999"
		case math.IsInf(f, -1):
			return "-1e9999"
		case math.IsNaN(f):
			return "(0/0)"
		}
		return formatHexFloat(f, "", false)
	case kindNil, kindBool:
		return v.String()
	}
	vm.Errorf("value has no literal form")
	return ""
}
