// Package brand centralizes the user-facing product identity of this
// Semantix-branded fork. Only display strings route through here:
// filesystem names, config paths, env vars, keyring services, binary
// names, and wire identifiers deliberately stay "reasonix" so existing
// installs, patches, and upstream merges keep working (see the semantix
// repo, docs/specs/h4-branding.md §3.1).
package brand

const (
	// Name is the product name shown to users (window title, transcript
	// header, CLI banners, generated document metadata).
	Name = "Semantix"

	// Vendor is the publisher shown in application metadata.
	Vendor = "Semantix"

	// Copyright is the user-visible copyright line.
	Copyright = "Copyright © 2026 Semantix Contributors"

	// Tagline is the one-line product description used on splash and
	// about surfaces.
	Tagline = "A self-evolving agent that learns how you work."
)
