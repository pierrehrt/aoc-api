// Command api is the Age of Conan Codex backend.
//
// This file is WIRING ONLY: read configuration, build the router, run the server,
// shut it down cleanly. Business logic lives in internal/<domain>/, HTTP concerns in
// internal/httpx/. If something here starts making a decision, it is in the wrong file.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pierrehrt/aoc-api/internal/assets"
	"github.com/pierrehrt/aoc-api/internal/httpx"
	"github.com/pierrehrt/aoc-api/internal/pages"
	"github.com/pierrehrt/aoc-api/internal/templates"
	"github.com/pierrehrt/aoc-api/internal/version"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// Fill in any build identity the linker could not — on Railway the commit arrives as a
	// runtime environment variable, not a build arg. Must happen before anything reports it.
	version.Resolve()

	// Railway assigns the port; 8080 is the local default.
	addr := ":" + envOr("PORT", "8080")

	// ⭐ Assets and templates are loaded HERE, before the server starts, and any failure
	// aborts the boot. That is deliberate: Railway keeps the previous deploy serving when a
	// new one fails to start, so a missing asset or a broken template becomes a failed
	// deploy rather than a live site with no stylesheet.
	assetSet, err := assets.Load()
	if err != nil {
		return fmt.Errorf("loading assets: %w", err)
	}
	tpl, err := templates.New(assetSet)
	if err != nil {
		return fmt.Errorf("parsing templates: %w", err)
	}

	// The origin canonical URLs are built from. Configured, never taken from the request:
	// see the comment on pages.Handler.baseURL.
	site := pages.New(tpl, envOr("PUBLIC_BASE_URL", "http://localhost:"+envOr("PORT", "8080")))

	srv := &http.Server{
		Addr:    addr,
		Handler: httpx.NewRouterWithSite(version.Version, version.Commit, site.Routes, assetSet.Handler()),
		// A server with no timeouts will eventually be held open by a slow or dead
		// client until it runs out of file descriptors. These are the three that
		// net/http leaves unset by default.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Shut down on SIGINT/SIGTERM: stop accepting, let in-flight requests finish.
	// Railway sends SIGTERM on every deploy, so without this each deploy cuts off
	// whatever was mid-request.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errc := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", addr, "version", version.Version, "commit", version.Commit)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		slog.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
