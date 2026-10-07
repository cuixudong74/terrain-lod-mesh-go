package terrain

import "math"

// DistanceLODSelector returns a LODSelector based on the distance from
// each chunk's center to a focus point: LOD 0 within LOD0Radius, with
// the radius doubling per level. LODBias is added before clamping to
// [0, MaxLOD].
//
// The selector is deterministic and chunk-local. Neighboring chunks
// may differ by more than one level when LOD0Radius is small relative
// to the chunk size; the stitcher handles arbitrary level ratios, so
// no adjacency constraint is required for correctness.
func DistanceLODSelector(focusX, focusZ float64) LODSelector {
	return func(c ChunkCoord, p *Params) LOD {
		size := p.ChunkWorldSize
		cx := (float64(c.X) + 0.5) * size
		cz := (float64(c.Z) + 0.5) * size
		dx := cx - focusX
		dz := cz - focusZ
		d := math.Sqrt(float64(dx*dx) + float64(dz*dz))
		level := 0
		if d > p.LOD0Radius {
			level = int(math.Ceil(math.Log2(d / p.LOD0Radius)))
		}
		level += int(p.LODBias)
		if level < 0 {
			level = 0
		}
		if level > int(p.MaxLOD) {
			level = int(p.MaxLOD)
		}
		return LOD(level)
	}
}

// UniformLODSelector returns a selector giving every chunk the same LOD.
func UniformLODSelector(l LOD) LODSelector {
	return func(c ChunkCoord, p *Params) LOD { return l }
}

// ValidateLODMap checks the classic adjacency constraint
// (|ΔLOD| <= 1 between edge neighbors) over a set of chunks. It is
// provided for callers that want strict policies; the mesh stitcher
// itself supports arbitrary level differences.
func ValidateLODMap(levels map[ChunkCoord]LOD) error {
	for c, l := range levels {
		for _, d := range [2]ChunkCoord{{X: 1, Z: 0}, {X: 0, Z: 1}} {
			n := ChunkCoord{X: c.X + d.X, Z: c.Z + d.Z}
			nl, ok := levels[n]
			if !ok {
				continue
			}
			diff := int(l) - int(nl)
			if diff < 0 {
				diff = -diff
			}
			if diff > 1 {
				return &Error{
					Kind:  ErrKindDegenerateLOD,
					Coord: c,
					Op:    "validate-lod-map",
					Msg:   "neighbor LOD differs by more than 1",
				}
			}
		}
	}
	return nil
}
