package log

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/rs/zerolog"
)

// redactKeys are the field keys that should be masked in logs.
var redactKeys = []string{"password", "authorization", "email"}

// Logger wraps zerolog.Logger.
type Logger struct {
	zerolog.Logger
}

// New creates a named Logger.
func New(name string) *Logger {
	l := zerolog.New(os.Stdout).With().Str("logger", name).Logger()
	return &Logger{l}
}

// With adds optional fields to the log line, redacting sensitive values.
func (l *Logger) With(key string, value interface{}) *Logger {
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

// WithContext returns a logger bound to context (pulls fields from context).
func (l *Logger) WithContext(ctx context.Context) *Logger {
	return &Logger{Logger: l.Logger.With().Ctx(ctx).Logger()}
}

// isSensitive checks if a key should be redacted.
func isSensitive(key string) bool {
	lower := strings.ToLower(key)
	for _, sensitive := range redactKeys {
		if lower == sensitive {
			return true
		}
	}
	return false
}

// MiddlewareAdapter returns a chi-style middleware handler.
func MiddlewareAdapter() func(next func()) func() {
	return func(next func()) func() {
		return func() {
			next()
		}
	}
}