package gateway

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"semantix/kernel/bm25"
	"semantix/kernel/slice"
	"semantix/kernel/usage"
)

// Server is the Semantix Gateway HTTP server (design §3.1–3.3). It owns the
// slice store + in-memory index (the L2/L3 backend, equivalent in-process
// to the `semantix lookup --json` subprocess protocol), the upstream set
// and the async write-memory worker.
type Server struct {
	cfg    Config
	store  slice.Store
	index  slice.Index
	up     *upstreamSet
	usage  *usage.Recorder // nil when recording is disabled
	mem    *memoryWorker   // nil when write-memory is disabled
	client *http.Client
	logf   func(format string, args ...any)
}

// New builds a Server from the effective config: opens the slice store,
// rebuilds the in-memory index, wires upstreams and the write-memory worker.
func New(cfg Config) (*Server, error) {
	store, err := slice.NewFileStore(cfg.StoreDB)
	if err != nil {
		return nil, fmt.Errorf("gateway: open store %s: %w", cfg.StoreDB, err)
	}
	idx := bm25.New()
	if err := indexFromStore(store, idx); err != nil {
		return nil, fmt.Errorf("gateway: index store: %w", err)
	}

	s := &Server{
		cfg:    cfg,
		store:  store,
		index:  idx,
		up:     newUpstreamSet(cfg.Upstreams),
		client: &http.Client{Timeout: 120 * time.Second},
		logf:   func(format string, args ...any) { log.Printf("[gateway] "+format, args...) },
	}
	if cfg.UsageDB != "" {
		rec, err := usage.NewRecorder(cfg.UsageDB)
		if err != nil {
			return nil, fmt.Errorf("gateway: usage recorder: %w", err)
		}
		s.usage = rec
	}
	if cfg.Ingest.SessionsDir != "" {
		s.mem = newMemoryWorker(s, cfg.Ingest.SessionsDir, cfg.Ingest.Extract)
	}
	return s, nil
}

// Close releases the server's resources.
func (s *Server) Close() error {
	if s.mem != nil {
		s.mem.close()
	}
	if closer, ok := s.store.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

// Handler returns the routed HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.recover(s.handleHealthz))
	mux.HandleFunc("/v1/models", s.recover(s.withAuth(s.handleModels)))
	mux.HandleFunc("/v1/chat/completions", s.recover(s.withAuth(s.handleChatCompletions)))
	mux.HandleFunc("/v1/completions", s.recover(s.withAuth(s.handleCompletions501)))
	return mux
}

// ListenAndServe runs the HTTP server on cfg.Addr until the context is
// cancelled (graceful shutdown). It returns the server error on failure.
func (s *Server) ListenAndServe(addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return srv.ListenAndServe()
}

// recover maps handler panics to OpenAI-format 500 responses.
func (s *Server) recover(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.logf("panic: %v", rec)
				writeAPIError(w, http.StatusInternalServerError,
					"server_error", "internal_error", "internal server error")
			}
		}()
		next(w, r)
	}
}

// withAuth enforces the gateway key (design §3.10): New API injects it as
// the channel key and forwards it as `Authorization: Bearer <key>`. An
// empty configured key disables auth (trusted-network / dev mode).
func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.GatewayKey != "" {
			got := ""
			if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
				got = strings.TrimPrefix(h, "Bearer ")
			}
			if subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.GatewayKey)) != 1 {
				writeAPIError(w, http.StatusUnauthorized, "authentication_error",
					"invalid_api_key", "invalid or missing API key")
				return
			}
		}
		next(w, r)
	}
}

// handleHealthz reports liveness: the slice store must be openable and the
// gateway must have at least one configured upstream (design §3.2).
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "invalid_request_error",
			"method_not_allowed", "healthz only supports GET")
		return
	}
	if _, err := s.store.ListAll(); err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "server_error",
			"store_unavailable", "slice store unavailable")
		return
	}
	if len(s.up.aliases()) == 0 {
		writeAPIError(w, http.StatusServiceUnavailable, "server_error",
			"no_upstream", "no upstreams configured")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, `{"status":"ok"}`+"\n")
}

// handleModels lists every routable model alias (design §3.2 / §4.2).
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "invalid_request_error",
			"method_not_allowed", "models only supports GET")
		return
	}
	aliases := s.up.aliases()
	data := make([]map[string]any, 0, len(aliases))
	for _, a := range aliases {
		data = append(data, map[string]any{
			"id":       a,
			"object":   "model",
			"owned_by": "semantix-gateway",
		})
	}
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	_ = enc.Encode(map[string]any{"object": "list", "data": data})
}

// handleCompletions501 implements the design's MVP scope decision: text
// completions are not supported in v1 (design §3.2 table).
func (s *Server) handleCompletions501(w http.ResponseWriter, r *http.Request) {
	writeAPIError(w, http.StatusNotImplemented, "invalid_request_error",
		"not_implemented", "POST /v1/completions is not implemented in v1; use /v1/chat/completions")
}

// indexFromStore rebuilds an in-memory index from the persistent store,
// covering every scope (mirrors cmd/semantix).
func indexFromStore(store slice.Store, idx slice.Index) error {
	for _, scope := range []slice.Scope{slice.Session, slice.Project, slice.User} {
		items, err := store.List(scope)
		if err != nil {
			return err
		}
		for _, sl := range items {
			if err := idx.Insert(sl); err != nil {
				return err
			}
		}
	}
	return nil
}
