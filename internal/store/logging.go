package store

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/logging"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// SQL text and parameters are deliberately excluded. Trace still reports query
// latency, rows, SQLSTATE and failures through the same contextual slog handler.
type databaseLogger struct{ level logger.LogLevel }

func (l databaseLogger) LogMode(level logger.LogLevel) logger.Interface {
	l.level = level
	return l
}
func (l databaseLogger) Info(ctx context.Context, msg string, args ...any) {
	if l.level >= logger.Info {
		l.diagnostic(ctx, slog.LevelDebug, msg, args...)
	}
}
func (l databaseLogger) Warn(ctx context.Context, msg string, args ...any) {
	if l.level >= logger.Warn {
		l.diagnostic(ctx, slog.LevelWarn, msg, args...)
	}
}
func (l databaseLogger) Error(ctx context.Context, msg string, args ...any) {
	if l.level >= logger.Error {
		l.diagnostic(ctx, slog.LevelError, msg, args...)
	}
}
func (l databaseLogger) diagnostic(ctx context.Context, level slog.Level, msg string, args ...any) {
	attrs := []any{"component", "gorm", "message", msg}
	for _, value := range args {
		if err, ok := value.(error); ok {
			attrs = append(attrs, databaseErrorAttrs(err)...)
			break
		}
	}
	logging.FromContext(ctx).Log(ctx, level, "database diagnostic", attrs...)
}
func (l databaseLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if l.level == logger.Silent {
		return
	}
	elapsed, level := time.Since(begin), slog.LevelDebug
	notFound := errors.Is(err, gorm.ErrRecordNotFound)
	switch {
	case err != nil && !notFound && !errors.Is(err, context.Canceled):
		if l.level < logger.Error {
			return
		}
		level = slog.LevelError
	case elapsed >= 200*time.Millisecond && !notFound && !errors.Is(err, context.Canceled):
		if l.level < logger.Warn {
			return
		}
		level = slog.LevelWarn
	default:
		if l.level < logger.Info {
			return
		}
	}
	log := logging.FromContext(ctx)
	if !log.Enabled(ctx, level) {
		return
	}
	_, rows := fc()
	attrs := []any{"component", "gorm", "duration_ms", elapsed.Milliseconds(), "rows", rows}
	if notFound {
		attrs = append(attrs, "outcome", "not_found")
	} else if err != nil {
		attrs = append(attrs, databaseErrorAttrs(err)...)
	}
	log.Log(ctx, level, "database query", attrs...)
}

func (databaseLogger) ParamsFilter(_ context.Context, sql string, _ ...any) (string, []any) {
	return sql, nil
}

func databaseErrorAttrs(err error) []any {
	var state interface{ SQLState() string }
	if errors.As(err, &state) {
		// PostgreSQL messages/DETAIL may echo a bound value or an entire row.
		return []any{"error", "PostgreSQL statement failed", "sqlstate", state.SQLState()}
	}
	return []any{"error", logging.Sanitize(err.Error())}
}
