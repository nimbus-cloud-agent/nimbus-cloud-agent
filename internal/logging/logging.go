// Package logging configures structured logs and carries operation identities.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
)

func Configure(level string, secrets ...string) error {
	var parsed slog.Level
	if err := parsed.UnmarshalText([]byte(level)); err != nil {
		return fmt.Errorf("invalid log level %q: %w", level, err)
	}
	slog.SetDefault(slog.New(NewHandler(os.Stdout, parsed, secrets...)))
	return nil
}

func NewHandler(w io.Writer, level slog.Level, secrets ...string) slog.Handler {
	values := append([]string(nil), secrets...)
	for _, secret := range secrets {
		if u, err := url.Parse(secret); err == nil && u.User != nil {
			if password, ok := u.User.Password(); ok && password != "" {
				values = append(values, password)
			}
		}
	}
	return slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level, ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
		v := a.Value.Resolve()
		var text string
		switch v.Kind() {
		case slog.KindString:
			text = v.String()
		case slog.KindAny:
			err, ok := v.Any().(error)
			if !ok {
				return a
			}
			text = err.Error()
		default:
			return a
		}
		for _, secret := range values {
			if secret != "" {
				text = strings.ReplaceAll(text, secret, "[REDACTED]")
			}
		}
		a.Value = slog.StringValue(text)
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
