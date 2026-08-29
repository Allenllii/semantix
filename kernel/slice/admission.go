package slice

// Scope admission matrix (Issue #268, spec docs/specs/issue-268-t-r-scope-rule.md).
//
// Principle: slice type is the proxy for abstraction level. Cross-project
// (User-scope) reuse is only safe for high-level abstractions; low-level raw
// trajectories transfer badly across projects — a tool sequence or an answer
// over-specialized to one codebase actively harms a new task (negative
// transfer), unlike a prompt pattern or a project-shape summary.
//
//	 high  P (prompt) / M (memory)  -> User scope allowed
//	 high  C (context)              -> User scope allowed
//	 low   T (tool pattern)         -> FORBIDDEN: downgraded to Project
//	 low   R (result)               -> FORBIDDEN: downgraded to Project
//
// Enforcement point: FileStore.Put — the single chokepoint every writer
// (extractor, promote, import, future Agent KB paths) goes through. The
// downgrade is silent and total: a T/R slice can never persist in User
// scope, so retrieval-side per-type thresholds cannot be defeated by an
// upstream writer bug.

// EnforceAdmission applies the type×scope admission matrix to s in place and
// reports whether the scope was adjusted. Only downward (User -> Project)
// adjustments happen; Session/Project scopes are never touched.
func EnforceAdmission(s *Slice) bool {
	if s == nil || s.Scope != User {
		return false
	}
	switch s.Type {
	case ToolPattern, Result:
		s.Scope = Project
		return true
	default:
		return false
	}
}
