package terrain

import "fmt"

// ErrorKind classifies failures so callers can distinguish causes.
type ErrorKind int

const (
	ErrKindInvalidParam   ErrorKind = iota // bad or extreme parameter
	ErrKindOutOfBounds                     // coordinate outside the representable world
	ErrKindDegenerateLOD                   // LOD request that cannot be satisfied
	ErrKindBudgetExceeded                  // vertex/triangle/request budget exceeded
	ErrKindCanceled                        // context canceled or timed out
)

// Error is a failure tied to a specific chunk and request.
type Error struct {
	Kind  ErrorKind
	Coord ChunkCoord // chunk involved; zero value if not chunk-specific
	Op    string     // operation that failed, e.g. "generate", "validate"
	Msg   string
}

func (e *Error) Error() string {
	return fmt.Sprintf("terrain: %s: chunk (%d,%d): %s", e.Op, e.Coord.X, e.Coord.Z, e.Msg)
}

// KindOf returns the classification of err, or -1 if err is nil.
func KindOf(err error) (ErrorKind, bool) {
	if err == nil {
		return 0, false
	}
	if te, ok := err.(*Error); ok {
		return te.Kind, true
	}
	return 0, false
}
