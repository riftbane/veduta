package world

// field is smooth value noise over integer cells: lattice points every scale cells get
// a hashed value, cells between them are blended with a smoothstep. Two octaves (the
// second four times finer at a quarter of the weight) break the straight edges. All
// arithmetic is integer (Q16 fixed point), so every architecture computes the same
// value.
type field struct {
	seed  uint64
	scale int32
}

// lattice is the value in [0, 2^32) of lattice point (i, j) of octave o.
func (f field) lattice(o int, i, j int32) int64 {
	return int64(uint32(hash(f.seed, saltBiome, int64(o), int64(i), int64(j))))
}

// smooth is the Q16 smoothstep of a Q16 t in [0, 65536]: 3t² − 2t³.
func smooth(t int64) int64 {
	return t * t * (3*65536 - 2*t) / (65536 * 65536)
}

// octave returns the value in [0, 2^32) of cell (x, z) for lattice spacing s.
func (f field) octave(o int, x, z, s int32) int64 {
	i, j := floorDiv(x, s), floorDiv(z, s)
	tx := smooth(int64(x-i*s) * 65536 / int64(s))
	tz := smooth(int64(z-j*s) * 65536 / int64(s))
	v00, v10 := f.lattice(o, i, j), f.lattice(o, i+1, j)
	v01, v11 := f.lattice(o, i, j+1), f.lattice(o, i+1, j+1)
	top := v00 + (v10-v00)*tx>>16
	bottom := v01 + (v11-v01)*tx>>16
	return top + (bottom-top)*tz>>16
}

// value returns the noise of cell (x, z) in [0, 2^32).
func (f field) value(x, z int32) uint32 {
	coarse := f.octave(0, x, z, f.scale)
	fine := f.octave(1, x, z, max(1, f.scale/4))
	return uint32((3*coarse + fine) / 4)
}
