package db

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestConfigWithDefaults(t *testing.T) {
	t.Parallel()

	defaults := DefaultConfig()

	tests := []struct {
		name string
		in   Config
		want Config
	}{
		{
			name: "all zero",
			in:   Config{},
			want: defaults,
		},
		{
			name: "max only",
			in:   Config{MaxConns: 10},
			want: Config{MinConns: defaults.MinConns, MaxConns: 10, ConnTimeout: defaults.ConnTimeout},
		},
		{
			name: "max below default min",
			in:   Config{MaxConns: 1},
			want: Config{MinConns: 1, MaxConns: 1, ConnTimeout: defaults.ConnTimeout},
		},
		{
			name: "min only",
			in:   Config{MinConns: 2},
			want: Config{MinConns: 2, MaxConns: defaults.MaxConns, ConnTimeout: defaults.ConnTimeout},
		},
		{
			name: "timeout only",
			in:   Config{ConnTimeout: time.Second},
			want: Config{MinConns: defaults.MinConns, MaxConns: defaults.MaxConns, ConnTimeout: time.Second},
		},
		{
			name: "fully specified",
			in:   Config{MinConns: 1, MaxConns: 2, ConnTimeout: 3 * time.Second},
			want: Config{MinConns: 1, MaxConns: 2, ConnTimeout: 3 * time.Second},
		},
		{
			name: "negative values are not defaults",
			in:   Config{MinConns: -1},
			want: Config{MinConns: -1, MaxConns: defaults.MaxConns, ConnTimeout: defaults.ConnTimeout},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.in.withDefaults(); got != tt.want {
				t.Fatalf("withDefaults(%+v) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name: "defaults",
			cfg:  DefaultConfig(),
		},
		{
			name: "zero merges to defaults",
			cfg:  Config{},
		},
		{
			name: "min equals max",
			cfg:  Config{MinConns: 5, MaxConns: 5, ConnTimeout: time.Second},
		},
		{
			name:    "min above max",
			cfg:     Config{MinConns: 10, MaxConns: 5},
			wantErr: "min conns (10) must not exceed max conns (5)",
		},
		{
			name: "max below default min",
			cfg:  Config{MaxConns: 1},
		},
		{
			name:    "negative min",
			cfg:     Config{MinConns: -1},
			wantErr: "min conns must not be negative",
		},
		{
			name:    "negative max",
			cfg:     Config{MaxConns: -1},
			wantErr: "max conns must not be negative",
		},
		{
			name:    "negative timeout",
			cfg:     Config{ConnTimeout: -time.Second},
			wantErr: "conn timeout must not be negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.cfg.withDefaults().validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validate() = nil, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validate() = %q, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestConnectRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	_, err := Connect(context.Background(), "not a dsn", Config{MinConns: 10, MaxConns: 5})
	if err == nil {
		t.Fatal("Connect() = nil error, want invalid config error")
	}
	if !strings.Contains(err.Error(), "invalid db config") {
		t.Fatalf("Connect() error = %q, want invalid db config error", err)
	}
}

func TestConnectRejectsInvalidDSN(t *testing.T) {
	t.Parallel()

	_, err := Connect(context.Background(), "not a dsn", DefaultConfig())
	if err == nil {
		t.Fatal("Connect() = nil error, want invalid DSN error")
	}
	if !strings.Contains(err.Error(), "failed to parse db config") {
		t.Fatalf("Connect() error = %q, want parse error", err)
	}
}

func TestMaskDSN(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{
			name: "url password",
			dsn:  "postgres://user:s3cret@db.example:5432/app?sslmode=disable",
			want: "postgres://user:REDACTED@db.example:5432/app?sslmode=disable",
		},
		{
			name: "url encoded password",
			dsn:  "postgres://user:p%40ss@db.example/app",
			want: "postgres://user:REDACTED@db.example/app",
		},
		{
			name: "url query password",
			dsn:  "postgres://db.example/app?password=s3cret&sslmode=disable",
			want: "postgres://db.example/app?password=REDACTED&sslmode=disable",
		},
		{
			name: "keyword password",
			dsn:  "host=db.example user=app password=s3cret dbname=app",
			want: "host=db.example user=app password=REDACTED dbname=app",
		},
		{
			name: "quoted keyword password",
			dsn:  "host=db password='s3cret with space' dbname=app",
			want: "host=db password=REDACTED dbname=app",
		},
		{
			name: "quoted keyword password with escaped quote",
			dsn:  `host=db password='s3\'cret' dbname=app`,
			want: "host=db password=REDACTED dbname=app",
		},
		{
			name: "unquoted keyword password with escaped space",
			dsn:  `host=db password=s3\ cret dbname=app`,
			want: "host=db password=REDACTED dbname=app",
		},
		{
			name: "url without password",
			dsn:  "postgres://user@db.example/app?sslmode=disable",
			want: "postgres://user@db.example/app?sslmode=disable",
		},
		{
			name: "keyword without password",
			dsn:  "host=db.example user=app dbname=app",
			want: "host=db.example user=app dbname=app",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := maskDSN(tt.dsn)
			if got != tt.want {
				t.Fatalf("maskDSN(%q) = %q, want %q", tt.dsn, got, tt.want)
			}
			if strings.Contains(got, "s3cret") {
				t.Fatalf("maskDSN(%q) revealed the password: %q", tt.dsn, got)
			}
		})
	}
}
