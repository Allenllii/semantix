package cli

import "testing"

// TestSemantixThemeStyle: the Semantix Design style (U39, blueprint §4)
// ships a dark base with the semantic green #2F967F accent and success
// color, and leaves every other style's palette untouched.
func TestSemantixThemeStyle(t *testing.T) {
	st, ok := cliThemeStyleByName("semantix")
	if !ok {
		t.Fatal("semantix theme style missing")
	}
	if st.mode != "dark" {
		t.Errorf("semantix mode = %q, want dark", st.mode)
	}
	if st.accent.hex != "#2F967F" {
		t.Errorf("semantix accent = %q, want #2F967F", st.accent.hex)
	}
	if st.success == nil || st.success.hex != "#2F967F" {
		t.Errorf("semantix success = %+v, want #2F967F override", st.success)
	}
}

// TestApplySemantixThemeStyle: applying the style overrides accent, selection
// and success; the reuse panel renders through activeCLITheme.success so the
// semantic green reaches the per-turn panel.
func TestApplySemantixThemeStyle(t *testing.T) {
	st, _ := cliThemeStyleByName("semantix")
	p := applyCLIThemeStyle(cliDarkTheme, st)
	if p.accent.hex != "#2F967F" || p.selection.hex != "#2F967F" || p.success.hex != "#2F967F" {
		t.Errorf("applied palette accent/selection/success = %s/%s/%s, want #2F967F",
			p.accent.hex, p.selection.hex, p.success.hex)
	}
}

// TestApplyThemeStyleKeepsSuccessDefault: styles without an explicit success
// override keep the palette default (no regression for the existing 8).
func TestApplyThemeStyleKeepsSuccessDefault(t *testing.T) {
	st, _ := cliThemeStyleByName("graphite")
	p := applyCLIThemeStyle(cliDarkTheme, st)
	if st.success != nil {
		t.Fatal("graphite must not declare a success override")
	}
	if p.success.hex != cliDarkTheme.success.hex {
		t.Errorf("graphite success = %s, want palette default %s", p.success.hex, cliDarkTheme.success.hex)
	}
}
