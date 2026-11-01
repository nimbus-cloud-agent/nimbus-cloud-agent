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
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
)

func Open(t *testing.T) *store.Store {
	s, _ := OpenWithProbe(t)
	return s
}

func OpenWithProbe(t *testing.T) (*store.Store, *Probe) {
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
	query.Set("application_name", schema)
	u.RawQuery = query.Encode()
	db, err := store.Open(u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, &Probe{admin: admin, application: schema}
}

// Probe inspects only this disposable Store's connections. Database access stays
// inside the DAO test utilities; HTTP tests receive typed observations only.
type Probe struct {
	admin       *sql.DB
	application string
}
type Activity struct {
	PID          int
	QueryStarted time.Time
}

func (p *Probe) Activity(ctx context.Context) ([]Activity, error) {
	rows, err := p.admin.QueryContext(ctx, "SELECT pid,query_start FROM pg_stat_activity WHERE application_name=$1 ORDER BY pid", p.application)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Activity{}
	for rows.Next() {
		var a Activity
		if err := rows.Scan(&a.PID, &a.QueryStarted); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}
func (p *Probe) DisconnectListener(ctx context.Context) error {
	_, err := p.admin.ExecContext(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name=$1", p.application+"-listener")
	return err
}

// RejectCompletedTurns injects a transaction failure only in this fixture's
// disposable schema. Business tests never receive SQL/ORM handles.
func (p *Probe) RejectCompletedTurns(ctx context.Context) error {
	_, err := p.admin.ExecContext(ctx, "ALTER TABLE "+pgx.Identifier{p.application, "outbox_messages"}.Sanitize()+" ADD CONSTRAINT reject_completed_fixture CHECK (outcome <> 'completed')")
	return err
}
