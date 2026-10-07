package terrain

import (
	"container/list"
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/cuixudong74/terrain-lod-mesh-go/mesh"
)

// Limits configures budgets and bounded-resource behavior.
type Limits struct {
	// MaxVertices and MaxTriangles bound a single chunk mesh.
	// Requests exceeding them fail with ErrBudgetExceeded.
	// Zero means "no limit" for that dimension.
	MaxVertices  int
	MaxTriangles int
	// MaxCachedChunks bounds the chunk cache (LRU eviction). 0 = no cache.
	MaxCachedChunks int
	// Workers bounds parallel chunk generation. <= 0 means 1 (serial).
	Workers int
	// WorldMinX/WorldMinZ/WorldMaxX/WorldMaxZ bound requestable chunk
	// coordinates (inclusive). Requests outside fail with ErrOutOfBounds.
	WorldMinX, WorldMinZ, WorldMaxX, WorldMaxZ int32
	// DefaultSkirtDepth is used for requests with SkirtDepth == 0.
	// A negative value disables skirts by default.
	DefaultSkirtDepth float64
}

func (l *Limits) workers() int {
	if l.Workers <= 0 {
		return 1
	}
	return l.Workers
}

// ChunkRequest asks for one chunk mesh at one LOD.
type ChunkRequest struct {
	ID ChunkID
	// SkirtDepth overrides the default skirt depth for this request
	// (0 = use Limits.DefaultSkirtDepth).
	SkirtDepth float64
}

// ChunkResult carries a fully built chunk mesh. A result is only ever
// delivered complete; canceled or failed chunks never appear partially.
type ChunkResult struct {
	ID   ChunkID
	Mesh *mesh.Mesh
}

// cacheKey includes the skirt depth because meshes differ by skirt.
type cacheKey struct {
	id    ChunkID
	skirt float64
}

// cacheEntry is one cached chunk mesh.
type cacheEntry struct {
	key  cacheKey
	mesh *mesh.Mesh
}

// Manager generates and caches chunk meshes for one terrain config,
// supporting incremental config updates and bounded parallel generation.
//
// Concurrency contract: Generate may be called from multiple goroutines;
// the cache is internally synchronized. Results are deterministic
// regardless of worker count because every chunk mesh is a pure function
// of (config, grid, chunk id, skirt depth).
type Manager struct {
	mu     sync.Mutex
	cfg    *Config
	grid   Grid
	limits Limits

	cache map[cacheKey]*list.Element // -> cacheEntry, front = most recent
	lru   *list.List
}

// NewManager creates a Manager. It validates cfg, grid and limits.
func NewManager(cfg *Config, grid *Grid, limits *Limits) (*Manager, error) {
	if cfg == nil || grid == nil || limits == nil {
		return nil, &Error{Code: ErrInvalidConfig, Msg: "nil cfg, grid or limits"}
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if err := grid.Validate(); err != nil {
		return nil, err
	}
	if limits.WorldMinX > limits.WorldMaxX || limits.WorldMinZ > limits.WorldMaxZ {
		return nil, &Error{Code: ErrInvalidConfig, Msg: "world bounds are inverted"}
	}
	c := *cfg
	g := *grid
	l := *limits
	return &Manager{
		cfg:    &c,
		grid:   g,
		limits: l,
		cache:  map[cacheKey]*list.Element{},
		lru:    list.New(),
	}, nil
}

// Config returns a copy of the current config.
func (m *Manager) Config() *Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := *m.cfg
	return &c
}

// validateRequest checks LOD, world bounds and budgets for one request.
func (m *Manager) validateRequest(req *ChunkRequest) error {
	id := req.ID
	if int(id.LOD) > m.grid.MaxLOD {
		return &Error{Code: ErrOutOfBounds, Chunk: &id,
			Msg: fmt.Sprintf("LOD %d exceeds MaxLOD %d", id.LOD, m.grid.MaxLOD)}
	}
	l := &m.limits
	if id.X < l.WorldMinX || id.X > l.WorldMaxX || id.Z < l.WorldMinZ || id.Z > l.WorldMaxZ {
		return &Error{Code: ErrOutOfBounds, Chunk: &id,
			Msg: fmt.Sprintf("chunk outside world bounds [%d..%d]x[%d..%d]",
				l.WorldMinX, l.WorldMaxX, l.WorldMinZ, l.WorldMaxZ)}
	}
	q := m.grid.Quads(int(id.LOD))
	verts := (q + 1) * (q + 1)
	tris := 2 * q * q
	if m.skirtDepth(req) > 0 {
		verts += 4 * (q + 1)
		tris += 4 * q
	}
	if l.MaxVertices > 0 && verts > l.MaxVertices {
		return &Error{Code: ErrBudgetExceeded, Chunk: &id,
			Msg: fmt.Sprintf("vertex budget exceeded: %d > %d", verts, l.MaxVertices)}
	}
	if l.MaxTriangles > 0 && tris > l.MaxTriangles {
		return &Error{Code: ErrBudgetExceeded, Chunk: &id,
			Msg: fmt.Sprintf("triangle budget exceeded: %d > %d", tris, l.MaxTriangles)}
	}
	return nil
}

func (m *Manager) skirtDepth(req *ChunkRequest) float64 {
	if req.SkirtDepth != 0 {
		return req.SkirtDepth
	}
	return m.limits.DefaultSkirtDepth
}

// buildChunk builds one chunk mesh. Pure with respect to the manager's
// current config snapshot passed in.
func buildChunk(cfg *Config, grid *Grid, id ChunkID, skirtDepth float64) *mesh.Mesh {
	ox, oz := grid.Origin(id.X, id.Z)
	lod := int(id.LOD)
	spec := mesh.GridSpec{
		OriginX:    ox,
		OriginZ:    oz,
		Quads:      grid.Quads(lod),
		Step:       grid.Step(lod),
		SkirtDepth: skirtDepth,
	}
	return mesh.Build(spec, func(x, z float64) float64 {
		return HeightAt(cfg, x, z)
	})
}

// Generate builds the requested chunks, honoring ctx cancellation.
//
// Results are returned in request order (never completion order), so the
// output is independent of worker count and scheduling. Only complete
// chunks are returned: on cancellation or failure, the chunks finished so
// far are returned together with a typed error (ErrCanceled, or
// ErrChunkFailed identifying the chunk). A chunk is never delivered
// half-written.
func (m *Manager) Generate(ctx context.Context, reqs []ChunkRequest) ([]ChunkResult, error) {
	m.mu.Lock()
	cfg := m.cfg
	m.mu.Unlock()

	// Validate all requests up front, in request order (deterministic).
	for i := range reqs {
		if err := m.validateRequest(&reqs[i]); err != nil {
			return nil, err
		}
	}

	results := make([]ChunkResult, len(reqs))
	done := make([]bool, len(reqs))

	// Serve cache hits.
	var missing []int
	for i := range reqs {
		if ms, ok := m.cacheGet(reqs[i]); ok {
			results[i] = ChunkResult{ID: reqs[i].ID, Mesh: ms}
			done[i] = true
		} else {
			missing = append(missing, i)
		}
	}
	if len(missing) == 0 {
		return results, nil
	}

	type jobResult struct {
		idx  int
		ms   *mesh.Mesh
		fail error
	}
	jobs := make(chan int)
	out := make(chan jobResult)

	workers := m.limits.workers()
	if workers > len(missing) {
		workers = len(missing)
	}
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				req := reqs[idx]
				ms, err := safeBuild(cfg, &m.grid, req, m.skirtDepth(&req))
				select {
				case out <- jobResult{idx: idx, ms: ms, fail: err}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
	feed:
		for _, idx := range missing {
			select {
			case jobs <- idx:
			case <-ctx.Done():
				break feed
			}
		}
		close(jobs)
	}()
	go func() {
		wg.Wait()
		close(out)
	}()

	var firstErr error
	for r := range out {
		if r.fail != nil && firstErr == nil {
			firstErr = r.fail
		}
		if r.fail == nil {
			results[r.idx] = ChunkResult{ID: reqs[r.idx].ID, Mesh: r.ms}
			done[r.idx] = true
			m.cachePut(reqs[r.idx], r.ms)
		}
	}
	if ctx.Err() != nil && firstErr == nil {
		firstErr = &Error{Code: ErrCanceled, Msg: "generation canceled: " + ctx.Err().Error()}
	}

	// Compact to completed chunks only, preserving request order.
	completed := make([]ChunkResult, 0, len(reqs))
	for i := range reqs {
		if done[i] {
			completed = append(completed, results[i])
		}
	}
	return completed, firstErr
}

// safeBuild wraps buildChunk with panic recovery so a single bad chunk
// can never crash the whole generation run.
func safeBuild(cfg *Config, grid *Grid, req ChunkRequest, skirtDepth float64) (ms *mesh.Mesh, err error) {
	defer func() {
		if r := recover(); r != nil {
			id := req.ID
			ms = nil
			err = &Error{Code: ErrChunkFailed, Chunk: &id,
				Msg: fmt.Sprintf("panic during build: %v", r)}
		}
	}()
	return buildChunk(cfg, grid, req.ID, skirtDepth), nil
}

// cacheGet returns a cached mesh if the request matches the cache policy
// (skirt depth must match the request, since meshes differ by skirt).
func (m *Manager) cacheGet(req ChunkRequest) (*mesh.Mesh, bool) {
	if m.limits.MaxCachedChunks <= 0 {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	el, ok := m.cache[cacheKey{id: req.ID, skirt: m.skirtDepth(&req)}]
	if !ok {
		return nil, false
	}
	m.lru.MoveToFront(el)
	return el.Value.(*cacheEntry).mesh, true
}

func (m *Manager) cachePut(req ChunkRequest, ms *mesh.Mesh) {
	if m.limits.MaxCachedChunks <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := cacheKey{id: req.ID, skirt: m.skirtDepth(&req)}
	if el, ok := m.cache[key]; ok {
		el.Value.(*cacheEntry).mesh = ms
		m.lru.MoveToFront(el)
		return
	}
	el := m.lru.PushFront(&cacheEntry{key: key, mesh: ms})
	m.cache[key] = el
	for m.lru.Len() > m.limits.MaxCachedChunks {
		back := m.lru.Back()
		m.lru.Remove(back)
		delete(m.cache, back.Value.(*cacheEntry).key)
	}
}

// UpdateConfig applies a new config incrementally: only cached chunks
// whose evaluated heights can differ are evicted (and thus recomputed on
// next access); unaffected cached chunks keep their exact previous bytes.
//
// Because chunk meshes are pure functions of the full config, a chunk
// recomputed after the update is bit-identical to one produced by a full
// rebuild from scratch, and repeated or reordered submissions of the same
// final config converge to the same state.
//
// The returned affected list is sorted for determinism.
func (m *Manager) UpdateConfig(cfg *Config) (affected []ChunkID, err error) {
	if cfg == nil {
		return nil, &Error{Code: ErrInvalidConfig, Msg: "nil config"}
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	old := m.cfg
	globalChanged := hashWithoutModifiers(old) != hashWithoutModifiers(cfg)

	// Collect footprints of added/removed/changed modifiers.
	type circle struct{ x, z, r float64 }
	var footprints []circle
	if !globalChanged {
		oldMods := map[string]*Modifier{}
		for i := range old.Modifiers {
			oldMods[old.Modifiers[i].ID] = &old.Modifiers[i]
		}
		newMods := map[string]*Modifier{}
		for i := range cfg.Modifiers {
			newMods[cfg.Modifiers[i].ID] = &cfg.Modifiers[i]
		}
		for id, nm := range newMods {
			om, ok := oldMods[id]
			if !ok || *om != *nm {
				footprints = append(footprints, circle{nm.CenterX, nm.CenterZ, nm.Radius})
				if ok {
					footprints = append(footprints, circle{om.CenterX, om.CenterZ, om.Radius})
				}
			}
		}
		for id, om := range oldMods {
			if _, ok := newMods[id]; !ok {
				footprints = append(footprints, circle{om.CenterX, om.CenterZ, om.Radius})
			}
		}
	}

	c := *cfg
	m.cfg = &c

	// Evict affected cached chunks.
	for key, el := range m.cache {
		id := key.id
		hit := globalChanged
		if !hit {
			ox, oz := m.grid.Origin(id.X, id.Z)
			size := m.grid.ChunkSize
			for _, f := range footprints {
				// Closest point on the chunk AABB to the circle center.
				cx := clamp(f.x, ox, ox+size)
				cz := clamp(f.z, oz, oz+size)
				dx, dz := f.x-cx, f.z-cz
				if dx*dx+dz*dz <= f.r*f.r {
					hit = true
					break
				}
			}
		}
		if hit {
			affected = append(affected, id)
			m.lru.Remove(el)
			delete(m.cache, key)
		}
	}
	sort.Slice(affected, func(i, j int) bool {
		a, b := affected[i], affected[j]
		if a.X != b.X {
			return a.X < b.X
		}
		if a.Z != b.Z {
			return a.Z < b.Z
		}
		return a.LOD < b.LOD
	})
	return affected, nil
}

// hashWithoutModifiers hashes everything except the modifier list, to
// detect global (non-localized) parameter changes.
func hashWithoutModifiers(c *Config) uint64 {
	strip := *c
	strip.Modifiers = nil
	return strip.Hash()
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
