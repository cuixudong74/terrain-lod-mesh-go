package terrain

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func coordRect(x0, z0, x1, z1 int32) []ChunkCoord {
	var out []ChunkCoord
	for x := x0; x < x1; x++ {
		for z := z0; z < z1; z++ {
			out = append(out, ChunkCoord{X: x, Z: z})
		}
	}
	return out
}

func exportBytes(t *testing.T, meshes []*Mesh) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteBinary(&buf, meshes); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestConcurrencyDeterminism(t *testing.T) {
	coords := coordRect(0, 0, 8, 8)
	var reference []byte
	for _, workers := range []int{1, 2, 7, 64} {
		p := testParams()
		p.Budgets.MaxChunksInFlight = workers
		w, err := NewWorld(p, DistanceLODSelector(40, 40))
		if err != nil {
			t.Fatal(err)
		}
		meshes, errs := w.Generate(context.Background(), coords)
		if len(errs) != 0 {
			t.Fatal(errs)
		}
		if len(meshes) != 64 {
			t.Fatalf("workers=%d: got %d meshes", workers, len(meshes))
		}
		b := exportBytes(t, meshes)
		if reference == nil {
			reference = b
		} else if !bytes.Equal(reference, b) {
			t.Fatalf("workers=%d: output differs from reference", workers)
		}
	}
}

func TestRepeatedRunsIdentical(t *testing.T) {
	coords := coordRect(-3, -3, 3, 3)
	var ref []byte
	for run := 0; run < 3; run++ {
		w, err := NewWorld(testParams(), DistanceLODSelector(0, 0))
		if err != nil {
			t.Fatal(err)
		}
		meshes, errs := w.Generate(context.Background(), coords)
		if len(errs) != 0 {
			t.Fatal(errs)
		}
		b := exportBytes(t, meshes)
		if ref == nil {
			ref = b
		} else if !bytes.Equal(ref, b) {
			t.Fatalf("run %d differs", run)
		}
	}
}

func TestIncrementalMatchesFullRebuild(t *testing.T) {
	coords := coordRect(0, 0, 6, 6)
	p := testParams()
	w, err := NewWorld(p, UniformLODSelector(1))
	if err != nil {
		t.Fatal(err)
	}
	if _, errs := w.Generate(context.Background(), coords); len(errs) != 0 {
		t.Fatal(errs)
	}
	// Truly local change: add a modifier inside a region.
	region := Region{MinX: 16, MinZ: 16, MaxX: 48, MaxZ: 48}
	invalidated, err := w.UpdateParams(region, func(p *Params) {
		p.Modifiers = append(p.Modifiers, Modifier{Region: region, HeightOffset: 7.5})
	})
	if err != nil {
		t.Fatal(err)
	}
	// Region [16,48) touches chunks 0..2 on each axis because chunk
	// vertex bounds are closed (the far edge vertex is shared).
	if len(invalidated) != 9 {
		t.Fatalf("expected 9 invalidated chunks, got %d", len(invalidated))
	}
	meshes, errs := w.Generate(context.Background(), coords)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	inc := exportBytes(t, meshes)

	// Full rebuild from scratch with the final params.
	p2 := testParams()
	p2.Modifiers = []Modifier{{Region: region, HeightOffset: 7.5}}
	w2, err := NewWorld(p2, UniformLODSelector(1))
	if err != nil {
		t.Fatal(err)
	}
	meshes2, errs := w2.Generate(context.Background(), coords)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	full := exportBytes(t, meshes2)
	if !bytes.Equal(inc, full) {
		t.Fatal("incremental result differs from full rebuild")
	}
}

func TestGlobalChangeMatchesFullRebuild(t *testing.T) {
	coords := coordRect(0, 0, 4, 4)
	w, err := NewWorld(testParams(), UniformLODSelector(1))
	if err != nil {
		t.Fatal(err)
	}
	if _, errs := w.Generate(context.Background(), coords); len(errs) != 0 {
		t.Fatal(errs)
	}
	// Global change: caller must pass AllRegion.
	if _, err := w.UpdateParams(AllRegion(), func(p *Params) {
		p.Noise.Octaves = 6
		p.Noise.Warp.Amplitude = 5.0
	}); err != nil {
		t.Fatal(err)
	}
	meshes, errs := w.Generate(context.Background(), coords)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	inc := exportBytes(t, meshes)

	p2 := testParams()
	p2.Noise.Octaves = 6
	p2.Noise.Warp.Amplitude = 5.0
	w2, err := NewWorld(p2, UniformLODSelector(1))
	if err != nil {
		t.Fatal(err)
	}
	meshes2, errs := w2.Generate(context.Background(), coords)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if !bytes.Equal(inc, exportBytes(t, meshes2)) {
		t.Fatal("global incremental result differs from full rebuild")
	}
}

func TestUnaffectedChunksNotRewritten(t *testing.T) {
	coords := coordRect(0, 0, 4, 4)
	w, err := NewWorld(testParams(), UniformLODSelector(0))
	if err != nil {
		t.Fatal(err)
	}
	if _, errs := w.Generate(context.Background(), coords); len(errs) != 0 {
		t.Fatal(errs)
	}
	far := ChunkCoord{X: 0, Z: 0}
	before := w.Cached(far)
	if before == nil {
		t.Fatal("expected cached chunk")
	}
	// Change confined to a far-away region via a local modifier.
	r := Region{MinX: 48, MinZ: 48, MaxX: 64, MaxZ: 64}
	_, err = w.UpdateParams(r, func(p *Params) {
		p.Modifiers = append(p.Modifiers, Modifier{Region: r, HeightOffset: 99})
	})
	if err != nil {
		t.Fatal(err)
	}
	after := w.Cached(far)
	if after != before {
		t.Fatal("unaffected cached chunk was rewritten")
	}
	// Affected chunk must have been dropped.
	if w.Cached(ChunkCoord{X: 3, Z: 3}) != nil {
		t.Fatal("affected chunk should have been invalidated")
	}
}

func TestUpdateIdempotentAndOrderIndependent(t *testing.T) {
	coords := coordRect(0, 0, 4, 4)
	mk := func() *World {
		w, err := NewWorld(testParams(), UniformLODSelector(0))
		if err != nil {
			t.Fatal(err)
		}
		if _, errs := w.Generate(context.Background(), coords); len(errs) != 0 {
			t.Fatal(errs)
		}
		return w
	}
	r1 := Region{MinX: 0, MinZ: 0, MaxX: 32, MaxZ: 32}
	r2 := Region{MinX: 16, MinZ: 16, MaxX: 64, MaxZ: 64}
	up1 := func(p *Params) { p.HeightScale = 20 }
	up2 := func(p *Params) { p.Noise.Gain = 0.25 }

	// World A: up1 then up2, with up1 submitted twice.
	wa := mk()
	if _, err := wa.UpdateParams(r1, up1); err != nil {
		t.Fatal(err)
	}
	if _, err := wa.UpdateParams(r1, up1); err != nil { // duplicate submit
		t.Fatal(err)
	}
	if _, err := wa.UpdateParams(r2, up2); err != nil {
		t.Fatal(err)
	}
	// World B: up2 then up1.
	wb := mk()
	if _, err := wb.UpdateParams(r2, up2); err != nil {
		t.Fatal(err)
	}
	if _, err := wb.UpdateParams(r1, up1); err != nil {
		t.Fatal(err)
	}

	ma, errs := wa.Generate(context.Background(), coords)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	mb, errs := wb.Generate(context.Background(), coords)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if !bytes.Equal(exportBytes(t, ma), exportBytes(t, mb)) {
		t.Fatal("update order/duplication changed the final result")
	}
}

func TestCancellation(t *testing.T) {
	p := testParams()
	p.Budgets.MaxChunksInFlight = 2
	w, err := NewWorld(p, UniformLODSelector(0))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before starting
	coords := coordRect(0, 0, 16, 16)
	meshes, errs := w.Generate(ctx, coords)
	if len(meshes) != 0 {
		t.Fatalf("canceled request produced %d meshes", len(meshes))
	}
	if len(errs) != len(coords) {
		t.Fatalf("got %d errors for %d canceled chunks", len(errs), len(coords))
	}
	for _, err := range errs {
		k, ok := KindOf(err)
		if !ok || k != ErrKindCanceled {
			t.Fatalf("expected canceled error, got %v", err)
		}
	}
	// Cache must not hold partial results.
	for _, c := range coords {
		if w.Cached(c) != nil {
			t.Fatalf("canceled chunk %v was cached", c)
		}
	}
}

func TestErrorClassification(t *testing.T) {
	// Invalid params.
	bad := testParams()
	bad.BaseSpacing = 0
	if _, err := NewWorld(bad, nil); err == nil {
		t.Fatal("expected invalid param error")
	} else if k, _ := KindOf(err); k != ErrKindInvalidParam {
		t.Fatalf("wrong kind: %v", err)
	}

	// Out-of-bounds chunk: use a large chunk size so a valid int32
	// coord can still exceed MaxWorldCoord.
	big := testParams()
	big.ChunkWorldSize = 65536
	big.BaseSpacing = 4096
	w, err := NewWorld(big, UniformLODSelector(0))
	if err != nil {
		t.Fatal(err)
	}
	far := ChunkCoord{X: 1 << 25, Z: 0} // x = 2^25 * 2^16 = 2^41 > 2^40
	_, errs := w.Generate(context.Background(), []ChunkCoord{far})
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if k, _ := KindOf(errs[0]); k != ErrKindOutOfBounds {
		t.Fatalf("wrong kind: %v", errs[0])
	}
	te := errs[0].(*Error)
	if te.Coord != far {
		t.Fatalf("error not attributed to chunk: %v", te)
	}

	// Budget exceeded.
	p := testParams()
	p.Budgets.MaxVerticesPerChunk = 10
	w2, err := NewWorld(p, UniformLODSelector(0))
	if err != nil {
		t.Fatal(err)
	}
	_, errs = w2.Generate(context.Background(), []ChunkCoord{{X: 0, Z: 0}})
	if len(errs) != 1 {
		t.Fatalf("expected budget error, got %v", errs)
	}
	if k, _ := KindOf(errs[0]); k != ErrKindBudgetExceeded {
		t.Fatalf("wrong kind: %v", errs[0])
	}

	// Request too large.
	p = testParams()
	p.Budgets.MaxChunksPerRequest = 2
	w3, err := NewWorld(p, UniformLODSelector(0))
	if err != nil {
		t.Fatal(err)
	}
	_, errs = w3.Generate(context.Background(), coordRect(0, 0, 3, 3))
	if len(errs) != 1 {
		t.Fatalf("expected request budget error, got %v", errs)
	}
	if k, _ := KindOf(errs[0]); k != ErrKindBudgetExceeded {
		t.Fatalf("wrong kind: %v", errs[0])
	}

	// Degenerate LOD: selector exceeding MaxLOD is clamped, so request
	// an impossible direct build instead.
	h, _ := NewHeightfield(testParams())
	_, err = buildChunkMesh(h, chunkSpec{coord: ChunkCoord{}, lod: 9})
	if k, _ := KindOf(err); k != ErrKindDegenerateLOD {
		t.Fatalf("wrong kind: %v", err)
	}
}

func TestGoldenHash(t *testing.T) {
	// Guards the whole pipeline against accidental output changes.
	w, err := NewWorld(testParams(), DistanceLODSelector(24, 24))
	if err != nil {
		t.Fatal(err)
	}
	meshes, errs := w.Generate(context.Background(), coordRect(0, 0, 4, 4))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	sum := sha256.Sum256(exportBytes(t, meshes))
	got := hex.EncodeToString(sum[:])
	const want = "917d6f9257fdea88f4ea942c6c3e65ac9be99146abe24f757f597e44f4121d33"
	if got != want {
		t.Fatalf("golden hash mismatch:\n got %s\nwant %s", got, want)
	}
}

func TestCacheBoundAndCorrectness(t *testing.T) {
	p := testParams()
	p.Budgets.MaxCachedChunks = 8
	p.Budgets.MaxChunksInFlight = 4
	w, err := NewWorld(p, UniformLODSelector(0))
	if err != nil {
		t.Fatal(err)
	}
	coords := coordRect(0, 0, 16, 16) // 256 chunks, cache cap 8
	meshes, errs := w.Generate(context.Background(), coords)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if len(meshes) != 256 {
		t.Fatalf("got %d meshes", len(meshes))
	}
	w.mu.Lock()
	n := len(w.cache)
	w.mu.Unlock()
	if n > 8 {
		t.Fatalf("cache holds %d entries, cap is 8", n)
	}
	// Results must still equal an uncached run bitwise.
	w2, err := NewWorld(testParams(), UniformLODSelector(0))
	if err != nil {
		t.Fatal(err)
	}
	meshes2, errs := w2.Generate(context.Background(), coords)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if !bytes.Equal(exportBytes(t, meshes), exportBytes(t, meshes2)) {
		t.Fatal("bounded-cache output differs from uncached output")
	}
}
