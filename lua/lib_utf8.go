package lua

import (
	"strings"
	"unicode/utf8"
)

// The utf8 library.

func openUTF8(vm *VM) {
	lib := NewTable(0, 8)
	for name, fn := range map[string]GoFunction{
		"char": utf8Char, "codepoint": utf8Codepoint, "codes": utf8Codes, "len": utf8Len, "offset": utf8Offset,
	} {
		lib.SetString(name, FunctionValue(NewFunction("utf8."+name, fn)))
	}
	lib.SetString("charpattern", String("[\x00-\x7F\xC2-\xFD][\x80-\xBF]*"))
	vm.globals.SetString("utf8", TableValue(lib))
}

func utf8Char(vm *VM, args []Value) []Value {
	var b strings.Builder
	for i := range args {
		c := vm.CheckInt(args, i, "char")
		if uint64(c) > 0x10FFFF {
			vm.ArgError(i, "char", "value out of range")
		}
		b.WriteRune(rune(c))
	}
	return vm.ret1(String(b.String()))
}

// decodeAt decodes the character at byte i, or fails for an invalid sequence.
func decodeAt(vm *VM, s string, i int) (rune, int) {
	r, n := utf8.DecodeRuneInString(s[i:])
	if r == utf8.RuneError && n <= 1 {
		vm.Errorf("invalid UTF-8 code")
	}
	return r, n
}

func utf8Codepoint(vm *VM, args []Value) []Value {
	s := vm.CheckString(args, 0, "codepoint")
	i := posRelative(vm.OptInt(args, 1, "codepoint", 1), len(s))
	j := endPosition(vm.OptInt(args, 2, "codepoint", i), len(s))
	if i < 1 {
		vm.ArgError(1, "codepoint", "out of bounds")
	}
	if j > int64(len(s)) {
		vm.ArgError(2, "codepoint", "out of bounds")
	}
	var out []Value
	for p := int(i - 1); p < int(j); {
		r, n := decodeAt(vm, s, p)
		out = append(out, Int(int64(r)))
		p += n
	}
	return out
}

func utf8Len(vm *VM, args []Value) []Value {
	s := vm.CheckString(args, 0, "len")
	i := posRelative(vm.OptInt(args, 1, "len", 1), len(s))
	j := vm.OptInt(args, 2, "len", -1)
	if j < 0 {
		j = int64(len(s)) + j + 1
	}
	if i < 1 || i > int64(len(s))+1 {
		vm.ArgError(1, "len", "initial position out of bounds")
	}
	if j > int64(len(s)) {
		vm.ArgError(2, "len", "final position out of bounds")
	}
	n := int64(0)
	for p := int(i - 1); p < int(j); n++ {
		r, size := utf8.DecodeRuneInString(s[p:])
		if r == utf8.RuneError && size <= 1 {
			return vm.Ret(Nil, Int(int64(p)+1))
		}
		p += size
	}
	return vm.ret1(Int(n))
}

func utf8Offset(vm *VM, args []Value) []Value {
	s := vm.CheckString(args, 0, "offset")
	n := vm.CheckInt(args, 1, "offset")
	def := int64(1)
	if n < 0 {
		def = int64(len(s)) + 1
	}
	i := posRelative(vm.OptInt(args, 2, "offset", def), len(s)) - 1
	if i < 0 || i > int64(len(s)) {
		vm.ArgError(2, "offset", "position out of bounds")
	}
	cont := func(p int64) bool { return p < int64(len(s)) && s[p]&0xC0 == 0x80 }
	if n == 0 {
		for i > 0 && cont(i) {
			i--
		}
		return vm.ret1(Int(i + 1))
	}
	if cont(i) {
		vm.Errorf("initial position is a continuation byte")
	}
	if n < 0 {
		for n < 0 && i > 0 {
			i--
			for i > 0 && cont(i) {
				i--
			}
			n++
		}
	} else {
		n--
		for n > 0 && i < int64(len(s)) {
			i++
			for cont(i) {
				i++
			}
			n--
		}
	}
	if n == 0 {
		return vm.ret1(Int(i + 1))
	}
	return vm.ret1(Nil)
}

func utf8Codes(vm *VM, args []Value) []Value {
	s := vm.CheckString(args, 0, "codes")
	iter := NewFunction("codes_iterator", func(vm *VM, args []Value) []Value {
		i := vm.CheckInt(args, 1, "codes")
		p := int(i)
		if p > 0 {
			_, n := decodeAt(vm, s, p-1)
			p += n - 1
		}
		if p >= len(s) {
			return vm.ret1(Nil)
		}
		r, _ := decodeAt(vm, s, p)
		return vm.Ret(Int(int64(p)+1), Int(int64(r)))
	})
	return vm.Ret(FunctionValue(iter), args[0], Int(0))
}
