package cli

import "github.com/charmbracelet/colorprofile"

// restoreThemeForTest restores the process-global theme state captured by a
// test before it mutated the active CLI theme (backfilled from the vendor
// baseline test suite; see harness/ATTRIBUTION.md).
func restoreThemeForTest(prevColor colorprofile.Profile, prevTheme cliPalette) {
	activeColorProfile = prevColor
	activeCLITheme = prevTheme
	refreshCLIStyles()
}
