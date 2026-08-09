package slice

// SliceType classifies a semantic slice (see architecture spec §3.1).
type SliceType int

const (
	// Prompt slice: reusable task templates and instruction blocks.
	Prompt SliceType = iota
	// Context slice: project structure summaries, frequent files, preferences.
	Context
	// ToolPattern slice: tool-call sequences (behavior patterns).
	ToolPattern
	// Result slice: high-frequency tool results / answers.
	Result
	// Memory slice: semantic units linked to the memory system.
	Memory
)

// Scope bounds where a slice is visible.
type Scope int

const (
	// Session scope: one session only.
	Session Scope = iota
	// Project scope: current workspace.
	Project
	// User scope: across projects for this user.
	User
)

// String returns the stable wire name of a SliceType.
func (t SliceType) String() string {
	switch t {
	case Prompt:
		return "prompt"
	case Context:
		return "context"
	case ToolPattern:
		return "tool_pattern"
	case Result:
		return "result"
	case Memory:
		return "memory"
	}
	return "unknown"
}

// String returns the stable wire name of a Scope.
func (s Scope) String() string {
	switch s {
	case Session:
		return "session"
	case Project:
		return "project"
	case User:
		return "user"
	}
	return "unknown"
}

// SliceStats tracks usage feedback used by the evolution engine.
type SliceStats struct {
	Hits        uint64
	Misses      uint64
	Injected    uint64
	Rejected    uint64
	UserFeedback float64 // +1 keep / -1 reject / 0 none
}

// SliceMeta records provenance.
type SliceMeta struct {
	SourceSession string
	TaskType      string
	Language      string
	ProjectSlug   string
}

// Slice is the core semantic slice value.
type Slice struct {
	ID        string      // stable UUID; content hash (sha256) is the version field
	Type      SliceType
	Scope     Scope
	Content   []byte      // normalized text (Prompt/Context/Result/Memory) or tool sequence (ToolPattern)
	Embedding []float32   // nil until an embedder is available (MVP: BM25 only)
	Stats     SliceStats
	Weight    float64     // value weight, updated by the evolution engine
	Meta      SliceMeta
}

// Hit is one search result.
type Hit struct {
	Slice *Slice
	Score float64
}
