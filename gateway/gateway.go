package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"semantix/kernel/bm25"
	"semantix/kernel/cache"
	"semantix/kernel/ingest"
	"semantix/kernel/inject"
	"semantix/kernel/slice"
	"semantix/kernel/usage"
	"semantix/kernel/zone"
)

// Gateway is the assembled Semantix Gateway v1. All semantic behavior is
// delegated to kernel packages (cache / inject / slice / ingest / usage);
// this type only wires them together and owns the HTTP transport.
type Gateway struct {
	cfg      *Config
	store    slice.Store
	index    slice.Index
	decider  *cache.L3Decider
	injector *inject.Injector
	usageLog *usage.Recorder
	client   *http.Client

	sessionsMu sync.Mutex // serializes sidecar JSONL appends
	ingestMu   sync.Mutex // serializes the async ingest writes
	ingestWG   sync.WaitGroup
	disabled   bool       // SEMANTIX_GATEWAY_DISABLE ablation switch

	now func() time.Time
}

// New assembles the gateway from config: opens the slice store, rebuilds the
// in-memory BM25 index from it, and wires the L2 injector + L3 decider. It
// never starts network listeners — the caller (cmd/semantix-gateway) does.
func New(cfg *Config) (*Gateway, error) {
	root := cfg.Store.DepsRoot
	if root == "" {
		root = "."
	}
	store, err := slice.NewFileStore(cfg.Store.DB)
	if err != nil {
		return nil, fmt.Errorf("gateway: open store %s: %w", cfg.Store.DB, err)
	}
	idx := bm25.New()
	if err := loadIndex(store, idx); err != nil {
		_ = closeStore(store)
		return nil, fmt.Errorf("gateway: rebuild index: %w", err)
	}

	scope, err := parseScope(cfg.Store.Scope)
	if err != nil {
		_ = closeStore(store)
		return nil, err
	}
	topK := cfg.Retrieval.TopK
	if topK <= 0 {
		topK = 5
	}
	budget := cfg.Retrieval.Budget
	if budget <= 0 {
		budget = inject.DefaultBudget
	}
	z := zone.Default()

	var rec *usage.Recorder
	if cfg.Ingest.UsageLog != "" {
		rec, err = usage.NewRecorder(cfg.Ingest.UsageLog)
		if err != nil {
			_ = closeStore(store)
			return nil, fmt.Errorf("gateway: usage recorder: %w", err)
		}
	}

	g := &Gateway{
		cfg:      cfg,
		store:    store,
		index:    idx,
		decider:  &cache.L3Decider{Index: idx, Store: store, Root: root, K: topK},
		injector: &inject.Injector{Index: idx, Store: store, Scope: scope, K: topK, Budget: budget, Zones: &z},
		usageLog: rec,
		client:   &http.Client{Timeout: 120 * time.Second},
		disabled: os.Getenv("SEMANTIX_GATEWAY_DISABLE") != "",
		now:      time.Now,
	}
	return g, nil
}

// Close waits for in-flight sidecar ingestion (write memory is best-effort
// but must not be silently dropped on shutdown), then releases the store.
func (g *Gateway) Close() error {
	g.ingestWG.Wait()
	return closeStore(g.store)
}

func closeStore(store slice.Store) error {
	if closer, ok := store.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

// loadIndex rebuilds the in-memory index from the persisted store so L2/L3
// retrieval sees previously extracted slices (same pattern as the CLI's
// lookup/search command assembly).
func loadIndex(store slice.Store, idx slice.Index) error {
	items, err := store.ListAll()
	if err != nil {
		return err
	}
	for _, s := range items {
		if err := idx.Insert(s); err != nil {
			return err
		}
	}
	return nil
}

// parseScope maps the configured scope name to the kernel scope enum.
func parseScope(s string) (slice.Scope, error) {
	switch s {
	case "", "project":
		return slice.Project, nil
	case "session":
		return slice.Session, nil
	case "user":
		return slice.User, nil
	default:
		return 0, fmt.Errorf("gateway config: [store] scope %q must be session, project, or user", s)
	}
}

// resolveScope picks the request scope: an optional x-semantix-scope header
// overrides the configured default (design §4.4 方案 B hook; the header is
// injected by the New API channel config, clients never touch it).
func (g *Gateway) resolveScope(r *http.Request) (slice.Scope, error) {
	v := r.Header.Get("x-semantix-scope")
	if v == "" {
		return g.injector.Scope, nil
	}
	return parseScope(v)
}

// cacheFresh applies the configured TTL window over Slice.CreatedAt.
// ttl_seconds<=0 or unknown age (CreatedAt==0) never expire (kernel gc
// semantics); the kernel dep-fingerprint chain stays the staleness authority.
func (g *Gateway) cacheFresh(s *slice.Slice) bool {
	ttl := g.cfg.Cache.TTLSeconds
	if ttl <= 0 || s.CreatedAt == 0 {
		return true
	}
	return g.now().Unix()-s.CreatedAt <= ttl
}

// randomID returns a hex id for sidecar session files.
func randomID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// ---------------------------------------------------------------------------
// session sidecar (write memory, design §3.7)

// recordSession appends this request/response pair to a session JSONL file
// (0600, ingest.JSONLSource-compatible lines) and asynchronously extracts it
// into the slice library. Best-effort: failures are logged, never returned
// to the client.
func (g *Gateway) recordSession(sessionID string, turns []map[string]any) {
	if sessionID == "" {
		sessionID = "gw-" + randomID()
	}
	dir := g.cfg.Ingest.SessionsDir
	if dir == "" {
		return
	}
	path := filepath.Join(dir, sessionID+".jsonl")

	g.sessionsMu.Lock()
	err := appendJSONLLines(path, turns)
	g.sessionsMu.Unlock()
	if err != nil {
		log.Printf("gateway: write session %s: %v", path, err)
		return
	}
	g.ingestWG.Add(1)
	go func() {
		defer g.ingestWG.Done()
		g.ingestSession(path)
	}()
}

// ingestSession drains one sidecar file into the slice library via the
// kernel ingest pipeline, wrapping the extractor so gateway-generated
// dependency-free Result slices honor the configured L3-safe default
// (design §3.5: gateway entries are NOT L3-eligible unless opted in).
func (g *Gateway) ingestSession(path string) {
	g.ingestMu.Lock()
	defer g.ingestMu.Unlock()
	src, err := ingest.NewJSONLSource(path)
	if err != nil {
		log.Printf("gateway: ingest %s: %v", path, err)
		return
	}
	p := ingest.Pipeline{
		Extractor: l3SafeExtractor{inner: slice.NewExtractor(), l3Safe: g.cfg.Ingest.L3SafeDefault},
		Store:     g.store,
		Index:     g.index,
		Scope:     g.injector.Scope,
		Project:   filepath.Base(path),
	}
	if _, err := p.Run(src); err != nil {
		log.Printf("gateway: ingest %s: %v", path, err)
	}
}

// l3SafeExtractor delegates to the kernel extractor, then applies the
// configured L3-safe default to dependency-free Result slices (the only
// case where L3Safe is consulted — slices with captured Deps are inherently
// safe, kernel-side).
type l3SafeExtractor struct {
	inner  slice.Extractor
	l3Safe bool
}

func (e l3SafeExtractor) Extract(tr []byte, meta slice.SliceMeta) ([]*slice.Slice, error) {
	items, err := e.inner.Extract(tr, meta)
	if err != nil {
		return nil, err
	}
	for _, s := range items {
		if s.Type == slice.Result && len(s.Meta.Deps) == 0 {
			s.Meta.L3Safe = e.l3Safe
		}
	}
	return items, nil
}

// appendJSONLLines appends JSON objects as lines, creating the file with
// 0600 and the parent directory with 0700 (kernel permission convention).
func appendJSONLLines(path string, lines []map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, line := range lines {
		raw, err := json.Marshal(line)
		if err != nil {
			return err
		}
		if _, err := f.Write(append(raw, '\n')); err != nil {
			return err
		}
	}
	return nil
}
