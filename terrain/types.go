// Package terrain turns a deterministic noise field into chunked,
// multi-LOD triangle meshes with crack-free stitching.
package terrain

import "github.com/cuixudong74/terrain-lod-mesh-go/noise"

// ChunkCoord identifies a chunk in the world grid. Chunks are square
// and cover ChunkWorldSize world units each; chunk (0,0) spans
// [0, ChunkWorldSize) on both axes.
type ChunkCoord struct {
	X, Z int32
}

// LOD is a detail level. LOD 0 is the finest; each coarser level
// doubles the vertex spacing. Vertex spacing at level l is 1<<l
// world units.
type LOD uint8

// Mesh is a complete, immutable chunk mesh. Positions are world-space
// XYZ triples (y = height). Indices are triangle lists referencing
// Positions.
type Mesh struct {
	Coord     ChunkCoord
	LOD       LOD
	Positions []float32 // len = 3 * vertex count
	Indices   []uint32
}

// VertexCount returns the number of vertices in the mesh.
func (m *Mesh) VertexCount() int { return len(m.Positions) / 3 }

// TriangleCount returns the number of triangles in the mesh.
func (m *Mesh) TriangleCount() int { return len(m.Indices) / 3 }

// Params holds every input that influences generated geometry.
// Two Worlds with equal Params produce bit-identical output.
type Params struct {
	Seed uint64

	// Heightfield shaping.
	HeightScale float64 // multiplies the noise value to get world height
	Noise       noise.Fractal

	// Chunking.
	ChunkWorldSize float64 // world units per chunk side; must be a power of two times the LOD0 spacing
	BaseSpacing    float64 // LOD0 vertex spacing in world units; must be > 0

	// LOD policy.
	MaxLOD     LOD     // coarsest allowed level
	LOD0Radius float64 // distance (world units) within which LOD0 is used; each coarser level doubles the radius
	LODBias    float64 // added to computed level before clamping; may be negative

	// Modifiers are region-scoped local edits applied in slice order
	// after the base heightfield. A modifier only affects chunks that
	// intersect its Region, which is what makes region-scoped
	// incremental recomputation exact.
	Modifiers []Modifier

	// Budgets and limits.
	Budgets Budgets
}

// Modifier is a local height edit over a world-space region.
type Modifier struct {
	Region       Region
	HeightOffset float64 // added to the height of every sample inside Region
}

// Budgets bound resource use. Zero-valued fields mean "unlimited"
// unless documented otherwise in docs/DESIGN.md.
type Budgets struct {
	MaxVerticesPerChunk  int // hard cap; generation fails with ErrBudgetExceeded
	MaxTrianglesPerChunk int // hard cap
	MaxChunksInFlight    int // max concurrently generating chunks; 0 => default
	MaxCachedChunks      int // cache capacity; 0 => default
	MaxChunksPerRequest  int // max chunks a single Generate call may request
}

// LODSelector maps a chunk to its detail level. It must be
// deterministic and must satisfy the adjacency constraint enforced by
// ValidateLODMap (neighbor levels differ by at most 1).
type LODSelector func(c ChunkCoord, p *Params) LOD
