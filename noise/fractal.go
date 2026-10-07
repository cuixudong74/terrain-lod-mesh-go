package noise

import "errors"

// Fractal stacks octaves of a source primitive (fBm-style) with an
// optional domain warp applied before the octave loop.
//
// Evaluation order is fixed: warp first (x displacement, then z),
// then octaves from 0 to Octaves-1, accumulating in order. This makes
// the floating-point summation order part of the contract.
type Fractal struct {
	Source Primitive2D // base primitive evaluated per octave

	Octaves    int     // number of octaves; must be >= 1
	Lacunarity float64 // frequency multiplier per octave; must be > 0
	Gain       float64 // amplitude multiplier per octave; must be >= 0

	Warp WarpConfig // domain warp; zero value disables warping
}

// WarpConfig configures domain warping: the input coordinates are
// displaced by an independent value-noise field before evaluation.
type WarpConfig struct {
	Amplitude float64 // displacement in world units; 0 disables warp
	Frequency float64 // warp field frequency; must be > 0 if Amplitude != 0
	Seed      uint64  // independent seed for the warp field
}

// Enabled reports whether the warp has any effect.
func (w WarpConfig) Enabled() bool { return w.Amplitude != 0 }

// At evaluates the fractal (with warp, if enabled) at (x, z).
func (f Fractal) At(x, z float64) float64 {
	if f.Warp.Enabled() {
		// Two independent warp fields via fixed seed derivation.
		wx := ValueNoise{Seed: f.Warp.Seed, Frequency: f.Warp.Frequency}
		wz := ValueNoise{Seed: f.Warp.Seed ^ 0x9E3779B97F4A7C15, Frequency: f.Warp.Frequency}
		dx := float64(wx.At(x, z) * f.Warp.Amplitude)
		dz := float64(wz.At(x, z) * f.Warp.Amplitude)
		x = x + dx
		z = z + dz
	}
	sum := 0.0
	amp := 1.0
	freq := 1.0
	for i := 0; i < f.Octaves; i++ {
		// Scale the sample coordinates rather than mutating the
		// source, so any Primitive2D works per-octave.
		s := scaled{src: f.Source, freq: freq}
		sum += float64(amp * s.At(x, z))
		amp = float64(amp * f.Gain)
		freq = float64(freq * f.Lacunarity)
	}
	return sum
}

// scaled evaluates src at frequency-multiplied coordinates.
type scaled struct {
	src  Primitive2D
	freq float64
}

func (s scaled) At(x, z float64) float64 {
	return s.src.At(float64(x*s.freq), float64(z*s.freq))
}

// Validate checks parameter sanity; returns the first problem found.
func (f Fractal) Validate() error {
	if f.Source == nil {
		return errors.New("noise: fractal source is nil")
	}
	if f.Octaves < 1 {
		return errors.New("noise: fractal octaves must be >= 1")
	}
	if f.Lacunarity <= 0 {
		return errors.New("noise: fractal lacunarity must be > 0")
	}
	if f.Gain < 0 {
		return errors.New("noise: fractal gain must be >= 0")
	}
	if f.Warp.Enabled() && f.Warp.Frequency <= 0 {
		return errors.New("noise: warp frequency must be > 0 when warp is enabled")
	}
	return nil
}
