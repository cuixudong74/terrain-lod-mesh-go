package terrain

import (
	"context"
	"runtime"
	"sort"
	"sync"
)

// Region is a world-space axis-aligned rectangle describing the area
// affected by a parameter update. The zero Region is empty.
type Region struct {
	MinX, MinZ, MaxX, MaxZ float64
}

// AllRegion returns the region covering the whole representable world.
func AllRegion() Region {
	return Region{MinX: -MaxWorldCoord, MinZ: -MaxWorldCoord, MaxX: MaxWorldCoord, MaxZ: MaxWorldCoord}
}

// containsPoint reports whether (x, z) lies in the half-open region
// [MinX,MaxX) x [MinZ,MaxZ). The half-open convention matches
// containsChunk, so a chunk is invalidated iff it can contain an
// affected sample point.
func (r Region) containsPoint(x, z float64) bool {
	return x >= r.MinX && x < r.MaxX && z >= r.MinZ && z < r.MaxZ
}

// containsChunk reports whether the region can affect any vertex of
// the chunk. Chunk vertices span the CLOSED interval [x0, x0+size]
// (the far edge is shared with the next chunk), so a region touching
// the chunk's min edge invalidates it.
func (r Region) containsChunk(c ChunkCoord, p *Params) bool {
	size := p.ChunkWorldSize
	x0 := float64(c.X) * size
	z0 := float64(c.Z) * size
	return x0 < r.MaxX && x0+size >= r.MinX && z0 < r.MaxZ && z0+size >= r.MinZ
}

type cacheEntry struct {
	lod     LOD
	version uint64
	mesh    *Mesh
}

// World owns the params, the LOD policy, a chunk cache, and the
// parallel generator. All exported results are complete chunks.
//
// Incremental contract: a chunk mesh is a pure function of
// (Params, ChunkCoord, neighbor LODs). UpdateParams drops exactly the
// cached chunks intersecting the caller-supplied region; every other
// cached chunk keeps its mesh bytes untouched. Regenerating dropped
// chunks under the new params yields bit-identical results to a full
// rebuild, because generation has no hidden state.
type World struct {
	mu       sync.Mutex
	params   Params
	hf       *Heightfield
	selector LODSelector
	version  uint64
	cache    map[ChunkCoord]*cacheEntry
}

// NewWorld validates params and returns a ready World. A nil selector
// means uniform LOD 0.
func NewWorld(p Params, sel LODSelector) (*World, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if sel == nil {
		sel = UniformLODSelector(0)
	}
	hf, err := NewHeightfield(p)
	if err != nil {
		return nil, err
	}
	return &World{params: p, hf: hf, selector: sel, cache: map[ChunkCoord]*cacheEntry{}}, nil
}

// lodAt computes the LOD for a chunk under the given params.
func (w *World) lodAt(c ChunkCoord, p *Params) LOD {
	l := w.selector(c, p)
	if l > p.MaxLOD {
		return p.MaxLOD
	}
	return l
}

// specFor builds the generation spec for a chunk under params p.
func (w *World) specFor(c ChunkCoord, p *Params) chunkSpec {
	return chunkSpec{
		coord: c,
		lod:   w.lodAt(c, p),
		north: w.lodAt(ChunkCoord{X: c.X, Z: c.Z - 1}, p),
		south: w.lodAt(ChunkCoord{X: c.X, Z: c.Z + 1}, p),
		west:  w.lodAt(ChunkCoord{X: c.X - 1, Z: c.Z}, p),
		east:  w.lodAt(ChunkCoord{X: c.X + 1, Z: c.Z}, p),
	}
}

// Generate produces meshes for the given chunk coords. Each returned
// mesh is complete; failed chunks are reported in the returned error
// slice and never appear as partial meshes. Generation is parallel;
// results are sorted by chunk coord and do not depend on concurrency
// level, scheduling, or request order. ctx cancellation aborts
// remaining work; canceled chunks are reported, never half-written.
func (w *World) Generate(ctx context.Context, coords []ChunkCoord) ([]*Mesh, []error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// Deduplicate deterministically, preserving first-seen order.
	seen := make(map[ChunkCoord]bool, len(coords))
	uniq := coords[:0:0]
	for _, c := range coords {
		if !seen[c] {
			seen[c] = true
			uniq = append(uniq, c)
		}
	}

	w.mu.Lock()
	if b := w.params.Budgets.MaxChunksPerRequest; b > 0 && len(uniq) > b {
		w.mu.Unlock()
		return nil, []error{&Error{
			Kind: ErrKindBudgetExceeded, Op: "generate-request",
			Msg: "request exceeds MaxChunksPerRequest budget",
		}}
	}
	params := w.params // snapshot (Params is a value; Fractal holds interface refs treated as immutable)
	hf := w.hf
	version := w.version
	specs := make([]chunkSpec, len(uniq))
	meshes := make([]*Mesh, len(uniq))
	var todo []int
	for i, c := range uniq {
		specs[i] = w.specFor(c, &params)
		if e, ok := w.cache[c]; ok && e.version == version && e.lod == specs[i].lod {
			meshes[i] = e.mesh
		} else {
			todo = append(todo, i)
		}
	}
	w.mu.Unlock()

	var errs []error
	var errsMu sync.Mutex
	if len(todo) > 0 {
		workers := params.Budgets.MaxChunksInFlight
		if workers <= 0 {
			workers = runtime.NumCPU()
		}
		if workers > len(todo) {
			workers = len(todo)
		}
		jobs := make(chan int)
		var wg sync.WaitGroup
		for k := 0; k < workers; k++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range jobs {
					if ctx.Err() != nil {
						errsMu.Lock()
						errs = append(errs, &Error{Kind: ErrKindCanceled, Coord: uniq[i], Op: "generate", Msg: ctx.Err().Error()})
						errsMu.Unlock()
						continue
					}
					m, err := buildChunkMesh(hf, specs[i])
					if err != nil {
						errsMu.Lock()
						errs = append(errs, err)
						errsMu.Unlock()
						continue
					}
					meshes[i] = m
				}
			}()
		}
		for _, i := range todo {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
	}

	// Cache freshly built meshes if params did not change underneath us.
	w.mu.Lock()
	if w.version == version {
		for _, i := range todo {
			if meshes[i] != nil {
				w.cache[uniq[i]] = &cacheEntry{lod: specs[i].lod, version: version, mesh: meshes[i]}
			}
		}
		w.evictLocked()
	}
	w.mu.Unlock()

	// Assemble deterministic output: successes sorted by coord.
	out := make([]*Mesh, 0, len(uniq))
	for _, m := range meshes {
		if m != nil {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Coord.X != out[b].Coord.X {
			return out[a].Coord.X < out[b].Coord.X
		}
		return out[a].Coord.Z < out[b].Coord.Z
	})
	sort.Slice(errs, func(a, b int) bool {
		ea, eb := errs[a].(*Error), errs[b].(*Error)
		if ea.Coord.X != eb.Coord.X {
			return ea.Coord.X < eb.Coord.X
		}
		return ea.Coord.Z < eb.Coord.Z
	})
	return out, errs
}

// evictLocked enforces MaxCachedChunks with FIFO-ish eviction. The
// eviction choice never affects generated bytes, only cache hits.
func (w *World) evictLocked() {
	cap := w.params.Budgets.MaxCachedChunks
	if cap <= 0 {
		cap = 4096
	}
	for len(w.cache) > cap {
		for c := range w.cache {
			delete(w.cache, c)
			break
		}
	}
}

// UpdateParams applies update to a copy of the current params, validates
// the result, and commits it. Cached chunks intersecting affected are
// dropped (recomputed lazily on next Generate); every other cached
// chunk keeps its mesh bytes and is adopted into the new version.
//
// The caller guarantees affected covers every chunk whose geometry the
// update can change (use AllRegion for global changes such as seed,
// noise, or LOD policy). Applying the same update twice is a no-op the
// second time; the final world state depends only on the final params.
func (w *World) UpdateParams(affected Region, update func(*Params)) ([]ChunkCoord, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	next := w.params
	update(&next)
	if err := next.Validate(); err != nil {
		return nil, err
	}
	hf, err := NewHeightfield(next)
	if err != nil {
		return nil, err
	}
	w.params = next
	w.hf = hf
	w.version++
	var invalidated []ChunkCoord
	for c, e := range w.cache {
		if affected.containsChunk(c, &next) {
			delete(w.cache, c)
			invalidated = append(invalidated, c)
		} else {
			e.version = w.version
		}
	}
	sort.Slice(invalidated, func(a, b int) bool {
		if invalidated[a].X != invalidated[b].X {
			return invalidated[a].X < invalidated[b].X
		}
		return invalidated[a].Z < invalidated[b].Z
	})
	return invalidated, nil
}

// Cached returns the cached mesh for coord if it is valid under the
// current params and LOD policy, or nil.
func (w *World) Cached(coord ChunkCoord) *Mesh {
	w.mu.Lock()
	defer w.mu.Unlock()
	e, ok := w.cache[coord]
	if !ok || e.version != w.version || e.lod != w.lodAt(coord, &w.params) {
		return nil
	}
	return e.mesh
}
