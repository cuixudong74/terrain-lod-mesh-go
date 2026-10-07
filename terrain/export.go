package terrain

import (
	"encoding/binary"
	"fmt"
	"io"
	"sort"
)

// sortedMeshes returns a copy of meshes sorted by (coordX, coordZ, lod)
// so exported bytes are independent of generation order.
func sortedMeshes(meshes []*Mesh) []*Mesh {
	out := make([]*Mesh, len(meshes))
	copy(out, meshes)
	sort.Slice(out, func(a, b int) bool {
		ma, mb := out[a], out[b]
		if ma.Coord.X != mb.Coord.X {
			return ma.Coord.X < mb.Coord.X
		}
		if ma.Coord.Z != mb.Coord.Z {
			return ma.Coord.Z < mb.Coord.Z
		}
		return ma.LOD < mb.LOD
	})
	return out
}

// WriteBinary writes meshes in a deterministic binary layout suitable
// for offline validation (byte-identical across runs and processes):
//
//	header: magic "TLM1", uint32 mesh count
//	per mesh: int32 coordX, int32 coordZ, uint8 lod,
//	          uint32 vertexCount, uint32 triangleCount,
//	          vertexCount*3 float32 positions (LE),
//	          triangleCount*3 uint32 indices (LE)
//
// Meshes are sorted by (coordX, coordZ, lod) before writing.
func WriteBinary(w io.Writer, meshes []*Mesh) error {
	if _, err := w.Write([]byte("TLM1")); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(len(meshes))); err != nil {
		return err
	}
	for _, m := range sortedMeshes(meshes) {
		header := struct {
			X, Z   int32
			LOD    uint8
			_      [3]byte
			NVerts uint32
			NTris  uint32
		}{X: m.Coord.X, Z: m.Coord.Z, LOD: uint8(m.LOD),
			NVerts: uint32(m.VertexCount()), NTris: uint32(m.TriangleCount())}
		if err := binary.Write(w, binary.LittleEndian, header); err != nil {
			return err
		}
		if err := binary.Write(w, binary.LittleEndian, m.Positions); err != nil {
			return err
		}
		if err := binary.Write(w, binary.LittleEndian, m.Indices); err != nil {
			return err
		}
	}
	return nil
}

// WriteOBJ writes meshes as Wavefront OBJ with deterministic vertex
// ordering and fixed float formatting, for inspection in DCC tools.
// Each mesh becomes one object named "chunk_<x>_<z>_lod<l>".
func WriteOBJ(w io.Writer, meshes []*Mesh) error {
	offset := 1 // OBJ indices are 1-based
	for _, m := range sortedMeshes(meshes) {
		if _, err := fmt.Fprintf(w, "o chunk_%d_%d_lod%d\n", m.Coord.X, m.Coord.Z, m.LOD); err != nil {
			return err
		}
		for v := 0; v < m.VertexCount(); v++ {
			if _, err := fmt.Fprintf(w, "v %.9g %.9g %.9g\n",
				m.Positions[v*3], m.Positions[v*3+1], m.Positions[v*3+2]); err != nil {
				return err
			}
		}
		for t := 0; t < m.TriangleCount(); t++ {
			if _, err := fmt.Fprintf(w, "f %d %d %d\n",
				offset+int(m.Indices[t*3]),
				offset+int(m.Indices[t*3+1]),
				offset+int(m.Indices[t*3+2])); err != nil {
				return err
			}
		}
		offset += m.VertexCount()
	}
	return nil
}
