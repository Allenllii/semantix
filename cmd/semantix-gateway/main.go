// Command semantix-gateway runs the Semantix Gateway v1 (Issue #133): an
// OpenAI-compatible HTTP gateway that fronts upstream LLMs with the kernel
// three-layer cache (L3 verified reuse → L2 injection → forward).
//
// Design: docs/specs/newapi-gateway-design.md.
//
// Usage:
//
//	semantix-gateway --config semantix-gateway.toml
//
// Credentials (gateway key, upstream API keys) are injected via ${VAR}
// references resolved from the environment; nothing secret lands in the
// config file.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"semantix/gateway"
)

func main() {
	configPath := flag.String("config", "semantix-gateway.toml", "gateway config file path")
	flag.Parse()

	cfg, err := gateway.Load(*configPath)
	if err != nil {
		log.Fatalf("semantix-gateway: %v", err)
	}
	g, err := gateway.New(cfg)
	if err != nil {
		log.Fatalf("semantix-gateway: %v", err)
	}

	srv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           g,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	log.Printf("semantix-gateway: listening on %s (%d upstream(s): %s)",
		cfg.Server.Addr, len(cfg.Upstreams), upstreamNames(cfg))

	// Graceful shutdown on SIGINT/SIGTERM: stop accepting, drain in-flight
	// requests, then Close() waits for the async write-memory to land —
	// without this, Ctrl+C would kill the process before sidecars extract.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("semantix-gateway: %v", err)
		}
	case <-ctx.Done():
		log.Print("semantix-gateway: shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("semantix-gateway: shutdown: %v", err)
		}
	}
	if err := g.Close(); err != nil {
		log.Printf("semantix-gateway: close: %v", err)
	}
}

func upstreamNames(cfg *gateway.Config) string {
	names := make([]string, 0, len(cfg.Upstreams))
	for _, u := range cfg.Upstreams {
		names = append(names, u.Name)
	}
	out := ""
	for i, n := range names {
		if i > 0 {
			out += ", "
		}
		out += n
	}
	return out
}
