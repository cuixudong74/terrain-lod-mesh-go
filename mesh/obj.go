package mesh

import (
	"bufio"
	"io"
	"strconv"
)

// WriteOBJ writes the mesh in Wavefront OBJ text format.
//
// Determinism: floats are formatted with strconv.FormatFloat(f, 'g', -1, 64),
// a pure function of the float64 bit pattern, and vertices/indices are
// emitted in slice order, so output bytes are identical across runs,
// goroutine schedules and process restarts.
func WriteOBJ(w io.Writer, m *Mesh) error {
	bw := bufio.NewWriter(w)
	for i := 0; i < m.VertexCount(); i++ {
		bw.WriteString("v ")
		bw.WriteString(formatFloat(m.Positions[i*3]))
		bw.WriteByte(' ')
		bw.WriteString(formatFloat(m.Positions[i*3+1]))
		bw.WriteByte(' ')
		bw.WriteString(formatFloat(m.Positions[i*3+2]))
		bw.WriteByte('\n')
	}
	for i := 0; i < m.TriangleCount(); i++ {
		bw.WriteString("f ")
		// OBJ indices are 1-based.
		bw.WriteString(strconv.FormatUint(uint64(m.Indices[i*3])+1, 10))
		bw.WriteByte(' ')
		bw.WriteString(strconv.FormatUint(uint64(m.Indices[i*3+1])+1, 10))
		bw.WriteByte(' ')
		bw.WriteString(strconv.FormatUint(uint64(m.Indices[i*3+2])+1, 10))
		bw.WriteByte('\n')
	}
	return bw.Flush()
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}
