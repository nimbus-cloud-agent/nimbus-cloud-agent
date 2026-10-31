package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"gorm.io/driver/postgres"
)

func listeningStore(t *testing.T) (*Store, Session) {
	t.Helper()
	s, session := typedStore(t)
	source, err := newNotificationSource(s.db.Dialector.(*postgres.Dialector).DSN)
	if err != nil {
		t.Fatal(err)
	}
	source.config.RuntimeParams["application_name"] = "nimbus-test-listener-" + uuid.NewString()
	s.notifications = source
	t.Cleanup(source.close)
	return s, session
}
func collectChanges(t *testing.T, s *Store) <-chan Change {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan Change, 32)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for change, err := range s.Changes(ctx) {
			if err != nil {
				t.Error(err)
				return
			}
			select {
			case events <- change:
			case <-ctx.Done():
				return
			}
		}
	}()
	t.Cleanup(func() { cancel(); <-done })
	awaitChange(t, events, ChangeResync, "")
	return events
}
func awaitChange(t *testing.T, events <-chan Change, kind ChangeKind, sid string) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case got := <-events:
			if got.Kind == kind && got.SessionID == sid {
				return
			}
		case <-deadline.C:
			t.Fatalf("missing %s change for %s", kind, sid)
		}
	}
}
func noChange(t *testing.T, events <-chan Change) {
	t.Helper()
	select {
	case change := <-events:
		t.Fatalf("unexpected uncommitted/unrelated change: %+v", change)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestNotificationsFollowCommitAndRollback(t *testing.T) {
	s, session := listeningStore(t)
	events := collectChanges(t, s)
	ctx := context.Background()
	ready := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	go func() {
		done <- s.Transaction(ctx, func(tx *Store) error {
			title := "committed"
			err := tx.UpdateSession(ctx, session.ID, SessionChanges{Title: &title})
			close(ready)
			<-release
			return err
		})
	}()
	<-ready
	noChange(t, events)
	unblock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	awaitChange(t, events, ChangeSession, session.ID)
	rollback := errors.New("rollback")
	err := s.Transaction(ctx, func(tx *Store) error {
		title := "discarded"
		if err := tx.UpdateSession(ctx, session.ID, SessionChanges{Title: &title}); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	noChange(t, events)
	// Even SQL executed outside DAO methods reaches the same trigger channel.
	if err := s.db.Exec("UPDATE sessions SET title='external writer' WHERE id=?", session.ID).Error; err != nil {
		t.Fatal(err)
	}
	awaitChange(t, events, ChangeSession, session.ID)
}
func TestAllStreamTablesNotifyAndSchemasAreIsolated(t *testing.T) {
	s, session := listeningStore(t)
	events := collectChanges(t, s)
	other, _ := listeningStore(t)
	otherEvents := collectChanges(t, other)
	ctx := context.Background()
	out := Outbox{ID: uuid.NewString(), SessionID: session.ID, TurnID: uuid.NewString(), ClientMsgID: uuid.NewString(), Options: session.Options, Status: "queued", Images: []model.MessageImage{}}
	if err := s.CreateOutbox(ctx, out); err != nil {
		t.Fatal(err)
	}
	awaitChange(t, events, ChangeOutbox, session.ID)
	sb := Sandbox{ID: uuid.NewString(), SessionID: session.ID, E2bSandboxID: "fixture", State: "running"}
	if err := s.CreateSandbox(ctx, sb); err != nil {
		t.Fatal(err)
	}
	awaitChange(t, events, ChangeSandbox, session.ID)
	q := UserQuestion{ID: uuid.NewString(), SessionID: session.ID, TurnID: out.TurnID, Status: "pending", Questions: []model.Question{{ID: "review"}}}
	if err := s.CreateQuestion(ctx, q); err != nil {
		t.Fatal(err)
	}
	awaitChange(t, events, ChangeQuestion, session.ID)
	if err := s.ResolveQuestion(ctx, q.ID, "dismissed", &model.QuestionAnswer{Dismissed: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	awaitChange(t, events, ChangeQuestion, session.ID)
	if err := s.CreateEvent(ctx, Event{SessionID: session.ID, Seq: 0, Type: "fixture", Payload: model.NativeRecord{Seq: 0, Type: "fixture"}}); err != nil {
		t.Fatal(err)
	}
	awaitChange(t, events, ChangeEvent, session.ID)
	if err := s.CreateFrame(ctx, StreamFrame{SessionID: session.ID, TurnID: out.TurnID, Payload: model.FramePayload{Type: "delta"}}); err != nil {
		t.Fatal(err)
	}
	awaitChange(t, events, ChangeFrame, session.ID)
	if err := s.DeleteSession(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	awaitChange(t, events, ChangeSession, session.ID)
	noChange(t, otherEvents)
}
func TestListenerReconnectRequestsDurableCatchUp(t *testing.T) {
	s, session := listeningStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signals := make(chan Change, 4)
	lost := make(chan struct{})
	resume := make(chan struct{})
	done := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(resume) }) }
	defer unblock()
	go func() {
		defer close(done)
		for change, err := range s.Changes(ctx) {
			if err != nil {
				close(lost)
				select {
				case <-resume:
				case <-ctx.Done():
					return
				}
				continue
			}
			select {
			case signals <- change:
			case <-ctx.Done():
				return
			}
		}
	}()
	t.Cleanup(func() { cancel(); <-done })
	awaitChange(t, signals, ChangeResync, "")
	if err := s.db.Exec("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name=?", s.notifications.config.RuntimeParams["application_name"]).Error; err != nil {
		t.Fatal(err)
	}
	select {
	case <-lost:
	case <-time.After(5 * time.Second):
		t.Fatal("listener did not notice disconnect")
	}
	title := "written while listener disconnected"
	if err := s.UpdateSession(ctx, session.ID, SessionChanges{Title: &title}); err != nil {
		t.Fatal(err)
	}
	unblock()
	awaitChange(t, signals, ChangeResync, "")
	row, err := s.Session(ctx, session.ID)
	if err != nil || row.Title != title {
		t.Fatalf("catch-up lost durable state: %v", err)
	}
	title = "live after reconnect"
	if err := s.UpdateSession(ctx, session.ID, SessionChanges{Title: &title}); err != nil {
		t.Fatal(err)
	}
	awaitChange(t, signals, ChangeSession, session.ID)
}
func TestFrameCursorCannotSkipConcurrentCommit(t *testing.T) {
	s, session := typedStore(t)
	ctx := context.Background()
	out := Outbox{ID: uuid.NewString(), SessionID: session.ID, TurnID: uuid.NewString(), ClientMsgID: uuid.NewString(), Options: session.Options, Status: "queued", Images: []model.MessageImage{}}
	if err := s.CreateOutbox(ctx, out); err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	release := make(chan struct{})
	first := make(chan error, 1)
	second := make(chan error, 1)
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	frame := func(text string) StreamFrame {
		return StreamFrame{SessionID: session.ID, TurnID: out.TurnID, Payload: model.FramePayload{Type: "tool-output", Text: text}}
	}
	go func() {
		first <- s.Transaction(ctx, func(tx *Store) error {
			err := tx.CreateFrame(ctx, frame("first"))
			close(ready)
			if err != nil {
				return err
			}
			<-release
			return nil
		})
	}()
	<-ready
	go func() { second <- s.CreateFrame(ctx, frame("second")) }()
	select {
	case err := <-second:
		t.Fatalf("later frame committed before earlier frame: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	rows, err := s.Frames(ctx, FrameFilter{SessionID: session.ID})
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
	unblock()
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	rows, err = s.Frames(ctx, FrameFilter{SessionID: session.ID})
	if err != nil || len(rows) != 2 || rows[0].Payload.Text != "first" || rows[1].Payload.Text != "second" {
		t.Fatal(rows, err)
	}
}
