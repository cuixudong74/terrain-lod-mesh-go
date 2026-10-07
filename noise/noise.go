// Package noise provides deterministic, seed-driven 2D noise primitives.
//
// Determinism contract: every primitive is a pure function of
// (seed, parameters, global x, z). No tables, no maps, no goroutines,
// no time dependence.
//
// Precision policy (see docs/DESIGN.md): all math is float64. Every
// intermediate product that could be fused into an FMA by the compiler
// is forced through an explicit float64 conversion, so results are
// bit-identical on any Go-supported architecture.
package noise

import "math"

// Primitive2D is a 2D noise primitive evaluated in global coordinates.
type Primitive2D interface {
	// At returns the noise value at global coordinates (x, z).
	At(x, z float64) float64
}

// --- deterministic integer hashing -------------------------------------

// mix is splitmix64 finalizer: a bijective, fully deterministic mix.
func mix(h uint64) uint64 {
	h ^= h >> 30
	h *= 0xBF58476D1CE4E5B9
	h ^= h >> 27
	h *= 0x94D049BB133111EB
	h ^= h >> 31
	return h
}

// hash2 maps (seed, lattice x, lattice z) to a pseudo-random uint64.
func hash2(seed uint64, x, z int64) uint64 {
	h := mix(seed ^ uint64(x)*0x9E3779B97F4A7C15)
	h = mix(h + uint64(z)*0xC2B2AE3D27D4EB4F)
	return h
}

// unit maps a hash to [0, 1) using the top 53 bits.
func unit(h uint64) float64 {
	return float64(h>>11) * (1.0 / 9007199254740992.0)
}

// signed maps a hash to [-1, 1).
func signed(h uint64) float64 {
	return float64(unit(h))*2.0 - 1.0
}

// --- fixed-order float helpers ------------------------------------------
//
// Explicit float64(...) conversions force an IEEE-754 double rounding
// at that point, which forbids the compiler from fusing the enclosed
// operation into an FMA with a different rounding. This is what makes
// results bit-reproducible across architectures.

// fade is the quintic curve 6t^5 - 15t^4 + 10t^3.
func fade(t float64) float64 {
	inner := float64(t*6.0) - 15.0
	mid := float64(t*inner) + 10.0
	t2 := float64(t * t)
	t3 := float64(t2 * t)
	return t3 * mid
}

// lerp interpolates a->b by t in [0,1].
func lerp(a, b, t float64) float64 {
	d := float64(b - a)
	return a + float64(t*d)
}

// lattice splits v into its integer cell and fractional offset.
func lattice(v float64) (int64, float64) {
	i := int64(math.Floor(v))
	return i, v - float64(i)
}

// --- ValueNoise -----------------------------------------------------------

// ValueNoise is hash-based value noise with quintic interpolation.
// Output range: [-1, 1).
type ValueNoise struct {
	Seed      uint64
	Frequency float64 // samples per world unit; must be > 0
}

// At implements Primitive2D.
func (v ValueNoise) At(x, z float64) float64 {
	fx := float64(x * v.Frequency)
	fz := float64(z * v.Frequency)
	ix, tx := lattice(fx)
	iz, tz := lattice(fz)
	s := v.Seed
	v00 := signed(hash2(s, ix, iz))
	v10 := signed(hash2(s, ix+1, iz))
	v01 := signed(hash2(s, ix, iz+1))
	v11 := signed(hash2(s, ix+1, iz+1))
	sx := fade(tx)
	sz := fade(tz)
	a := lerp(v00, v10, sx)
	b := lerp(v01, v11, sx)
	return lerp(a, b, sz)
}

// --- PerlinNoise ----------------------------------------------------------

// PerlinNoise is classic gradient noise with 8 hashed gradient
// directions. Output range: approximately [-1.5, 1.5] (scaled by
// sqrt(2) from the theoretical [-1,1] gradient bound).
type PerlinNoise struct {
	Seed      uint64
	Frequency float64 // must be > 0
}

// sqrtHalf is 1/sqrt(2), the diagonal gradient component.
const sqrtHalf = 0.70710678118654752440084436210484903928483593768847

// gradient returns one of 8 axis/diagonal unit gradients by hash.
func gradient(h uint64) (float64, float64) {
	switch h & 7 {
	case 0:
		return 1, 0
	case 1:
		return -1, 0
	case 2:
		return 0, 1
	case 3:
		return 0, -1
	case 4:
		return sqrtHalf, sqrtHalf
	case 5:
		return -sqrtHalf, sqrtHalf
	case 6:
		return sqrtHalf, -sqrtHalf
	default:
		return -sqrtHalf, -sqrtHalf
	}
}

// dot gradients at corner (ix,iz) against offset (dx,dz).
func perlinDot(seed uint64, ix, iz int64, dx, dz float64) float64 {
	gx, gz := gradient(hash2(seed, ix, iz))
	return float64(gx*dx) + float64(gz*dz)
}

// At implements Primitive2D.
func (p PerlinNoise) At(x, z float64) float64 {
	fx := float64(x * p.Frequency)
	fz := float64(z * p.Frequency)
	ix, tx := lattice(fx)
	iz, tz := lattice(fz)
	s := p.Seed
	d00 := perlinDot(s, ix, iz, tx, tz)
	d10 := perlinDot(s, ix+1, iz, tx-1.0, tz)
	d01 := perlinDot(s, ix, iz+1, tx, tz-1.0)
	d11 := perlinDot(s, ix+1, iz+1, tx-1.0, tz-1.0)
	sx := fade(tx)
	sz := fade(tz)
	a := lerp(d00, d10, sx)
	b := lerp(d01, d11, sx)
	v := lerp(a, b, sz)
	return float64(v * math.Sqrt2)
}

// --- WorleyNoise ----------------------------------------------------------

// WorleyNoise is cellular F1 noise: distance to the nearest hashed
// feature point, clamped to [0, 1].
type WorleyNoise struct {
	Seed      uint64
	Frequency float64 // must be > 0
}

// featurePoint returns the jittered feature point of cell (ix, iz).
func featurePoint(seed uint64, ix, iz int64) (float64, float64) {
	h := hash2(seed, ix, iz)
	fx := float64(ix) + unit(h)
	fz := float64(iz) + unit(mix(h^0x2545F4914F6CDD1D))
	return fx, fz
}

// At implements Primitive2D.
func (w WorleyNoise) At(x, z float64) float64 {
	fx := float64(x * w.Frequency)
	fz := float64(z * w.Frequency)
	ix, _ := lattice(fx)
	iz, _ := lattice(fz)
	best := math.MaxFloat64
	// Fixed 3x3 scan order: dz outer -1..1, dx inner -1..1.
	for dz := int64(-1); dz <= 1; dz++ {
		for dx := int64(-1); dx <= 1; dx++ {
			px, pz := featurePoint(w.Seed, ix+dx, iz+dz)
			ox := float64(px - fx)
			oz := float64(pz - fz)
			d2 := float64(ox*ox) + float64(oz*oz)
			if d2 < best {
				best = d2
			}
		}
	}
	d := math.Sqrt(best)
	if d > 1.0 {
		return 1.0
	}
	return d
}
