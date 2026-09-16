package lua

// Lua patterns, as lstrlib.c matches them: character classes, sets, the quantifiers * + - ?,
// captures and position captures, back-references, %b and %f, and the anchors ^ and $.
// Classes follow the C locale, so matching never depends on the machine.

const (
	maxCaptures   = 32
	capUnfinished = -1
	capPosition   = -2
	maxMatchDepth = 200
)

type matchState struct {
	vm      *VM
	src     string
	pat     string
	depth   int
	level   int
	capture [maxCaptures]struct {
		init int
		len  int
	}
}

// at returns pat[i], or 0 past its end (C's terminating zero).
func (ms *matchState) pat0(i int) byte {
	if i < len(ms.pat) {
		return ms.pat[i]
	}
	return 0
}

func (ms *matchState) src0(i int) byte {
	if i < len(ms.src) {
		return ms.src[i]
	}
	return 0
}

func (ms *matchState) reset() {
	ms.level = 0
	ms.depth = maxMatchDepth
}

func (ms *matchState) classEnd(p int) int {
	c := ms.pat0(p)
	p++
	switch c {
	case '%':
		if p >= len(ms.pat) {
			ms.vm.Errorf("malformed pattern (ends with '%%')")
		}
		return p + 1
	case '[':
		if ms.pat0(p) == '^' {
			p++
		}
		for {
			if p >= len(ms.pat) {
				ms.vm.Errorf("malformed pattern (missing ']')")
			}
			c := ms.pat[p]
			p++
			if c == '%' && p < len(ms.pat) {
				p++
			}
			if ms.pat0(p) == ']' {
				return p + 1
			}
		}
	}
	return p
}

func isLower(c byte) bool  { return c >= 'a' && c <= 'z' }
func isUpper(c byte) bool  { return c >= 'A' && c <= 'Z' }
func isCntrl(c byte) bool  { return c < 32 || c == 127 }
func isPunct(c byte) bool  { return c > 32 && c < 127 && !isAlnumC(c) }
func isAlnumC(c byte) bool { return isLower(c) || isUpper(c) || isDigit(c) }
func isXDigit(c byte) bool { _, ok := hexDigit(c); return ok }

func matchClass(c, cl byte) bool {
	var res bool
	lower := cl
	if isUpper(cl) {
		lower = cl + ('a' - 'A')
	}
	switch lower {
	case 'a':
		res = isLower(c) || isUpper(c)
	case 'c':
		res = isCntrl(c)
	case 'd':
		res = isDigit(c)
	case 'g':
		res = c > 32 && c < 127
	case 'l':
		res = isLower(c)
	case 'p':
		res = isPunct(c)
	case 's':
		res = c == ' ' || c >= '\t' && c <= '\r'
	case 'u':
		res = isUpper(c)
	case 'w':
		res = isAlnumC(c)
	case 'x':
		res = isXDigit(c)
	default:
		return cl == c
	}
	if isUpper(cl) {
		res = !res
	}
	return res
}

// matchBracketClass matches c against the set pat[p:ec+1], where pat[p] is '[' and pat[ec]
// is ']'.
func (ms *matchState) matchBracketClass(c byte, p, ec int) bool {
	sig := true
	if ms.pat0(p+1) == '^' {
		sig = false
		p++
	}
	for p++; p < ec; p++ {
		switch {
		case ms.pat[p] == '%':
			p++
			if matchClass(c, ms.pat0(p)) {
				return sig
			}
		case ms.pat0(p+1) == '-' && p+2 < ec:
			p += 2
			if ms.pat[p-2] <= c && c <= ms.pat[p] {
				return sig
			}
		case ms.pat[p] == c:
			return sig
		}
	}
	return !sig
}

func (ms *matchState) singleMatch(s, p, ep int) bool {
	if s >= len(ms.src) {
		return false
	}
	c := ms.src[s]
	switch ms.pat[p] {
	case '.':
		return true
	case '%':
		return matchClass(c, ms.pat0(p+1))
	case '[':
		return ms.matchBracketClass(c, p, ep-1)
	}
	return ms.pat[p] == c
}

// match returns the end of the match of pat[p:] at src[s:], or -1.
func (ms *matchState) match(s, p int) int {
	ms.depth--
	if ms.depth == 0 {
		ms.vm.Errorf("pattern too complex")
	}
	defer func() { ms.depth++ }()
	for {
		if p == len(ms.pat) {
			return s
		}
		switch ms.pat[p] {
		case '(':
			if ms.pat0(p+1) == ')' {
				return ms.startCapture(s, p+2, capPosition)
			}
			return ms.startCapture(s, p+1, capUnfinished)
		case ')':
			return ms.endCapture(s, p+1)
		case '$':
			if p+1 == len(ms.pat) {
				if s == len(ms.src) {
					return s
				}
				return -1
			}
		case '%':
			switch next := ms.pat0(p + 1); {
			case next == 'b':
				s = ms.matchBalance(s, p+2)
				if s == -1 {
					return -1
				}
				p += 4
				continue
			case next == 'f':
				p += 2
				if ms.pat0(p) != '[' {
					ms.vm.Errorf("missing '[' after '%%f' in pattern")
				}
				ep := ms.classEnd(p)
				var prev byte
				if s > 0 {
					prev = ms.src[s-1]
				}
				if !ms.matchBracketClass(prev, p, ep-1) && ms.matchBracketClass(ms.src0(s), p, ep-1) {
					p = ep
					continue
				}
				return -1
			case isDigit(next):
				s = ms.matchCapture(s, next)
				if s == -1 {
					return -1
				}
				p += 2
				continue
			}
		}
		// a single character class with an optional quantifier
		ep := ms.classEnd(p)
		epc := ms.pat0(ep)
		if !ms.singleMatch(s, p, ep) {
			if epc == '*' || epc == '?' || epc == '-' {
				p = ep + 1
				continue
			}
			return -1
		}
		switch epc {
		case '?':
			if r := ms.match(s+1, ep+1); r != -1 {
				return r
			}
			p = ep + 1
			continue
		case '+':
			return ms.maxExpand(s+1, p, ep)
		case '*':
			return ms.maxExpand(s, p, ep)
		case '-':
			return ms.minExpand(s, p, ep)
		}
		s++
		p = ep
	}
}

func (ms *matchState) maxExpand(s, p, ep int) int {
	i := 0
	for ms.singleMatch(s+i, p, ep) {
		i++
	}
	for ; i >= 0; i-- {
		if r := ms.match(s+i, ep+1); r != -1 {
			return r
		}
	}
	return -1
}

func (ms *matchState) minExpand(s, p, ep int) int {
	for {
		if r := ms.match(s, ep+1); r != -1 {
			return r
		}
		if !ms.singleMatch(s, p, ep) {
			return -1
		}
		s++
	}
}

func (ms *matchState) startCapture(s, p, what int) int {
	if ms.level >= maxCaptures {
		ms.vm.Errorf("too many captures")
	}
	ms.capture[ms.level].init = s
	ms.capture[ms.level].len = what
	ms.level++
	r := ms.match(s, p)
	if r == -1 {
		ms.level--
	}
	return r
}

func (ms *matchState) endCapture(s, p int) int {
	l := -1
	for i := ms.level - 1; i >= 0; i-- {
		if ms.capture[i].len == capUnfinished {
			l = i
			break
		}
	}
	if l < 0 {
		ms.vm.Errorf("invalid pattern capture")
	}
	ms.capture[l].len = s - ms.capture[l].init
	r := ms.match(s, p)
	if r == -1 {
		ms.capture[l].len = capUnfinished
	}
	return r
}

func (ms *matchState) matchBalance(s, p int) int {
	if p+1 >= len(ms.pat) {
		ms.vm.Errorf("malformed pattern (missing arguments to '%%b')")
	}
	if s >= len(ms.src) || ms.src[s] != ms.pat[p] {
		return -1
	}
	b, e := ms.pat[p], ms.pat[p+1]
	cont := 1
	for s++; s < len(ms.src); s++ {
		switch ms.src[s] {
		case e:
			cont--
			if cont == 0 {
				return s + 1
			}
		case b:
			cont++
		}
	}
	return -1
}

func (ms *matchState) matchCapture(s int, l byte) int {
	i := int(l - '1')
	if i < 0 || i >= ms.level || ms.capture[i].len == capUnfinished {
		ms.vm.Errorf("invalid capture index %%%d", i+1)
	}
	n := ms.capture[i].len
	if len(ms.src)-s >= n && ms.src[ms.capture[i].init:ms.capture[i].init+n] == ms.src[s:s+n] {
		return s + n
	}
	return -1
}

// oneCapture returns capture i of a match of src[s:e]: the whole match when the pattern
// has no captures.
func (ms *matchState) oneCapture(i, s, e int) Value {
	if i >= ms.level {
		if i != 0 {
			ms.vm.Errorf("invalid capture index %%%d", i+1)
		}
		return String(ms.src[s:e])
	}
	c := ms.capture[i]
	switch c.len {
	case capUnfinished:
		ms.vm.Errorf("unfinished capture")
	case capPosition:
		return Int(int64(c.init) + 1)
	}
	return String(ms.src[c.init : c.init+c.len])
}

// captures returns the captures of a match of src[s:e]; wholeIfNone adds the whole match
// when the pattern has none.
func (ms *matchState) captures(s, e int, wholeIfNone bool) []Value {
	n := ms.level
	if n == 0 && wholeIfNone {
		n = 1
	}
	out := make([]Value, n)
	for i := range out {
		out[i] = ms.oneCapture(i, s, e)
	}
	return out
}
