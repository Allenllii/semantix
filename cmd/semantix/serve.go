package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"syscall"

	"semantix/gateway"
)

// runServe starts the Semantix Gateway HTTP service (Issue #133, design
// docs/specs/newapi-gateway-design.md): an OpenAI-compatible endpoint that
// runs requests through the kernel L3/L2 semantic cache before forwarding
// to the configured upstream. Configuration: semantix-gateway.toml +
// SEMANTIX_GATEWAY_* env (see gateway.Load). The command blocks until
// SIGINT/SIGTERM, then shuts down gracefully (exit 0).
func runServe(args []string, stdout, stderr io.Writer, deps dependencies) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	addr := flags.String("addr", "", "listen address (default :8080; env SEMANTIX_GATEWAY_ADDR)")
	config := flags.String("config", "", "gateway config file (default semantix-gateway.toml)")
	db := flags.String("db", "", "slice store path override (env SEMANTIX_GATEWAY_DB)")
	scope := flags.String("scope", "", "slice scope override: session|project|user")
	disable := flags.Bool("disable", false, "disable the semantic cache (pure pass-through; env SEMANTIX_GATEWAY_DISABLE=1)")
	if err := flags.Parse(args); err != nil {
		return usageWrap(err)
	}
	if flags.NArg() != 0 {
		return usagef("serve: unexpected arguments: %v", flags.Args())
	}

	cfg, err := gateway.Load(gateway.Options{
		ConfigPath: *config,
		Addr:       *addr,
		StoreDB:    *db,
		Scope:      *scope,
		Disable:    *disable,
	})
	if err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	if len(cfg.Upstreams) == 0 {
		return fmt.Errorf("serve: no upstreams configured (add [[upstreams]] to %q or set SEMANTIX_GATEWAY_* env)",
			configFileOrDefault(*config))
	}

	srv, err := gateway.New(*cfg)
	if err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	defer srv.Close()

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("serve: listen %s: %w", cfg.Addr, err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Fprintf(stdout, "semantix serve: gateway listening on %s (models: %v, cache: %s)\n",
		cfg.Addr, srv.ModelAliases(), cacheMode(cfg))

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx, ln) }()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		fmt.Fprintln(stdout, "semantix serve: shutting down")
		return nil
	}
}

func configFileOrDefault(flagPath string) string {
	if flagPath != "" {
		return flagPath
	}
	if p := os.Getenv("SEMANTIX_GATEWAY_CONFIG"); p != "" {
		return p
	}
	return "semantix-gateway.toml"
}

func cacheMode(cfg *gateway.Config) string {
	if cfg.Disable {
		return "disabled (pure pass-through)"
	}
	return "L3 + L2"
}
