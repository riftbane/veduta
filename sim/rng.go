// Package sim is Veduta's deterministic simulation core: the seeded random number
// generator, per-tick input, the structured trace and its hash, invariants and input
// scripts. Nothing here reads the clock, the environment or the filesystem.
package sim

import "math/bits"

// RNG is a xoshiro256** generator. It is the only source of randomness a game may use;
// the engine seeds it from the scenario or command line so runs are reproducible.
// The zero value is not valid; use NewRNG.
type RNG struct {
	s [4]uint64
}

// NewRNG returns a generator seeded with seed (expanded with splitmix64, so every seed,
// including 0, gives a well-mixed state).
func NewRNG(seed uint64) *RNG {
	r := &RNG{}
	r.Seed(seed)
	return r
}

// Seed resets the generator to the state derived from seed.
func (r *RNG) Seed(seed uint64) {
	x := seed
	for i := range r.s {
		x += 0x9e3779b97f4a7c15
		z := x
		z = (z ^ z>>30) * 0xbf58476d1ce4e5b9
		z = (z ^ z>>27) * 0x94d049bb133111eb
		r.s[i] = z ^ z>>31
	}
}

// State returns the internal state, for snapshots.
func (r *RNG) State() [4]uint64 { return r.s }

// SetState restores a state returned by State.
func (r *RNG) SetState(s [4]uint64) { r.s = s }

// Uint64 returns the next 64 random bits.
func (r *RNG) Uint64() uint64 {
	s := &r.s
	out := bits.RotateLeft64(s[1]*5, 7) * 9
	t := s[1] << 17
	s[2] ^= s[0]
	s[3] ^= s[1]
	s[1] ^= s[2]
	s[0] ^= s[3]
	s[2] ^= t
	s[3] = bits.RotateLeft64(s[3], 45)
	return out
}

// Uint32 returns the next 32 random bits.
func (r *RNG) Uint32() uint32 { return uint32(r.Uint64() >> 32) }

// Float64 returns a uniform float64 in [0, 1).
func (r *RNG) Float64() float64 { return float64(r.Uint64()>>11) * 0x1p-53 }

// Float32 returns a uniform float32 in [0, 1).
func (r *RNG) Float32() float32 { return float32(r.Uint64()>>40) * 0x1p-24 }

// Range returns a uniform float32 in [lo, hi).
func (r *RNG) Range(lo, hi float32) float32 { return lo + (hi-lo)*r.Float32() }

// Intn returns a uniform int in [0, n); it panics if n <= 0. Unbiased (Lemire).
func (r *RNG) Intn(n int) int {
	if n <= 0 {
		panic("sim: RNG.Intn with n <= 0")
	}
	un := uint64(n)
	hi, lo := bits.Mul64(r.Uint64(), un)
	if lo < un {
		thresh := -un % un
		for lo < thresh {
			hi, lo = bits.Mul64(r.Uint64(), un)
		}
	}
	return int(hi)
}

// IntRange returns a uniform int in [lo, hi]; it panics if hi < lo.
func (r *RNG) IntRange(lo, hi int) int { return lo + r.Intn(hi-lo+1) }

// Bool returns a uniform boolean.
func (r *RNG) Bool() bool { return r.Uint64()>>63 == 1 }

// Chance returns true with probability p.
func (r *RNG) Chance(p float32) bool { return r.Float32() < p }

// Shuffle permutes n elements with swap (Fisher–Yates), deterministically.
func (r *RNG) Shuffle(n int, swap func(i, j int)) {
	for i := n - 1; i > 0; i-- {
		swap(i, r.Intn(i+1))
	}
}
