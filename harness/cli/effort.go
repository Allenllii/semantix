package cli

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"semantix/harness/config"
	"semantix/harness/control"
	"semantix/harness/i18n"
)

// runEffortCommand persists the level to the user config and then applies it
// to the live controller in place — the same shape /reasoning-language and
// /preset use. No Snapshot, no session-lease rebind, no controller rebuild:
// the setter (control.Settings.SetEffort, #330) updates the session-scoped
// depth for subsequent model rounds and the per-request EffortOverride
// channel carries it to every adapter family (#331).
func (m *chatTUI) runEffortCommand(input string) tea.Cmd {
	entry, _, entryErr := m.currentConfigProvider()
	var cap config.EffortCapability
	if entryErr == nil {
		cap = config.EffortCapabilityForEntry(entry)
	} else if m.ctrl != nil {
		// Synthetic extension/plugin refs never resolve against the user
		// config; the session's own capability still answers the read/list
		// path (Issue #333). Only persisting a level needs a config entry.
		cap = m.ctrl.EffortCapability()
	}
	if !cap.Supported {
		if entryErr != nil {
			m.notice(fmt.Sprintf(i18n.M.EffortErrorFmt, entryErr.Error()))
		} else {
			m.notice(fmt.Sprintf(i18n.M.EffortNotConfigurableFmt, entry.Name))
		}
		return nil
	}

	args := tokenizeArgs(input)
	if len(args) < 2 {
		provider, current := "", ""
		if entryErr == nil {
			provider = entry.Name
			current = config.EffortDisplay(entry)
		} else if m.ctrl != nil {
			provider = m.ctrl.ModelRef()
			current = m.ctrl.SessionEffort()
			if current == "" {
				current = "auto"
			}
		}
		m.commitLine(renderEfforts(m.width, provider, current, cap))
		return nil
	}
	if entryErr != nil {
		// Setting a level persists it in the user config, which a synthetic
		// entry cannot round-trip; surface the resolution error instead of a
		// confusing downstream failure.
		m.notice(fmt.Sprintf(i18n.M.EffortErrorFmt, entryErr.Error()))
		return nil
	}
	if len(args) > 2 {
		m.notice(fmt.Sprintf(i18n.M.EffortUsageFmt, strings.Join(cap.Levels, "|")))
		return nil
	}
	effort, err := config.NormalizeEffort(entry, args[1])
	if err != nil {
		m.notice(localizeEffortError(err))
		return nil
	}
	if m.ctrl == nil {
		m.notice(i18n.M.EffortSwitchUnavailable)
		return nil
	}
	if m.modelSwitchPending {
		m.notice(i18n.M.EffortSwitchPending)
		return nil
	}
	if m.runtimeSwitchBusy() {
		m.notice(i18n.M.EffortSwitchBusy)
		return nil
	}

	path := config.UserConfigPath()
	if path == "" {
		m.notice(i18n.M.EffortNoConfigDir)
		return nil
	}
	// Lock only the load-modify-save cycle; the in-place session switch below
	// runs off-lock. The persisted level is what survives a later /model or
	// /reload rebuild — do not drop this write.
	if err := func() error {
		unlock := config.LockUserConfigEdits()
		defer unlock()
		edit := config.LoadForEdit(path)
		if _, ok := edit.Provider(entry.Name); !ok {
			if err := edit.UpsertProvider(*entry); err != nil {
				return err
			}
		}
		if entry.Kind == "anthropic" && effort != "" && entry.Thinking == "" {
			if err := edit.SetProviderThinking(entry.Name, "adaptive"); err != nil {
				return err
			}
		}
		if err := edit.SetProviderEffort(entry.Name, effort); err != nil {
			return err
		}
		return edit.SaveTo(path)
	}(); err != nil {
		m.notice(fmt.Sprintf(i18n.M.EffortErrorFmt, err.Error()))
		return nil
	}

	// In-place switch: the level reaches the live controller immediately.
	sessionLevel := effort
	if sessionLevel == "" {
		// An explicit auto is a choice of the provider's configured depth and
		// stands the reasoning governor down (control.SetEffort contract).
		sessionLevel = "auto"
	}
	if err := m.ctrl.SetEffort(sessionLevel); err != nil {
		m.notice(fmt.Sprintf(i18n.M.EffortErrorFmt, err.Error()))
		return nil
	}
	display := effort
	if display == "" {
		display = "auto"
	}
	if m.effortApplied != nil {
		m.effortApplied(display)
	}
	m.effortLevel = display
	m.notice(fmt.Sprintf(i18n.M.EffortSwitchedFmt, entry.Name, display))
	return nil
}

// renderEfforts renders bare /effort as a described, current-marked list,
// modelled on renderAgentPresets and reading control.EffortLevelHint so
// completion and this list cannot drift (Issue #333).
func renderEfforts(width int, provider, current string, cap config.EffortCapability) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", viewHeader(i18n.M.EffortListHeaderFmt, provider))
	for _, level := range cap.Levels {
		status := ""
		if level == current {
			status = "  " + viewStatus(i18n.M.ArgModelCurrent)
		}
		nameWidth := viewPadWidth(level, 10)
		desc := viewCompactText(control.EffortLevelHint(level), viewBudget(width, 2+nameWidth+1+visibleWidth(status)))
		fmt.Fprintf(&b, "  %-*s %s%s\n", nameWidth, level, viewMeta(desc), status)
	}
	b.WriteString(viewHint(i18n.M.EffortListHint))
	return strings.TrimRight(b.String(), "\n")
}

// localizeEffortError renders a NormalizeEffort error through the active i18n
// catalogue. The config layer carries the legal level vocabulary as typed data
// (UnsupportedEffortError / EffortNotConfigurableError) instead of a
// pre-rendered English string, so the CLI can honor the session locale (#335).
func localizeEffortError(err error) string {
	var usageErr *config.UnsupportedEffortError
	if errors.As(err, &usageErr) {
		return fmt.Sprintf(i18n.M.EffortUsageFmt, strings.Join(usageErr.Levels, "|"))
	}
	var notConfigurableErr *config.EffortNotConfigurableError
	if errors.As(err, &notConfigurableErr) {
		return fmt.Sprintf(i18n.M.EffortNotConfigurableFmt, notConfigurableErr.Provider)
	}
	return fmt.Sprintf(i18n.M.EffortErrorFmt, err.Error())
}

func (m *chatTUI) currentConfigProvider() (*config.ProviderEntry, string, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, "", err
	}
	// When the per-tab ref is empty we are inheriting the configured
	// default — let resolveModelForCLI fall through a keyless default to
	// the next configured provider (issue #6996). When m.modelRef is
	// already set we honor it verbatim: the user picked that model
	// explicitly (via /model, on the model switcher, or in the bootstrap
	// step) and we must not silently swap to a different provider just
	// because the entry happens to be keyless.
	ref := m.modelRef
	if strings.TrimSpace(ref) == "" {
		var rerr error
		ref, _, rerr = resolveModelForCLI("", cfg)
		if rerr != nil {
			return nil, "", rerr
		}
	}
	entry, ok := cfg.ResolveModel(ref)
	if !ok {
		return nil, "", fmt.Errorf("unknown model %q", ref)
	}
	if ref == entry.Name || !strings.Contains(ref, "/") {
		ref = entry.Name + "/" + entry.Model
	}
	return entry, ref, nil
}

func (m *chatTUI) refreshEffortStatus() {
	m.effortLevel = ""
	entry, _, err := m.currentConfigProvider()
	if err != nil {
		return
	}
	if !config.EffortCapabilityForEntry(entry).Supported {
		return
	}
	m.effortLevel = config.EffortDisplay(entry)
}
