package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Config is everything the pool needs. Read from the environment in main, never here:
// a package that reads os.Getenv cannot be tested against two configurations.
type Config struct {
	URL string

	// MaxConns caps connections from THIS process. It is deliberately small.
	//
	// Railway's Postgres has a fixed connection limit shared by everything that talks to
	// it — the app, a migration run, a psql session, a backup job. A pool sized for
	// "plenty" exhausts that limit and the next connection fails, which presents as the
	// site being down rather than as a pool that is too big. The read path is cached and
	// this is one small service; 10 is more than it needs.
	MaxConns int32

	// MaxConnLifetime recycles connections. Without it, a pool holds the same TCP
	// connections indefinitely and never notices that the database was replaced
	// underneath it — which is exactly what a Railway Postgres restart does.
	MaxConnLifetime time.Duration

	// MaxConnIdleTime returns unused connections so an idle service is not holding
	// slots that something else needs.
	MaxConnIdleTime time.Duration

	// ConnectTimeout bounds the initial handshake. Unset, a bad host makes startup hang
	// forever, and a deploy that hangs is worse than one that fails: Railway keeps the
	// previous version serving only if the new one actually exits.
	ConnectTimeout time.Duration
}

// DefaultConfig returns sane limits for this service. url is required.
func DefaultConfig(url string) Config {
	return Config{
		URL:             url,
		MaxConns:        10,
		MaxConnLifetime: time.Hour,
		MaxConnIdleTime: 5 * time.Minute,
		ConnectTimeout:  5 * time.Second,
	}
}

// New builds the pool and PROVES it works before returning.
//
// ⭐ It pings. A pgxpool is lazy: pgxpool.New succeeds against a completely wrong URL
// because nothing connects until the first query. Without the ping the service boots
// "successfully" and then fails on the first request a visitor makes — the failure
// arrives far from its cause, on someone else's screen. Ping moves it to startup, where
// Railway keeps the previous deploy serving.
func New(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("db: DATABASE_URL is empty")
	}
	pc, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		// Deliberately does not include cfg.URL: it carries the password.
		return nil, fmt.Errorf("db: DATABASE_URL is not a valid connection string: %w", err)
	}
	pc.MaxConns = cfg.MaxConns
	pc.MaxConnLifetime = cfg.MaxConnLifetime
	pc.MaxConnIdleTime = cfg.MaxConnIdleTime
	pc.ConnConfig.ConnectTimeout = cfg.ConnectTimeout

	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("db: creating the pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: cannot reach the database: %w", err)
	}
	return pool, nil
}
