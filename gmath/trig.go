package gmath

import "math"

// Deterministic trigonometry.
//
// The kernels follow the classic fdlibm approach: Cody-Waite argument reduction by pi/2
// followed by minimax polynomials on [-pi/4, pi/4]. Every product is wrapped in an
// explicit float64 conversion, which the Go specification defines as a rounding point,
// so no architecture may fuse it into an FMA. The results are bit-identical everywhere.
//
// Arguments are reduced accurately for |x| < 2^20*pi/2; beyond that they are first folded
// into (-2pi, 2pi) with the exact math.Mod, so the functions stay deterministic and
// bounded but lose precision, which is irrelevant for game angles.

// reduceMax is the bound of accurate Cody-Waite reduction (k*pio2Hi stays exact).
const reduceMax = (1 << 20) * (Pi / 2)

const (
	pio2Hi  = 1.57079632673412561417e+00 // first 33 bits of pi/2
	pio2Mid = 6.07710050630396597660e-11 // next 33 bits
	pio2Lo  = 2.02226624871116645580e-21 // remaining bits
	twoOPi  = 6.36619772367581382433e-01 // 2/pi

	sinS1 = -1.66666666666666324348e-01
	sinS2 = 8.33333333332248946124e-03
	sinS3 = -1.98412698298579493134e-04
	sinS4 = 2.75573137070700676789e-06
	sinS5 = -2.50507602534068634195e-08
	sinS6 = 1.58969099521155010221e-10

	cosC1 = 4.16666666666666019037e-02
	cosC2 = -1.38888888888741095749e-03
	cosC3 = 2.48015872894767294178e-05
	cosC4 = -2.75573143513906633035e-07
	cosC5 = 2.08757232129817482790e-09
	cosC6 = -1.13596475577881948265e-11
)

// m rounds a product explicitly, preventing multiply-add fusion.
func m(a, b float64) float64 { return float64(a * b) }

// reduce returns r and quadrant q such that x = q*pi/2 + r with |r| <= ~pi/4.
func reduce(x float64) (r float64, q int64) {
	k := math.Floor(float64(m(x, twoOPi)) + 0.5)
	r = x - m(k, pio2Hi)
	r -= m(k, pio2Mid)
	r -= m(k, pio2Lo)
	return r, int64(k)
}

func kernelSin(r float64) float64 {
	z := m(r, r)
	p := sinS5 + m(z, sinS6)
	p = sinS4 + m(z, p)
	p = sinS3 + m(z, p)
	p = sinS2 + m(z, p)
	p = sinS1 + m(z, p)
	return r + m(m(r, z), p)
}

func kernelCos(r float64) float64 {
	z := m(r, r)
	p := cosC5 + m(z, cosC6)
	p = cosC4 + m(z, p)
	p = cosC3 + m(z, p)
	p = cosC2 + m(z, p)
	p = cosC1 + m(z, p)
	hz := m(0.5, z)
	w := 1 - hz
	return w + (((1 - w) - hz) + m(m(z, z), p))
}

// SinCos64 returns sin(x) and cos(x) deterministically.
func SinCos64(x float64) (sin, cos float64) {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return math.NaN(), math.NaN()
	}
	if x == 0 {
		return x, 1 // keeps the sign of zero: sin(-0) = -0
	}
	if math.Abs(x) >= reduceMax {
		// Fold into (-2pi, 2pi) with the exact fmod so the Cody-Waite reduction below
		// stays in range; without this, huge arguments give values far outside [-1, 1].
		x = math.Mod(x, 2*Pi)
	}
	r, q := reduce(x)
	s, c := kernelSin(r), kernelCos(r)
	switch q & 3 {
	case 0:
		return s, c
	case 1:
		return c, -s
	case 2:
		return -s, -c
	default:
		return -c, s
	}
}

// Sin64 returns sin(x) deterministically.
func Sin64(x float64) float64 { s, _ := SinCos64(x); return s }

// Cos64 returns cos(x) deterministically.
func Cos64(x float64) float64 { _, c := SinCos64(x); return c }

// Tan64 returns tan(x) deterministically.
func Tan64(x float64) float64 { s, c := SinCos64(x); return s / c }

const (
	atanHi0 = 4.63647609000806093515e-01 // atan(0.5) hi
	atanHi1 = 7.85398163397448278999e-01 // atan(1.0) hi
	atanHi2 = 9.82793723247329054082e-01 // atan(1.5) hi
	atanHi3 = 1.57079632679489655800e+00 // atan(inf) hi
	atanLo0 = 2.26987774529616870924e-17
	atanLo1 = 3.06161699786838301793e-17
	atanLo2 = 1.39033110312309984516e-17
	atanLo3 = 6.12323399573676603587e-17

	aT0  = 3.33333333333329318027e-01
	aT1  = -1.99999999998764832476e-01
	aT2  = 1.42857142725034663711e-01
	aT3  = -1.11111104054623557880e-01
	aT4  = 9.09088713343650656196e-02
	aT5  = -7.69187620504482999495e-02
	aT6  = 6.66107313738753120669e-02
	aT7  = -5.83357013379057348645e-02
	aT8  = 4.97687799461593236017e-02
	aT9  = -3.65315727442169155270e-02
	aT10 = 1.62858201153657823623e-02
)

// Atan64 returns atan(x) deterministically.
func Atan64(x float64) float64 {
	if math.IsNaN(x) {
		return x
	}
	neg := x < 0
	if neg {
		x = -x
	}
	var hi, lo float64
	id := -1
	switch {
	case x >= 1<<66:
		r := atanHi3 + atanLo3
		if neg {
			return -r
		}
		return r
	case x < 0.4375:
		if x < 1.0/(1<<29) {
			if neg {
				return -x
			}
			return x
		}
	case x < 0.6875:
		id = 0
		x = (m(2, x) - 1) / (2 + x)
	case x < 1.1875:
		id = 1
		x = (x - 1) / (x + 1)
	case x < 2.4375:
		id = 2
		x = (x - 1.5) / (1 + m(1.5, x))
	default:
		id = 3
		x = -1 / x
	}
	z := m(x, x)
	w := m(z, z)
	s1 := aT8 + m(w, aT10)
	s1 = aT6 + m(w, s1)
	s1 = aT4 + m(w, s1)
	s1 = aT2 + m(w, s1)
	s1 = aT0 + m(w, s1)
	s1 = m(z, s1)
	s2 := aT7 + m(w, aT9)
	s2 = aT5 + m(w, s2)
	s2 = aT3 + m(w, s2)
	s2 = aT1 + m(w, s2)
	s2 = m(w, s2)
	var r float64
	if id < 0 {
		r = x - m(x, s1+s2)
	} else {
		switch id {
		case 0:
			hi, lo = atanHi0, atanLo0
		case 1:
			hi, lo = atanHi1, atanLo1
		case 2:
			hi, lo = atanHi2, atanLo2
		default:
			hi, lo = atanHi3, atanLo3
		}
		r = hi - ((m(x, s1+s2) - lo) - x)
	}
	if neg {
		return -r
	}
	return r
}

// Atan264 returns atan(y/x) in (-pi, pi] using the signs of both arguments,
// deterministically. Special cases follow math.Atan2.
func Atan264(y, x float64) float64 {
	switch {
	case math.IsNaN(x) || math.IsNaN(y):
		return math.NaN()
	case y == 0:
		if x >= 0 && !math.Signbit(x) {
			return math.Copysign(0, y)
		}
		return math.Copysign(Pi, y)
	case x == 0:
		return math.Copysign(Pi/2, y)
	case math.IsInf(x, 0):
		if math.IsInf(x, 1) {
			if math.IsInf(y, 0) {
				return math.Copysign(Pi/4, y)
			}
			return math.Copysign(0, y)
		}
		if math.IsInf(y, 0) {
			return math.Copysign(3*Pi/4, y)
		}
		return math.Copysign(Pi, y)
	case math.IsInf(y, 0):
		return math.Copysign(Pi/2, y)
	}
	q := Atan64(y / x)
	if x < 0 {
		// Choose the half-plane from the sign of y, not of q: when y/x underflows to
		// +0 (y = -1e-300, x = -1e300), q carries no sign information and testing q
		// would give +pi, as math.Atan2 does, instead of -pi.
		if y > 0 {
			return q + Pi
		}
		return q - Pi
	}
	return q
}

// Asin64 returns asin(x) deterministically; NaN outside [-1, 1].
func Asin64(x float64) float64 {
	if x < -1 || x > 1 || math.IsNaN(x) {
		return math.NaN()
	}
	return Atan264(x, math.Sqrt(float64((1-x)*(1+x))))
}

// Acos64 returns acos(x) deterministically; NaN outside [-1, 1].
func Acos64(x float64) float64 {
	if x < -1 || x > 1 || math.IsNaN(x) {
		return math.NaN()
	}
	return Atan264(math.Sqrt(float64((1-x)*(1+x))), x)
}

// Sin returns sin(x) deterministically.
func Sin(x float32) float32 { return float32(Sin64(float64(x))) }

// Cos returns cos(x) deterministically.
func Cos(x float32) float32 { return float32(Cos64(float64(x))) }

// SinCos returns sin(x) and cos(x) deterministically.
func SinCos(x float32) (sin, cos float32) {
	s, c := SinCos64(float64(x))
	return float32(s), float32(c)
}

// Tan returns tan(x) deterministically.
func Tan(x float32) float32 { return float32(Tan64(float64(x))) }

// Atan returns atan(x) deterministically.
func Atan(x float32) float32 { return float32(Atan64(float64(x))) }

// Atan2 returns atan2(y, x) deterministically.
func Atan2(y, x float32) float32 { return float32(Atan264(float64(y), float64(x))) }

// Asin returns asin(x) deterministically.
func Asin(x float32) float32 { return float32(Asin64(float64(x))) }

// Acos returns acos(x) deterministically.
func Acos(x float32) float32 { return float32(Acos64(float64(x))) }
