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
	// LastUsed is the unix-seconds time this slice last served a hit or an
	// injection. 0 = never used (or legacy line without the field). Unlike
	// the counters it merges by max, not by accumulation — see mergeStats.
	LastUsed int64 `json:"last_used,omitempty"`
}

// mergeStats folds delta into cur. Counters accumulate; LastUsed max-merges.
// Live UpdateStats and journal replay share this single rule — if they ever
// disagreed, replaying a journal would compute different stats than the
// process that wrote it.
func mergeStats(cur *SliceStats, delta SliceStats) {
	cur.Hits += delta.Hits
	cur.Misses += delta.Misses
	cur.Injected += delta.Injected
	cur.Rejected += delta.Rejected
	cur.UserFeedback += delta.UserFeedback
	if delta.LastUsed > cur.LastUsed {
		cur.LastUsed = delta.LastUsed
	}
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
	// ContextHash records the messages-context fingerprint of the request
	// that produced this slice (Issue #133 gateway). The L3 gate compares
	// it against the live request so the same query under a different
	// conversation history never reuses another session's outcome. Empty
	// means unknown/legacy — the L3 gate skips the check then.
	ContextHash string `json:"context_hash,omitempty"`
	// Model records the client-visible model alias that produced this
	// slice (Issue #133 gateway). Cross-model reuse is never allowed, so
	// the L3 gate requires a match when non-empty.
	Model string `json:"model,omitempty"`
}

// Slice is the core semantic slice value.
type Slice struct {
	ID        string      // stable UUID; content hash (sha256) is the version field
	Type      SliceType
	Scope     Scope
	Content   []byte      // normalized text (Prompt/Context/Result/Memory) or tool sequence (ToolPattern)
	// Embedding is persisted only for model-produced vectors (hash vectors
	// are recomputable from Content and are not stored). Store reads return
	// nil by contract — nothing in the repo consumes persisted vectors; the
	// raw bytes still round-trip through Export and compaction.
	Embedding []float32 `json:",omitempty"`
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
	// Lexical is the lexical-support score in [0,1] contributed by the
	// lexical (BM25) route of a fused retrieval: 0 means the candidate was
	// a pure-vector hit with no term overlap (Issue #260 lexical support
	// gate). In single-route modes it carries the query-token coverage
	// (vector mode) or 1 (bm25 mode). LexicalValid=false (zero value)
	// means the index did not evaluate lexical support — consumers must
	// treat that as "not measured", not "unsupported", so legacy and
	// third-party Index implementations are never blocked by default.
	Lexical      float64 `json:"lexical,omitempty"`
	LexicalValid bool    `json:"-"`
}
