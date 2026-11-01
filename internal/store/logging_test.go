package store

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/logging"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestDatabaseDiagnosticsUseSlogWithoutSQLOrBoundValues(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		elapsed time.Duration
		level   string
	}{
		{"success", nil, time.Millisecond, "DEBUG"},
		{"slow", nil, time.Second, "WARN"},
		{"failure", &pgconn.PgError{Code: "23505", Message: "private-database-value", Detail: "private-row-data"}, time.Millisecond, "ERROR"},
		{"not_found", gorm.ErrRecordNotFound, time.Second, "DEBUG"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			ctx := logging.WithLogger(context.Background(), slog.New(logging.NewHandler(&out, slog.LevelDebug)).With("request_id", "db-request"))
			databaseLogger{level: logger.Info}.Trace(ctx, time.Now().Add(-tc.elapsed), func() (string, int64) { return "SELECT 'private-sql-parameter'", 3 }, tc.err)
			var entry map[string]any
			if err := json.Unmarshal(out.Bytes(), &entry); err != nil {
				t.Fatal(err)
			}
			if entry["level"] != tc.level || entry["rows"] != float64(3) || entry["request_id"] != "db-request" {
				t.Fatal(entry)
			}
			if tc.name == "failure" && entry["sqlstate"] != "23505" {
				t.Fatal(entry)
			}
			if strings.Contains(out.String(), "private-") || strings.Contains(out.String(), "SELECT") {
				t.Fatal("logged SQL or database values")
			}
		})
	}
}
