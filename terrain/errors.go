package terrain

import "fmt"

// ErrorCode classifies failures so callers can distinguish causes.
type ErrorCode int

const (
	ErrInvalidConfig ErrorCode = iota
	ErrOutOfBounds
	ErrBudgetExceeded
	ErrCanceled
	ErrChunkFailed
)

// Error is a typed failure, optionally bound to a specific chunk request.
type Error struct {
	Code  ErrorCode
	Chunk *ChunkID // nil if not chunk-specific
	Msg   string
}

func (e *Error) Error() string {
	if e.Chunk != nil {
		return fmt.Sprintf("terrain: chunk (%d,%d,lod%d): %s", e.Chunk.X, e.Chunk.Z, e.Chunk.LOD, e.Msg)
	}
	return "terrain: " + e.Msg
}
