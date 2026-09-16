package lua

// Debugger follows a VM made with Options.Debugger. Statement is called before each
// statement runs, on the goroutine running the code, which waits for it to return: a
// debugger stops the program by not returning, and inspects it meanwhile with Stack,
// Locals and Upvalues.
type Debugger interface {
	// Statement is about to run the statement at line of chunk. depth is the number of Lua
	// calls active, 1 for the outermost.
	Statement(vm *VM, chunk string, line, depth int)
}

// StackEntry is one active Lua call.
type StackEntry struct {
	Chunk string
	Name  string // the function's name, as a traceback shows it
	Line  int    // the statement running, or the function's first line before any
}

// Variable is a named value of a Lua call.
type Variable struct {
	Name  string
	Value Value
}

// Stack returns the active Lua calls, innermost first.
func (vm *VM) Stack() []StackEntry {
	var out []StackEntry
	for d := vm.depth; d >= 1; d-- {
		fr := &vm.frames[d]
		if fr.fn == nil || fr.fn.proto == nil {
			continue
		}
		out = append(out, StackEntry{Chunk: fr.fn.proto.chunk, Name: fr.fn.proto.name, Line: fr.line})
	}
	return out
}

// frameAt is the Lua call level calls out from the innermost (0), or nil.
func (vm *VM) frameAt(level int) *frame {
	for d := vm.depth; d >= 1; d-- {
		fr := &vm.frames[d]
		if fr.fn == nil || fr.fn.proto == nil {
			continue
		}
		if level == 0 {
			return fr
		}
		level--
	}
	return nil
}

// Locals returns the local variables visible where the call at level (as Stack numbers
// them) stopped, in declaration order. Only code loaded with a debugger records them.
func (vm *VM) Locals(level int) []Variable {
	fr := vm.frameAt(level)
	if fr == nil {
		return nil
	}
	out := make([]Variable, 0, len(fr.vis))
	for _, v := range fr.vis {
		val := Nil
		if v.captured {
			if c := fr.boxes[v.slot]; c != nil {
				val = c.v
			}
		} else {
			val = fr.reg[v.slot]
		}
		out = append(out, Variable{Name: v.name, Value: val})
	}
	return out
}

// Upvalues returns the variables of enclosing functions the call at level uses.
func (vm *VM) Upvalues(level int) []Variable {
	fr := vm.frameAt(level)
	if fr == nil {
		return nil
	}
	names := fr.fn.proto.upvalNames
	out := make([]Variable, 0, len(names))
	for i, n := range names {
		if i < len(fr.fn.upvals) && fr.fn.upvals[i] != nil {
			out = append(out, Variable{Name: n, Value: fr.fn.upvals[i].v})
		}
	}
	return out
}
