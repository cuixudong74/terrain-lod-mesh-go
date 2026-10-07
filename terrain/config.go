// Package terrain provides deterministic chunked heightfield generation
// with multiple levels of detail, incremental recomputation, bounded
// parallelism and strict error reporting.
package terrain

import (
	"encoding/binary"
	"hash/fnv"
	"math"

	"github.com/cuixudong74/terrain-lod-mesh-go/noise"
)

// LayerConfig is one composable noise layer of the heightfield.
type LayerConfig struct {
	// Primitive selects the base noise kind.
	Primitive noise.Primitive
	// SeedOffset is added to the world seed for this layer, so layers
	// are decorrelated while remaining deterministic.
	SeedOffset uint64
	// Frequency scales world coordinates before sampling.
	Frequency float64
	// Amplitude scales the layer output (world units).
	Amplitude float64
	// Fractal configures fBm stacking and domain warp for this layer.
	Fractal noise.FractalConfig
}

// Modifier is a localized height adjustment (e.g. a hand-placed hill).
// Its contribution is exactly zero at and beyond Radius, so chunks fully
// outside the footprint are bit-identical with or without the modifier.
type Modifier struct {
	// ID uniquely identifies the modifier for incremental diffs.
	ID      string
	CenterX float64
	CenterZ float64
	Radius  float64
	Height  float64
	// Falloff exponent shaping the bump profile (>= 0).
	Falloff float64
}

// Config is the full deterministic terrain description.
type Config struct {
	// Seed is the world seed.
	Seed uint64
	// BaseHeight is added to every sample (world units).
	BaseHeight float64
	// Layers are summed in slice order (fixed order => deterministic).
	Layers []LayerConfig
	// Modifiers are applied after layers, in slice order.
	Modifiers []Modifier
}

// Validate checks the config for degenerate or unsupported values.
func (c *Config) Validate() error {
	if math.IsNaN(c.BaseHeight) || math.IsInf(c.BaseHeight, 0) {
		return &Error{Code: ErrInvalidConfig, Msg: "BaseHeight must be finite"}
	}
	for _, l := range c.Layers {
		if l.Primitive != noise.ValueNoise && l.Primitive != noise.GradientNoise {
			return &Error{Code: ErrInvalidConfig, Msg: "layer has unknown primitive"}
		}
		if bad(l.Frequency) || bad(l.Amplitude) {
			return &Error{Code: ErrInvalidConfig, Msg: "layer Frequency/Amplitude must be finite"}
		}
		if l.Frequency <= 0 {
			return &Error{Code: ErrInvalidConfig, Msg: "layer Frequency must be > 0"}
		}
		f := l.Fractal
		if bad(f.Lacunarity) || bad(f.Gain) || bad(f.WarpAmp) || bad(f.WarpFreq) {
			return &Error{Code: ErrInvalidConfig, Msg: "layer fractal parameters must be finite"}
		}
		if f.Octaves > 32 {
			return &Error{Code: ErrInvalidConfig, Msg: "layer fractal Octaves exceeds 32"}
		}
	}
	seen := map[string]bool{}
	for _, m := range c.Modifiers {
		if bad(m.CenterX) || bad(m.CenterZ) || bad(m.Radius) || bad(m.Height) || bad(m.Falloff) {
			return &Error{Code: ErrInvalidConfig, Msg: "modifier " + m.ID + ": parameters must be finite"}
		}
		if m.Radius <= 0 {
			return &Error{Code: ErrInvalidConfig, Msg: "modifier " + m.ID + ": Radius must be > 0"}
		}
		if m.Falloff < 0 {
			return &Error{Code: ErrInvalidConfig, Msg: "modifier " + m.ID + ": Falloff must be >= 0"}
		}
		if seen[m.ID] {
			return &Error{Code: ErrInvalidConfig, Msg: "duplicate modifier ID " + m.ID}
		}
		seen[m.ID] = true
	}
	return nil
}

func bad(f float64) bool { return math.IsNaN(f) || math.IsInf(f, 0) }

// Hash returns a deterministic 64-bit hash of the canonical config
// serialization (FNV-1a over little-endian float64 bit patterns).
// Used for cache invalidation and reproducibility checks.
func (c *Config) Hash() uint64 {
	h := fnv.New64a()
	var buf [8]byte
	putU64 := func(v uint64) {
		binary.LittleEndian.PutUint64(buf[:], v)
		h.Write(buf[:])
	}
	putF64 := func(f float64) { putU64(math.Float64bits(f)) }
	putStr := func(s string) {
		putU64(uint64(len(s)))
		h.Write([]byte(s))
	}

	putU64(c.Seed)
	putF64(c.BaseHeight)
	putU64(uint64(len(c.Layers)))
	for _, l := range c.Layers {
		putU64(uint64(l.Primitive))
		putU64(l.SeedOffset)
		putF64(l.Frequency)
		putF64(l.Amplitude)
		putU64(uint64(int64(l.Fractal.Octaves)))
		putF64(l.Fractal.Lacunarity)
		putF64(l.Fractal.Gain)
		putF64(l.Fractal.WarpAmp)
		putF64(l.Fractal.WarpFreq)
	}
	putU64(uint64(len(c.Modifiers)))
	for _, m := range c.Modifiers {
		putStr(m.ID)
		putF64(m.CenterX)
		putF64(m.CenterZ)
		putF64(m.Radius)
		putF64(m.Height)
		putF64(m.Falloff)
	}
	return h.Sum64()
}
