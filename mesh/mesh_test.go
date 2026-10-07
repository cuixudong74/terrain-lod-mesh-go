package mesh

import (
	"bytes"
	"strings"
	"testing"
)

func height(x, z float64) float64 { return 0.25*x - 0.5*z }

func TestBuildGridCounts(t *testing.T) {
	m := Build(GridSpec{OriginX: 0, OriginZ: 0, Quads: 4, Step: 1}, height)
	if got, want := m.VertexCount(), 25; got != want {
		t.Fatalf("vertices: got %d want %d", got, want)
	}
	if got, want := m.TriangleCount(), 32; got != want {
		t.Fatalf("triangles: got %d want %d", got, want)
	}
}

func TestBuildSkirtCounts(t *testing.T) {
	m := Build(GridSpec{OriginX: 0, OriginZ: 0, Quads: 4, Step: 1, SkirtDepth: 2}, height)
	if got, want := m.VertexCount(), 25+20; got != want {
		t.Fatalf("vertices: got %d want %d", got, want)
	}
	if got, want := m.TriangleCount(), 32+32; got != want {
		t.Fatalf("triangles: got %d want %d", got, want)
	}
	// Skirt vertices are exactly SkirtDepth below their border source.
	// Surface heights range from height(0,4)=-2 to height(4,0)=1.
	min, max := Bounds(m)
	if min[1] != height(0, 4)-2 || max[1] != height(4, 0) {
		t.Fatalf("unexpected height span: %v..%v", min[1], max[1])
	}
}

func TestSharedEdgeSameLOD(t *testing.T) {
	a := Build(GridSpec{OriginX: 0, OriginZ: 0, Quads: 4, Step: 1}, height)
	b := Build(GridSpec{OriginX: 4, OriginZ: 0, Quads: 4, Step: 1}, height)
	if err := CheckSharedEdge(a, EdgePosX, b, EdgeNegX); err != nil {
		t.Fatal(err)
	}
}

func TestSharedEdgeCrossLOD(t *testing.T) {
	coarse := Build(GridSpec{OriginX: 0, OriginZ: 0, Quads: 2, Step: 2}, height)
	fine := Build(GridSpec{OriginX: 4, OriginZ: 0, Quads: 4, Step: 1}, height)
	if err := CheckSharedEdge(coarse, EdgePosX, fine, EdgeNegX); err != nil {
		t.Fatal(err)
	}
}

func TestSharedEdgeMismatchDetected(t *testing.T) {
	a := Build(GridSpec{OriginX: 0, OriginZ: 0, Quads: 4, Step: 1}, height)
	// Neighbor built from a different height function: seam must fail.
	b := Build(GridSpec{OriginX: 4, OriginZ: 0, Quads: 4, Step: 1}, func(x, z float64) float64 {
		return height(x, z) + 1
	})
	if err := CheckSharedEdge(a, EdgePosX, b, EdgeNegX); err == nil {
		t.Fatal("expected seam mismatch to be detected")
	}
}

func TestOBJDeterministic(t *testing.T) {
	m := Build(GridSpec{OriginX: -3.5, OriginZ: 7.25, Quads: 8, Step: 0.5, SkirtDepth: 1}, height)
	var b1, b2 bytes.Buffer
	if err := WriteOBJ(&b1, m); err != nil {
		t.Fatal(err)
	}
	if err := WriteOBJ(&b2, m); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b1.Bytes(), b2.Bytes()) {
		t.Fatal("OBJ output not deterministic")
	}
	out := b1.String()
	if !strings.Contains(out, "v ") || !strings.Contains(out, "f ") {
		t.Fatal("OBJ output missing vertices or faces")
	}
	lines := strings.Count(out, "\n")
	if want := m.VertexCount() + m.TriangleCount(); lines != want {
		t.Fatalf("OBJ line count %d, want %d", lines, want)
	}
}
