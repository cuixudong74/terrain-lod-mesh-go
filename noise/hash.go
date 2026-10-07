// Package noise provides deterministic, seed-driven noise primitives.
//
// Determinism policy: all lattice hashing is integer-only (uint64 mixing,
// SplitMix64-style finalizers). All floating point evaluation is float64
// with a fixed, left-to-right operation order; no map iteration, no
// goroutine-dependent accumulation, no math library functions whose
// results vary across platforms (only +,-,*,/ and float64<->int64
// conversions are used in noise evaluation). Results depend only on
// (seed, coordinates), never on call order, chunk partitioning,
// goroutine scheduling, or process state.
package noise

// splitmix64 is the SplitMix64 finalizer: a bijective 64-bit mix with
// excellent avalanche behavior. Pure integer arithmetic, fully
// deterministic on every platform.
func splitmix64(h uint64) uint64 {
	h ^= h >> 30
	h *= 0xbf58476d1ce4e5b9
	h ^= h >> 27
	h *= 0x94d049bb133111eb
	h ^= h >> 31
	return h
}

// hash2 mixes a seed with integer lattice coordinates into a uint64.
func hash2(seed uint64, x, y int64) uint64 {
	h := seed
	h ^= uint64(x)*0x9e3779b97f4a7c15 + 0x632be59bd9b4e019
	h = splitmix64(h)
	h ^= uint64(y)*0xc2b2ae3d27d4eb4f + 0x9e3779b97f4a7c15
	h = splitmix64(h)
	return h
}

// hash3 mixes a seed with three integer coordinates.
func hash3(seed uint64, x, y, z int64) uint64 {
	h := hash2(seed, x, y)
	h ^= uint64(z)*0x165667b19e3779f9 + 0x27d4eb2f165667c5
	h = splitmix64(h)
	return h
}

// unitFloat maps a hash to a float64 in [0, 1) using the top 53 bits.
// Division by 2^53 is exact in binary floating point.
func unitFloat(h uint64) float64 {
	return float64(h>>11) * (1.0 / 9007199254740992.0)
}

// signedFloat maps a hash to a float64 in [-1, 1).
func signedFloat(h uint64) float64 {
	return unitFloat(h)*2.0 - 1.0
}
