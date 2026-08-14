package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"semantix/kernel/fingerprint"
	"semantix/kernel/slice"
)

// This file implements the gateway L3 write-back (design §3.5): a fresh
// upstream response becomes a reusable Result slice only when the operator
// opted in (cache.l3_safe) AND a deps fingerprint of the configured project
// root was captured — dep-less entries never enter L3 (fail-closed: the
// fingerprint/RuleGate chain would be a no-op). Tool-call responses are
// never cached (side effects).

// maybeWriteBack stores a verified-safe response as an L3 candidate.
func (s *Server) maybeWriteBack(meta forwardMeta, content string, toolCalls bool) {
	if s.cfg.Disable || !s.cfg.Cache.L3Safe || s.cfg.Cache.DepsRoot == "" {
		return
	}
	if content == "" || toolCalls {
		// Design §3.5: only safely-reusable plain answers enter L3.
		return
	}
	deps, mtimes, err := captureTree(s.cfg.Cache.DepsRoot)
	if err != nil {
		s.logf("L3 write-back: capture deps: %v", err)
		return
	}
	if len(deps) == 0 {
		// An empty capture is not evidence of stability — do not cache.
		s.logf("L3 write-back: deps tree empty, skip")
		return
	}
	sl := &slice.Slice{
		ID:        gatewaySliceID(content, s.cfg.Scope),
		Type:      slice.Result,
		Scope:     s.cfg.Scope,
		Content:   []byte(content),
		Weight:    1.0,
		CreatedAt: time.Now().Unix(),
		Meta: slice.SliceMeta{
			Model:       meta.model,
			ContextHash: meta.ctxHash,
			Deps:        deps,
			Mtimes:      mtimes,
		},
	}
	if err := s.store.Put(sl); err != nil {
		s.logf("L3 write-back: put: %v", err)
		return
	}
	if err := s.index.Insert(sl); err != nil {
		s.logf("L3 write-back: index: %v", err)
	}
	s.logf("L3 write-back: cached %s (%d bytes, %d deps)", sl.ID, len(content), len(deps))
}

// captureTree fingerprints every regular file under root (relative paths,
// sorted) with sha256 digests + mtimes — the L3 entry's dependency snapshot
// (design §3.5: 文件一变缓存即失效).
func captureTree(root string) (fingerprint.Deps, map[string]int64, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, err
	}
	var paths []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Type()&os.ModeSymlink != 0 {
			return nil // symlinked deps are rejected (verify never follows links)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if !filepath.IsLocal(rel) {
			return fmt.Errorf("non-local dep path %q", rel)
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(paths)
	deps, err := fingerprint.Capture(root, paths)
	if err != nil {
		return nil, nil, err
	}
	mtimes := make(map[string]int64, len(paths))
	for _, p := range paths {
		fi, err := os.Stat(filepath.Join(root, p))
		if err != nil {
			return nil, nil, err
		}
		mtimes[p] = fi.ModTime().Unix()
	}
	return deps, mtimes, nil
}

// gatewaySliceID derives a deterministic content ID (mirrors the extractor
// discipline: same content + type + scope → same ID, so re-caching dedups).
func gatewaySliceID(content string, scope slice.Scope) string {
	h := sha256.New()
	h.Write([]byte{byte(slice.Result), byte(scope)})
	h.Write([]byte(content))
	return hex.EncodeToString(h.Sum(nil)[:16])
}
