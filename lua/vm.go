package lua

import (
	"fmt"
	"io"
	"strings"
)

// GoFunction is a function written in Go. It receives the arguments of the call and returns
// its results; the slice it returns may be the argument slice or VM.Ret's buffer. It reports
// errors by panicking with an *Error, which VM.Errorf and the argument checks do.
type GoFunction func(vm *VM, args []Value) []Value

// Function is a Lua or Go function.
type Function struct {
	proto  *proto
	upvals []*cell
	gofn   GoFunction
	name   string // of a Go function
}

// NewFunction returns a Go function; name is used in error messages.
func NewFunction(name string, fn GoFunction) *Function { return &Function{gofn: fn, name: name} }

// Userdata is a Go value handed to Lua, with the metatable that gives it behaviour.
type Userdata struct {
	Data any
	Meta *Table
}

// cell holds a local variable that a nested function captured.
type cell struct{ v Value }

// proto is a compiled Lua function.
type proto struct {
	name    string
	chunk   string
	line    int
	params  []slotRef // where each parameter lives
	vararg  bool
	nreg    int // registers
	nbox    int // cell slots
	upvals  []upvalSource
	body    stmtFn
	nparams int
}

// slotRef is a local's place: a register, or a cell slot when captured.
type slotRef struct {
	boxed bool
	slot  int
}

// upvalSource says where a closure takes an upvalue from when it is created: a cell slot of
// the enclosing function, or one of its upvalues.
type upvalSource struct {
	fromBox bool
	index   int
}

// frame is one active Lua call.
type frame struct {
	vm      *VM
	reg     []Value
	boxes   []*cell
	varargs []Value
	fn      *Function
	ret     []Value
	line    int
}

// Error is a Lua error: the value raised, with the Lua stack when it was raised.
type Error struct {
	Value     Value
	Traceback string
}

func (e *Error) Error() string {
	msg := e.Value.String()
	if _, ok := e.Value.Str(); !ok && e.Value.k != kindInt && e.Value.k != kindFloat {
		msg = "(error object is a " + e.Value.Type().String() + " value)"
	}
	if e.Traceback == "" {
		return msg
	}
	return msg + "\n" + e.Traceback
}

// BudgetError is returned when a call runs longer than its budget. Lua code cannot catch it.
type BudgetError struct{ Steps int64 }

func (e *BudgetError) Error() string {
	return fmt.Sprintf("the script ran for more than %d steps without returning (an endless loop?)", e.Steps)
}

// Options configures a VM.
type Options struct {
	// Stdout receives what print writes; nil discards it.
	Stdout io.Writer
	// MaxDepth bounds nested Lua calls (default 200).
	MaxDepth int
}

// maxStack bounds the value stack.
const maxStack = 1_000_000

// VM is a Lua state: globals, the value stack and the call stack. A VM is not safe for
// concurrent use.
type VM struct {
	globals    *Table
	stringMeta *Table
	stdout     io.Writer

	stack  []Value
	top    int
	boxes  []*cell
	btop   int
	frames []frame
	depth  int
	ret    []Value

	budget    int64 // steps left in the current call
	budgetMax int64 // the budget the call started with; 0: unlimited
}

// New returns a VM with the base library loaded.
func New(o Options) *VM {
	if o.MaxDepth <= 0 {
		o.MaxDepth = 200
	}
	vm := &VM{
		globals: NewTable(0, 64),
		stdout:  o.Stdout,
		stack:   make([]Value, 1024),
		boxes:   make([]*cell, 256),
		frames:  make([]frame, o.MaxDepth+1),
		ret:     make([]Value, 8),
		budget:  1<<63 - 1,
	}
	if vm.stdout == nil {
		vm.stdout = io.Discard
	}
	openBase(vm)
	return vm
}

// Globals returns the global table.
func (vm *VM) Globals() *Table { return vm.globals }

// SetGlobal sets a global variable.
func (vm *VM) SetGlobal(name string, v Value) { vm.globals.SetString(name, v) }

// Global returns a global variable.
func (vm *VM) Global(name string) Value { return vm.globals.GetString(name) }

// SetStringMetatable sets the metatable shared by all strings, whose __index makes
// ("x"):upper() work.
func (vm *VM) SetStringMetatable(t *Table) { vm.stringMeta = t }

// Load compiles a chunk into a function. chunk names it in messages ("main.lua").
func (vm *VM) Load(chunk, src string) (*Function, error) {
	pf, err := parse(chunk, src)
	if err != nil {
		return nil, err
	}
	p := compileFunc(chunk, pf, nil)
	return &Function{proto: p}, nil
}

// SetBudget limits every following Call to steps loop iterations and calls; 0 removes the
// limit.
func (vm *VM) SetBudget(steps int64) { vm.budgetMax = steps }

// Call calls f with args and returns a copy of its results. A Lua error comes back as an
// *Error, an exhausted budget as a *BudgetError, and a syntax error in a chunk loaded by the
// script as an *Error too. Call may be used from inside a Go function.
func (vm *VM) Call(f Value, args ...Value) (results []Value, err error) {
	outer := vm.depth == 0
	if outer {
		vm.budget = 1<<63 - 1
		if vm.budgetMax > 0 {
			vm.budget = vm.budgetMax
		}
	}
	depth, top, btop := vm.depth, vm.top, vm.btop
	defer func() {
		if r := recover(); r != nil {
			vm.unwind(depth, top, btop)
			switch e := r.(type) {
			case *Error:
				err = e
			case *BudgetError:
				if !outer {
					panic(e)
				}
				err = e
			default:
				panic(r)
			}
		}
	}()
	base := vm.top
	vm.ensure(base + len(args))
	copy(vm.stack[base:], args)
	vm.top = base + len(args)
	res := vm.call(f, vm.stack[base:base+len(args)], "")
	out := append([]Value(nil), res...)
	vm.top = base
	return out, nil
}

// unwind restores the stacks after an error.
func (vm *VM) unwind(depth, top, btop int) {
	for d := vm.depth; d > depth; d-- {
		vm.frames[d] = frame{}
	}
	clear(vm.stack[top:vm.top])
	clear(vm.boxes[btop:vm.btop])
	vm.depth, vm.top, vm.btop = depth, top, btop
}

// ensure makes the value stack at least n long.
func (vm *VM) ensure(n int) {
	if n <= len(vm.stack) {
		return
	}
	if n > maxStack {
		vm.Errorf("stack overflow")
	}
	s := make([]Value, max(2*len(vm.stack), n))
	copy(s, vm.stack[:vm.top])
	vm.stack = s
}

// Ret returns vals in the VM's result buffer, for a Go function to return without
// allocating. The buffer is overwritten by the next call.
func (vm *VM) Ret(vals ...Value) []Value {
	if cap(vm.ret) < len(vals) {
		vm.ret = make([]Value, len(vals))
	}
	r := vm.ret[:len(vals)]
	copy(r, vals)
	return r
}

// ret1 returns one value in the result buffer.
func (vm *VM) ret1(v Value) []Value {
	r := vm.ret[:1]
	r[0] = v
	return r
}

// step spends one unit of the budget.
func (vm *VM) step() {
	vm.budget--
	if vm.budget < 0 {
		panic(&BudgetError{Steps: vm.budgetMax})
	}
}

// call calls f, whatever it is, with args. The results are valid until the next call. desc
// names f for an error message ("global 'f'").
func (vm *VM) call(f Value, args []Value, desc string) []Value {
	if fn, ok := f.p.(*Function); ok && f.k == kindFunction {
		if fn.gofn != nil {
			vm.step()
			return fn.gofn(vm, args)
		}
		return vm.callLua(fn, args)
	}
	h := vm.metaField(f, "__call")
	if h.IsNil() {
		vm.typeError(f, "call", desc)
	}
	base := vm.top
	vm.ensure(base + len(args) + 1)
	vm.stack[base] = f
	copy(vm.stack[base+1:], args)
	vm.top = base + len(args) + 1
	res := vm.call(h, vm.stack[base:vm.top], desc)
	vm.top = base
	return res
}

func (vm *VM) callLua(f *Function, args []Value) []Value {
	p := f.proto
	if vm.depth+1 >= len(vm.frames) {
		vm.Errorf("stack overflow")
	}
	vm.step()
	base, bbase := vm.top, vm.btop
	vm.ensure(base + p.nreg)
	reg := vm.stack[base : base+p.nreg : base+p.nreg]
	clear(reg)
	vm.top = base + p.nreg
	var boxes []*cell
	if p.nbox > 0 {
		if bbase+p.nbox > len(vm.boxes) {
			b := make([]*cell, max(2*len(vm.boxes), bbase+p.nbox))
			copy(b, vm.boxes[:bbase])
			vm.boxes = b
		}
		boxes = vm.boxes[bbase : bbase+p.nbox : bbase+p.nbox]
		vm.btop = bbase + p.nbox
	}
	vm.depth++
	fr := &vm.frames[vm.depth]
	*fr = frame{vm: vm, reg: reg, boxes: boxes, fn: f, line: p.line}
	for i, ps := range p.params {
		v := Nil
		if i < len(args) {
			v = args[i]
		}
		if ps.boxed {
			boxes[ps.slot] = &cell{v}
		} else {
			reg[ps.slot] = v
		}
	}
	if p.vararg && len(args) > len(p.params) {
		fr.varargs = args[len(p.params):]
	}
	var res []Value
	if p.body(fr) == ctlReturn {
		res = fr.ret
	}
	*fr = frame{}
	vm.depth--
	clear(reg)
	if boxes != nil {
		clear(boxes)
	}
	vm.top, vm.btop = base, bbase
	return res
}

// Errorf raises a Lua error whose message is prefixed with the position of the Lua code
// running, as errors raised by Lua itself are.
func (vm *VM) Errorf(format string, args ...any) {
	panic(vm.newError(String(vm.where(1) + fmt.Sprintf(format, args...))))
}

// newError returns an error carrying v and the current traceback.
func (vm *VM) newError(v Value) *Error {
	return &Error{Value: v, Traceback: vm.traceback()}
}

// where returns "chunk:line: " for the Lua function level levels up the stack (1: the one
// running), or "" when there is none.
func (vm *VM) where(level int) string {
	d := vm.depth - (level - 1)
	if d < 1 || d > vm.depth {
		return ""
	}
	fr := &vm.frames[d]
	if fr.fn == nil || fr.fn.proto == nil {
		return ""
	}
	return fmt.Sprintf("%s:%d: ", fr.fn.proto.chunk, fr.line)
}

// traceback describes the Lua call stack, innermost first.
func (vm *VM) traceback() string {
	if vm.depth == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("stack traceback:")
	for d := vm.depth; d >= 1; d-- {
		fr := &vm.frames[d]
		if fr.fn == nil || fr.fn.proto == nil {
			continue
		}
		fmt.Fprintf(&b, "\n\t%s:%d: in %s", fr.fn.proto.chunk, fr.line, fr.fn.proto.name)
	}
	return b.String()
}

// typeError raises "attempt to <op> a <type> value (<desc>)".
func (vm *VM) typeError(v Value, op, desc string) {
	msg := fmt.Sprintf("attempt to %s a %s value", op, vm.typeName(v))
	if desc != "" {
		msg += " (" + desc + ")"
	}
	vm.Errorf("%s", msg)
}

// typeName is the type of v, or its metatable's __name.
func (vm *VM) typeName(v Value) string {
	if m := vm.metatable(v); m != nil {
		if n, ok := m.GetString("__name").Str(); ok {
			return n
		}
	}
	return v.Type().String()
}

// metatable returns v's metatable, or nil.
func (vm *VM) metatable(v Value) *Table {
	switch v.k {
	case kindTable:
		return v.p.(*Table).meta
	case kindUserdata:
		return v.p.(*Userdata).Meta
	case kindString:
		return vm.stringMeta
	}
	return nil
}

// metaField returns field name of v's metatable, or nil.
func (vm *VM) metaField(v Value, name string) Value {
	if m := vm.metatable(v); m != nil {
		return m.GetString(name)
	}
	return Nil
}
