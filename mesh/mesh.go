// Package mesh builds triangle meshes from heightfields and exports them
// in deterministic formats for offline validation and rendering.
package mesh

// Mesh is an indexed triangle mesh. Positions are XYZ triples in world
// space. Indices reference Positions/3.
type Mesh struct {
	Positions []float64
	Indices   []uint32
}

// VertexCount returns the number of vertices.
func (m *Mesh) VertexCount() int { return len(m.Positions) / 3 }

// TriangleCount returns the number of triangles.
func (m *Mesh) TriangleCount() int { return len(m.Indices) / 3 }
