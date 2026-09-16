package lua

// The syntax tree the parser builds and the compiler turns into closures. Names are
// resolved while parsing: a name expression points at the local variable it reads, or at
// none for a global.

type expr interface{ exprLine() int }

type stmt interface{ stmtLine() int }

// localVar is one declared local variable.
type localVar struct {
	name     string
	fn       *parseFunc // the function that declares it
	captured bool       // read or written by a nested function: lives in a cell
	constant bool       // declared <const>
	slot     int        // register or cell slot, assigned by the compiler
}

type (
	nilExpr    struct{ line int }
	trueExpr   struct{ line int }
	falseExpr  struct{ line int }
	varargExpr struct{ line int }
	constExpr  struct {
		line int
		v    Value // a number or a string
	}
	funcExpr struct {
		line int
		f    *parseFunc
	}
	tableExpr struct {
		line   int
		fields []tableField
	}
	binExpr struct {
		line int
		op   tok
		l, r expr
	}
	unExpr struct {
		line int
		op   tok
		e    expr
	}
	nameExpr struct {
		line int
		name string
		v    *localVar // nil: a global
	}
	indexExpr struct {
		line     int
		obj, key expr
	}
	callExpr struct {
		line int
		fn   expr
		args []expr
	}
	methodExpr struct {
		line int
		obj  expr
		name string
		args []expr
	}
	parenExpr struct {
		line int
		e    expr
	}
)

// tableField is one field of a table constructor; key is nil for a positional field.
type tableField struct {
	key, val expr
}

func (e *nilExpr) exprLine() int    { return e.line }
func (e *trueExpr) exprLine() int   { return e.line }
func (e *falseExpr) exprLine() int  { return e.line }
func (e *varargExpr) exprLine() int { return e.line }
func (e *constExpr) exprLine() int  { return e.line }
func (e *funcExpr) exprLine() int   { return e.line }
func (e *tableExpr) exprLine() int  { return e.line }
func (e *binExpr) exprLine() int    { return e.line }
func (e *unExpr) exprLine() int     { return e.line }
func (e *nameExpr) exprLine() int   { return e.line }
func (e *indexExpr) exprLine() int  { return e.line }
func (e *callExpr) exprLine() int   { return e.line }
func (e *methodExpr) exprLine() int { return e.line }
func (e *parenExpr) exprLine() int  { return e.line }

// isMulti reports whether e can produce several values: a call or '...'.
func isMulti(e expr) bool {
	switch e.(type) {
	case *callExpr, *methodExpr, *varargExpr:
		return true
	}
	return false
}

type (
	localStmt struct {
		line  int
		vars  []*localVar
		exprs []expr
	}
	assignStmt struct {
		line    int
		targets []expr // nameExpr or indexExpr
		exprs   []expr
	}
	callStmt struct {
		line int
		call expr
	}
	doStmt struct {
		line int
		body []stmt
	}
	whileStmt struct {
		line int
		cond expr
		body []stmt
	}
	repeatStmt struct {
		line int
		body []stmt
		cond expr
	}
	ifStmt struct {
		line   int
		conds  []expr
		blocks [][]stmt
		els    []stmt // nil when there is no else
	}
	numForStmt struct {
		line               int
		v                  *localVar
		start, limit, step expr // step nil: 1
		body               []stmt
	}
	genForStmt struct {
		line  int
		vars  []*localVar
		exprs []expr
		body  []stmt
	}
	localFuncStmt struct {
		line int
		v    *localVar
		f    *parseFunc
	}
	returnStmt struct {
		line  int
		exprs []expr
	}
	breakStmt struct{ line int }
)

func (s *localStmt) stmtLine() int     { return s.line }
func (s *assignStmt) stmtLine() int    { return s.line }
func (s *callStmt) stmtLine() int      { return s.line }
func (s *doStmt) stmtLine() int        { return s.line }
func (s *whileStmt) stmtLine() int     { return s.line }
func (s *repeatStmt) stmtLine() int    { return s.line }
func (s *ifStmt) stmtLine() int        { return s.line }
func (s *numForStmt) stmtLine() int    { return s.line }
func (s *genForStmt) stmtLine() int    { return s.line }
func (s *localFuncStmt) stmtLine() int { return s.line }
func (s *returnStmt) stmtLine() int    { return s.line }
func (s *breakStmt) stmtLine() int     { return s.line }

// parseFunc is a function being parsed, and then its syntax tree.
type parseFunc struct {
	parent *parseFunc
	name   string // for tracebacks: "main chunk", "function 'f'", …
	line   int
	params []*localVar
	vararg bool
	body   []stmt
	scopes [][]*localVar // open blocks while parsing, innermost last
	loops  int           // open loops while parsing, for break
}
