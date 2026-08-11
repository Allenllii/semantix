// Package fingerprint implements the dependency-fingerprint gate (Issue #8,
// stage ① of the two-level verification chain): a slice's answer is only
// reusable if the files it depends on have not changed since the slice was
// captured. This proves "files didn't change" — the cheap deterministic part
// that runs before any LLM judge cost.
package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Deps maps a dependency path (as captured) to its sha256 hex digest.
type Deps map[string]string

// Capture computes sha256 digests of the given paths (relative to root).
// Missing/unreadable paths yield an error so a partial capture is never
// silently trusted. Paths must be relative (no abs, no "..") to keep Deps
// portable between machines.
func Capture(root string, paths []string) (Deps, error) {
	out := make(Deps, len(paths))
	for _, p := range paths {
		if !filepath.IsLocal(p) {
			return nil, fmt.Errorf("fingerprint: non-local path %q", p)
		}
		full := filepath.Join(root, p)
		f, err := os.Open(full)
		if err != nil {
			return nil, fmt.Errorf("fingerprint: open %s: %w", p, err)
		}
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			f.Close()
			return nil, fmt.Errorf("fingerprint: read %s: %w", p, err)
		}
		f.Close()
		out[p] = hex.EncodeToString(h.Sum(nil))
	}
	return out, nil
}

// Verify recomputes the digests of the captured paths (relative to root) and
// returns the list of paths whose content changed (or that disappeared).
// A nil/empty Deps verifies clean.
func Verify(root string, deps Deps) ([]string, error) {
	var changed []string
	for p, want := range deps {
		if !filepath.IsLocal(p) {
			changed = append(changed, p)
			continue
		}
		got, err := Capture(root, []string{p})
		if err != nil {
			changed = append(changed, p) // missing/unreadable = changed
			continue
		}
		if got[p] != want {
			changed = append(changed, p)
		}
	}
	return changed, nil
}
