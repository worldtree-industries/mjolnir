package db

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dfryer1193/mjolnir/utils/log"
)

// maskedPassword replaces a credential in a DSN before the DSN is logged.
const maskedPassword = "REDACTED"

// Config holds pool sizing and timeouts.
// A zero field means "use the default"; negative values are invalid.
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

// withDefaults fills each zero field from DefaultConfig, leaving explicit
// values untouched.
func (c Config) withDefaults() Config {
	defaults := DefaultConfig()
	if c.MinConns == 0 {
		c.MinConns = defaults.MinConns
	}
	if c.MaxConns == 0 {
		c.MaxConns = defaults.MaxConns
	}
	if c.ConnTimeout == 0 {
		c.ConnTimeout = defaults.ConnTimeout
	}
	return c
}

// validate rejects negative values and a minimum above the maximum.
func (c Config) validate() error {
	if c.MinConns < 0 {
		return fmt.Errorf("min conns must not be negative: %d", c.MinConns)
	}
	if c.MaxConns < 0 {
		return fmt.Errorf("max conns must not be negative: %d", c.MaxConns)
	}
	if c.ConnTimeout < 0 {
		return fmt.Errorf("conn timeout must not be negative: %s", c.ConnTimeout)
	}
	if c.MinConns > c.MaxConns {
		return fmt.Errorf("min conns (%d) must not exceed max conns (%d)", c.MinConns, c.MaxConns)
	}
	return nil
}

// Connect creates a pool from config + DSN.
// Performs an initial healthcheck before returning.
func Connect(ctx context.Context, dsn string, cfg Config) (*Pool, error) {
	cfg = cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid db config: %w", err)
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

	log.GetLogger("db").InfoContext(ctx, "database pool connected",
		slog.Int("min_conns", cfg.MinConns),
		slog.Int("max_conns", cfg.MaxConns),
		slog.String("dsn", maskDSN(dsn)),
	)

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

// Begin starts a transaction on the pool.
func (p *Pool) Begin(ctx context.Context) (pgx.Tx, error) {
	return p.pool.Begin(ctx)
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

// maskDSN hides credentials in a DSN before it is logged.
func maskDSN(dsn string) string {
	if strings.Contains(dsn, "://") {
		return maskURLDSN(dsn)
	}
	return maskKeywordDSN(dsn)
}

// maskURLDSN masks a password in URL-style DSNs, whether it appears in the
// userinfo or in the query string.
func maskURLDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return maskKeywordDSN(dsn)
	}
	hasUserPassword := false
	if u.User != nil {
		_, hasUserPassword = u.User.Password()
		if hasUserPassword {
			u.User = url.UserPassword(u.User.Username(), maskedPassword)
		}
	}
	query := u.Query()
	hasQueryPassword := query.Has("password")
	if hasQueryPassword {
		query.Set("password", maskedPassword)
		u.RawQuery = query.Encode()
	}
	if !hasUserPassword && !hasQueryPassword {
		return dsn
	}
	return u.String()
}

// maskKeywordDSN masks a password in keyword/value DSNs such as
// "host=db password=secret". Quoted passwords may contain spaces.
func maskKeywordDSN(dsn string) string {
	password := regexp.MustCompile(`(?i)(\bpassword\s*=\s*)('[^']*'|"[^"]*"|\S+)`)
	return password.ReplaceAllString(dsn, "${1}"+maskedPassword)
}
