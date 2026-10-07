package terrain

import "fmt"

// ChunkID identifies one chunk at one level of detail.
type ChunkID struct {
	X   int32
	Z   int32
	LOD uint8
}

// Grid describes the chunk partitioning of the world.
type Grid struct {
	// ChunkSize is the world-unit side length of a chunk (constant across LODs).
	ChunkSize float64
	// BaseQuads is the number of quads per chunk side at LOD 0.
	// LOD l uses BaseQuads >> l quads per side; BaseQuads must be a power of two.
	BaseQuads int
	// MaxLOD is the highest supported level of detail.
	MaxLOD int
}

// Validate checks grid parameters.
func (g *Grid) Validate() error {
	if g.ChunkSize <= 0 {
		return &Error{Code: ErrInvalidConfig, Msg: "ChunkSize must be > 0"}
	}
	if g.BaseQuads <= 0 || g.BaseQuads&(g.BaseQuads-1) != 0 {
		return &Error{Code: ErrInvalidConfig, Msg: "BaseQuads must be a positive power of two"}
	}
	if g.MaxLOD < 0 {
		return &Error{Code: ErrInvalidConfig, Msg: "MaxLOD must be >= 0"}
	}
	if g.BaseQuads>>uint(g.MaxLOD) < 1 {
		return &Error{Code: ErrInvalidConfig,
			Msg: fmt.Sprintf("MaxLOD %d too high for BaseQuads %d (would yield 0 quads)", g.MaxLOD, g.BaseQuads)}
	}
	return nil
}

// Quads returns the number of quads per side at the given LOD.
// The caller must ensure lod <= MaxLOD (validated at request time).
func (g *Grid) Quads(lod int) int {
	return g.BaseQuads >> uint(lod)
}

// Origin returns the world-space minimum corner of chunk (cx, cz).
func (g *Grid) Origin(cx, cz int32) (x, z float64) {
	return float64(cx) * g.ChunkSize, float64(cz) * g.ChunkSize
}

// Step returns the world-space vertex spacing at the given LOD.
func (g *Grid) Step(lod int) float64 {
	return g.ChunkSize / float64(g.Quads(lod))
}
