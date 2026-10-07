package terrain

import "fmt"

// chunkSpec describes one chunk generation job: the chunk itself plus
// the LODs of its four edge neighbors, which determine stitching.
type chunkSpec struct {
	coord        ChunkCoord
	lod          LOD
	north, south LOD // neighbor LODs at z-1, z+1
	west, east   LOD // neighbor LODs at x-1, x+1
}

// buildChunkMesh generates a complete mesh for spec. It is pure: the
// output depends only on spec and the heightfield.
//
// Seam strategy: vertex world positions are derived from a GLOBAL
// integer lattice index (in units of BaseSpacing), so two chunks that
// share an edge compute bit-identical coordinates and heights for the
// shared vertices, at any LOD. When an edge neighbor is coarser, this
// chunk snaps border vertices to the neighbor's spacing (dropping
// T-junction vertices from the triangulation), so the shared edge is
// exactly the coarse neighbor's edge polyline: no cracks, no holes.
func buildChunkMesh(h *Heightfield, spec chunkSpec) (*Mesh, error) {
	p := &h.p
	fail := func(kind ErrorKind, msg string) (*Mesh, error) {
		return nil, &Error{Kind: kind, Coord: spec.coord, Op: "generate", Msg: msg}
	}
	if spec.lod > p.MaxLOD {
		return fail(ErrKindDegenerateLOD,
			fmt.Sprintf("lod %d exceeds max lod %d", spec.lod, p.MaxLOD))
	}

	n := p.QuadsPerSide(spec.lod)
	if n < 1 {
		return fail(ErrKindDegenerateLOD, "lod leaves zero quads per side")
	}
	nv := n + 1
	if p.Budgets.MaxVerticesPerChunk > 0 && nv*nv > p.Budgets.MaxVerticesPerChunk {
		return fail(ErrKindBudgetExceeded,
			fmt.Sprintf("vertex count %d exceeds budget %d", nv*nv, p.Budgets.MaxVerticesPerChunk))
	}
	if p.Budgets.MaxTrianglesPerChunk > 0 && 2*n*n > p.Budgets.MaxTrianglesPerChunk {
		return fail(ErrKindBudgetExceeded,
			fmt.Sprintf("triangle count %d exceeds budget %d", 2*n*n, p.Budgets.MaxTrianglesPerChunk))
	}

	// Global lattice index of vertex (0,0), in units of BaseSpacing.
	// The chunk origin is lod-independent; the vertex interval grows.
	ratio := int64(p.QuadsPerSide(0))
	shift := uint(spec.lod)
	gx0 := int64(spec.coord.X) * ratio
	gz0 := int64(spec.coord.Z) * ratio
	step := uint64(1) << shift // lattice units per vertex interval

	// Bounds check on the extreme corners, in world units.
	base := p.BaseSpacing
	corners := [4][2]int64{{gx0, gz0}, {gx0 + int64(n)*int64(step), gz0},
		{gx0, gz0 + int64(n)*int64(step)}, {gx0 + int64(n)*int64(step), gz0 + int64(n)*int64(step)}}
	for _, c := range corners {
		wx := float64(c[0]) * base
		wz := float64(c[1]) * base
		if wx >= MaxWorldCoord || wx <= -MaxWorldCoord || wz >= MaxWorldCoord || wz <= -MaxWorldCoord {
			return fail(ErrKindOutOfBounds, "chunk extends beyond the representable world")
		}
	}

	// Vertices: row-major (j z-axis, i x-axis), world-space positions.
	pos := make([]float32, 0, nv*nv*3)
	for j := 0; j < nv; j++ {
		gz := gz0 + int64(j)*int64(step)
		wz := float64(gz) * base
		for i := 0; i < nv; i++ {
			gx := gx0 + int64(i)*int64(step)
			wx := float64(gx) * base
			y := h.HeightAt(wx, wz)
			pos = append(pos, float32(wx), float32(y), float32(wz))
		}
	}

	// Stitching: per-side snap modulus in fine vertex intervals. A
	// coarser neighbor (neighbor lod > ours) forces border vertices to
	// snap down to multiples of 2^(neighborLod - lod).
	snapMod := func(neighbor LOD) int {
		if neighbor <= spec.lod {
			return 1
		}
		return 1 << uint(neighbor-spec.lod)
	}
	modN, modS := snapMod(spec.north), snapMod(spec.south)
	modW, modE := snapMod(spec.west), snapMod(spec.east)

	vid := func(i, j int) uint32 {
		// Snap coordinates that lie on a stitched (coarser-neighbor) edge.
		if j == 0 {
			i = i / modN * modN
		}
		if j == n {
			i = i / modS * modS
		}
		if i == 0 {
			j = j / modW * modW
		}
		if i == n {
			j = j / modE * modE
		}
		return uint32(j*nv + i)
	}

	idx := make([]uint32, 0, 2*n*n*3)
	emit := func(a, b, c uint32) {
		if a == b || b == c || a == c {
			return // degenerate triangle from snapped edge; skip
		}
		idx = append(idx, a, b, c)
	}
	for j := 0; j < n; j++ {
		for i := 0; i < n; i++ {
			v00 := vid(i, j)
			v10 := vid(i+1, j)
			v01 := vid(i, j+1)
			v11 := vid(i+1, j+1)
			emit(v00, v01, v10)
			emit(v10, v01, v11)
		}
	}

	m := &Mesh{Coord: spec.coord, LOD: spec.lod, Positions: pos, Indices: idx}
	compact(m)
	return m, nil
}

// compact drops vertices not referenced by any triangle (stitched-away
// border vertices) and remaps indices. Iteration is in fixed vertex
// order, so the result is deterministic.
func compact(m *Mesh) {
	nv := m.VertexCount()
	used := make([]bool, nv)
	for _, id := range m.Indices {
		used[id] = true
	}
	remap := make([]uint32, nv)
	next := uint32(0)
	for v := 0; v < nv; v++ {
		if used[v] {
			remap[v] = next
			next++
		}
	}
	if int(next) == nv {
		return
	}
	pos := make([]float32, 0, int(next)*3)
	for v := 0; v < nv; v++ {
		if used[v] {
			pos = append(pos, m.Positions[v*3:v*3+3]...)
		}
	}
	for k, id := range m.Indices {
		m.Indices[k] = remap[id]
	}
	m.Positions = pos
}
