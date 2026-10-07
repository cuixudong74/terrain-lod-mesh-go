package terrain

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cuixudong74/terrain-lod-mesh-go/mesh"
	"github.com/cuixudong74/terrain-lod-mesh-go/noise"
)

func testConfig() *Config {
	return &Config{
		Seed:       12345,
		BaseHeight: 2.0,
		Layers: []LayerConfig{
			{
				Primitive:  noise.GradientNoise,
				SeedOffset: 0,
				Frequency:  0.05,
				Amplitude:  10.0,
				Fractal:    noise.FractalConfig{Octaves: 4, Lacunarity: 2, Gain: 0.5, WarpAmp: 3, WarpFreq: 0.02},
			},
			{
				Primitive:  noise.ValueNoise,
				SeedOffset: 777,
				Frequency:  0.4,
				Amplitude:  0.5,
				Fractal:    noise.FractalConfig{Octaves: 2, Lacunarity: 2, Gain: 0.5},
			},
		},
	}
}

func testGrid() *Grid {
	return &Grid{ChunkSize: 16, BaseQuads: 16, MaxLOD: 4}
}

func testLimits(workers int) *Limits {
	return &Limits{
		MaxVertices:     100000,
		MaxTriangles:    200000,
		MaxCachedChunks: 256,
		Workers:         workers,
		WorldMinX:       -4, WorldMaxX: 4,
		WorldMinZ: -4, WorldMaxZ: 4,
		DefaultSkirtDepth: 1.0,
	}
}

func newManager(t *testing.T, cfg *Config, workers int) *Manager {
	t.Helper()
	m, err := NewManager(cfg, testGrid(), testLimits(workers))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

func serialize(t *testing.T, ms *mesh.Mesh) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := mesh.WriteOBJ(&buf, ms); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func genAll(t *testing.T, m *Manager, reqs []ChunkRequest) []ChunkResult {
	t.Helper()
	res, err := m.Generate(context.Background(), reqs)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(res) != len(reqs) {
		t.Fatalf("got %d results for %d requests", len(res), len(reqs))
	}
	return res
}

func regionReqs(x0, x1, z0, z1 int32, lod uint8) []ChunkRequest {
	var reqs []ChunkRequest
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			reqs = append(reqs, ChunkRequest{ID: ChunkID{X: x, Z: z, LOD: lod}})
		}
	}
	return reqs
}

func TestHeightAtDeterministic(t *testing.T) {
	cfg := testConfig()
	want := HeightAt(cfg, 3.7, -11.2)
	for i := 0; i < 1000; i++ {
		if got := HeightAt(cfg, 3.7, -11.2); got != want {
			t.Fatalf("eval %d: %v != %v", i, got, want)
		}
	}
}

func TestHeightAtChunkIndependent(t *testing.T) {
	// The same global coordinate evaluated "from" different chunk grids
	// must give identical heights: sample points on a chunk boundary via
	// both neighbors' local parameterizations.
	cfg := testConfig()
	g := testGrid()
	ox0, _ := g.Origin(1, 0) // boundary x between chunk 0 and 1
	for j := 0; j <= 16; j++ {
		z := float64(j) * g.Step(0)
		hFromLeft := HeightAt(cfg, ox0, z)  // chunk (1,0) local x=0
		hFromRight := HeightAt(cfg, ox0, z) // chunk (0,0) local x=size
		if hFromLeft != hFromRight {
			t.Fatalf("boundary mismatch at z=%v", z)
		}
	}
}

func TestGenerateDeterministicAcrossWorkers(t *testing.T) {
	reqs := regionReqs(-2, 2, -2, 2, 0)
	reqs = append(reqs, regionReqs(-1, 1, -1, 1, 1)...)
	reqs = append(reqs, regionReqs(0, 1, 0, 1, 2)...)

	serial := genAll(t, newManager(t, testConfig(), 1), reqs)
	for _, w := range []int{2, 4, 8, 16} {
		par := genAll(t, newManager(t, testConfig(), w), reqs)
		if len(par) != len(serial) {
			t.Fatalf("workers=%d: result count differs", w)
		}
		for i := range serial {
			if par[i].ID != serial[i].ID {
				t.Fatalf("workers=%d: result order differs at %d", w, i)
			}
			if !bytes.Equal(serialize(t, par[i].Mesh), serialize(t, serial[i].Mesh)) {
				t.Fatalf("workers=%d: mesh bytes differ for chunk %+v", w, serial[i].ID)
			}
		}
	}
}

func TestGenerateRepeatRunsIdentical(t *testing.T) {
	reqs := regionReqs(-1, 1, -1, 1, 0)
	a := genAll(t, newManager(t, testConfig(), 4), reqs)
	b := genAll(t, newManager(t, testConfig(), 4), reqs)
	for i := range a {
		if !bytes.Equal(serialize(t, a[i].Mesh), serialize(t, b[i].Mesh)) {
			t.Fatalf("run-to-run mismatch at chunk %+v", a[i].ID)
		}
	}
}

func TestSameLODSeams(t *testing.T) {
	m := newManager(t, testConfig(), 4)
	res := genAll(t, m, regionReqs(0, 2, 0, 2, 0))
	at := func(x, z int32) *mesh.Mesh {
		for _, r := range res {
			if r.ID.X == x && r.ID.Z == z {
				return r.Mesh
			}
		}
		t.Fatalf("missing chunk %d,%d", x, z)
		return nil
	}
	for x := int32(0); x <= 2; x++ {
		for z := int32(0); z <= 2; z++ {
			if x < 2 {
				if err := mesh.CheckSharedEdge(at(x, z), mesh.EdgePosX, at(x+1, z), mesh.EdgeNegX); err != nil {
					t.Fatalf("x-seam (%d,%d)-(%d,%d): %v", x, z, x+1, z, err)
				}
			}
			if z < 2 {
				if err := mesh.CheckSharedEdge(at(x, z), mesh.EdgePosZ, at(x, z+1), mesh.EdgeNegZ); err != nil {
					t.Fatalf("z-seam (%d,%d)-(%d,%d): %v", x, z, x, z+1, err)
				}
			}
		}
	}
}

func TestCrossLODSeams(t *testing.T) {
	m := newManager(t, testConfig(), 4)
	var reqs []ChunkRequest
	for lod := uint8(0); lod <= 3; lod++ {
		reqs = append(reqs,
			ChunkRequest{ID: ChunkID{X: 0, Z: 0, LOD: lod}},
			ChunkRequest{ID: ChunkID{X: 1, Z: 0, LOD: lod + 1}},
		)
	}
	res := genAll(t, m, reqs)
	at := func(x int32, lod uint8) *mesh.Mesh {
		for _, r := range res {
			if r.ID.X == x && r.ID.LOD == lod {
				return r.Mesh
			}
		}
		t.Fatalf("missing chunk x=%d lod=%d", x, lod)
		return nil
	}
	for lod := uint8(0); lod <= 3; lod++ {
		if err := mesh.CheckSharedEdge(at(0, lod), mesh.EdgePosX, at(1, lod+1), mesh.EdgeNegX); err != nil {
			t.Fatalf("cross-LOD seam lod%d-lod%d: %v", lod, lod+1, err)
		}
	}
}

func TestIncrementalMatchesFullRebuild(t *testing.T) {
	cfg := testConfig()
	m := newManager(t, cfg, 4)
	reqs := regionReqs(-2, 2, -2, 2, 0)
	before := genAll(t, m, reqs)

	// Localized change: add a hill inside chunk (0,0)..(1,1) area.
	cfg2 := testConfig()
	cfg2.Modifiers = append(cfg2.Modifiers, Modifier{
		ID: "hill", CenterX: 8, CenterZ: 8, Radius: 20, Height: 15, Falloff: 2,
	})
	affected, err := m.UpdateConfig(cfg2)
	if err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	if len(affected) == 0 {
		t.Fatal("expected some affected chunks")
	}
	// Footprint circle (8,8,r=20) covers chunks roughly x,z in [-1..1].
	for _, id := range affected {
		ox, oz := testGrid().Origin(id.X, id.Z)
		cx, cz := ox+8, oz+8 // chunk center
		d2 := (cx-8)*(cx-8) + (cz-8)*(cz-8)
		if d2 > (20+16)*(20+16) { // generous bound: radius + chunk diagonal/2 slack
			t.Fatalf("chunk %+v reported affected but is far outside footprint", id)
		}
	}

	after := genAll(t, m, reqs)

	// Full rebuild from scratch with the new config.
	fresh := genAll(t, newManager(t, cfg2, 1), reqs)
	for i := range after {
		if !bytes.Equal(serialize(t, after[i].Mesh), serialize(t, fresh[i].Mesh)) {
			t.Fatalf("incremental != full rebuild at chunk %+v", after[i].ID)
		}
	}

	// Unaffected chunks must be untouched (same cached mesh object).
	affectedSet := map[ChunkID]bool{}
	for _, id := range affected {
		affectedSet[id] = true
	}
	for i := range after {
		if !affectedSet[after[i].ID] && after[i].Mesh != before[i].Mesh {
			t.Fatalf("unaffected chunk %+v was rewritten", after[i].ID)
		}
	}
}

func TestUpdateIdempotentAndOrderIndependent(t *testing.T) {
	mkCfg := func(mods ...Modifier) *Config {
		c := testConfig()
		c.Modifiers = mods
		return c
	}
	hill := Modifier{ID: "hill", CenterX: 0, CenterZ: 0, Radius: 30, Height: 10, Falloff: 1}
	dip := Modifier{ID: "dip", CenterX: 40, CenterZ: -40, Radius: 25, Height: -8, Falloff: 1}
	final := mkCfg(hill, dip)
	reqs := regionReqs(-3, 3, -3, 3, 0)

	// Path A: apply both at once.
	mA := newManager(t, testConfig(), 2)
	genAll(t, mA, reqs)
	if _, err := mA.UpdateConfig(final); err != nil {
		t.Fatal(err)
	}
	resA := genAll(t, mA, reqs)

	// Path B: apply one, then the other, then re-apply the final config
	// twice (duplicate submissions must not matter).
	mB := newManager(t, testConfig(), 2)
	genAll(t, mB, reqs)
	if _, err := mB.UpdateConfig(mkCfg(hill)); err != nil {
		t.Fatal(err)
	}
	genAll(t, mB, reqs)
	if _, err := mB.UpdateConfig(final); err != nil {
		t.Fatal(err)
	}
	if _, err := mB.UpdateConfig(final); err != nil {
		t.Fatal(err)
	}
	resB := genAll(t, mB, reqs)

	// Reference: fresh full build.
	ref := genAll(t, newManager(t, final, 1), reqs)

	for i := range ref {
		bA := serialize(t, resA[i].Mesh)
		bB := serialize(t, resB[i].Mesh)
		bRef := serialize(t, ref[i].Mesh)
		if !bytes.Equal(bA, bRef) || !bytes.Equal(bB, bRef) {
			t.Fatalf("update path mismatch at chunk %+v", ref[i].ID)
		}
	}
}

func TestCancellationNoPartialChunks(t *testing.T) {
	m := newManager(t, testConfig(), 4)
	reqs := regionReqs(-4, 4, -4, 4, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	res, err := m.Generate(ctx, reqs)
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	terr, ok := err.(*Error)
	if !ok || terr.Code != ErrCanceled {
		t.Fatalf("expected ErrCanceled, got %v", err)
	}
	// Every returned chunk must be complete (mesh fully built).
	for _, r := range res {
		if r.Mesh == nil || r.Mesh.VertexCount() == 0 {
			t.Fatalf("partial chunk %+v delivered", r.ID)
		}
	}
}

func TestTimeoutNoPartialChunks(t *testing.T) {
	m := newManager(t, testConfig(), 2)
	reqs := regionReqs(-4, 4, -4, 4, 0)
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond) // ensure the deadline passes
	_, err := m.Generate(ctx, reqs)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if terr, ok := err.(*Error); !ok || terr.Code != ErrCanceled {
		t.Fatalf("expected ErrCanceled, got %v", err)
	}
}

func TestErrorOutOfBounds(t *testing.T) {
	m := newManager(t, testConfig(), 1)
	_, err := m.Generate(context.Background(), []ChunkRequest{{ID: ChunkID{X: 99, Z: 0}}})
	terr, ok := err.(*Error)
	if !ok || terr.Code != ErrOutOfBounds {
		t.Fatalf("expected ErrOutOfBounds, got %v", err)
	}
	if terr.Chunk == nil || terr.Chunk.X != 99 {
		t.Fatalf("error not bound to chunk: %v", terr)
	}
}

func TestErrorLODTooHigh(t *testing.T) {
	m := newManager(t, testConfig(), 1)
	_, err := m.Generate(context.Background(), []ChunkRequest{{ID: ChunkID{X: 0, Z: 0, LOD: 9}}})
	if terr, ok := err.(*Error); !ok || terr.Code != ErrOutOfBounds {
		t.Fatalf("expected ErrOutOfBounds for degenerate LOD, got %v", err)
	}
}

func TestErrorBudgetExceeded(t *testing.T) {
	lim := testLimits(1)
	lim.MaxVertices = 10 // way below 17x17 grid
	m, err := NewManager(testConfig(), testGrid(), lim)
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Generate(context.Background(), []ChunkRequest{{ID: ChunkID{X: 0, Z: 0}}})
	if terr, ok := err.(*Error); !ok || terr.Code != ErrBudgetExceeded {
		t.Fatalf("expected ErrBudgetExceeded, got %v", err)
	}
}

func TestErrorInvalidConfig(t *testing.T) {
	bad := testConfig()
	bad.Layers[0].Frequency = -1
	if _, err := NewManager(bad, testGrid(), testLimits(1)); err == nil {
		t.Fatal("expected invalid config error")
	}
	bad2 := testConfig()
	bad2.Modifiers = []Modifier{{ID: "x", Radius: -5}}
	if _, err := NewManager(bad2, testGrid(), testLimits(1)); err == nil {
		t.Fatal("expected invalid modifier error")
	}
	if _, err := NewManager(testConfig(), &Grid{ChunkSize: 16, BaseQuads: 12, MaxLOD: 1}, testLimits(1)); err == nil {
		t.Fatal("expected invalid grid error (BaseQuads not power of two)")
	}
}

func TestCacheBoundedAndCorrect(t *testing.T) {
	lim := testLimits(4)
	lim.MaxCachedChunks = 8
	m, err := NewManager(testConfig(), testGrid(), lim)
	if err != nil {
		t.Fatal(err)
	}
	reqs := regionReqs(-4, 4, -4, 4, 0) // 81 chunks, cache holds 8
	res := genAll(t, m, reqs)
	// Reference without cache.
	limNoCache := testLimits(1)
	limNoCache.MaxCachedChunks = 0
	ref, err := NewManager(testConfig(), testGrid(), limNoCache)
	if err != nil {
		t.Fatal(err)
	}
	want := genAll(t, ref, reqs)
	for i := range res {
		if !bytes.Equal(serialize(t, res[i].Mesh), serialize(t, want[i].Mesh)) {
			t.Fatalf("cached generation differs at chunk %+v", res[i].ID)
		}
	}
}

func TestConfigHashStable(t *testing.T) {
	a := testConfig()
	b := testConfig()
	if a.Hash() != b.Hash() {
		t.Fatal("identical configs hash differently")
	}
	b.BaseHeight += 1
	if a.Hash() == b.Hash() {
		t.Fatal("different configs hash identically")
	}
}

func ExampleManager_Generate() {
	cfg := &Config{
		Seed: 1,
		Layers: []LayerConfig{{
			Primitive: noise.GradientNoise, Frequency: 0.1, Amplitude: 5,
			Fractal: noise.DefaultFractal(),
		}},
	}
	grid := &Grid{ChunkSize: 32, BaseQuads: 32, MaxLOD: 5}
	limits := &Limits{Workers: 4, MaxCachedChunks: 64, WorldMaxX: 8, WorldMaxZ: 8, WorldMinX: -8, WorldMinZ: -8}
	m, _ := NewManager(cfg, grid, limits)
	res, _ := m.Generate(context.Background(), []ChunkRequest{{ID: ChunkID{X: 0, Z: 0, LOD: 0}}})
	fmt.Println(res[0].Mesh.VertexCount() > 0)
	// Output: true
}
