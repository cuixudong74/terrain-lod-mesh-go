package terrain

import (
	"math"
)

// MaxWorldCoord is the documented world bound: coordinates with
// |x| or |z| >= MaxWorldCoord are out of bounds and rejected with
// ErrKindOutOfBounds. Inside the bound, lattice arithmetic stays
// exact in float64/int64 for every supported frequency.
const MaxWorldCoord = 1 << 40

// Heightfield evaluates world height from global coordinates using the
// world Params. It is a pure function: no caching, no state.
type Heightfield struct {
	p Params
}

// NewHeightfield validates params and returns a heightfield.
func NewHeightfield(p Params) (*Heightfield, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &Heightfield{p: p}, nil
}

// HeightAt returns the world-space height at global (x, z).
// The result depends only on params and (x, z), never on chunking,
// so samples taken from different chunks agree on shared edges.
func (h *Heightfield) HeightAt(x, z float64) float64 {
	n := h.p.Noise.At(x, z)
	y := float64(n * h.p.HeightScale)
	// Modifiers apply in fixed slice order; each is a single add.
	for _, m := range h.p.Modifiers {
		if m.Region.containsPoint(x, z) {
			y += m.HeightOffset
		}
	}
	return y
}

// QuadsPerSide returns the number of quads per chunk side at lod.
func (p *Params) QuadsPerSide(lod LOD) int {
	n := int(p.ChunkWorldSize / p.BaseSpacing)
	return n >> uint(lod)
}

// Validate checks all parameters; returns the first *Error found.
func (p *Params) Validate() error {
	bad := func(msg string) error {
		return &Error{Kind: ErrKindInvalidParam, Op: "validate", Msg: msg}
	}
	if err := p.Noise.Validate(); err != nil {
		return bad(err.Error())
	}
	if math.IsNaN(p.HeightScale) || math.IsInf(p.HeightScale, 0) {
		return bad("height scale must be finite")
	}
	if p.BaseSpacing <= 0 || math.IsNaN(p.BaseSpacing) {
		return bad("base spacing must be > 0")
	}
	if p.ChunkWorldSize <= 0 || math.IsNaN(p.ChunkWorldSize) {
		return bad("chunk world size must be > 0")
	}
	ratio := p.ChunkWorldSize / p.BaseSpacing
	n := int(ratio)
	if float64(n) != ratio || n < 1 {
		return bad("chunk world size must be an integer multiple of base spacing")
	}
	if n&(n-1) != 0 {
		return bad("quads per chunk side (chunk size / base spacing) must be a power of two")
	}
	if p.MaxLOD >= 31 {
		return bad("max LOD is absurd (>= 31)")
	}
	if n>>uint(p.MaxLOD) < 1 {
		return bad("max LOD leaves fewer than one quad per chunk side")
	}
	if p.LOD0Radius <= 0 || math.IsNaN(p.LOD0Radius) {
		return bad("LOD0 radius must be > 0")
	}
	if math.IsNaN(p.LODBias) {
		return bad("LOD bias must not be NaN")
	}
	for i, m := range p.Modifiers {
		if math.IsNaN(m.HeightOffset) || math.IsInf(m.HeightOffset, 0) {
			return bad("modifier height offset must be finite")
		}
		if !(m.Region.MinX < m.Region.MaxX) || !(m.Region.MinZ < m.Region.MaxZ) {
			return bad("modifier region must be non-empty")
		}
		_ = i
	}
	b := p.Budgets
	if b.MaxVerticesPerChunk < 0 || b.MaxTrianglesPerChunk < 0 ||
		b.MaxChunksInFlight < 0 || b.MaxCachedChunks < 0 || b.MaxChunksPerRequest < 0 {
		return bad("budgets must be >= 0 (0 means unlimited/default)")
	}
	return nil
}
