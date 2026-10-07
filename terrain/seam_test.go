package terrain

import (
	"bytes"
	"testing"

	"github.com/cuixudong74/terrain-lod-mesh-go/noise"
)

func testParams() Params {
	return Params{
		Seed:        1234,
		HeightScale: 12.0,
		Noise: noise.Fractal{
			Source:     noise.PerlinNoise{Seed: 1234, Frequency: 0.05},
			Octaves:    4,
			Lacunarity: 2.0,
			Gain:       0.5,
			Warp:       noise.WarpConfig{Amplitude: 2.5, Frequency: 0.02, Seed: 77},
		},
		ChunkWorldSize: 16,
		BaseSpacing:    1,
		MaxLOD:         3,
		LOD0Radius:     24,
	}
}

// edgeVerts returns the positions of all USED vertices lying on the
// plane x == xBound (within exact float32 equality), as [z]y pairs.
func edgeVertsOnX(m *Mesh, xBound float32) map[float32]float32 {
	used := make(map[uint32]bool)
	for _, id := range m.Indices {
		used[id] = true
	}
	out := map[float32]float32{}
	for id := range used {
		x := m.Positions[id*3]
		if x == xBound {
			z := m.Positions[id*3+2]
			y := m.Positions[id*3+1]
			out[z] = y
		}
	}
	return out
}

func TestSameLODSeam(t *testing.T) {
	p := testParams()
	w, err := NewWorld(p, UniformLODSelector(0))
	if err != nil {
		t.Fatal(err)
	}
	meshes, errs := w.Generate(nil, []ChunkCoord{{X: 0, Z: 0}, {X: 1, Z: 0}})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if len(meshes) != 2 {
		t.Fatalf("got %d meshes", len(meshes))
	}
	west, east := meshes[0], meshes[1]
	// East edge of chunk (0,0) is at x = 16; west edge of (1,0) too.
	a := edgeVertsOnX(west, 16)
	b := edgeVertsOnX(east, 16)
	if len(a) != 17 || len(b) != 17 {
		t.Fatalf("edge vertex counts: %d, %d (want 17)", len(a), len(b))
	}
	for z, ya := range a {
		yb, ok := b[z]
		if !ok {
			t.Fatalf("missing shared edge vertex at z=%v", z)
		}
		if ya != yb {
			t.Fatalf("height mismatch at shared edge z=%v: %v vs %v", z, ya, yb)
		}
	}
}

func TestCrossLODSeam(t *testing.T) {
	p := testParams()
	// Fine (lod 0) for x < 2, coarse (lod 1) for x >= 2.
	sel := func(c ChunkCoord, p *Params) LOD {
		if c.X < 2 {
			return 0
		}
		return 1
	}
	w, err := NewWorld(p, sel)
	if err != nil {
		t.Fatal(err)
	}
	meshes, errs := w.Generate(nil, []ChunkCoord{{X: 1, Z: 0}, {X: 2, Z: 0}})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	fine, coarse := meshes[0], meshes[1]
	if fine.Coord != (ChunkCoord{X: 1, Z: 0}) || coarse.Coord != (ChunkCoord{X: 2, Z: 0}) {
		t.Fatalf("unexpected order: %v %v", fine.Coord, coarse.Coord)
	}
	// Shared boundary is x = 32. The fine chunk must have stitched its
	// edge down to the coarse spacing: identical vertex sets.
	fe := edgeVertsOnX(fine, 32)
	ce := edgeVertsOnX(coarse, 32)
	if len(ce) != 9 { // 8 quads at lod 1 -> 9 edge vertices
		t.Fatalf("coarse edge has %d vertices, want 9", len(ce))
	}
	if len(fe) != len(ce) {
		t.Fatalf("fine edge used %d vertices, coarse %d: T-junction leak", len(fe), len(ce))
	}
	for z, yf := range fe {
		yc, ok := ce[z]
		if !ok {
			t.Fatalf("fine edge vertex at z=%v has no coarse counterpart", z)
		}
		if yf != yc {
			t.Fatalf("cross-LOD height mismatch at z=%v: %v vs %v", z, yf, yc)
		}
	}
}

func TestCrossLODSeamRatio4(t *testing.T) {
	p := testParams()
	// lod 0 next to lod 2 (ratio 4): stitcher must handle any ratio.
	sel := func(c ChunkCoord, p *Params) LOD {
		if c.X < 2 {
			return 0
		}
		return 2
	}
	w, err := NewWorld(p, sel)
	if err != nil {
		t.Fatal(err)
	}
	meshes, errs := w.Generate(nil, []ChunkCoord{{X: 1, Z: 0}, {X: 2, Z: 0}})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	fe := edgeVertsOnX(meshes[0], 32)
	ce := edgeVertsOnX(meshes[1], 32)
	if len(ce) != 5 || len(fe) != 5 {
		t.Fatalf("edge vertices: fine %d, coarse %d, want 5/5", len(fe), len(ce))
	}
	for z, yf := range fe {
		if ce[z] != yf {
			t.Fatalf("mismatch at z=%v", z)
		}
	}
}

func TestChunkBuildDeterministic(t *testing.T) {
	p := testParams()
	h, err := NewHeightfield(p)
	if err != nil {
		t.Fatal(err)
	}
	spec := chunkSpec{coord: ChunkCoord{X: 3, Z: -2}, lod: 1, north: 1, south: 2, west: 0, east: 1}
	m1, err := buildChunkMesh(h, spec)
	if err != nil {
		t.Fatal(err)
	}
	m2, err := buildChunkMesh(h, spec)
	if err != nil {
		t.Fatal(err)
	}
	var b1, b2 bytes.Buffer
	if err := WriteBinary(&b1, []*Mesh{m1}); err != nil {
		t.Fatal(err)
	}
	if err := WriteBinary(&b2, []*Mesh{m2}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b1.Bytes(), b2.Bytes()) {
		t.Fatal("same spec produced different bytes")
	}
}
