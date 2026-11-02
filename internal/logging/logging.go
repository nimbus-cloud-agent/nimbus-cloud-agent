// Package logging configures structured logs and carries operation identities.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync/atomic"
)

func Configure(level string, secrets ...string) error {
	var parsed slog.Level
	if err := parsed.UnmarshalText([]byte(level)); err != nil {
		return fmt.Errorf("invalid log level %q: %w", level, err)
	}
	redactor := newRedactor(secrets...)
	configuredRedactor.Store(redactor)
	slog.SetDefault(slog.New(newHandler(os.Stdout, parsed, redactor)))
	return nil
}

func NewHandler(w io.Writer, level slog.Level, secrets ...string) slog.Handler {
	return newHandler(w, level, newRedactor(secrets...))
}

var configuredRedactor atomic.Pointer[redactor]

// RegisterSecrets includes credentials updated in Settings without querying the
// database from the logging or panic-recovery path.
func RegisterSecrets(secrets ...string) {
	if r := configuredRedactor.Load(); r != nil {
		r.add(secrets...)
	}
}

func newHandler(w io.Writer, level slog.Level, r *redactor) slog.Handler {
	return slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level, ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
		if sensitiveKey(a.Key) {
			a.Value = slog.StringValue("[REDACTED]")
			return a
		}
		a.Value = r.value(a.Value.Resolve())
		return a
	}})
}

type contextKey struct{}

func FromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(contextKey{}).(*slog.Logger); ok {
		return logger
	}
	return slog.Default()
}

func WithAttrs(ctx context.Context, attrs ...any) context.Context {
	return context.WithValue(ctx, contextKey{}, FromContext(ctx).With(attrs...))
}

func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, logger)
}
