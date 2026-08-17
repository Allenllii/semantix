package cli

// resourceGauge is the resource-dashboard side-pane mount point (U39, spec
// C5 §7 "资源仪表本期只留挂点"): a component that renders live resource usage
// (model / cache / concurrency / budget) once the C1-C3 data plane is wired
// (U40/U41). This milestone ships the interface and a no-op implementation —
// the TUI renders nothing and never depends on gauge internals.
type resourceGauge interface {
	// Render returns the gauge's view lines for the given viewport width.
	// An empty result hides the gauge entirely (zero noise).
	Render(width int) []string
}

// nilGauge is the U39 placeholder implementation: it renders nothing. The
// TUI always holds a non-nil gauge so render paths need no nil checks; the
// real gauge (ResourceCatalog-driven side pane) replaces it in U40/U41.
type nilGauge struct{}

func (nilGauge) Render(int) []string { return nil }
