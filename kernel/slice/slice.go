package slice

import "semantix/kernel/fingerprint"

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
	// Deps captures the dependency fingerprint at slice time (path -> sha256,
	// Issue #8): reuse is gated on these files not having changed.
	Deps fingerprint.Deps `json:"deps,omitempty"`
	// Mtimes captures file modification times at slice time (path -> unix
	// seconds, U16): cheap fast-fail check before the sha256 re-read.
	Mtimes map[string]int64 `json:"mtimes,omitempty"`
	// L3Safe marks a dependency-free Result slice as explicitly reusable at
	// the L3 gate (opt-in via extract --l3-safe; U16 MEDIUM fix). Slices
	// with captured Deps are inherently safe — this flag is only consulted
	// when Deps is empty, so a shared/injected library cannot silently
	// mark results reusable.
	L3Safe bool `json:"l3_safe,omitempty"`
	// EmbedModel / EmbedDim record the provenance of Slice.Embedding
	// (Issue #63): which embedder produced the vector and its dimension, so
	// future retrieval can detect mixed-dimension libraries instead of
	// silently skipping dimension-mismatched vectors.
	EmbedModel string `json:"embed_model,omitempty"`
	EmbedDim   int    `json:"embed_dim,omitempty"`
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
	// CreatedAt is the unix-seconds creation time (maintenance gc retention
	// basis). Zero means unknown (legacy/imported lines without the field):
	// retention never expires unknown-age slices.
	CreatedAt int64 `json:"created_at,omitempty"`
}

// Hit is one search result.
type Hit struct {
	Slice *Slice
	Score float64
}
