package pluginpkg

import (
	"os"
	"path/filepath"
	"testing"
)

// Plugins built for the upstream Reasonix harness must keep installing after
// the rebrand (docs/specs/semantix-full-rebrand.md §2): the legacy manifest
// file name and the legacy apiVersion spelling are both read as fallbacks.
func TestParseDirAcceptsLegacyReasonixManifest(t *testing.T) {
	root := t.TempDir()
	manifest := `{"apiVersion":"reasonix.io/plugin/v2","name":"legacy-plugin","version":"1.0.0","description":"built for upstream"}`
	if err := os.WriteFile(filepath.Join(root, LegacyNativeManifest), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	pkg, _, err := ParseDir(root)
	if err != nil {
		t.Fatalf("legacy manifest must parse: %v", err)
	}
	if pkg.ManifestKind != "semantix" || pkg.Manifest.Name != "legacy-plugin" {
		t.Fatalf("pkg = kind %q name %q, want native kind with legacy name", pkg.ManifestKind, pkg.Manifest.Name)
	}
}

// The canonical name wins when both manifests exist side by side.
func TestParseDirPrefersCanonicalManifestOverLegacy(t *testing.T) {
	root := t.TempDir()
	canonical := `{"apiVersion":"semantix.io/plugin/v2","name":"canonical","version":"1.0.0","description":"new"}`
	legacy := `{"apiVersion":"reasonix.io/plugin/v2","name":"old","version":"1.0.0","description":"old"}`
	if err := os.WriteFile(filepath.Join(root, NativeManifest), []byte(canonical), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, LegacyNativeManifest), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	pkg, _, err := ParseDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Manifest.Name != "canonical" {
		t.Fatalf("Manifest.Name = %q, canonical file must win over legacy", pkg.Manifest.Name)
	}
}
