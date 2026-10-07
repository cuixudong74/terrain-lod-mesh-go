package terrain

import (
	"math"

	"github.com/cuixudong74/terrain-lod-mesh-go/noise"
)

// HeightAt evaluates the terrain height at world coordinates (x, z).
//
// It is a pure function of (cfg, x, z): identical across chunks,
// goroutines and process restarts. Layers are summed in slice order,
// modifiers afterwards in slice order; the final result is canonicalized
// so -0.0 can never leak into exported data.
func HeightAt(cfg *Config, x, z float64) float64 {
	h := cfg.BaseHeight
	for i := range cfg.Layers {
		l := &cfg.Layers[i]
		seed := cfg.Seed + l.SeedOffset
		h += l.Amplitude * noise.FBM2(l.Primitive, seed, l.Fractal, x*l.Frequency, z*l.Frequency)
	}
	for i := range cfg.Modifiers {
		h += modifierContribution(&cfg.Modifiers[i], x, z)
	}
	// Canonicalize zero: -0.0 == 0.0 is true, and this assignment forces
	// the +0.0 bit pattern, keeping exported bytes stable.
	if h == 0 {
		h = 0
	}
	return h
}

// modifierContribution returns the height delta of m at (x, z); exactly 0
// at and beyond the modifier radius, so chunks outside the footprint are
// never perturbed (not even by floating point dust).
func modifierContribution(m *Modifier, x, z float64) float64 {
	dx := x - m.CenterX
	dz := z - m.CenterZ
	d := math.Sqrt(dx*dx + dz*dz)
	if d >= m.Radius {
		return 0
	}
	// w in (0, 1]: 1 at the center, approaching 0 at the rim.
	w := 1.0 - d/m.Radius
	// math.Pow in Go is a pure software implementation, deterministic
	// across platforms for identical inputs.
	return m.Height * math.Pow(w, m.Falloff+1.0)
}
