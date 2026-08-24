package control

import (
	"testing"

	"semantix/harness/config"
)

func ptrOf(s string) *string { return &s }

// TestResolveSessionEffortValidatesAgainstCarryableDepths keeps the validation
// exhaustive without touching disk: SetEffort's only other job is loading the
// entry.
//
// The load-bearing case is "adaptive on MiniMax" — advertised by
// EffortCapabilityForEntry and therefore accepted by any check that stops
// there, yet dropped by the transport, which would make the setter report
// success for a level that never reaches a request.
func TestResolveSessionEffortValidatesAgainstCarryableDepths(t *testing.T) {
	var (
		noReasoning = &config.ProviderEntry{Kind: "openai", BaseURL: "https://example.com/v1", Name: "plain"}
		minimax     = &config.ProviderEntry{Kind: "openai", BaseURL: "https://api.minimaxi.com/v1", Name: "minimax"}
		ollama      = &config.ProviderEntry{Kind: "openai", BaseURL: "https://ollama.com/v1", Name: "ollama"}
	)
	for _, tc := range []struct {
		name    string
		entry   *config.ProviderEntry
		level   string
		want    *string
		wantErr bool
	}{
		{name: "empty clears the override", entry: ollama, level: "", want: nil},
		{name: "empty clears even without a resolved entry", entry: nil, level: "", want: nil},
		{name: "auto is an explicit level, not a clear", entry: ollama, level: "auto", want: ptrOf("")},
		{name: "auto is case-insensitive", entry: ollama, level: "AUTO", want: ptrOf("")},
		{name: "a carryable depth is stored", entry: ollama, level: "high", want: ptrOf("high")},
		{name: "surrounding space is trimmed", entry: ollama, level: "  max  ", want: ptrOf("max")},

		{name: "entry without reasoning is rejected", entry: noReasoning, level: "high", wantErr: true},
		{name: "auto on an entry without reasoning is rejected", entry: noReasoning, level: "auto", wantErr: true},
		{name: "unresolved entry is rejected", entry: nil, level: "high", wantErr: true},
		{name: "level outside the vocabulary is rejected", entry: ollama, level: "bogus", wantErr: true},
		{
			// Advertised by the capability, stripped by the transport.
			name: "advertised thinking toggle is rejected", entry: minimax, level: "adaptive", wantErr: true,
		},
		{name: "entry whose whole vocabulary is toggles is rejected", entry: minimax, level: "high", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveSessionEffort(tc.entry, tc.level)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolveSessionEffort = %v, want an error", derefEffort(got))
				}
				if got != nil {
					t.Errorf("rejected level still produced a value %q", *got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveSessionEffort: %v", err)
			}
			switch {
			case tc.want == nil && got != nil:
				t.Errorf("got %q, want a clear", *got)
			case tc.want != nil && got == nil:
				t.Errorf("got a clear, want %q", *tc.want)
			case tc.want != nil && *got != *tc.want:
				t.Errorf("got %q, want %q", *got, *tc.want)
			}
		})
	}
}

func derefEffort(p *string) string {
	if p == nil {
		return "<clear>"
	}
	return *p
}

// TestControllerSessionEffortRoundTripsThroughTheLocalCopy: with no executor
// attached the Controller still has to answer its own port honestly, and the
// three states must stay distinguishable at that surface too — "" is an
// untouched dial, "auto" is a deliberate one.
func TestControllerSessionEffortRoundTripsThroughTheLocalCopy(t *testing.T) {
	c := &Controller{}
	if got := c.SessionEffort(); got != "" {
		t.Fatalf("fresh controller = %q, want empty", got)
	}

	c.storeSessionEffort(ptrOf("high"))
	if got := c.SessionEffort(); got != "high" {
		t.Fatalf("after depth = %q, want high", got)
	}

	c.storeSessionEffort(ptrOf(""))
	if got := c.SessionEffort(); got != "auto" {
		t.Fatalf("after explicit auto = %q, want auto — the port spells it out so it stays distinct from unset", got)
	}

	c.storeSessionEffort(nil)
	if got := c.SessionEffort(); got != "" {
		t.Fatalf("after clear = %q, want empty", got)
	}
}

// TestSetEffortRejectsWithoutMutating: a rejected level must leave the dial
// exactly where it was, not half-applied.
func TestSetEffortRejectsWithoutMutating(t *testing.T) {
	c := &Controller{}
	c.storeSessionEffort(ptrOf("high"))

	if err := c.SetEffort("definitely-not-a-level"); err == nil {
		t.Fatal("want an error for an unresolvable level")
	}
	if got := c.SessionEffort(); got != "high" {
		t.Errorf("rejected SetEffort changed the level to %q, want high untouched", got)
	}
}

// TestSetEffortOnNilController stays a no-op, matching SetAgentPreset.
func TestSetEffortOnNilController(t *testing.T) {
	var c *Controller
	if err := c.SetEffort("high"); err == nil {
		t.Fatal("a nil controller must report the failure rather than pretend")
	}
	if got := c.SessionEffort(); got != "" {
		t.Errorf("nil controller = %q, want empty", got)
	}
}
