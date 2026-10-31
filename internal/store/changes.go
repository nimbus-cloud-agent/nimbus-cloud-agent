package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ChangeKind string

const (
	ChangeSession  ChangeKind = "session"
	ChangeSandbox  ChangeKind = "sandbox"
	ChangeOutbox   ChangeKind = "outbox"
	ChangeQuestion ChangeKind = "question"
	ChangeEvent    ChangeKind = "event"
	ChangeFrame    ChangeKind = "frame"
	// ChangeResync follows every successful LISTEN, including initial connection.
	// Consumers must reread durable state: notifications are not a replay log.
	ChangeResync ChangeKind = "resync"
)

// Change is the DAO's typed invalidation record, not a database driver message.
type Change struct {
	SessionID string     `json:"session_id"`
	Kind      ChangeKind `json:"kind"`
}

// notificationSource owns one dedicated connection, independently of GORM's
// transaction pool. A Store has at most one active Changes iterator.
type notificationSource struct {
	config *pgx.ConnConfig
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	active bool
	done   chan struct{}
}

func newNotificationSource(dsn string) (*notificationSource, error) {
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	config.ConnectTimeout = 5 * time.Second
	name := config.RuntimeParams["application_name"]
	if name == "" {
		name = "nimbus"
	}
	config.RuntimeParams["application_name"] = name + "-listener"
	ctx, cancel := context.WithCancel(context.Background())
	return &notificationSource{config: config, ctx: ctx, cancel: cancel}, nil
}
func (n *notificationSource) close() {
	n.mu.Lock()
	n.cancel()
	done := n.done
	n.mu.Unlock()
	if done != nil {
		<-done
	}
}
func (n *notificationSource) begin() (func(), error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if err := n.ctx.Err(); err != nil {
		return nil, err
	}
	if n.active {
		return nil, errors.New("Store already has an active change listener")
	}
	n.active = true
	n.done = make(chan struct{})
	return func() { n.mu.Lock(); n.active = false; close(n.done); n.mu.Unlock() }, nil
}

// Changes is a single-use stream. It waits on the socket, never polls tables.
// Transient connection failures are yielded and retried; a subsequent Resync
// instructs consumers to fill any gap. Returning from range closes the connection.
func (s *Store) Changes(ctx context.Context) iter.Seq2[Change, error] {
	return func(yield func(Change, error) bool) {
		n := s.notifications
		if n == nil {
			yield(Change{}, errors.New("change listener requires a root Store"))
			return
		}
		end, err := n.begin()
		if err != nil {
			yield(Change{}, err)
			return
		}
		defer end()
		ctx, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(n.ctx, cancel)
		defer stop()
		defer cancel()
		delay := 250 * time.Millisecond
		for ctx.Err() == nil {
			connectedAt := time.Now()
			err := n.listen(ctx, yield)
			if ctx.Err() != nil || err == nil {
				return
			}
			if !yield(Change{}, err) {
				return
			}
			if time.Since(connectedAt) >= 10*time.Second {
				delay = 250 * time.Millisecond
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			delay = min(delay*2, 5*time.Second)
		}
	}
}
func (n *notificationSource) listen(ctx context.Context, yield func(Change, error) bool) error {
	conn, err := pgx.ConnectConfig(ctx, n.config.Copy())
	if err != nil {
		return fmt.Errorf("connect change listener: %w", err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = conn.Close(closeCtx)
	}()
	var channel string
	if err := conn.QueryRow(ctx, "SELECT 'nimbus_changes_' || md5(current_schema())").Scan(&channel); err != nil {
		return err
	}
	// Exec outside an explicit transaction commits LISTEN before the initial
	// resync, preventing the subscribe/snapshot race described by PostgreSQL.
	if _, err = conn.Exec(ctx, "LISTEN "+pgx.Identifier{channel}.Sanitize()); err != nil {
		return err
	}
	if !yield(Change{Kind: ChangeResync}, nil) {
		return nil
	}
	for {
		message, err := conn.WaitForNotification(ctx)
		if err != nil {
			return fmt.Errorf("wait for database change: %w", err)
		}
		if message.Channel != channel {
			continue
		}
		change, err := decodeChange(message.Payload)
		if err != nil {
			// A malformed invalidation cannot silently strand a client. Retain the
			// connection and request a full reread, without exposing payload contents.
			if !yield(Change{}, err) || !yield(Change{Kind: ChangeResync}, nil) {
				return nil
			}
			continue
		}
		if !yield(change, nil) {
			return nil
		}
	}
}
func decodeChange(payload string) (Change, error) {
	var change Change
	if err := json.Unmarshal([]byte(payload), &change); err != nil {
		return Change{}, errors.New("invalid database change payload")
	}
	if _, err := uuid.Parse(change.SessionID); err != nil {
		return Change{}, errors.New("invalid database change identity")
	}
	switch change.Kind {
	case ChangeSession, ChangeSandbox, ChangeOutbox, ChangeQuestion, ChangeEvent, ChangeFrame:
		return change, nil
	default:
		return Change{}, errors.New("unknown database change kind")
	}
}
