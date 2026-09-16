package lua

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// token kinds
type tok int

const (
	tEOF tok = iota
	tName
	tNumber
	tString

	// keywords
	tAnd
	tBreak
	tDo
	tElse
	tElseif
	tEnd
	tFalse
	tFor
	tFunction
	tGoto
	tIf
	tIn
	tLocal
	tNil
	tNot
	tOr
	tRepeat
	tReturn
	tThen
	tTrue
	tUntil
	tWhile

	// symbols
	tPlus     // +
	tMinus    // -
	tStar     // *
	tSlash    // /
	tDSlash   // //
	tPercent  // %
	tCaret    // ^
	tHash     // #
	tAmp      // &
	tTilde    // ~
	tPipe     // |
	tShl      // <<
	tShr      // >>
	tEq       // ==
	tNe       // ~=
	tLe       // <=
	tGe       // >=
	tLt       // <
	tGt       // >
	tAssign   // =
	tLParen   // (
	tRParen   // )
	tLBrace   // {
	tRBrace   // }
	tLBracket // [
	tRBracket // ]
	tDColon   // ::
	tSemi     // ;
	tColon    // :
	tComma    // ,
	tDot      // .
	tConcat   // ..
	tDots     // ...
)

var keywords = map[string]tok{
	"and": tAnd, "break": tBreak, "do": tDo, "else": tElse, "elseif": tElseif, "end": tEnd,
	"false": tFalse, "for": tFor, "function": tFunction, "goto": tGoto, "if": tIf, "in": tIn,
	"local": tLocal, "nil": tNil, "not": tNot, "or": tOr, "repeat": tRepeat, "return": tReturn,
	"then": tThen, "true": tTrue, "until": tUntil, "while": tWhile,
}

var twoCharToks = map[string]tok{"//": tDSlash, "<<": tShl, ">>": tShr, "==": tEq, "~=": tNe, "<=": tLe, ">=": tGe, "::": tDColon, "..": tConcat}

var oneCharToks = map[byte]tok{'+': tPlus, '-': tMinus, '*': tStar, '/': tSlash, '%': tPercent, '^': tCaret, '#': tHash,
	'&': tAmp, '~': tTilde, '|': tPipe, '<': tLt, '>': tGt, '=': tAssign, '(': tLParen, ')': tRParen,
	'{': tLBrace, '}': tRBrace, '[': tLBracket, ']': tRBracket, ';': tSemi, ':': tColon, ',': tComma, '.': tDot}

var tokNames = map[tok]string{
	tEOF: "<eof>", tName: "<name>", tNumber: "<number>", tString: "<string>",
	tPlus: "+", tMinus: "-", tStar: "*", tSlash: "/", tDSlash: "//", tPercent: "%", tCaret: "^",
	tHash: "#", tAmp: "&", tTilde: "~", tPipe: "|", tShl: "<<", tShr: ">>", tEq: "==", tNe: "~=",
	tLe: "<=", tGe: ">=", tLt: "<", tGt: ">", tAssign: "=", tLParen: "(", tRParen: ")",
	tLBrace: "{", tRBrace: "}", tLBracket: "[", tRBracket: "]", tDColon: "::", tSemi: ";",
	tColon: ":", tComma: ",", tDot: ".", tConcat: "..", tDots: "...",
}

func (t tok) String() string {
	if s, ok := tokNames[t]; ok {
		return s
	}
	for k, v := range keywords {
		if v == t {
			return k
		}
	}
	return fmt.Sprintf("token(%d)", int(t))
}

// token is one lexical token.
type token struct {
	t    tok
	line int
	s    string // name or string contents, or the source text of a number
	num  Value  // number value
}

// lexer splits a chunk into tokens.
type lexer struct {
	chunk string // chunk name for messages
	src   string
	pos   int
	line  int
}

// SyntaxError is a compile error at a line of a chunk.
type SyntaxError struct {
	Chunk string
	Line  int
	Msg   string
}

func (e *SyntaxError) Error() string { return fmt.Sprintf("%s:%d: %s", e.Chunk, e.Line, e.Msg) }

func (l *lexer) errorf(near string, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if near != "" {
		msg += " near " + near
	}
	panic(&SyntaxError{Chunk: l.chunk, Line: l.line, Msg: msg})
}

func (l *lexer) peekByte(off int) byte {
	if l.pos+off < len(l.src) {
		return l.src[l.pos+off]
	}
	return 0
}

// next returns the next token.
func (l *lexer) next() token {
	l.skipSpace()
	if l.pos >= len(l.src) {
		return token{t: tEOF, line: l.line}
	}
	start, line := l.pos, l.line
	c := l.src[l.pos]
	switch {
	case isAlpha(c):
		for l.pos < len(l.src) && isAlnum(l.src[l.pos]) {
			l.pos++
		}
		word := l.src[start:l.pos]
		if k, ok := keywords[word]; ok {
			return token{t: k, line: line, s: word}
		}
		return token{t: tName, line: line, s: word}
	case isDigit(c) || c == '.' && isDigit(l.peekByte(1)):
		return l.number()
	case c == '"' || c == '\'':
		return token{t: tString, line: line, s: l.shortString(c)}
	case c == '[' && (l.peekByte(1) == '[' || l.peekByte(1) == '='):
		if level := l.longBracket(); level >= 0 {
			return token{t: tString, line: line, s: l.longString(level)}
		}
		l.pos++
		return token{t: tLBracket, line: line}
	}
	three := ""
	if l.pos+3 <= len(l.src) {
		three = l.src[l.pos : l.pos+3]
	}
	if three == "..." {
		l.pos += 3
		return token{t: tDots, line: line}
	}
	two := ""
	if l.pos+2 <= len(l.src) {
		two = l.src[l.pos : l.pos+2]
	}
	if t, ok := twoCharToks[two]; ok {
		l.pos += 2
		return token{t: t, line: line}
	}
	if t, ok := oneCharToks[c]; ok {
		l.pos++
		return token{t: t, line: line}
	}
	r, _ := utf8.DecodeRuneInString(l.src[l.pos:])
	l.errorf(fmt.Sprintf("'%c'", r), "unexpected symbol")
	return token{}
}

func (l *lexer) skipSpace() {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == '\n':
			l.line++
			l.pos++
		case c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v':
			l.pos++
		case c == '-' && l.peekByte(1) == '-':
			l.pos += 2
			if l.peekByte(0) == '[' {
				if level := l.longBracket(); level >= 0 {
					l.longString(level)
					continue
				}
			}
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.pos++
			}
		case c == '#' && l.pos == 0 && l.peekByte(1) == '!':
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.pos++
			}
		default:
			return
		}
	}
}

// longBracket reads the opening [==[ at pos and returns its level, or -1 (pos unchanged)
// when the text there is not one.
func (l *lexer) longBracket() int {
	i := l.pos + 1
	level := 0
	for i < len(l.src) && l.src[i] == '=' {
		level++
		i++
	}
	if i < len(l.src) && l.src[i] == '[' {
		l.pos = i + 1
		return level
	}
	return -1
}

// longString reads up to the closing bracket of level, skipping a first newline.
func (l *lexer) longString(level int) string {
	closing := "]" + strings.Repeat("=", level) + "]"
	if l.peekByte(0) == '\r' {
		l.pos++
	}
	if l.peekByte(0) == '\n' {
		l.line++
		l.pos++
	}
	end := strings.Index(l.src[l.pos:], closing)
	if end < 0 {
		l.errorf("<eof>", "unfinished long string or comment")
	}
	s := l.src[l.pos : l.pos+end]
	l.line += strings.Count(s, "\n")
	l.pos += end + len(closing)
	return s
}

func (l *lexer) shortString(quote byte) string {
	l.pos++
	var b strings.Builder
	for {
		if l.pos >= len(l.src) {
			l.errorf("<eof>", "unfinished string")
		}
		c := l.src[l.pos]
		switch c {
		case quote:
			l.pos++
			return b.String()
		case '\n':
			l.errorf("<string>", "unfinished string")
		case '\\':
			l.pos++
			l.escape(&b)
		default:
			b.WriteByte(c)
			l.pos++
		}
	}
}

func (l *lexer) escape(b *strings.Builder) {
	if l.pos >= len(l.src) {
		l.errorf("<eof>", "unfinished string")
	}
	c := l.src[l.pos]
	l.pos++
	switch c {
	case 'a':
		b.WriteByte('\a')
	case 'b':
		b.WriteByte('\b')
	case 'f':
		b.WriteByte('\f')
	case 'n':
		b.WriteByte('\n')
	case 'r':
		b.WriteByte('\r')
	case 't':
		b.WriteByte('\t')
	case 'v':
		b.WriteByte('\v')
	case '\\', '"', '\'':
		b.WriteByte(c)
	case '\n':
		l.line++
		b.WriteByte('\n')
	case 'x':
		v := 0
		for i := 0; i < 2; i++ {
			d, ok := hexDigit(l.peekByte(0))
			if !ok {
				l.errorf("'\\x'", "hexadecimal digit expected")
			}
			v = v*16 + d
			l.pos++
		}
		b.WriteByte(byte(v))
	case 'z':
		for l.pos < len(l.src) && isSpace(l.src[l.pos]) {
			if l.src[l.pos] == '\n' {
				l.line++
			}
			l.pos++
		}
	case 'u':
		if l.peekByte(0) != '{' {
			l.errorf("'\\u'", "missing '{' in \\u{xxxx}")
		}
		l.pos++
		v := 0
		n := 0
		for {
			d, ok := hexDigit(l.peekByte(0))
			if !ok {
				break
			}
			v = v*16 + d
			if v > 0x7FFFFFFF {
				l.errorf("'\\u'", "UTF-8 value too large")
			}
			n++
			l.pos++
		}
		if n == 0 || l.peekByte(0) != '}' {
			l.errorf("'\\u'", "malformed \\u{xxxx}")
		}
		l.pos++
		b.WriteString(string(rune(v)))
	default:
		if isDigit(c) {
			v := int(c - '0')
			for i := 0; i < 2 && isDigit(l.peekByte(0)); i++ {
				v = v*10 + int(l.peekByte(0)-'0')
				l.pos++
			}
			if v > 255 {
				l.errorf("'\\"+fmt.Sprint(v)+"'", "decimal escape too large")
			}
			b.WriteByte(byte(v))
			return
		}
		l.errorf(fmt.Sprintf("'\\%c'", c), "invalid escape sequence")
	}
}

func (l *lexer) number() token {
	start, line := l.pos, l.line
	isHex := l.src[l.pos] == '0' && (l.peekByte(1) == 'x' || l.peekByte(1) == 'X')
	if isHex {
		l.pos += 2
	}
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case isAlnum(c) || c == '.':
			l.pos++
		case (c == '+' || c == '-') && l.pos > start && isExponent(l.src[l.pos-1], isHex):
			l.pos++
		default:
			goto done
		}
	}
done:
	text := l.src[start:l.pos]
	v, ok := parseNumber(text)
	if !ok {
		l.errorf("'"+text+"'", "malformed number")
	}
	return token{t: tNumber, line: line, s: text, num: v}
}

func isExponent(c byte, hex bool) bool {
	if hex {
		return c == 'p' || c == 'P'
	}
	return c == 'e' || c == 'E'
}

func isAlpha(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' }
func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isAlnum(c byte) bool { return isAlpha(c) || isDigit(c) }
func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

func hexDigit(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10, true
	}
	return 0, false
}
