// Package brand centralizes the user-facing product identity. Display
// strings route through here; filesystem names, config paths, env vars,
// keyring services, binary names, and wire identifiers also carry the
// Semantix name since the full rebrand (docs/specs/semantix-full-rebrand.md,
// which superseded the h4-branding keep-list).
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
