package mesh

// HeightFunc samples terrain height at world coordinates (x, z).
type HeightFunc func(x, z float64) float64

// GridSpec describes a regular grid patch to triangulate.
type GridSpec struct {
	// OriginX/OriginZ is the world-space minimum corner.
	OriginX, OriginZ float64
	// Quads is the number of quads per side.
	Quads int
	// Step is the world-space spacing between adjacent grid vertices.
	Step float64
	// SkirtDepth, when > 0, adds a vertical skirt around the perimeter
	// dropping this far below the edge vertices. Skirts guarantee the
	// stitched surface has no holes even between chunks of different
	// LODs, whose shared edges are otherwise tessellated differently.
	SkirtDepth float64
}

// Build triangulates the grid described by spec, sampling heights with h.
//
// Determinism: vertices are emitted in row-major order and heights are
// sampled at exact global coordinates (Origin + i*Step), so vertices on
// shared chunk borders are computed from identical global coordinates and
// same-LOD neighbors share bit-identical border vertices.
func Build(spec GridSpec, h HeightFunc) *Mesh {
	n := spec.Quads + 1 // vertices per side
	m := &Mesh{}

	// Main grid, row-major (j = z index, i = x index).
	for j := 0; j < n; j++ {
		z := spec.OriginZ + float64(j)*spec.Step
		for i := 0; i < n; i++ {
			x := spec.OriginX + float64(i)*spec.Step
			m.Positions = append(m.Positions, x, h(x, z), z)
		}
	}

	// Two triangles per quad, fixed winding (counter-clockwise seen from
	// above, +Y up).
	idx := func(i, j int) uint32 { return uint32(j*n + i) }
	for j := 0; j < spec.Quads; j++ {
		for i := 0; i < spec.Quads; i++ {
			a := idx(i, j)
			b := idx(i+1, j)
			c := idx(i, j+1)
			d := idx(i+1, j+1)
			m.Indices = append(m.Indices, a, c, b, b, c, d)
		}
	}

	if spec.SkirtDepth > 0 {
		buildSkirts(m, spec, n)
	}
	return m
}

// buildSkirts appends a dropped duplicate of each border edge and the
// quads connecting the border to it. Skirt vertices for the four edges
// are emitted independently (corners appear once per adjacent edge);
// their positions are exact copies of border positions minus SkirtDepth.
func buildSkirts(m *Mesh, spec GridSpec, n int) {
	// edges lists border vertex indices in consistent order per edge.
	// Edge order: -Z (j=0), +X (i=n-1), +Z (j=n-1), -X (i=0).
	edges := [4][]uint32{
		make([]uint32, n),
		make([]uint32, n),
		make([]uint32, n),
		make([]uint32, n),
	}
	for i := 0; i < n; i++ {
		edges[0][i] = uint32(i)                     // -Z, left to right
		edges[1][i] = uint32(i*n + n - 1)           // +X, bottom to top
		edges[2][i] = uint32((n-1)*n + (n - 1 - i)) // +Z, right to left
		edges[3][i] = uint32((n - 1 - i) * n)       // -X, top to bottom
	}
	for e := 0; e < 4; e++ {
		edge := edges[e]
		base := uint32(m.VertexCount())
		for _, vi := range edge {
			x := m.Positions[vi*3]
			y := m.Positions[vi*3+1]
			z := m.Positions[vi*3+2]
			m.Positions = append(m.Positions, x, y-spec.SkirtDepth, z)
		}
		for i := 0; i < n-1; i++ {
			a := edge[i]
			b := edge[i+1]
			c := base + uint32(i)
			d := base + uint32(i+1)
			// Winding faces outward, away from the patch interior.
			m.Indices = append(m.Indices, a, b, c, b, d, c)
		}
	}
}
