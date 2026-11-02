package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Preserve the public-schema bigint key used by older Nimbus versions. Every
// other schema uses its own two-integer key, including disposable test schemas.
const tryOwnershipSQL = `SELECT CASE WHEN current_schema() = 'public' THEN pg_try_advisory_lock(846291710) ELSE pg_try_advisory_lock(846291710, hashtext(current_schema())) END`
const waitOwnershipSQL = `SELECT CASE WHEN current_schema() = 'public' THEN pg_advisory_lock(846291710) ELSE pg_advisory_lock(846291710, hashtext(current_schema())) END`

// Only an explicitly opted-in deployment waiting on this exact lock requests a
// handoff. Ordinary CLI/database connections cannot cause the owner to exit.
const waitingDeploymentSQL = `SELECT EXISTS (
 SELECT 1 FROM pg_locks held
 JOIN pg_locks waiting USING (locktype, database, classid, objid, objsubid)
 JOIN pg_stat_activity candidate ON candidate.pid = waiting.pid
 WHERE held.pid = pg_backend_pid() AND held.locktype = 'advisory'
 AND held.granted AND NOT waiting.granted
 AND candidate.application_name = 'nimbus-deploy-handoff'
)`

type ownership struct {
	conn   *sql.Conn
	cancel context.CancelFunc
	done   chan struct{}
	events chan error
	once   sync.Once
}

var ErrDatabaseOwned = errors.New("database is already owned by another Nimbus process")

func acquireOwnership(ctx context.Context, db *sql.DB, handoff, wait bool) (*ownership, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	acquired := false
	defer func() {
		if !acquired {
			_ = conn.Close()
		}
	}()
	if handoff {
		if _, err := conn.ExecContext(ctx, `SELECT set_config('application_name', 'nimbus-deploy-handoff', false)`); err != nil {
			return nil, fmt.Errorf("configure database ownership: %w", err)
		}
	}
	var locked bool
	if err := conn.QueryRowContext(ctx, tryOwnershipSQL).Scan(&locked); err != nil {
		return nil, fmt.Errorf("acquire database ownership: %w", err)
	}
	if !locked {
		if !wait {
			return nil, ErrDatabaseOwned
		}
		started := time.Now()
		slog.InfoContext(ctx, "waiting for database ownership handoff")
		if _, err := conn.ExecContext(ctx, waitOwnershipSQL); err != nil {
			return nil, fmt.Errorf("wait for database ownership handoff: %w", err)
		}
		slog.InfoContext(ctx, "database ownership handoff acquired", "duration_ms", time.Since(started).Milliseconds())
	} else {
		slog.DebugContext(ctx, "database ownership acquired", "handoff_enabled", handoff)
	}
	acquired = true
	o := &ownership{conn: conn}
	if handoff {
		monitorCtx, cancel := context.WithCancel(context.Background())
		o.cancel, o.done, o.events = cancel, make(chan struct{}), make(chan error, 1)
		go o.monitor(monitorCtx)
	}
	return o, nil
}

func (o *ownership) monitor(ctx context.Context) {
	defer close(o.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		var waiting bool
		err := o.conn.QueryRowContext(checkCtx, waitingDeploymentSQL).Scan(&waiting)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			o.events <- fmt.Errorf("database ownership connection lost: %w", err)
			return
		}
		if waiting {
			o.events <- nil
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (o *ownership) close() {
	o.once.Do(func() {
		if o.cancel != nil {
			o.cancel()
			<-o.done
		}
		_ = o.conn.Close()
	})
}
