package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/driver/postgres"
)

func ownershipDSN(t *testing.T) string {
	t.Helper()
	db, _ := migrationDB(t, "000")
	return db.Dialector.(*postgres.Dialector).DSN
}

func TestOwnershipHandoffWaitsForDrain(t *testing.T) {
	dsn := ownershipDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	owner, err := OpenWithHandoff(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	if _, err := Open(dsn); !errors.Is(err, ErrDatabaseOwned) {
		t.Fatalf("ordinary open: %v", err)
	}
	type result struct {
		db  *Store
		err error
	}
	next := make(chan result, 1)
	go func() { db, err := OpenWithHandoff(ctx, dsn); next <- result{db, err} }()
	select {
	case err := <-owner.OwnershipDone():
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("owner did not observe waiting deployment")
	}
	select {
	case got := <-next:
		if got.db != nil {
			_ = got.db.Close()
		}
		t.Fatal("successor acquired ownership before the owner finished draining", got.err)
	default:
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	var successor *Store
	select {
	case got := <-next:
		if got.err != nil {
			t.Fatal(got.err)
		}
		successor = got.db
	case <-ctx.Done():
		t.Fatal("successor failed to acquire released lock")
	}
	defer successor.Close()
	var count int64
	if err := successor.db.Table("schema_migrations").Count(&count).Error; err != nil || count != 8 {
		t.Fatalf("migrations applied incorrectly: %d, %v", count, err)
	}
	if _, err := TryOpenWithHandoff(ctx, dsn); !errors.Is(err, ErrDatabaseOwned) {
		t.Fatalf("retired owner tried to take over: %v", err)
	}
	select {
	case err := <-successor.OwnershipDone():
		t.Fatalf("nonblocking standby probe requested another handoff: %v", err)
	case <-time.After(1200 * time.Millisecond):
	}
}

func TestOwnershipWaitCancellationPreservesLegacyOwner(t *testing.T) {
	dsn := ownershipDSN(t)
	owner, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := OpenWithHandoff(ctx, dsn); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting was not canceled: %v", err)
	}
	if _, err := owner.Sessions(context.Background()); err != nil {
		t.Fatalf("legacy owner disrupted: %v", err)
	}
}

func TestOwnershipConnectionLossStopsOwner(t *testing.T) {
	dsn := ownershipDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	owner, err := OpenWithHandoff(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	var pid int
	if err := owner.lock.conn.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if err := owner.db.Exec("SELECT pg_terminate_backend(?)", pid).Error; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-owner.OwnershipDone():
		if err == nil {
			t.Fatal("lost lock reported as a cooperative handoff")
		}
	case <-ctx.Done():
		t.Fatal("lost ownership connection was not detected")
	}
}
