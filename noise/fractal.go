package noise

// FractalConfig configures fractal Brownian motion (fBm) stacking and
// domain warping applied on top of a base primitive.
type FractalConfig struct {
	// Octaves is the number of stacked frequencies (>= 1; values < 1 are
	// treated as 1).
	Octaves int
	// Lacunarity is the frequency multiplier between octaves (typically 2;
	// values <= 0 are treated as 2).
	Lacunarity float64
	// Gain is the amplitude multiplier between octaves (typically 0.5;
	// values <= 0 are treated as 0.5).
	Gain float64
	// WarpAmp is the domain-warp displacement amplitude, in units of the
	// caller's coordinate space (0 disables warp).
	WarpAmp float64
	// WarpFreq is the frequency of the domain-warp field (values <= 0 are
	// treated as 1).
	WarpFreq float64
}

// DefaultFractal returns a sane default fractal configuration.
func DefaultFractal() FractalConfig {
	return FractalConfig{
		Octaves:    4,
		Lacunarity: 2.0,
		Gain:       0.5,
		WarpAmp:    0.0,
		WarpFreq:   1.0,
	}
}

// warpSeedX/warpSeedY derive independent seeds for the two warp
// components by XOR-ing distinct constants into the layer seed.
const (
	warpSeedX = 0x51ed270b00000001
	warpSeedY = 0x51ed270b00000002
)

// FBM2 evaluates fractal noise: base primitive p stacked per cfg, with
// optional domain warp applied to the sample coordinates first.
//
// Determinism: octaves are accumulated in a fixed ascending order, the
// normalization weight sum is accumulated in the same order, and the warp
// field uses fixed derived seeds. No map iteration or runtime-dependent
// state is involved.
func FBM2(p Primitive, seed uint64, cfg FractalConfig, x, y float64) float64 {
	octaves := cfg.Octaves
	if octaves < 1 {
		octaves = 1
	}
	lacunarity := cfg.Lacunarity
	if lacunarity <= 0 {
		lacunarity = 2.0
	}
	gain := cfg.Gain
	if gain <= 0 {
		gain = 0.5
	}
	warpFreq := cfg.WarpFreq
	if warpFreq <= 0 {
		warpFreq = 1.0
	}

	if cfg.WarpAmp != 0 {
		wx := Eval(p, seed^warpSeedX, x*warpFreq, y*warpFreq)
		wy := Eval(p, seed^warpSeedY, x*warpFreq, y*warpFreq)
		x += cfg.WarpAmp * wx
		y += cfg.WarpAmp * wy
	}

	sum := 0.0
	amp := 1.0
	freq := 1.0
	norm := 0.0
	for i := 0; i < octaves; i++ {
		sum += amp * Eval(p, seed, x*freq, y*freq)
		norm += amp
		amp *= gain
		freq *= lacunarity
	}
	// norm > 0 always (amp starts at 1 and gain > 0), so the division is safe.
	return sum / norm
}
