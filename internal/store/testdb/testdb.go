// Package testdb creates disposable PostgreSQL schemas for integration tests.
// Only the DAO is returned; application tests never receive a database handle.
package testdb

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
)

func Open(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("NIMBUS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set NIMBUS_TEST_DATABASE_URL for PostgreSQL integration tests")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "nimbus_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.ExecContext(context.Background(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
		_ = admin.Close()
	})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	db, err := store.Open(u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
