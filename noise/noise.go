package noise

import "math"

// Primitive identifies a base noise function kind.
type Primitive int

const (
	// ValueNoise interpolates random values at integer lattice points.
	ValueNoise Primitive = iota
	// GradientNoise is Perlin-style gradient noise.
	GradientNoise
)

// fade is the classic smootherstep-free Perlin fade 6t^5-15t^4+10t^3,
// evaluated in a fixed Horner order for determinism.
func fade(t float64) float64 {
	return t * t * t * (t*(t*6.0-15.0) + 10.0)
}

// lerp interpolates a->b by t. Fixed evaluation order.
func lerp(a, b, t float64) float64 {
	return a + t*(b-a)
}

// Value2 evaluates 2D value noise at (x, y). Output range is [-1, 1).
func Value2(seed uint64, x, y float64) float64 {
	ix := int64(math.Floor(x))
	iy := int64(math.Floor(y))
	fx := x - float64(ix)
	fy := y - float64(iy)

	v00 := signedFloat(hash2(seed, ix, iy))
	v10 := signedFloat(hash2(seed, ix+1, iy))
	v01 := signedFloat(hash2(seed, ix, iy+1))
	v11 := signedFloat(hash2(seed, ix+1, iy+1))

	sx := fade(fx)
	sy := fade(fy)

	// Fixed interpolation order: x first, then y.
	return lerp(lerp(v00, v10, sx), lerp(v01, v11, sx), sy)
}

// gradX/gradY are 8 unit-length gradient directions (axis-aligned and
// diagonal). Unit length keeps the output range bounded by sqrt(2)/2
// before scaling.
const invSqrt2 = 0.7071067811865476 // 1/sqrt(2)

var gradX = [8]float64{1, -1, 0, 0, invSqrt2, -invSqrt2, invSqrt2, -invSqrt2}
var gradY = [8]float64{0, 0, 1, -1, invSqrt2, invSqrt2, -invSqrt2, -invSqrt2}

// gradDot computes the dot product of the hashed gradient at lattice
// corner (ix, iy) with the offset vector (dx, dy).
func gradDot(seed uint64, ix, iy int64, dx, dy float64) float64 {
	h := hash2(seed, ix, iy) & 7
	return gradX[h]*dx + gradY[h]*dy
}

// Gradient2 evaluates 2D gradient (Perlin-style) noise at (x, y).
// Output range is approximately [-1, 1].
func Gradient2(seed uint64, x, y float64) float64 {
	ix := int64(math.Floor(x))
	iy := int64(math.Floor(y))
	fx := x - float64(ix)
	fy := y - float64(iy)

	n00 := gradDot(seed, ix, iy, fx, fy)
	n10 := gradDot(seed, ix+1, iy, fx-1.0, fy)
	n01 := gradDot(seed, ix, iy+1, fx, fy-1.0)
	n11 := gradDot(seed, ix+1, iy+1, fx-1.0, fy-1.0)

	sx := fade(fx)
	sy := fade(fy)

	// Fixed interpolation order: x first, then y. With unit gradients the
	// raw range is bounded by sqrt(2)/2 (attained at cell centers), so
	// scaling by sqrt(2) normalizes to approximately [-1, 1].
	const sqrt2 = 1.4142135623730951
	return sqrt2 * lerp(lerp(n00, n10, sx), lerp(n01, n11, sx), sy)
}

// Eval evaluates the given primitive at (x, y). Unknown primitives fall
// back to ValueNoise so behavior is total and deterministic.
func Eval(p Primitive, seed uint64, x, y float64) float64 {
	if p == GradientNoise {
		return Gradient2(seed, x, y)
	}
	return Value2(seed, x, y)
}
