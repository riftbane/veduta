package lua

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"weak"
)

// Coroutines. The interpreter runs Lua on Go's stack (a call is a Go call), so a coroutine
// cannot be a saved slice of frames: each one runs on a goroutine of its own, with its own
// value, cell and call stacks, sharing the globals of the VM that created it. Control
// passes by unbuffered channels: resume sends and waits, yield sends and waits, so exactly
// one of them runs at any time and the program is as deterministic as without coroutines.
// A yield may happen anywhere in the coroutine, inside pcall or a metamethod too.

// Coroutine is a Lua thread, the value coroutine.create returns.
type Coroutine struct{ c *coState }

// coState is what the goroutine holds: never the Coroutine handle, so that a coroutine
// nothing refers to any more can be collected and its goroutine ended (newCoroutine sets the finalizer).
type coState struct {
	fn     Value
	vm     *VM // the coroutine's stacks
	status coStatus
	in     chan coMsg // resume → coroutine: arguments, or the order to end
	out    chan coMsg // coroutine → resume: yielded or returned values, or the error
	start  sync.Once
	endMu  sync.Mutex              // ending may come from a finalizer's goroutine
	self   weak.Pointer[Coroutine] // the handle, for coroutine.running, without keeping it alive
}

type coStatus uint8

const (
	coSuspended coStatus = iota
	coRunning
	coNormal // resumed another coroutine
	coDead
)

var coStatusNames = [...]string{"suspended", "running", "normal", "dead"}

type coMsg struct {
	vals []Value
	err  any  // the panic that ended the coroutine
	done bool // the function returned
	kill bool // the coroutine is to end without running further
}

// errKilled unwinds a suspended coroutine that is closed or collected. Nothing catches it:
// pcall passes on what is not a Lua error.
var errKilled = errors.New("lua: coroutine closed")

// ThreadValue returns co as a value.
func ThreadValue(co *Coroutine) Value {
	if co == nil {
		return Nil
	}
	return Value{k: kindThread, p: co}
}

// Thread returns v's coroutine, or nil.
func (v Value) Thread() *Coroutine {
	co, _ := v.p.(*Coroutine)
	return co
}

// newCoroutine makes a suspended coroutine running fn, with stacks of its own.
func (vm *VM) newCoroutine(fn Value) *Coroutine {
	t := &VM{
		globals:    vm.globals,
		stringMeta: vm.stringMeta,
		stdout:     vm.stdout,
		stack:      make([]Value, 256),
		boxes:      make([]*cell, 64),
		frames:     make([]frame, len(vm.frames)),
		ret:        make([]Value, 8),
		budget:     1<<63 - 1,
		budgetMax:  vm.budgetMax,
		random:     vm.random,
		debugger:   vm.debugger,
		root:       vm.rootVM(),
	}
	c := &coState{fn: fn, vm: t, in: make(chan coMsg), out: make(chan coMsg)}
	t.co = c
	root := t.root
	root.coMu.Lock()
	root.coroutines[c] = struct{}{}
	root.coMu.Unlock()
	h := &Coroutine{c: c}
	c.self = weak.Make(h)
	runtime.SetFinalizer(h, func(h *Coroutine) { h.c.end() })
	return h
}

// rootVM is the VM that is not a coroutine, which keeps the list of coroutines.
func (vm *VM) rootVM() *VM {
	if vm.root != nil {
		return vm.root
	}
	vm.coMu.Lock()
	if vm.coroutines == nil {
		vm.coroutines = map[*coState]struct{}{}
	}
	vm.coMu.Unlock()
	return vm
}

// resume runs c until it yields, returns or fails. ok is false with the error value when
// it fails or cannot be resumed; an exhausted budget is not the coroutine's to report and
// goes on up.
func (vm *VM) resume(c *coState, args []Value) (vals []Value, errv Value, ok bool) {
	switch c.status {
	case coDead:
		return nil, String("cannot resume dead coroutine"), false
	case coRunning, coNormal:
		return nil, String("cannot resume non-suspended coroutine"), false
	}
	if vm.co != nil {
		vm.co.status = coNormal
	}
	c.status = coRunning
	c.vm.budget = vm.budget
	msg := coMsg{vals: append([]Value(nil), args...)}
	started := false
	c.start.Do(func() {
		started = true
		go c.run(msg)
	})
	if !started {
		c.in <- msg
	}
	reply := <-c.out
	vm.budget = c.vm.budget
	if vm.co != nil {
		vm.co.status = coRunning
	}
	switch {
	case reply.err != nil:
		c.finish()
		switch e := reply.err.(type) {
		case *Error:
			return nil, e.Value, false
		case *BudgetError:
			panic(e)
		}
		panic(reply.err)
	case reply.done:
		c.finish()
	default:
		c.status = coSuspended
	}
	return reply.vals, Nil, true
}

// run is the coroutine's goroutine.
func (c *coState) run(first coMsg) {
	defer func() {
		if r := recover(); r != nil {
			if r == errKilled {
				c.out <- coMsg{done: true}
				return
			}
			c.out <- coMsg{err: r}
		}
	}()
	res := c.vm.call(c.fn, first.vals, "")
	c.out <- coMsg{vals: append([]Value(nil), res...), done: true}
}

// yield hands vals to the resume that is waiting and waits to be resumed.
func (vm *VM) yield(vals []Value) []Value {
	c := vm.co
	if c == nil {
		vm.Errorf("attempt to yield from outside a coroutine")
	}
	c.out <- coMsg{vals: append([]Value(nil), vals...)}
	msg := <-c.in
	if msg.kill {
		panic(errKilled)
	}
	return msg.vals
}

// finish marks c dead and forgets it.
func (c *coState) finish() {
	c.status = coDead
	root := c.vm.root
	root.coMu.Lock()
	delete(root.coroutines, c)
	root.coMu.Unlock()
}

// end ends a suspended coroutine: its goroutine unwinds without running more Lua. A
// coroutine never resumed has no goroutine to end.
func (c *coState) end() {
	c.endMu.Lock()
	defer c.endMu.Unlock()
	if c.status == coDead {
		return
	}
	started := true
	c.start.Do(func() { started = false })
	if started && c.status == coSuspended {
		c.in <- coMsg{kill: true}
		<-c.out
	}
	c.finish()
}

// Close ends every coroutine left suspended, so that their goroutines do not outlive the
// VM. The VM must not run afterwards.
func (vm *VM) Close() {
	root := vm.rootVM()
	root.coMu.Lock()
	var list []*coState
	for c := range root.coroutines {
		list = append(list, c)
	}
	root.coMu.Unlock()
	for _, c := range list {
		if c.status == coSuspended {
			c.end()
		}
	}
}

func openCoroutine(vm *VM) {
	lib := NewTable(0, 8)
	check := func(vm *VM, args []Value, fname string) *coState {
		if co := Arg(args, 0).Thread(); co != nil && Arg(args, 0).k == kindThread {
			return co.c
		}
		vm.typeArgError(args, 0, fname, "thread")
		return nil
	}
	setFuncs(lib, "coroutine.", map[string]GoFunction{
		"create": func(vm *VM, args []Value) []Value {
			fn := vm.CheckFunction(args, 0, "create")
			return vm.ret1(ThreadValue(vm.newCoroutine(fn)))
		},
		"resume": func(vm *VM, args []Value) []Value {
			c := check(vm, args, "resume")
			vals, errv, ok := vm.resume(c, args[1:])
			if !ok {
				return vm.Ret(False, errv)
			}
			out := make([]Value, len(vals)+1)
			out[0] = True
			copy(out[1:], vals)
			return out
		},
		"yield": func(vm *VM, args []Value) []Value {
			return vm.yield(args)
		},
		"status": func(vm *VM, args []Value) []Value {
			return vm.ret1(String(coStatusNames[check(vm, args, "status").status]))
		},
		"isyieldable": func(vm *VM, args []Value) []Value {
			return vm.ret1(Bool(vm.co != nil))
		},
		"running": func(vm *VM, args []Value) []Value {
			if vm.co == nil {
				return vm.Ret(Nil, True)
			}
			return vm.Ret(ThreadValue(vm.co.handle()), False)
		},
		"wrap": func(vm *VM, args []Value) []Value {
			fn := vm.CheckFunction(args, 0, "wrap")
			co := vm.newCoroutine(fn)
			return vm.ret1(FunctionValue(NewFunction("wrap", func(vm *VM, args []Value) []Value {
				vals, errv, ok := vm.resume(co.c, args)
				if !ok {
					panic(vm.newError(errv)) // raised again as it came, position included
				}
				return vals
			})))
		},
		"close": func(vm *VM, args []Value) []Value {
			c := check(vm, args, "close")
			switch c.status {
			case coRunning, coNormal:
				vm.Errorf("cannot close a %s coroutine", coStatusNames[c.status])
			case coSuspended:
				c.end()
			}
			return vm.ret1(True)
		},
	})
	vm.globals.SetString("coroutine", TableValue(lib))
}

// handle returns the Coroutine of a running coroutine, for coroutine.running: the one Lua
// holds, which a running coroutine's resumer keeps alive.
func (c *coState) handle() *Coroutine {
	if h := c.self.Value(); h != nil {
		return h
	}
	return &Coroutine{c: c}
}

func (c *coState) String() string { return fmt.Sprintf("thread: %p", c) }
