// Session-scoped reasoning depth: an in-place runtime dial, the counterpart to
// SetAgentPreset. The construction-time level (boot.Options.EffortOverride)
// stays the session's starting point; this layers an override on top of it and
// rebuilds nothing.
package control

import (
	"fmt"
	"slices"
	"strings"

	"semantix/harness/config"
	"semantix/harness/i18n"
)

// SetEffort sets the session-scoped reasoning depth used by subsequent model
// rounds, without rebuilding the controller, provider, or tool schemas.
//
// Three inputs, three outcomes — the middle one is why the port takes a string
// rather than a bool-plus-value:
//
//	""     clears the override; whatever drove the slot before drives it again
//	"auto" is an explicit choice of the provider's configured depth
//	a depth is that depth
//
// A rejected level changes nothing. Errors are translated except the one
// forwarded from config.NormalizeEffort, which owns the vocabulary and reports
// in the raw English its layer uses throughout.
func (c *Controller) SetEffort(level string) error {
	if c == nil {
		return fmt.Errorf("controller is nil")
	}
	resolved, err := resolveSessionEffort(c.effortEntry(), level)
	if err != nil {
		return err
	}
	c.storeSessionEffort(resolved)
	if setter, ok := c.runner.(interface{ SetSessionEffort(*string) }); ok {
		setter.SetSessionEffort(resolved)
	}
	if c.executor != nil {
		c.executor.SetSessionEffort(resolved)
	}
	return nil
}

// SessionEffort reports the session-scoped depth: "" when the dial has never
// been touched, "auto" when it was deliberately set back to the provider
// default, or the depth. Those first two are different states — one lets the
// reasoning governor drive the slot, the other does not — so the port spells
// auto out rather than collapsing both to "".
func (c *Controller) SessionEffort() string {
	if c == nil {
		return ""
	}
	if c.executor != nil {
		return spellSessionEffort(c.executor.SessionEffort())
	}
	if getter, ok := c.runner.(interface {
		SessionEffort() (string, bool)
	}); ok {
		return spellSessionEffort(getter.SessionEffort())
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sessionEffort == nil {
		return ""
	}
	return spellSessionEffort(*c.sessionEffort, true)
}

func spellSessionEffort(level string, set bool) string {
	switch {
	case !set:
		return ""
	case level == "":
		return "auto"
	default:
		return level
	}
}

func (c *Controller) storeSessionEffort(level *string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if level == nil {
		c.sessionEffort = nil
		return
	}
	stored := *level
	c.sessionEffort = &stored
}

// effortEntry resolves the provider entry the current model points at.
//
// A disk read per call, deliberately: the alternative is caching the entry at
// build time and refreshing it on every /model switch, which buys a rare slash
// command some microseconds at the price of two more places for the cached
// copy to go stale. The same read already happens on a far hotter path —
// currentEffortEntry runs from the /effort completion dropdown, once per
// keystroke.
func (c *Controller) effortEntry() *config.ProviderEntry {
	cfg, err := config.LoadForRoot(c.workspaceRoot)
	if err != nil {
		return nil
	}
	ref := strings.TrimSpace(c.modelRef)
	if ref == "" {
		ref = cfg.DefaultModel
	}
	if ref == "" {
		return nil
	}
	entry, _ := cfg.ResolveModel(ref)
	return entry
}

// resolveSessionEffort validates a requested level against the resolved entry
// and returns the value to store: nil clears, a pointer to "" is an explicit
// auto, otherwise the normalized depth.
//
// It checks the depth vocabulary, not just the capability's Levels. The two
// disagree on purpose: EffortCapabilityForEntry advertises thinking on/off
// tokens (adaptive, enabled, none) that are meaningful for a config write but
// cannot ride a per-request override, so validating against Levels alone would
// accept a level the transport then silently discards — a setter reporting
// success for something that never reaches a request.
func resolveSessionEffort(entry *config.ProviderEntry, level string) (*string, error) {
	level = strings.TrimSpace(level)
	if level == "" {
		return nil, nil
	}
	if !config.EffortCapabilityForEntry(entry).Supported {
		return nil, fmt.Errorf(i18n.M.SessionEffortUnsupportedFmt, effortEntryName(entry))
	}
	if strings.EqualFold(level, "auto") {
		auto := ""
		return &auto, nil
	}
	normalized, err := config.NormalizeEffort(entry, level)
	if err != nil {
		return nil, err
	}
	if normalized == "" {
		auto := ""
		return &auto, nil
	}
	if !slices.Contains(config.DepthEffortLevels(entry), normalized) {
		return nil, fmt.Errorf(i18n.M.SessionEffortNotADepthFmt, normalized, effortEntryName(entry))
	}
	return &normalized, nil
}

func effortEntryName(e *config.ProviderEntry) string {
	if e != nil && strings.TrimSpace(e.Name) != "" {
		return e.Name
	}
	return "this model"
}
