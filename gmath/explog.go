package gmath

import "math"

// Deterministic exponential, logarithm and power.
//
// The math package computes Exp and Log with assembly on amd64 and arm64, and the two do not
// agree in the last bit. These follow fdlibm's e_exp.c and e_log.c (Copyright (C) 1993 by
// Sun Microsystems, Inc.; permission to use, copy, modify, and distribute is freely granted,
// provided that this notice is preserved), with every product that feeds an addition wrapped
// in a conversion, so that no architecture fuses it: results are bit-identical everywhere.

const (
	ln2Hi = 6.93147180369123816490e-01
	ln2Lo = 1.90821492927058770002e-10

	expOverflow  = 7.09782712893383973096e+02
	expUnderflow = -7.45133219101941108420e+02
	invLn2       = 1.44269504088896338700e+00

	expP1 = 1.66666666666666657415e-01
	expP2 = -2.77777777770155933842e-03
	expP3 = 6.61375632143793436117e-05
	expP4 = -1.65339022054652515390e-06
	expP5 = 4.13813679705723846039e-08

	logL1 = 6.666666666666735130e-01
	logL2 = 3.999999999940941908e-01
	logL3 = 2.857142874366239149e-01
	logL4 = 2.222219843214978396e-01
	logL5 = 1.818357216161805012e-01
	logL6 = 1.531383769920937332e-01
	logL7 = 1.479819860511658591e-01
)

// Exp64 returns e**x deterministically.
func Exp64(x float64) float64 {
	switch {
	case math.IsNaN(x) || math.IsInf(x, 1):
		return x
	case math.IsInf(x, -1):
		return 0
	case x > expOverflow:
		return math.Inf(1)
	case x < expUnderflow:
		return 0
	case -1.0/(1<<28) < x && x < 1.0/(1<<28):
		return 1 + x
	}
	var k int
	if x < 0 {
		k = int(m(invLn2, x) - 0.5)
	} else {
		k = int(m(invLn2, x) + 0.5)
	}
	fk := float64(k)
	hi := x - m(fk, ln2Hi)
	lo := m(fk, ln2Lo)
	r := hi - lo
	t := m(r, r)
	p := expP4 + m(t, expP5)
	p = expP3 + m(t, p)
	p = expP2 + m(t, p)
	p = expP1 + m(t, p)
	c := r - m(t, p)
	y := 1 - ((lo - float64(m(r, c)/(2-c))) - hi)
	return math.Ldexp(y, k)
}

// Log64 returns the natural logarithm of x deterministically.
func Log64(x float64) float64 {
	switch {
	case math.IsNaN(x) || math.IsInf(x, 1):
		return x
	case x < 0:
		return math.NaN()
	case x == 0:
		return math.Inf(-1)
	}
	f1, ki := math.Frexp(x)
	if f1 < math.Sqrt2/2 {
		f1 = m(f1, 2)
		ki--
	}
	f := f1 - 1
	k := float64(ki)
	s := f / (2 + f)
	s2 := m(s, s)
	s4 := m(s2, s2)
	t1 := m(s2, logL1+m(s4, logL3+m(s4, logL5+m(s4, logL7))))
	t2 := m(s4, logL2+m(s4, logL4+m(s4, logL6)))
	r := t1 + t2
	hfsq := m(m(0.5, f), f)
	return m(k, ln2Hi) - ((hfsq - (m(s, hfsq+r) + m(k, ln2Lo))) - f)
}

// Pow64 returns x**y deterministically, with the special cases of C's pow. An integral
// exponent up to 1024 in size is computed by repeated squaring, so small powers are exact
// when the result is representable; any other is exp(y·log|x|).
func Pow64(x, y float64) float64 {
	switch {
	case y == 0 || x == 1:
		return 1
	case y == 1:
		return x
	case math.IsNaN(x) || math.IsNaN(y):
		return math.NaN()
	case x == 0:
		odd := isOddInt(y)
		switch {
		case y < 0 && odd:
			return math.Copysign(math.Inf(1), x)
		case y < 0:
			return math.Inf(1)
		case odd:
			return x
		}
		return 0
	case math.IsInf(y, 0):
		switch {
		case x == -1:
			return 1
		case (math.Abs(x) < 1) == math.IsInf(y, 1):
			return 0
		}
		return math.Inf(1)
	case math.IsInf(x, 0):
		if math.IsInf(x, -1) {
			return Pow64(1/x, -y) // (-0)**(-y)
		}
		if y < 0 {
			return 0
		}
		return math.Inf(1)
	case y == 0.5 && x > 0:
		return math.Sqrt(x)
	}
	yi, yf := math.Modf(math.Abs(y))
	if yf != 0 && x < 0 {
		return math.NaN()
	}
	neg := x < 0 && isOddInt(y)
	ax := math.Abs(x)
	var r float64
	if yf == 0 && yi <= 1024 {
		r = 1
		base := ax
		for n := uint64(yi); n > 0; n >>= 1 {
			if n&1 == 1 {
				r = m(r, base)
			}
			base = m(base, base)
		}
		if y < 0 {
			r = 1 / r
		}
	} else {
		r = Exp64(m(y, Log64(ax)))
	}
	if neg {
		r = -r
	}
	return r
}

// isOddInt reports whether y is an odd integer.
func isOddInt(y float64) bool {
	if math.Abs(y) >= 1<<53 {
		return false // every float that large is even
	}
	yi, yf := math.Modf(y)
	return yf == 0 && int64(yi)&1 == 1
}
