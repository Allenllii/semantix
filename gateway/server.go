package gateway

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"
)

// maxBodyBytes caps the request body (bounding memory; OpenAI chat bodies
// are far smaller).
const maxBodyBytes = 10 << 20

// ServeHTTP routes the OpenAI-compatible surface: /v1/models, the core
// /v1/chat/completions pipeline, and the New API health probe /healthz.
// Every /v1/* request is authenticated against the gateway key (the key
// New API attaches as the channel credential — clients never see it).
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer func() {
		log.Printf("gateway: %s %s (%s)", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	}()

	switch r.URL.Path {
	case "/healthz":
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method not allowed")
			return
		}
		g.handleHealth(w)
		return
	case "/v1/models":
		if !g.authenticate(w, r) {
			return
		}
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method not allowed")
			return
		}
		g.handleModels(w)
		return
	case "/v1/chat/completions":
		if !g.authenticate(w, r) {
			return
		}
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method not allowed")
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "read body: "+err.Error())
			return
		}
		g.handleChat(w, r, body)
		return
	default:
		writeAPIError(w, http.StatusNotFound, "not_found", "no such endpoint: "+r.URL.Path)
	}
}

// authenticate checks the Authorization: Bearer header against the gateway
// key using a constant-time compare. /healthz deliberately skips this — New
// API's channel health check does not carry the channel key.
func (g *Gateway) authenticate(w http.ResponseWriter, r *http.Request) bool {
	const prefix = "Bearer "
	auth := r.Header.Get("Authorization")
	if len(auth) <= len(prefix) || auth[:len(prefix)] != prefix {
		writeAPIError(w, http.StatusUnauthorized, "authentication_error", "missing bearer token")
		return false
	}
	token := auth[len(prefix):]
	want := g.cfg.Server.GatewayKey
	if len(token) != len(want) || subtle.ConstantTimeCompare([]byte(token), []byte(want)) != 1 {
		writeAPIError(w, http.StatusUnauthorized, "authentication_error", "invalid gateway key")
		return false
	}
	return true
}

// handleHealth answers the New API channel probe: the store is already open
// by construction (New failed otherwise), so 200 means "ready".
func (g *Gateway) handleHealth(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"status":"ok"}`)
}

// handleModels lists every routable model alias (design §3.2).
func (g *Gateway) handleModels(w http.ResponseWriter) {
	list := g.cfg.ModelList()
	models := make([]map[string]any, 0, len(list))
	for _, m := range list {
		models = append(models, map[string]any{
			"id": m, "object": "model", "created": 0, "owned_by": "semantix-gateway",
		})
	}
	body := map[string]any{"object": "list", "data": models}
	raw, err := json.Marshal(body)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}

// apiErrorBody is the OpenAI error envelope New API and clients parse.
type apiErrorBody struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
}

// writeAPIError writes an OpenAI-format error response.
func writeAPIError(w http.ResponseWriter, status int, typ, message string) {
	raw, _ := json.Marshal(apiErrorBody{Error: apiError{Message: message, Type: typ}})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}
