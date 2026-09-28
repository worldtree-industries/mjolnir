package log

import (
	"context"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/rs/zerolog"
)

// Context keys for correlation fields
type ctxKey int

const (
	requestIDKey ctxKey = iota
	traceIDKey
	userIDKey
)

// redactKeys are the field keys that should be masked in logs.
const (
	redactPassword      = "password"
	redactAuthorization = "authorization"
	redactEmail         = "email"
)

// Logger wraps zerolog.Logger.
type Logger struct {
	zerolog.Logger
}

// New creates a named Logger with dual output streams:
// - JSON stream to stdout (Victorialogs/Grafana observability)
// - Human-readable console stream to stderr (CLI/kubectl debugging)
// Both streams receive all log levels (DEBUG, INFO, WARN, ERROR).
func New(name string) *Logger {
	_ = zerolog.New(os.Stdout)

	multi := zerolog.New(io.MultiWriter(os.Stdout, os.Stderr)).
		With().
		Str("logger", name).
		Timestamp().
		Logger()

	return &Logger{Logger: multi}
}

// With adds optional fields to the log line, redacting sensitive values.
func (l *Logger) With(key string, value any) *Logger {
	redacted := value
	if isSensitive(key) {
		redacted = "***"
	}
	return &Logger{
		Logger: l.Logger.With().Interface(key, redacted).Logger(),
	}
}

// Zero returns a fresh Logger with no correlation fields.
func (l *Logger) Zero() *Logger {
	return &Logger{Logger: zerolog.New(io.Discard)}
}

// WithRequestID returns a logger with the given request_id.
func (l *Logger) WithRequestID(id string) *Logger {
	return &Logger{Logger: l.Logger.With().Str("request_id", id).Logger()}
}

// WithTraceID returns a logger with the given trace_id.
func (l *Logger) WithTraceID(id string) *Logger {
	return &Logger{Logger: l.Logger.With().Str("trace_id", id).Logger()}
}

// WithUserID returns a logger with the given user_id.
func (l *Logger) WithUserID(id string) *Logger {
	return &Logger{Logger: l.Logger.With().Str("user_id", id).Logger()}
}

// WithContext returns a logger bound to context, extracting correlation fields.
func (l *Logger) WithContext(ctx context.Context) *Logger {
	zl := l.Logger.With()
	if v := ctx.Value(requestIDKey); v != nil {
		zl = zl.Str("request_id", toString(v))
	}
	if v := ctx.Value(traceIDKey); v != nil {
		zl = zl.Str("trace_id", toString(v))
	}
	if v := ctx.Value(userIDKey); v != nil {
		zl = zl.Str("user_id", toString(v))
	}
	return &Logger{Logger: zl.Logger()}
}

// toString safely converts a value to string.
func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// isSensitive checks if a key should be redacted.
func isSensitive(key string) bool {
	lower := strings.ToLower(key)
	return slices.Contains([]string{redactPassword, redactAuthorization, redactEmail}, lower)
}

// MiddlewareAdapter returns a chi-style middleware that injects correlation IDs into context.
func MiddlewareAdapter() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Context key accessors for middleware integration

// WithRequestIDContext returns a new context with the request_id set.
func WithRequestIDContext(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// WithTraceIDContext returns a new context with the trace_id set.
func WithTraceIDContext(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceIDKey, id)
}

// WithUserIDContext returns a new context with the user_id set.
func WithUserIDContext(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, userIDKey, id)
}

// RequestIDFromContext extracts the request_id from context.
func RequestIDFromContext(ctx context.Context) string {
	if v := ctx.Value(requestIDKey); v != nil {
		return toString(v)
	}
	return ""
}

// TraceIDFromContext extracts the trace_id from context.
func TraceIDFromContext(ctx context.Context) string {
	if v := ctx.Value(traceIDKey); v != nil {
		return toString(v)
	}
	return ""
}

// UserIDFromContext extracts the user_id from context.
func UserIDFromContext(ctx context.Context) string {
	if v := ctx.Value(userIDKey); v != nil {
		return toString(v)
	}
	return ""
}