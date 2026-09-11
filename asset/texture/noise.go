package texture

import "math"

// noiseField is fractal value noise: octave o is a lattice of random values in [0, 1)
// with twice the cells of octave o−1, interpolated with smoothstep weights, weighted
// 1/2^o, and the sum divided by the total weight so the result stays in [0, 1).
type noiseField struct {
	seed   uint64
	w, h   int
	tiling bool
	cx, cy []float64 // lattice cells across the width and the height, per octave
	px, py []int64   // lattice periods when tiling (= cx, cy)
	norm   float64   // 1 / sum of octave weights
}

// newNoiseField resolves the lattice of a noise layer.
//
// scale counts lattice cells across the texture width. Without tiling, cells are square:
// the height has h·scale/w cells. With tiling, scale is rounded to the nearest integer
// S ≥ 1 (halves round up), the height gets Sy = max(1, round(h·S/w)) cells, and octave o
// wraps with a period of S·2^o × Sy·2^o cells, which makes every octave, and the sum,
// repeat exactly every w × h pixels.
func newNoiseField(s *spec, l *layerSpec) *noiseField {
	f := &noiseField{seed: uint64(l.seed), w: s.w, h: s.h, tiling: s.tiling}
	sx := l.scale
	sy := float64(s.h) * l.scale / float64(s.w)
	if s.tiling {
		sx = max(1, math.Floor(l.scale+0.5))
		sy = max(1, math.Floor(float64(s.h)*sx/float64(s.w)+0.5))
	}
	weight, total := 1.0, 0.0
	for o := 0; o < l.octaves; o++ {
		mul := float64(int64(1) << o)
		f.cx = append(f.cx, sx*mul)
		f.cy = append(f.cy, sy*mul)
		if s.tiling {
			f.px = append(f.px, int64(sx)<<o)
			f.py = append(f.py, int64(sy)<<o)
		}
		total += weight
		weight /= 2
	}
	f.norm = 1 / total
	return f
}

// at returns the noise value in [0, 1) at the center of pixel (x, y).
func (f *noiseField) at(x, y int) float32 {
	sum, weight := 0.0, 1.0
	for o := range f.cx {
		// Lattice coordinates of the pixel center: (x + 0.5)·cells/w, computed as one
		// division of integers-times-cells so wrapped edges line up exactly.
		u := float64(2*x+1) * f.cx[o] / float64(2*f.w)
		v := float64(2*y+1) * f.cy[o] / float64(2*f.h)
		fu, fv := math.Floor(u), math.Floor(v)
		tu, tv := u-fu, v-fv
		i0, j0 := int64(fu), int64(fv)
		i1, j1 := i0+1, j0+1
		if f.tiling {
			i0, i1 = wrapInt(i0, f.px[o]), wrapInt(i1, f.px[o])
			j0, j1 = wrapInt(j0, f.py[o]), wrapInt(j1, f.py[o])
		}
		su := tu * tu * (3 - 2*tu)
		sv := tv * tv * (3 - 2*tv)
		v00 := f.lattice(o, i0, j0)
		v10 := f.lattice(o, i1, j0)
		v01 := f.lattice(o, i0, j1)
		v11 := f.lattice(o, i1, j1)
		top := v00 + (v10-v00)*su
		bottom := v01 + (v11-v01)*su
		sum += weight * (top + (bottom-top)*sv)
		weight /= 2
	}
	return float32(sum * f.norm)
}

// lattice is the random value in [0, 1) of lattice point (i, j) of octave o: a chain of
// SplitMix64 finalizers over the seed, the octave and the coordinates.
func (f *noiseField) lattice(o int, i, j int64) float64 {
	h := mix64(f.seed ^ 0x9e3779b97f4a7c15*uint64(o+1))
	h = mix64(h ^ uint64(i))
	h = mix64(h ^ uint64(j))
	return float64(h>>11) / (1 << 53)
}

// mix64 is the SplitMix64 output function, a bijective 64-bit hash.
func mix64(h uint64) uint64 {
	h ^= h >> 30
	h *= 0xbf58476d1ce4e5b9
	h ^= h >> 27
	h *= 0x94d049bb133111eb
	h ^= h >> 31
	return h
}

// wrapInt returns i mod n in [0, n).
func wrapInt(i, n int64) int64 {
	i %= n
	if i < 0 {
		i += n
	}
	return i
}
