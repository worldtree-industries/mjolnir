package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

// Config holds pool sizing and timeouts.
type Config struct {
	MinConns    int
	MaxConns    int
	ConnTimeout time.Duration
}

// DefaultConfig returns opinionated defaults.
func DefaultConfig() Config {
	return Config{
		MinConns:    5,                //nolint:mnd // default value
		MaxConns:    25,               //nolint:mnd // default value
		ConnTimeout: 30 * time.Second, //nolint:mnd // default value
	}
}

// Connect creates a pool from config + DSN.
// Performs an initial healthcheck before returning.
func Connect(ctx context.Context, dsn string, cfg Config) (*Pool, error) {
	if cfg.MaxConns == 0 {
		cfg = DefaultConfig()
	}
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse db config: %w", err)
	}
	//nolint:gosec // int32 conversion required by pgx.
	poolConfig.MinConns = int32(cfg.MinConns)
	//nolint:gosec // int32 conversion required by pgx.
	poolConfig.MaxConns = int32(cfg.MaxConns)
	poolConfig.ConnConfig.ConnectTimeout = cfg.ConnTimeout

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create pool: %w", err)
	}

	// Initial healthcheck
	if pingErr := pool.Ping(ctx); pingErr != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", pingErr)
	}

	log.Info().
		Int("min_conns", cfg.MinConns).
		Int("max_conns", cfg.MaxConns).
		Str("dsn", maskDSN(dsn)).
		Msg("database pool connected")

	return &Pool{pool: pool, cfg: cfg}, nil
}

// Pool wraps pgxpool.Pool with framework defaults.
type Pool struct {
	pool *pgxpool.Pool
	cfg  Config
}

// Raw returns the underlying *pgxpool.Pool.
func (p *Pool) Raw() *pgxpool.Pool {
	return p.pool
}

// Config returns the pool config.
func (p *Pool) Config() Config {
	return p.cfg
}

// Stats returns pool statistics.
func (p *Pool) Stats() *pgxpool.Stat {
	return p.pool.Stat()
}

// Close closes the pool.
func (p *Pool) Close() {
	p.pool.Close()
}

// maskDSN hides the password from the DSN string for logging.
func maskDSN(dsn string) string {
	for i := range dsn {
		if dsn[i] == '@' {
			return "***" + dsn[i:]
		}
	}
	return dsn
}
