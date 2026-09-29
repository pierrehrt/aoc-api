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

	"github.com/go-chi/chi/v5"
	"github.com/pierrehrt/aoc-api/internal/assets"
	"github.com/pierrehrt/aoc-api/internal/db"
	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/httpx"
	"github.com/pierrehrt/aoc-api/internal/items"
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
	// Shut down on SIGINT/SIGTERM: stop accepting, let in-flight requests finish.
	// Railway sends SIGTERM on every deploy, so without this each deploy cuts off
	// whatever was mid-request. Established first so the database connect respects it.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

	// ENV is a plain label: it names which environment answered on /health. Defaulting to
	// "local" means an unset value never claims to be production.
	build := httpx.Build{
		Version: version.Version,
		Commit:  version.Commit,
		Env:     envOr("ENV", "local"),
	}

	// The database pool: built ONCE here and passed down, never a package-level global
	// (a global cannot be swapped in a test and hides who depends on it).
	//
	// ⭐ Required, not optional. DATABASE_URL is set in Railway, and db.New PINGS, so a
	// wrong or unreachable database fails the BOOT — where Railway keeps the previous
	// deploy serving — instead of failing on the first request a visitor makes. Nothing
	// queries it yet; AOC-009 brings the first real tables.
	pool, err := db.New(ctx, db.DefaultConfig(os.Getenv("DATABASE_URL")))
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()
	slog.Info("database connected", "max_conns", pool.Config().MaxConns)

	// The origin canonical URLs are built from. Configured, never taken from the request:
	// see the comment on pages.Handler.baseURL.
	site := pages.New(tpl, assetSet, envOr("PUBLIC_BASE_URL", "http://localhost:"+envOr("PORT", "8080")))

	// The read surface over the armory (AOC-012). The service layer is built here and handed to
	// the JSON handlers; the HTML armory page will be handed the SAME *items.Service rather than
	// calling the JSON endpoints or re-implementing the filtering (CLAUDE.md rule 5b).
	q := sqlcgen.New(pool)
	itemsAPI := items.NewHandler(items.NewService(q), items.NewTaxonomyService(q))

	srv := &http.Server{
		Addr: addr,
		Handler: httpx.NewRouterWithAPI(build, site.Routes, assetSet.Handler(), func(v1 chi.Router) {
			v1.Mount("/items", itemsAPI.Routes())
			v1.Mount("/taxonomies", itemsAPI.TaxonomyRoutes())
		}),
		// A server with no timeouts will eventually be held open by a slow or dead
		// client until it runs out of file descriptors. These are the three that
		// net/http leaves unset by default.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

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
