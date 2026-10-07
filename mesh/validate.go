package mesh

import (
	"fmt"
	"sort"
)

// Edge identifies one border of a grid patch.
type Edge int

const (
	EdgeNegZ Edge = iota // minimum Z
	EdgePosX             // maximum X
	EdgePosZ             // maximum Z
	EdgeNegX             // minimum X
)

// Bounds returns the axis-aligned bounding box of the mesh positions.
func Bounds(m *Mesh) (min, max [3]float64) {
	for i := 0; i < 3; i++ {
		min[i] = m.Positions[i]
		max[i] = m.Positions[i]
	}
	for v := 1; v < m.VertexCount(); v++ {
		for i := 0; i < 3; i++ {
			p := m.Positions[v*3+i]
			if p < min[i] {
				min[i] = p
			}
			if p > max[i] {
				max[i] = p
			}
		}
	}
	return min, max
}

// ExtractEdge returns the surface vertex positions along the given edge:
// for each distinct coordinate along the edge, the top-most (max Y)
// vertex is kept, which skips skirt duplicates. The result is sorted
// along the edge for deterministic comparison.
func ExtractEdge(m *Mesh, edge Edge) [][3]float64 {
	min, max := Bounds(m)
	var planeAxis int
	var val float64
	switch edge {
	case EdgeNegZ:
		planeAxis, val = 2, min[2]
	case EdgePosZ:
		planeAxis, val = 2, max[2]
	case EdgeNegX:
		planeAxis, val = 0, min[0]
	case EdgePosX:
		planeAxis, val = 0, max[0]
	}

	// Group border-plane vertices by their coordinate along the edge,
	// keeping the top-most Y for each (skirt vertices share X/Z but are
	// lower).
	alongAxis := 0
	if planeAxis == 0 {
		alongAxis = 2
	}
	type best struct {
		y   float64
		pos [3]float64
		set bool
	}
	byCoord := map[float64]*best{}
	var order []float64
	for v := 0; v < m.VertexCount(); v++ {
		p := [3]float64{m.Positions[v*3], m.Positions[v*3+1], m.Positions[v*3+2]}
		if p[planeAxis] != val {
			continue
		}
		c := p[alongAxis]
		b, ok := byCoord[c]
		if !ok {
			b = &best{}
			byCoord[c] = b
			order = append(order, c)
		}
		if !b.set || p[1] > b.y {
			b.y = p[1]
			b.pos = p
			b.set = true
		}
	}
	sort.Float64s(order)
	out := make([][3]float64, 0, len(order))
	for _, c := range order {
		out = append(out, byCoord[c].pos)
	}
	return out
}

// CheckSharedEdge verifies that two meshes are continuous across a shared
// edge: every surface vertex of the sparser edge must appear with a
// bit-identical position in the denser edge. For same-LOD neighbors the
// vertex sets must be identical. It returns a descriptive error otherwise.
//
// Note: between different LODs the finer edge has extra intermediate
// vertices that do not lie on the coarser edge segments; watertightness
// there is provided by skirts, not by vertex matching. This check
// validates exactly the alignable part: all shared grid points.
func CheckSharedEdge(a *Mesh, ea Edge, b *Mesh, eb Edge) error {
	va := ExtractEdge(a, ea)
	vb := ExtractEdge(b, eb)
	if len(va) == 0 || len(vb) == 0 {
		return fmt.Errorf("mesh: empty edge (|a|=%d, |b|=%d)", len(va), len(vb))
	}
	// Index b's vertices by exact position.
	set := make(map[[3]float64]bool, len(vb))
	for _, p := range vb {
		set[p] = true
	}
	// The sparser edge must be fully contained in the denser one.
	sparse, denseSet := va, set
	if len(vb) < len(va) {
		sparse = vb
		denseSet = make(map[[3]float64]bool, len(va))
		for _, p := range va {
			denseSet[p] = true
		}
	}
	for _, p := range sparse {
		if !denseSet[p] {
			return fmt.Errorf("mesh: shared edge mismatch at (%g, %g, %g): present in sparser edge, missing in denser edge", p[0], p[1], p[2])
		}
	}
	return nil
}
