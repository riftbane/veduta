package lua

import "math"

// The compiler turns a parsed function into Go closures. Locals live in registers of the
// frame, or in cells when a nested function captures them; temporaries (call arguments,
// values of a multiple assignment) live on the VM's value stack above the registers.

type ctl uint8

const (
	ctlNone ctl = iota
	ctlBreak
	ctlReturn
)

type (
	stmtFn  func(fr *frame) ctl
	exprFn  func(fr *frame) Value
	condFn  func(fr *frame) bool
	multiFn func(fr *frame) []Value // results valid until the next call
	argsFn  func(fr *frame) int     // pushes values at the stack top and returns how many
)

type compiler struct {
	chunk   string
	pf      *parseFunc
	parent  *compiler
	nreg    int
	maxreg  int
	nbox    int
	maxbox  int
	upvals  []*localVar
	sources []upvalSource
}

func compileFunc(chunk string, pf *parseFunc, parent *compiler) *proto {
	c := &compiler{chunk: chunk, pf: pf, parent: parent}
	p := &proto{name: pf.name, chunk: chunk, line: pf.line, vararg: pf.vararg, nparams: len(pf.params)}
	for _, v := range pf.params {
		p.params = append(p.params, c.declare(v))
	}
	p.body = c.block(pf.body)
	p.nreg, p.nbox = c.maxreg, c.maxbox
	p.upvals = c.sources
	return p
}

// declare gives a local its slot.
func (c *compiler) declare(v *localVar) slotRef {
	if v.captured {
		v.slot = c.nbox
		c.nbox++
		c.maxbox = max(c.maxbox, c.nbox)
		return slotRef{boxed: true, slot: v.slot}
	}
	v.slot = c.nreg
	c.nreg++
	c.maxreg = max(c.maxreg, c.nreg)
	return slotRef{slot: v.slot}
}

// upval returns the index of an enclosing function's local among this function's upvalues.
func (c *compiler) upval(v *localVar) int {
	for i, u := range c.upvals {
		if u == v {
			return i
		}
	}
	src := upvalSource{fromBox: true, index: v.slot}
	if v.fn != c.parent.pf {
		src = upvalSource{index: c.parent.upval(v)}
	}
	c.upvals = append(c.upvals, v)
	c.sources = append(c.sources, src)
	return len(c.upvals) - 1
}

// describe names an expression for an error message.
func (c *compiler) describe(e expr) string {
	switch e := e.(type) {
	case *nameExpr:
		switch {
		case e.v == nil:
			return "global '" + e.name + "'"
		case e.v.fn != c.pf:
			return "upvalue '" + e.name + "'"
		}
		return "local '" + e.name + "'"
	case *indexExpr:
		if k, ok := e.key.(*constExpr); ok && k.v.k == kindString {
			return "field '" + k.v.s() + "'"
		}
	case *methodExpr:
		return "method '" + e.name + "'"
	case *constExpr:
		if e.v.k == kindString {
			return "constant '" + e.v.s() + "'"
		}
	}
	return ""
}

// block compiles statements in a scope of their own.
func (c *compiler) block(stmts []stmt) stmtFn {
	reg, box := c.nreg, c.nbox
	fns := make([]stmtFn, 0, len(stmts))
	for _, s := range stmts {
		fns = append(fns, c.stmt(s))
	}
	c.nreg, c.nbox = reg, box
	return seq(fns)
}

func seq(fns []stmtFn) stmtFn {
	switch len(fns) {
	case 0:
		return func(*frame) ctl { return ctlNone }
	case 1:
		return fns[0]
	case 2:
		a, b := fns[0], fns[1]
		return func(fr *frame) ctl {
			if x := a(fr); x != ctlNone {
				return x
			}
			return b(fr)
		}
	case 3:
		a, b, d := fns[0], fns[1], fns[2]
		return func(fr *frame) ctl {
			if x := a(fr); x != ctlNone {
				return x
			}
			if x := b(fr); x != ctlNone {
				return x
			}
			return d(fr)
		}
	}
	return func(fr *frame) ctl {
		for _, f := range fns {
			if x := f(fr); x != ctlNone {
				return x
			}
		}
		return ctlNone
	}
}

func (c *compiler) stmt(s stmt) stmtFn {
	switch s := s.(type) {
	case *localStmt:
		return c.localStmt(s)
	case *assignStmt:
		return c.assignStmt(s)
	case *callStmt:
		m, line := c.multi(s.call), s.line
		return func(fr *frame) ctl {
			fr.line = line
			m(fr)
			return ctlNone
		}
	case *doStmt:
		return c.block(s.body)
	case *whileStmt:
		cond, body, line := c.cond(s.cond), c.block(s.body), s.line
		return func(fr *frame) ctl {
			vm := fr.vm
			for {
				fr.line = line
				if !cond(fr) {
					return ctlNone
				}
				if vm.budget--; vm.budget < 0 {
					vm.step()
				}
				switch body(fr) {
				case ctlBreak:
					return ctlNone
				case ctlReturn:
					return ctlReturn
				}
			}
		}
	case *repeatStmt:
		reg, box := c.nreg, c.nbox
		fns := make([]stmtFn, 0, len(s.body))
		for _, b := range s.body {
			fns = append(fns, c.stmt(b))
		}
		cond := c.cond(s.cond)
		c.nreg, c.nbox = reg, box
		body := seq(fns)
		return func(fr *frame) ctl {
			vm := fr.vm
			for {
				if vm.budget--; vm.budget < 0 {
					vm.step()
				}
				switch body(fr) {
				case ctlBreak:
					return ctlNone
				case ctlReturn:
					return ctlReturn
				}
				if cond(fr) {
					return ctlNone
				}
			}
		}
	case *ifStmt:
		return c.ifStmt(s)
	case *numForStmt:
		return c.numFor(s)
	case *genForStmt:
		return c.genFor(s)
	case *localFuncStmt:
		ref := c.declare(s.v)
		p := compileFunc(c.chunk, s.f, c)
		line := s.line
		if ref.boxed {
			slot := ref.slot
			return func(fr *frame) ctl {
				fr.line = line
				cl := &cell{}
				fr.boxes[slot] = cl // before the closure, which captures it for recursion
				cl.v = makeClosure(fr, p)
				return ctlNone
			}
		}
		slot := ref.slot
		return func(fr *frame) ctl {
			fr.reg[slot] = makeClosure(fr, p)
			return ctlNone
		}
	case *returnStmt:
		return c.returnStmt(s)
	case *breakStmt:
		return func(*frame) ctl { return ctlBreak }
	}
	panic("lua: unknown statement")
}

func (c *compiler) ifStmt(s *ifStmt) stmtFn {
	conds := make([]condFn, len(s.conds))
	blocks := make([]stmtFn, len(s.blocks))
	for i := range s.conds {
		conds[i] = c.cond(s.conds[i])
		blocks[i] = c.block(s.blocks[i])
	}
	var els stmtFn
	if s.els != nil {
		els = c.block(s.els)
	}
	line := s.line
	if len(conds) == 1 {
		cond, then := conds[0], blocks[0]
		if els == nil {
			return func(fr *frame) ctl {
				fr.line = line
				if cond(fr) {
					return then(fr)
				}
				return ctlNone
			}
		}
		return func(fr *frame) ctl {
			fr.line = line
			if cond(fr) {
				return then(fr)
			}
			return els(fr)
		}
	}
	return func(fr *frame) ctl {
		fr.line = line
		for i, cond := range conds {
			if cond(fr) {
				return blocks[i](fr)
			}
		}
		if els != nil {
			return els(fr)
		}
		return ctlNone
	}
}

// assign returns a function that stores a value into a local.
func (c *compiler) storeLocal(ref slotRef, fresh bool) func(fr *frame, v Value) {
	slot := ref.slot
	switch {
	case !ref.boxed:
		return func(fr *frame, v Value) { fr.reg[slot] = v }
	case fresh:
		return func(fr *frame, v Value) { fr.boxes[slot] = &cell{v} }
	}
	return func(fr *frame, v Value) { fr.boxes[slot].v = v }
}

func (c *compiler) localStmt(s *localStmt) stmtFn {
	line := s.line
	if len(s.vars) == 1 && len(s.exprs) <= 1 && (len(s.exprs) == 0 || !isMulti(s.exprs[0])) {
		var e exprFn
		if len(s.exprs) == 1 {
			e = c.expr(s.exprs[0])
		}
		ref := c.declare(s.vars[0])
		slot := ref.slot
		switch {
		case e == nil && ref.boxed:
			return func(fr *frame) ctl { fr.boxes[slot] = &cell{}; return ctlNone }
		case e == nil:
			return func(fr *frame) ctl { fr.reg[slot] = Nil; return ctlNone }
		case ref.boxed:
			return func(fr *frame) ctl {
				fr.line = line
				fr.boxes[slot] = &cell{e(fr)}
				return ctlNone
			}
		}
		return func(fr *frame) ctl {
			fr.line = line
			v := e(fr)
			fr.reg[slot] = v
			return ctlNone
		}
	}
	vals := c.exprList(s.exprs, len(s.vars))
	stores := make([]func(*frame, Value), len(s.vars))
	for i, v := range s.vars {
		stores[i] = c.storeLocal(c.declare(v), true)
	}
	n := len(s.vars)
	return func(fr *frame) ctl {
		fr.line = line
		vm := fr.vm
		base := vm.top
		vals(fr)
		for i := 0; i < n; i++ {
			stores[i](fr, vm.stack[base+i])
		}
		clear(vm.stack[base : base+n])
		vm.top = base
		return ctlNone
	}
}

// exprList returns a function that pushes exactly want values at the stack top, adjusting
// the list as Lua does: extra values dropped, missing ones nil, a final call or '...'
// expanded.
func (c *compiler) exprList(exprs []expr, want int) func(fr *frame) {
	fixed := exprs
	var last multiFn
	if len(exprs) > 0 && isMulti(exprs[len(exprs)-1]) {
		last = c.multi(exprs[len(exprs)-1])
		fixed = exprs[:len(exprs)-1]
	}
	fns := make([]exprFn, len(fixed))
	for i, e := range fixed {
		fns[i] = c.expr(e)
	}
	return func(fr *frame) {
		vm := fr.vm
		base := vm.top
		vm.ensure(base + max(want, len(fns)))
		for i, f := range fns {
			vm.top = base + i
			v := f(fr)
			if i < want {
				vm.stack[base+i] = v
			}
		}
		n := min(len(fns), want)
		vm.top = base + n
		if last != nil {
			res := last(fr)
			for _, v := range res {
				if n >= want {
					break
				}
				vm.stack[base+n] = v
				n++
			}
		}
		for ; n < want; n++ {
			vm.stack[base+n] = Nil
		}
		vm.top = base + want
	}
}

func (c *compiler) assignStmt(s *assignStmt) stmtFn {
	line := s.line
	if len(s.targets) == 1 && len(s.exprs) == 1 {
		e := c.expr(s.exprs[0])
		switch t := s.targets[0].(type) {
		case *nameExpr:
			store := c.nameStore(t)
			return func(fr *frame) ctl {
				fr.line = line
				store(fr, e(fr))
				return ctlNone
			}
		case *indexExpr:
			obj, desc := c.expr(t.obj), c.describe(t.obj)
			if k, ok := t.key.(*constExpr); ok && k.v.k == kindString {
				name, key := k.v.s(), k.v
				return func(fr *frame) ctl {
					fr.line = line
					o := obj(fr)
					v := e(fr)
					if tb, ok := o.p.(*Table); ok && o.k == kindTable && tb.meta == nil {
						tb.SetString(name, v)
						return ctlNone
					}
					fr.vm.setIndex(o, key, v, desc)
					return ctlNone
				}
			}
			key := c.expr(t.key)
			return func(fr *frame) ctl {
				fr.line = line
				o := obj(fr)
				k := key(fr)
				v := e(fr)
				if tb, ok := o.p.(*Table); ok && o.k == kindTable && tb.meta == nil && k.k == kindInt {
					tb.SetInt(k.i(), v)
					return ctlNone
				}
				fr.vm.setIndex(o, k, v, desc)
				return ctlNone
			}
		}
	}
	// The general case: evaluate the tables and keys of the targets, then the values, then
	// assign from left to right.
	type target struct {
		obj, key exprFn
		store    func(*frame, Value)
		desc     string
	}
	targets := make([]target, len(s.targets))
	nidx := 0
	for i, t := range s.targets {
		switch t := t.(type) {
		case *nameExpr:
			targets[i].store = c.nameStore(t)
		case *indexExpr:
			targets[i] = target{obj: c.expr(t.obj), key: c.expr(t.key), desc: c.describe(t.obj)}
			nidx++
		}
	}
	vals := c.exprList(s.exprs, len(s.targets))
	n := len(targets)
	return func(fr *frame) ctl {
		fr.line = line
		vm := fr.vm
		base := vm.top
		vm.ensure(base + 2*nidx)
		j := 0
		for _, t := range targets {
			if t.obj != nil {
				vm.top = base + j
				o := t.obj(fr)
				vm.stack[base+j] = o
				vm.top = base + j + 1
				k := t.key(fr)
				vm.stack[base+j+1] = k
				j += 2
			}
		}
		vm.top = base + j
		vals(fr)
		j = 0
		for i, t := range targets {
			v := vm.stack[base+2*nidx+i]
			if t.obj != nil {
				vm.setIndex(vm.stack[base+j], vm.stack[base+j+1], v, t.desc)
				j += 2
			} else {
				t.store(fr, v)
			}
		}
		clear(vm.stack[base : base+2*nidx+n])
		vm.top = base
		return ctlNone
	}
}

// nameStore returns a function that assigns to a variable.
func (c *compiler) nameStore(e *nameExpr) func(*frame, Value) {
	v := e.v
	switch {
	case v == nil:
		name, key := e.name, String(e.name)
		return func(fr *frame, val Value) {
			g := fr.vm.globals
			if g.meta == nil {
				g.SetString(name, val)
				return
			}
			fr.vm.setIndex(TableValue(g), key, val, "")
		}
	case v.fn == c.pf:
		return c.storeLocal(slotRef{boxed: v.captured, slot: v.slot}, false)
	}
	i := c.upval(v)
	return func(fr *frame, val Value) { fr.fn.upvals[i].v = val }
}

func (c *compiler) returnStmt(s *returnStmt) stmtFn {
	line := s.line
	switch {
	case len(s.exprs) == 0:
		return func(fr *frame) ctl {
			fr.ret = nil
			return ctlReturn
		}
	case len(s.exprs) == 1 && isMulti(s.exprs[0]):
		m := c.multi(s.exprs[0])
		return func(fr *frame) ctl {
			fr.line = line
			fr.ret = m(fr)
			return ctlReturn
		}
	case len(s.exprs) == 1:
		e := c.expr(s.exprs[0])
		return func(fr *frame) ctl {
			fr.line = line
			v := e(fr)
			fr.ret = fr.vm.ret1(v)
			return ctlReturn
		}
	}
	fixed := s.exprs
	var last multiFn
	if isMulti(s.exprs[len(s.exprs)-1]) {
		last = c.multi(s.exprs[len(s.exprs)-1])
		fixed = s.exprs[:len(s.exprs)-1]
	}
	fns := make([]exprFn, len(fixed))
	for i, e := range fixed {
		fns[i] = c.expr(e)
	}
	return func(fr *frame) ctl {
		fr.line = line
		vm := fr.vm
		base := vm.top
		vm.ensure(base + len(fns))
		for i, f := range fns {
			vm.top = base + i
			v := f(fr)
			vm.stack[base+i] = v
		}
		n := len(fns)
		vm.top = base + n
		if last != nil {
			res := last(fr)
			vm.ensure(base + n + len(res))
			copy(vm.stack[base+n:], res)
			n += len(res)
		}
		fr.ret = vm.Ret(vm.stack[base : base+n]...)
		clear(vm.stack[base : base+n])
		vm.top = base
		return ctlReturn
	}
}

func (c *compiler) numFor(s *numForStmt) stmtFn {
	start, limit := c.expr(s.start), c.expr(s.limit)
	var step exprFn
	if s.step != nil {
		step = c.expr(s.step)
	}
	reg, box := c.nreg, c.nbox
	store := c.storeLocal(c.declare(s.v), true)
	body := c.block(s.body)
	c.nreg, c.nbox = reg, box
	line := s.line
	return func(fr *frame) ctl {
		fr.line = line
		vm := fr.vm
		a, b, st := start(fr), limit(fr), Int(1)
		if step != nil {
			st = step(fr)
		}
		for _, x := range [...]struct {
			v    Value
			what string
		}{{a, "initial value"}, {b, "limit"}, {st, "step"}} {
			if !x.v.IsNumber() {
				vm.Errorf("bad 'for' %s (number expected, got %s)", x.what, vm.typeName(x.v))
			}
		}
		if a.k == kindInt && st.k == kindInt {
			i, s := a.i(), st.i()
			if s == 0 {
				vm.Errorf("'for' step is zero")
			}
			lim, skip := forLimit(i, b, s)
			if skip {
				return ctlNone
			}
			var count uint64
			if s > 0 {
				count = (uint64(lim) - uint64(i)) / uint64(s)
			} else {
				count = (uint64(i) - uint64(lim)) / (uint64(-(s + 1)) + 1)
			}
			for {
				store(fr, Int(i))
				switch body(fr) {
				case ctlBreak:
					return ctlNone
				case ctlReturn:
					return ctlReturn
				}
				if count == 0 {
					return ctlNone
				}
				count--
				i += s
				if vm.budget--; vm.budget < 0 {
					vm.step()
				}
			}
		}
		x, _ := a.Float()
		l, _ := b.Float()
		sf, _ := st.Float()
		if sf == 0 {
			vm.Errorf("'for' step is zero")
		}
		for ; sf > 0 && x <= l || sf < 0 && x >= l; x += sf {
			store(fr, Float(x))
			switch body(fr) {
			case ctlBreak:
				return ctlNone
			case ctlReturn:
				return ctlReturn
			}
			if vm.budget--; vm.budget < 0 {
				vm.step()
			}
		}
		return ctlNone
	}
}

// forLimit converts the limit of an integer loop as Lua does and reports whether the loop
// runs no times at all.
func forLimit(init int64, lim Value, step int64) (int64, bool) {
	var l int64
	if lim.k == kindInt {
		l = lim.i()
	} else {
		f := lim.f()
		if step < 0 {
			f = math.Ceil(f)
		} else {
			f = math.Floor(f)
		}
		switch {
		case math.IsNaN(f):
			return 0, true
		case f >= 0x1p63:
			if step < 0 {
				return 0, true
			}
			l = math.MaxInt64
		case f < -0x1p63:
			if step > 0 {
				return 0, true
			}
			l = math.MinInt64
		default:
			l = int64(f)
		}
	}
	if step > 0 {
		return l, init > l
	}
	return l, init < l
}

func (c *compiler) genFor(s *genForStmt) stmtFn {
	vals := c.exprList(s.exprs, 3)
	reg, box := c.nreg, c.nbox
	stores := make([]func(*frame, Value), len(s.vars))
	for i, v := range s.vars {
		stores[i] = c.storeLocal(c.declare(v), true)
	}
	body := c.block(s.body)
	c.nreg, c.nbox = reg, box
	line := s.line
	nvars := len(s.vars)
	return func(fr *frame) ctl {
		fr.line = line
		vm := fr.vm
		base := vm.top
		vals(fr)
		f, state, control := vm.stack[base], vm.stack[base+1], vm.stack[base+2]
		clear(vm.stack[base : base+3])
		vm.top = base
		if fn := f.Function(); fn != nil && fn.gofn != nil {
			if t := state.Table(); t != nil {
				switch fn {
				case nextFunction:
					return iterateNext(fr, t, control, stores, body)
				case ipairsIterator:
					if t.meta == nil {
						return iterateArray(fr, t, stores, body)
					}
				}
			}
		}
		for {
			vm.ensure(base + 2)
			vm.stack[base], vm.stack[base+1] = state, control
			vm.top = base + 2
			fr.line = line
			res := vm.call(f, vm.stack[base:base+2], "for iterator")
			vm.top = base
			if len(res) == 0 || res[0].IsNil() {
				return ctlNone
			}
			control = res[0]
			// The results live in a buffer the body's calls overwrite: store them first.
			for i := 0; i < nvars; i++ {
				v := Nil
				if i < len(res) {
					v = res[i]
				}
				stores[i](fr, v)
			}
			switch body(fr) {
			case ctlBreak:
				return ctlNone
			case ctlReturn:
				return ctlReturn
			}
		}
	}
}

// iterateNext runs a for loop over pairs(t) without calling next through the VM.
func iterateNext(fr *frame, t *Table, k Value, stores []func(*frame, Value), body stmtFn) ctl {
	vm := fr.vm
	for {
		nk, v, ok, err := t.Next(k)
		if err != nil {
			vm.Errorf("%s", err.(*Error).Value.s())
		}
		if !ok {
			return ctlNone
		}
		k = nk
		if len(stores) > 0 {
			stores[0](fr, nk)
		}
		if len(stores) > 1 {
			stores[1](fr, v)
			for _, s := range stores[2:] {
				s(fr, Nil)
			}
		}
		switch body(fr) {
		case ctlBreak:
			return ctlNone
		case ctlReturn:
			return ctlReturn
		}
		if vm.budget--; vm.budget < 0 {
			vm.step()
		}
	}
}

// iterateArray runs a for loop over ipairs(t) for a table without a metatable.
func iterateArray(fr *frame, t *Table, stores []func(*frame, Value), body stmtFn) ctl {
	vm := fr.vm
	for i := int64(1); ; i++ {
		v := t.GetInt(i)
		if v.IsNil() {
			return ctlNone
		}
		if len(stores) > 0 {
			stores[0](fr, Int(i))
		}
		if len(stores) > 1 {
			stores[1](fr, v)
			for _, s := range stores[2:] {
				s(fr, Nil)
			}
		}
		switch body(fr) {
		case ctlBreak:
			return ctlNone
		case ctlReturn:
			return ctlReturn
		}
		if vm.budget--; vm.budget < 0 {
			vm.step()
		}
	}
}

// makeClosure creates a closure of p in the frame running.
func makeClosure(fr *frame, p *proto) Value {
	f := &Function{proto: p}
	if len(p.upvals) > 0 {
		f.upvals = make([]*cell, len(p.upvals))
		for i, src := range p.upvals {
			if src.fromBox {
				f.upvals[i] = fr.boxes[src.index]
			} else {
				f.upvals[i] = fr.fn.upvals[src.index]
			}
		}
	}
	return FunctionValue(f)
}

// cond compiles an expression used as a condition.
func (c *compiler) cond(e expr) condFn {
	switch e := e.(type) {
	case *trueExpr:
		return func(*frame) bool { return true }
	case *falseExpr, *nilExpr:
		return func(*frame) bool { return false }
	case *unExpr:
		if e.op == tNot {
			x := c.cond(e.e)
			return func(fr *frame) bool { return !x(fr) }
		}
	case *parenExpr:
		return c.cond(e.e)
	case *binExpr:
		switch e.op {
		case tAnd:
			l, r := c.cond(e.l), c.cond(e.r)
			return func(fr *frame) bool { return l(fr) && r(fr) }
		case tOr:
			l, r := c.cond(e.l), c.cond(e.r)
			return func(fr *frame) bool { return l(fr) || r(fr) }
		case tEq, tNe, tLt, tLe, tGt, tGe:
			return c.compare(e)
		}
	}
	x := c.expr(e)
	return func(fr *frame) bool { return x(fr).Truthy() }
}

// compare compiles a comparison into a condition.
func (c *compiler) compare(e *binExpr) condFn {
	l, r := c.expr(e.l), c.expr(e.r)
	switch e.op {
	case tEq, tNe:
		ne := e.op == tNe
		if k, ok := e.r.(*constExpr); ok && k.v.k == kindString {
			s := k.v.s()
			return func(fr *frame) bool {
				a := l(fr)
				return (a.k == kindString && a.s() == s) != ne
			}
		}
		return func(fr *frame) bool {
			a, b := l(fr), r(fr)
			if a.k == b.k && (a.k == kindInt || a.k == kindBool || a.k == kindNil) {
				return (a.n == b.n) != ne
			}
			return fr.vm.equal(a, b) != ne
		}
	case tLt, tGt:
		if e.op == tGt {
			l, r = r, l
		}
		if k, ok := e.r.(*constExpr); ok && k.v.k == kindInt && e.op == tLt {
			n := k.v.i()
			return func(fr *frame) bool {
				a := l(fr)
				if a.k == kindInt {
					return a.i() < n
				}
				return fr.vm.lessThan(a, Int(n))
			}
		}
		return func(fr *frame) bool {
			a, b := l(fr), r(fr)
			if a.k == kindInt && b.k == kindInt {
				return a.i() < b.i()
			}
			if a.k == kindFloat && b.k == kindFloat {
				return a.f() < b.f()
			}
			return fr.vm.lessThan(a, b)
		}
	}
	// <= and >=
	if e.op == tGe {
		l, r = r, l
	}
	return func(fr *frame) bool {
		a, b := l(fr), r(fr)
		if a.k == kindInt && b.k == kindInt {
			return a.i() <= b.i()
		}
		if a.k == kindFloat && b.k == kindFloat {
			return a.f() <= b.f()
		}
		return fr.vm.lessEqual(a, b)
	}
}

// multi compiles an expression that may produce several values.
func (c *compiler) multi(e expr) multiFn {
	switch e := e.(type) {
	case *callExpr:
		return c.call(e)
	case *methodExpr:
		return c.method(e)
	case *varargExpr:
		return func(fr *frame) []Value { return fr.varargs }
	}
	x := c.expr(e)
	return func(fr *frame) []Value { return fr.vm.ret1(x(fr)) }
}

// args compiles call arguments.
func (c *compiler) args(es []expr) argsFn {
	if len(es) == 0 {
		return func(*frame) int { return 0 }
	}
	fixed := es
	var last multiFn
	if isMulti(es[len(es)-1]) {
		last = c.multi(es[len(es)-1])
		fixed = es[:len(es)-1]
	}
	fns := make([]exprFn, len(fixed))
	for i, e := range fixed {
		fns[i] = c.expr(e)
	}
	if last == nil && len(fns) == 1 {
		a := fns[0]
		return func(fr *frame) int {
			vm := fr.vm
			base := vm.top
			v := a(fr)
			vm.ensure(base + 1)
			vm.stack[base] = v
			vm.top = base + 1
			return 1
		}
	}
	return func(fr *frame) int {
		vm := fr.vm
		base := vm.top
		vm.ensure(base + len(fns))
		for i, f := range fns {
			vm.top = base + i
			v := f(fr)
			vm.stack[base+i] = v
		}
		n := len(fns)
		vm.top = base + n
		if last != nil {
			res := last(fr)
			vm.ensure(base + n + len(res))
			copy(vm.stack[base+n:], res)
			n += len(res)
			vm.top = base + n
		}
		return n
	}
}

func (c *compiler) call(e *callExpr) multiFn {
	fn, args, desc, line := c.expr(e.fn), c.args(e.args), c.describe(e.fn), e.line
	return func(fr *frame) []Value {
		f := fn(fr)
		vm := fr.vm
		base := vm.top
		n := args(fr)
		fr.line = line
		res := vm.call(f, vm.stack[base:base+n], desc)
		vm.top = base
		return res
	}
}

func (c *compiler) method(e *methodExpr) multiFn {
	obj, args, line := c.expr(e.obj), c.args(e.args), e.line
	name, key, desc := e.name, String(e.name), c.describe(e)
	objDesc := c.describe(e.obj)
	return func(fr *frame) []Value {
		o := obj(fr)
		vm := fr.vm
		fr.line = line
		var f Value
		if t, ok := o.p.(*Table); ok && o.k == kindTable {
			f = t.GetString(name)
			if f.IsNil() && t.meta != nil {
				f = vm.index(o, key, objDesc)
			}
		} else {
			f = vm.index(o, key, objDesc)
		}
		base := vm.top
		vm.ensure(base + 1)
		vm.stack[base] = o
		vm.top = base + 1
		n := args(fr)
		fr.line = line
		res := vm.call(f, vm.stack[base:base+1+n], desc)
		vm.top = base
		return res
	}
}

// expr compiles an expression that produces one value.
func (c *compiler) expr(e expr) exprFn {
	switch e := e.(type) {
	case *nilExpr:
		return func(*frame) Value { return Nil }
	case *trueExpr:
		return func(*frame) Value { return True }
	case *falseExpr:
		return func(*frame) Value { return False }
	case *constExpr:
		v := e.v
		return func(*frame) Value { return v }
	case *varargExpr:
		return func(fr *frame) Value {
			if len(fr.varargs) > 0 {
				return fr.varargs[0]
			}
			return Nil
		}
	case *parenExpr:
		if isMulti(e.e) {
			m := c.multi(e.e)
			return func(fr *frame) Value {
				if r := m(fr); len(r) > 0 {
					return r[0]
				}
				return Nil
			}
		}
		return c.expr(e.e)
	case *callExpr, *methodExpr:
		m := c.multi(e)
		return func(fr *frame) Value {
			if r := m(fr); len(r) > 0 {
				return r[0]
			}
			return Nil
		}
	case *funcExpr:
		p := compileFunc(c.chunk, e.f, c)
		return func(fr *frame) Value { return makeClosure(fr, p) }
	case *nameExpr:
		return c.name(e)
	case *indexExpr:
		return c.index(e)
	case *tableExpr:
		return c.table(e)
	case *unExpr:
		return c.unary(e)
	case *binExpr:
		return c.binary(e)
	}
	panic("lua: unknown expression")
}

func (c *compiler) name(e *nameExpr) exprFn {
	v := e.v
	switch {
	case v == nil:
		name, key := e.name, String(e.name)
		return func(fr *frame) Value {
			g := fr.vm.globals
			val := g.GetString(name)
			if val.IsNil() && g.meta != nil {
				return fr.vm.index(TableValue(g), key, "")
			}
			return val
		}
	case v.fn == c.pf && v.captured:
		slot := v.slot
		return func(fr *frame) Value { return fr.boxes[slot].v }
	case v.fn == c.pf:
		slot := v.slot
		return func(fr *frame) Value { return fr.reg[slot] }
	}
	i := c.upval(v)
	return func(fr *frame) Value { return fr.fn.upvals[i].v }
}

func (c *compiler) index(e *indexExpr) exprFn {
	obj, desc := c.expr(e.obj), c.describe(e.obj)
	if k, ok := e.key.(*constExpr); ok && k.v.k == kindString {
		name, key := k.v.s(), k.v
		return func(fr *frame) Value {
			o := obj(fr)
			if t, ok := o.p.(*Table); ok && o.k == kindTable {
				v := t.GetString(name)
				if !v.IsNil() || t.meta == nil {
					return v
				}
			}
			return fr.vm.index(o, key, desc)
		}
	}
	key := c.expr(e.key)
	return func(fr *frame) Value {
		o := obj(fr)
		k := key(fr)
		if t, ok := o.p.(*Table); ok && o.k == kindTable {
			var v Value
			if k.k == kindInt {
				v = t.GetInt(k.i())
			} else {
				v = t.Get(k)
			}
			if !v.IsNil() || t.meta == nil {
				return v
			}
		}
		return fr.vm.index(o, k, desc)
	}
}

func (c *compiler) table(e *tableExpr) exprFn {
	type field struct {
		key, val exprFn
		multi    multiFn
	}
	fields := make([]field, len(e.fields))
	npos, nkey := 0, 0
	for i, f := range e.fields {
		switch {
		case f.key != nil:
			fields[i] = field{key: c.expr(f.key), val: c.expr(f.val)}
			nkey++
		case i == len(e.fields)-1 && isMulti(f.val):
			fields[i] = field{multi: c.multi(f.val)}
		default:
			fields[i] = field{val: c.expr(f.val)}
			npos++
		}
	}
	if len(fields) == 0 {
		return func(*frame) Value { return TableValue(&Table{}) }
	}
	// Positional values are stored in batches of 50, after the keyed fields evaluated before
	// them, as Lua's own compiler does: in {[1] = "a", "b"}, t[1] is "b".
	const batch = 50
	return func(fr *frame) Value {
		vm := fr.vm
		t := NewTable(npos, nkey)
		base := vm.top
		pos := int64(1)
		pending := 0
		flush := func() {
			for i := 0; i < pending; i++ {
				t.SetInt(pos, vm.stack[base+i])
				pos++
			}
			clear(vm.stack[base : base+pending])
			pending = 0
			vm.top = base
		}
		for _, f := range fields {
			switch {
			case f.multi != nil:
				res := f.multi(fr)
				flush()
				for _, v := range res {
					t.SetInt(pos, v)
					pos++
				}
			case f.key != nil:
				k := f.key(fr)
				v := f.val(fr)
				vm.rawSet(t, k, v)
			default:
				v := f.val(fr)
				vm.ensure(base + pending + 1)
				vm.stack[base+pending] = v
				pending++
				vm.top = base + pending
				if pending == batch {
					flush()
				}
			}
		}
		flush()
		return TableValue(t)
	}
}

func (c *compiler) unary(e *unExpr) exprFn {
	if e.op == tNot {
		cond := c.cond(e.e)
		return func(fr *frame) Value { return Bool(!cond(fr)) }
	}
	x, desc := c.expr(e.e), c.describe(e.e)
	switch e.op {
	case tMinus:
		return func(fr *frame) Value {
			v := x(fr)
			switch v.k {
			case kindInt:
				return Int(-v.i())
			case kindFloat:
				return Float(-v.f())
			}
			return fr.vm.arith(opUnm, v, v, desc, desc)
		}
	case tHash:
		return func(fr *frame) Value {
			v := x(fr)
			if t, ok := v.p.(*Table); ok && v.k == kindTable && t.meta == nil {
				return Int(int64(t.Len()))
			}
			return fr.vm.length(v, desc)
		}
	case tTilde:
		return func(fr *frame) Value {
			v := x(fr)
			if v.k == kindInt {
				return Int(^v.i())
			}
			return fr.vm.arith(opBnot, v, v, desc, desc)
		}
	}
	panic("lua: unknown unary operator")
}

var binaryOps = map[tok]arithOp{
	tPlus: opAdd, tMinus: opSub, tStar: opMul, tSlash: opDiv, tPercent: opMod, tCaret: opPow,
	tDSlash: opIDiv, tAmp: opBand, tPipe: opBor, tTilde: opBxor, tShl: opShl, tShr: opShr,
}

func (c *compiler) binary(e *binExpr) exprFn {
	switch e.op {
	case tAnd:
		l, r := c.expr(e.l), c.expr(e.r)
		return func(fr *frame) Value {
			if a := l(fr); !a.Truthy() {
				return a
			}
			return r(fr)
		}
	case tOr:
		l, r := c.expr(e.l), c.expr(e.r)
		return func(fr *frame) Value {
			if a := l(fr); a.Truthy() {
				return a
			}
			return r(fr)
		}
	case tEq, tNe, tLt, tLe, tGt, tGe:
		cond := c.compare(e)
		return func(fr *frame) Value { return Bool(cond(fr)) }
	case tConcat:
		return c.concat(e)
	}
	op := binaryOps[e.op]
	l, r := c.expr(e.l), c.expr(e.r)
	ld, rd := c.describe(e.l), c.describe(e.r)
	if k, ok := e.r.(*constExpr); ok && k.v.k == kindInt && (op == opAdd || op == opSub) {
		n := k.v.i()
		if op == opSub {
			n = -n
		}
		kf := float64(n)
		return func(fr *frame) Value {
			a := l(fr)
			switch a.k {
			case kindInt:
				return Int(a.i() + n)
			case kindFloat:
				return Float(a.f() + kf)
			}
			return fr.vm.arith(op, a, k.v, ld, rd)
		}
	}
	switch op {
	case opAdd:
		return func(fr *frame) Value {
			a, b := l(fr), r(fr)
			if a.k == kindInt && b.k == kindInt {
				return Int(a.i() + b.i())
			}
			if a.k == kindFloat && b.k == kindFloat {
				return Float(a.f() + b.f())
			}
			if a.IsNumber() && b.IsNumber() {
				return fr.vm.numArith(op, a, b)
			}
			return fr.vm.arith(op, a, b, ld, rd)
		}
	case opSub:
		return func(fr *frame) Value {
			a, b := l(fr), r(fr)
			if a.k == kindInt && b.k == kindInt {
				return Int(a.i() - b.i())
			}
			if a.k == kindFloat && b.k == kindFloat {
				return Float(a.f() - b.f())
			}
			if a.IsNumber() && b.IsNumber() {
				return fr.vm.numArith(op, a, b)
			}
			return fr.vm.arith(op, a, b, ld, rd)
		}
	case opMul:
		return func(fr *frame) Value {
			a, b := l(fr), r(fr)
			if a.k == kindInt && b.k == kindInt {
				return Int(a.i() * b.i())
			}
			if a.k == kindFloat && b.k == kindFloat {
				return Float(float64(a.f() * b.f()))
			}
			if a.IsNumber() && b.IsNumber() {
				return fr.vm.numArith(op, a, b)
			}
			return fr.vm.arith(op, a, b, ld, rd)
		}
	case opDiv:
		return func(fr *frame) Value {
			a, b := l(fr), r(fr)
			if a.k == kindFloat && b.k == kindFloat {
				return Float(a.f() / b.f())
			}
			if a.IsNumber() && b.IsNumber() {
				return fr.vm.numArith(op, a, b)
			}
			return fr.vm.arith(op, a, b, ld, rd)
		}
	}
	return func(fr *frame) Value { return fr.vm.arith(op, l(fr), r(fr), ld, rd) }
}

// concat compiles a chain a .. b .. c into one operation.
func (c *compiler) concat(e *binExpr) exprFn {
	var parts []expr
	for {
		parts = append(parts, e.l)
		next, ok := e.r.(*binExpr)
		if !ok || next.op != tConcat {
			parts = append(parts, e.r)
			break
		}
		e = next
	}
	fns := make([]exprFn, len(parts))
	descs := make([]string, len(parts))
	for i, p := range parts {
		fns[i], descs[i] = c.expr(p), c.describe(p)
	}
	if len(fns) == 2 {
		a, b := fns[0], fns[1]
		return func(fr *frame) Value {
			x := a(fr)
			y := b(fr)
			if x.k == kindString && y.k == kindString {
				return String(x.s() + y.s())
			}
			return fr.vm.concat2(x, y, descs[0], descs[1])
		}
	}
	return func(fr *frame) Value {
		vm := fr.vm
		base := vm.top
		vm.ensure(base + len(fns))
		for i, f := range fns {
			vm.top = base + i
			v := f(fr)
			vm.stack[base+i] = v
		}
		vm.top = base + len(fns)
		r := vm.concat(vm.stack[base:base+len(fns)], descs)
		clear(vm.stack[base : base+len(fns)])
		vm.top = base
		return r
	}
}
