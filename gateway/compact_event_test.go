package gateway

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	kernelevent "semantix/kernel/event"
	"semantix/kernel/slice"
)

// Startup eviction emits a library-level Compact event (Issue #277): when
// the store exceeds the configured cap, New() runs the typed eviction pass
// and writes the observation to sessions/maintenance.jsonl with
// trigger="evict" and the per-type distribution.
func TestStartupEvictionEmitsCompactEvent(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Server.GatewayKey = "test-key"
	cfg.Store.DB = filepath.Join(dir, "db.jsonl")
	cfg.Store.DepsRoot = dir
	cfg.Ingest.SessionsDir = filepath.Join(dir, "sessions")
	cfg.Upstreams = []UpstreamConfig{{
		Name: "deepseek", BaseURL: "http://127.0.0.1:1", APIKey: "up-key",
		ModelAlias: []string{"deepseek-chat"}, UpstreamModel: "deepseek-chat",
		Vendor: "deepseek",
	}}
	cap := 2
	cfg.Store.MaxSlices = &cap

	// Over-cap library: 3 Results + 1 Context, all equally stale so the
	// type key decides — two Results are evicted, the Context survives.
	store, err := slice.NewFileStore(cfg.Store.DB)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * 24 * time.Hour).Unix()
	for i := 0; i < 3; i++ {
		if err := store.Put(&slice.Slice{ID: fmt.Sprintf("r%d", i), Type: slice.Result,
			Scope: slice.Project, Content: []byte("x"), CreatedAt: old}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Put(&slice.Slice{ID: "c1", Type: slice.Context,
		Scope: slice.Project, Content: []byte("c"), CreatedAt: old}); err != nil {
		t.Fatal(err)
	}
	if closer, ok := store.(interface{ Close() error }); ok {
		_ = closer.Close() // release the journal handle before New() reopens
	}

	g, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_ = g.Close() // flush async sidecar writes before reading the event

	events := readKernelEvents(t, g, "maintenance")
	if len(events) != 1 || events[0].Kind != kernelevent.Compact {
		t.Fatalf("maintenance events = %+v, want one Compact", events)
	}
	var payload kernelevent.CompactPayload
	if err := kernelevent.UnmarshalData(events[0], &payload); err != nil {
		t.Fatalf("unmarshal Compact payload: %v", err)
	}
	if payload.Trigger != "evict" {
		t.Fatalf("trigger = %q, want evict", payload.Trigger)
	}
	if payload.Before != 4 || payload.After != 2 {
		t.Fatalf("before/after = %d/%d, want 4/2 (slice counts)", payload.Before, payload.After)
	}
	if want := map[string]int{"result": 2}; !equalCounts(payload.EvictedByType, want) {
		t.Fatalf("EvictedByType = %v, want %v (two Results evicted, Context kept)", payload.EvictedByType, want)
	}
}

// No over-cap growth at startup → no maintenance event at all.
func TestStartupNoEvictionNoCompactEvent(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Server.GatewayKey = "test-key"
	cfg.Store.DB = filepath.Join(dir, "db.jsonl")
	cfg.Store.DepsRoot = dir
	cfg.Ingest.SessionsDir = filepath.Join(dir, "sessions")
	cfg.Upstreams = []UpstreamConfig{{
		Name: "deepseek", BaseURL: "http://127.0.0.1:1", APIKey: "up-key",
		ModelAlias: []string{"deepseek-chat"}, UpstreamModel: "deepseek-chat",
		Vendor: "deepseek",
	}}
	store, err := slice.NewFileStore(cfg.Store.DB)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(&slice.Slice{ID: "r1", Type: slice.Result,
		Scope: slice.Project, Content: []byte("x"), CreatedAt: time.Now().Add(-30 * 24 * time.Hour).Unix()}); err != nil {
		t.Fatal(err)
	}
	if closer, ok := store.(interface{ Close() error }); ok {
		_ = closer.Close()
	}

	g, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_ = g.Close()

	if _, err := os.Stat(filepath.Join(cfg.Ingest.SessionsDir, "maintenance.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("maintenance.jsonl exists without eviction (err=%v)", err)
	}
}

func equalCounts(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
