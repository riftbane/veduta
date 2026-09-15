// Package world generates the content of a compiled world (asset.World) from its seed:
// biomes, scattered prefabs, sites and explicit places, chunk by chunk. Everything is a
// pure function of the world, its prefabs and integer cell coordinates, computed with
// integer arithmetic only, so a chunk is the same whatever was generated before it and on
// every machine. The package also validates placements, finds free cells and describes
// regions for the tools; it never touches the clock or the filesystem.
package world

// mix64 is the SplitMix64 output function, a bijective 64-bit hash.
func mix64(h uint64) uint64 {
	h ^= h >> 30
	h *= 0xbf58476d1ce4e5b9
	h ^= h >> 27
	h *= 0x94d049bb133111eb
	h ^= h >> 31
	return h
}

// Salts keep the hash streams of the generator's concerns apart.
const (
	saltBiome   = 0x62696f6d65 // "biome"
	saltScatter = 0x7363617474 // "scatt"
	saltSite    = 0x73697465   // "site"
)

// hash chains SplitMix64 over the seed, a salt and three coordinates.
func hash(seed, salt uint64, a, b, c int64) uint64 {
	h := mix64(seed ^ 0x9e3779b97f4a7c15*salt)
	h = mix64(h ^ uint64(a))
	h = mix64(h ^ uint64(b))
	h = mix64(h ^ uint64(c))
	return h
}

// floorDiv returns floor(a / b) for b > 0.
func floorDiv(a, b int32) int32 {
	q := a / b
	if a%b != 0 && a < 0 {
		q--
	}
	return q
}

// share returns the 32-bit threshold of a share in (0, 1]: a hash below it is "in".
func share(v float32) uint64 {
	return uint64(float64(v) * (1 << 32))
}
