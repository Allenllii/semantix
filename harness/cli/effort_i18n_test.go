package cli

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"semantix/harness/config"
	"semantix/harness/control"
	"semantix/harness/i18n"
)

// TestEffortCommandMistypedLevelLocalized reproduces #335: a mistyped /effort
// level is rejected by config.NormalizeEffort, whose error was surfaced through
// m.notice(err.Error()) without going through the i18n catalogue, leaking the
// English "usage: /effort ..." string into a zh session.
func TestEffortCommandMistypedLevelLocalized(t *testing.T) {
	isolateUserConfig(t)

	// i18n.M is process-global, so this test must not run in parallel.
	prev := i18n.M
	i18n.M = i18n.Chinese
	t.Cleanup(func() { i18n.M = prev })

	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{Label: "deepseek-flash"})
	m.modelRef = "deepseek-flash/deepseek-v4-flash"

	if cmd := m.runEffortCommand("/effort bogus-level"); cmd != nil {
		t.Fatal("mistyped level must not queue a rebuild")
	}

	joined := ansi.Strip(strings.Join(m.transcript, "\n"))
	zhMarker := fmt.Sprintf(i18n.Chinese.EffortUsageFmt, "")
	zhMarker = strings.TrimSpace(strings.TrimSuffix(zhMarker, "%s"))
	if !strings.Contains(joined, zhMarker) {
		t.Fatalf("mistyped level should render the localized usage hint containing %q, got transcript:\n%s", zhMarker, joined)
	}
	if strings.Contains(joined, "usage: /effort") {
		t.Fatalf("mistyped level leaked English, got transcript:\n%s", joined)
	}
}

// TestEffortCommandUsageErrorCarriesLevels guards the typed error so a future
// regression cannot silently drop the level list that the localized usage hint
// depends on.
func TestEffortCommandUsageErrorCarriesLevels(t *testing.T) {
	e := &config.ProviderEntry{Kind: "openai", BaseURL: "https://api.minimaxi.com/v1", Model: "MiniMax-M3"}
	_, err := config.NormalizeEffort(e, "turbo")
	if err == nil {
		t.Fatal("NormalizeEffort should reject an unrecognised level")
	}
	var usageErr *config.UnsupportedEffortError
	if !errors.As(err, &usageErr) {
		t.Fatalf("NormalizeEffort error should be *config.UnsupportedEffortError, got %T: %v", err, err)
	}
	if got := strings.Join(usageErr.Levels, "|"); got != "auto|adaptive|disabled" {
		t.Fatalf("UnsupportedEffortError.Levels = %q, want auto|adaptive|disabled", got)
	}
}
