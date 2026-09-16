package lua

import "fmt"

// parser is a recursive-descent parser for Lua 5.4, resolving names as it goes.
type parser struct {
	lx    lexer
	tok   token // current token
	ahead token
	has   bool // ahead holds a token
	fn    *parseFunc
}

// parse parses a chunk into its main function, a vararg function with no parameters.
func parse(chunk, src string) (f *parseFunc, err error) {
	defer func() {
		if r := recover(); r != nil {
			se, ok := r.(*SyntaxError)
			if !ok {
				panic(r)
			}
			err = se
		}
	}()
	p := &parser{lx: lexer{chunk: chunk, src: src, line: 1}}
	p.advance()
	main := &parseFunc{name: "main chunk", line: 0, vararg: true}
	p.fn = main
	p.openScope()
	main.body = p.block()
	p.closeScope()
	if p.tok.t != tEOF {
		p.errorExpected("<eof>")
	}
	return main, nil
}

func (p *parser) advance() {
	if p.has {
		p.tok, p.has = p.ahead, false
		return
	}
	p.tok = p.lx.next()
}

func (p *parser) peek() token {
	if !p.has {
		p.ahead, p.has = p.lx.next(), true
	}
	return p.ahead
}

// near describes the current token for an error message.
func (p *parser) near() string {
	switch p.tok.t {
	case tEOF:
		return "<eof>"
	case tName, tNumber:
		return "'" + p.tok.s + "'"
	case tString:
		return "'" + p.tok.s + "'"
	}
	return "'" + p.tok.t.String() + "'"
}

func (p *parser) errorf(format string, args ...any) {
	panic(&SyntaxError{Chunk: p.lx.chunk, Line: p.tok.line, Msg: fmt.Sprintf(format, args...) + " near " + p.near()})
}

func (p *parser) errorExpected(what string) { p.errorf("%s expected", what) }

func (p *parser) check(t tok) {
	if p.tok.t != t {
		name := t.String()
		if t > tString {
			name = "'" + name + "'"
		}
		p.errorExpected(name)
	}
}

func (p *parser) expect(t tok) {
	p.check(t)
	p.advance()
}

// match closes what was opened at line with the token what: the message names both when
// they are on different lines.
func (p *parser) match(what, who tok, line int) {
	if p.tok.t == what {
		p.advance()
		return
	}
	if line == p.tok.line {
		p.errorExpected("'" + what.String() + "'")
	}
	p.errorf("'%s' expected (to close '%s' at line %d)", what, who, line)
}

func (p *parser) name() string {
	p.check(tName)
	s := p.tok.s
	p.advance()
	return s
}

func (p *parser) openScope()  { p.fn.scopes = append(p.fn.scopes, nil) }
func (p *parser) closeScope() { p.fn.scopes = p.fn.scopes[:len(p.fn.scopes)-1] }

// declare adds a local to the innermost scope. It is visible from then on.
func (p *parser) declare(v *localVar) {
	v.fn = p.fn
	s := &p.fn.scopes[len(p.fn.scopes)-1]
	*s = append(*s, v)
}

// resolve finds the local a name refers to, marking it captured when it belongs to an
// enclosing function; nil means a global.
func (p *parser) resolve(name string) *localVar {
	for f := p.fn; f != nil; f = f.parent {
		for i := len(f.scopes) - 1; i >= 0; i-- {
			sc := f.scopes[i]
			for j := len(sc) - 1; j >= 0; j-- {
				if sc[j].name == name {
					if f != p.fn {
						sc[j].captured = true
					}
					return sc[j]
				}
			}
		}
	}
	return nil
}

// blockEnd reports whether the current token ends a block.
func (p *parser) blockEnd() bool {
	switch p.tok.t {
	case tEOF, tEnd, tElse, tElseif, tUntil:
		return true
	}
	return false
}

// block parses statements up to the end of a block; the caller opens and closes its scope.
func (p *parser) block() []stmt {
	var out []stmt
	for !p.blockEnd() {
		if p.tok.t == tReturn {
			out = append(out, p.retstat())
			break
		}
		if s := p.statement(); s != nil {
			out = append(out, s)
		}
	}
	return out
}

func (p *parser) scopedBlock() []stmt {
	p.openScope()
	b := p.block()
	p.closeScope()
	return b
}

func (p *parser) retstat() stmt {
	line := p.tok.line
	p.advance()
	s := &returnStmt{line: line}
	if !p.blockEnd() && p.tok.t != tSemi {
		s.exprs = p.exprList()
	}
	if p.tok.t == tSemi {
		p.advance()
	}
	if !p.blockEnd() {
		p.errorExpected("<eof>")
	}
	return s
}

func (p *parser) statement() stmt {
	line := p.tok.line
	switch p.tok.t {
	case tSemi:
		p.advance()
		return nil
	case tIf:
		return p.ifStat(line)
	case tWhile:
		p.advance()
		cond := p.expr()
		p.expect(tDo)
		p.fn.loops++
		body := p.scopedBlock()
		p.fn.loops--
		p.match(tEnd, tWhile, line)
		return &whileStmt{line: line, cond: cond, body: body}
	case tDo:
		p.advance()
		body := p.scopedBlock()
		p.match(tEnd, tDo, line)
		return &doStmt{line: line, body: body}
	case tFor:
		return p.forStat(line)
	case tRepeat:
		p.advance()
		p.fn.loops++
		p.openScope() // the condition sees the body's locals
		body := p.block()
		p.match(tUntil, tRepeat, line)
		cond := p.expr()
		p.closeScope()
		p.fn.loops--
		return &repeatStmt{line: line, body: body, cond: cond}
	case tFunction:
		return p.funcStat(line)
	case tLocal:
		p.advance()
		if p.tok.t == tFunction {
			p.advance()
			v := &localVar{name: p.name()}
			p.declare(v) // visible in its own body, for recursion
			f := p.funcBody(line, "local function '"+v.name+"'", false)
			return &localFuncStmt{line: line, v: v, f: f}
		}
		return p.localStat(line)
	case tDColon:
		p.errorf("labels are not supported")
	case tReturn:
		return p.retstat()
	case tBreak:
		p.advance()
		if p.fn.loops == 0 {
			panic(&SyntaxError{Chunk: p.lx.chunk, Line: line, Msg: fmt.Sprintf("break outside loop at line %d", line)})
		}
		return &breakStmt{line: line}
	case tGoto:
		p.errorf("goto is not supported")
	}
	return p.exprStat(line)
}

func (p *parser) ifStat(line int) stmt {
	s := &ifStmt{line: line}
	p.advance()
	s.conds = append(s.conds, p.expr())
	p.expect(tThen)
	s.blocks = append(s.blocks, p.scopedBlock())
	for p.tok.t == tElseif {
		p.advance()
		s.conds = append(s.conds, p.expr())
		p.expect(tThen)
		s.blocks = append(s.blocks, p.scopedBlock())
	}
	if p.tok.t == tElse {
		p.advance()
		s.els = p.scopedBlock()
		if s.els == nil {
			s.els = []stmt{}
		}
	}
	p.match(tEnd, tIf, line)
	return s
}

func (p *parser) forStat(line int) stmt {
	p.advance()
	first := p.name()
	if p.tok.t == tAssign {
		p.advance()
		s := &numForStmt{line: line}
		s.start = p.expr()
		p.expect(tComma)
		s.limit = p.expr()
		if p.tok.t == tComma {
			p.advance()
			s.step = p.expr()
		}
		p.expect(tDo)
		p.openScope()
		s.v = &localVar{name: first}
		p.declare(s.v)
		p.fn.loops++
		s.body = p.block()
		p.fn.loops--
		p.closeScope()
		p.match(tEnd, tFor, line)
		return s
	}
	names := []string{first}
	for p.tok.t == tComma {
		p.advance()
		names = append(names, p.name())
	}
	p.expect(tIn)
	s := &genForStmt{line: line}
	s.exprs = p.exprList()
	p.expect(tDo)
	p.openScope()
	for _, n := range names {
		v := &localVar{name: n}
		p.declare(v)
		s.vars = append(s.vars, v)
	}
	p.fn.loops++
	s.body = p.block()
	p.fn.loops--
	p.closeScope()
	p.match(tEnd, tFor, line)
	return s
}

// funcStat parses function a.b.c:m() … end, an assignment of a function.
func (p *parser) funcStat(line int) stmt {
	p.advance()
	n := p.name()
	var target expr = &nameExpr{line: line, name: n, v: p.resolve(n)}
	full := n
	method := false
	for p.tok.t == tDot || p.tok.t == tColon {
		sep := p.tok.t
		p.advance()
		key := p.name()
		target = &indexExpr{line: line, obj: target, key: &constExpr{line: line, v: String(key)}}
		if sep == tColon {
			full += ":" + key
			method = true
			break
		}
		full += "." + key
	}
	kind := "function"
	if method {
		kind = "method"
	}
	f := p.funcBody(line, kind+" '"+full+"'", method)
	return &assignStmt{line: line, targets: []expr{target}, exprs: []expr{&funcExpr{line: line, f: f}}}
}

func (p *parser) localStat(line int) stmt {
	s := &localStmt{line: line}
	for {
		v := &localVar{name: p.name()}
		if p.tok.t == tLt {
			p.advance()
			switch attr := p.name(); attr {
			case "const":
				v.constant = true
			case "close":
				p.errorf("to-be-closed variables are not supported")
			default:
				p.errorf("unknown attribute '%s'", attr)
			}
			p.expect(tGt)
		}
		s.vars = append(s.vars, v)
		if p.tok.t != tComma {
			break
		}
		p.advance()
	}
	if p.tok.t == tAssign {
		p.advance()
		s.exprs = p.exprList()
	}
	for _, v := range s.vars { // declared after the expressions: local x = x reads the outer x
		p.declare(v)
	}
	return s
}

func (p *parser) exprStat(line int) stmt {
	e := p.suffixedExpr()
	if p.tok.t == tAssign || p.tok.t == tComma {
		targets := []expr{p.assignable(e)}
		for p.tok.t == tComma {
			p.advance()
			targets = append(targets, p.assignable(p.suffixedExpr()))
		}
		p.expect(tAssign)
		return &assignStmt{line: line, targets: targets, exprs: p.exprList()}
	}
	switch e.(type) {
	case *callExpr, *methodExpr:
		return &callStmt{line: line, call: e}
	}
	p.errorf("syntax error")
	return nil
}

func (p *parser) assignable(e expr) expr {
	switch t := e.(type) {
	case *nameExpr:
		if t.v != nil && t.v.constant {
			panic(&SyntaxError{Chunk: p.lx.chunk, Line: t.line, Msg: fmt.Sprintf("attempt to assign to const variable '%s'", t.name)})
		}
		return e
	case *indexExpr:
		return e
	}
	p.errorf("syntax error")
	return nil
}

func (p *parser) funcBody(line int, name string, method bool) *parseFunc {
	f := &parseFunc{parent: p.fn, name: name, line: line}
	p.fn = f
	p.openScope()
	if method {
		self := &localVar{name: "self"}
		p.declare(self)
		f.params = append(f.params, self)
	}
	p.expect(tLParen)
	if p.tok.t != tRParen {
		for {
			if p.tok.t == tDots {
				p.advance()
				f.vararg = true
				break
			}
			v := &localVar{name: p.name()}
			p.declare(v)
			f.params = append(f.params, v)
			if p.tok.t != tComma {
				break
			}
			p.advance()
		}
	}
	p.expect(tRParen)
	f.body = p.block()
	p.match(tEnd, tFunction, line)
	p.closeScope()
	p.fn = f.parent
	return f
}

func (p *parser) exprList() []expr {
	list := []expr{p.expr()}
	for p.tok.t == tComma {
		p.advance()
		list = append(list, p.expr())
	}
	return list
}

func (p *parser) primaryExpr() expr {
	line := p.tok.line
	switch p.tok.t {
	case tName:
		n := p.tok.s
		p.advance()
		return &nameExpr{line: line, name: n, v: p.resolve(n)}
	case tLParen:
		p.advance()
		e := p.expr()
		p.match(tRParen, tLParen, line)
		return &parenExpr{line: line, e: e}
	}
	p.errorf("unexpected symbol")
	return nil
}

func (p *parser) suffixedExpr() expr {
	e := p.primaryExpr()
	for {
		line := p.tok.line
		switch p.tok.t {
		case tDot:
			p.advance()
			e = &indexExpr{line: line, obj: e, key: &constExpr{line: line, v: String(p.name())}}
		case tLBracket:
			p.advance()
			k := p.expr()
			p.expect(tRBracket)
			e = &indexExpr{line: line, obj: e, key: k}
		case tColon:
			p.advance()
			n := p.name()
			e = &methodExpr{line: line, obj: e, name: n, args: p.callArgs()}
		case tLParen, tString, tLBrace:
			e = &callExpr{line: line, fn: e, args: p.callArgs()}
		default:
			return e
		}
	}
}

func (p *parser) callArgs() []expr {
	line := p.tok.line
	switch p.tok.t {
	case tString:
		s := p.tok.s
		p.advance()
		return []expr{&constExpr{line: line, v: String(s)}}
	case tLBrace:
		return []expr{p.tableCons()}
	case tLParen:
		p.advance()
		var args []expr
		if p.tok.t != tRParen {
			args = p.exprList()
		}
		p.match(tRParen, tLParen, line)
		return args
	}
	p.errorf("function arguments expected")
	return nil
}

func (p *parser) tableCons() expr {
	line := p.tok.line
	p.expect(tLBrace)
	t := &tableExpr{line: line}
	for p.tok.t != tRBrace {
		switch {
		case p.tok.t == tLBracket:
			p.advance()
			k := p.expr()
			p.expect(tRBracket)
			p.expect(tAssign)
			t.fields = append(t.fields, tableField{key: k, val: p.expr()})
		case p.tok.t == tName && p.peek().t == tAssign:
			k := &constExpr{line: p.tok.line, v: String(p.tok.s)}
			p.advance()
			p.advance()
			t.fields = append(t.fields, tableField{key: k, val: p.expr()})
		default:
			t.fields = append(t.fields, tableField{val: p.expr()})
		}
		if p.tok.t != tComma && p.tok.t != tSemi {
			break
		}
		p.advance()
	}
	p.match(tRBrace, tLBrace, line)
	return t
}

func (p *parser) simpleExpr() expr {
	line := p.tok.line
	switch p.tok.t {
	case tNumber:
		v := p.tok.num
		p.advance()
		return &constExpr{line: line, v: v}
	case tString:
		s := p.tok.s
		p.advance()
		return &constExpr{line: line, v: String(s)}
	case tNil:
		p.advance()
		return &nilExpr{line: line}
	case tTrue:
		p.advance()
		return &trueExpr{line: line}
	case tFalse:
		p.advance()
		return &falseExpr{line: line}
	case tDots:
		if !p.fn.vararg {
			p.errorf("cannot use '...' outside a vararg function")
		}
		p.advance()
		return &varargExpr{line: line}
	case tLBrace:
		return p.tableCons()
	case tFunction:
		p.advance()
		return &funcExpr{line: line, f: p.funcBody(line, "anonymous function", false)}
	}
	return p.suffixedExpr()
}

// binary operator priorities: left and right, as in Lua's own parser.
var binaryPriority = map[tok][2]int{
	tPlus: {10, 10}, tMinus: {10, 10},
	tStar: {11, 11}, tPercent: {11, 11}, tSlash: {11, 11}, tDSlash: {11, 11},
	tCaret: {14, 13},
	tAmp:   {6, 6}, tTilde: {5, 5}, tPipe: {4, 4},
	tShl: {7, 7}, tShr: {7, 7},
	tConcat: {9, 8},
	tEq:     {3, 3}, tNe: {3, 3}, tLt: {3, 3}, tLe: {3, 3}, tGt: {3, 3}, tGe: {3, 3},
	tAnd: {2, 2}, tOr: {1, 1},
}

const unaryPriority = 12

func (p *parser) expr() expr { return p.subExpr(0) }

// subExpr parses an expression whose binary operators bind tighter than limit.
func (p *parser) subExpr(limit int) expr {
	var e expr
	switch op := p.tok.t; op {
	case tNot, tMinus, tHash, tTilde:
		line := p.tok.line
		p.advance()
		e = foldUnary(&unExpr{line: line, op: op, e: p.subExpr(unaryPriority)})
	default:
		e = p.simpleExpr()
	}
	for {
		op := p.tok.t
		prio, ok := binaryPriority[op]
		if !ok || prio[0] <= limit {
			return e
		}
		line := p.tok.line
		p.advance()
		e = &binExpr{line: line, op: op, l: e, r: p.subExpr(prio[1])}
	}
}

// foldUnary computes the negation of a numeric constant, so that -1 and the smallest
// integer are constants.
func foldUnary(u *unExpr) expr {
	c, ok := u.e.(*constExpr)
	if !ok || u.op != tMinus {
		return u
	}
	switch c.v.k {
	case kindInt:
		return &constExpr{line: u.line, v: Int(-c.v.i())}
	case kindFloat:
		return &constExpr{line: u.line, v: Float(-c.v.f())}
	}
	return u
}
