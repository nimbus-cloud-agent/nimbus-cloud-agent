package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"gorm.io/gorm"
)

func historyOutbox(t *testing.T, s *Store, session Session, id string, at time.Time, delivered *int64) Outbox {
	t.Helper()
	row := Outbox{ID: id, SessionID: session.ID, TurnID: uuid.NewString(), ClientMsgID: uuid.NewString(), Options: session.Options, Images: []model.MessageImage{}, CreatedAt: at, Status: "queued", DeliveredSeq: delivered}
	if delivered != nil {
		row.Status = "delivered"
	}
	if err := s.CreateOutbox(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	return row
}
func historyRecord(t *testing.T, s *Store, sid string, seq int64, kind string, ms int64, data any) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"seq": seq, "type": kind, "time": ms, "data": data})
	if err != nil {
		t.Fatal(err)
	}
	var payload model.NativeRecord
	if err = json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if err = s.CreateEvent(context.Background(), Event{SessionID: sid, Seq: seq, Type: kind, Payload: payload}); err != nil {
		t.Fatal(err)
	}
}

func TestHistoryKeysetsEqualTimestampsAndSessionOwnership(t *testing.T) {
	s, session := typedStore(t)
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Microsecond)
	rows := make([]Outbox, 13)
	for i := range rows {
		rows[i] = historyOutbox(t, s, session, fmt.Sprintf("00000000-0000-0000-0000-%012d", i+1), at, nil)
	}
	for _, out := range []Outbox{rows[0], rows[12]} {
		if err := s.CreateQuestion(ctx, UserQuestion{ID: uuid.NewString(), SessionID: session.ID, TurnID: out.TurnID, Status: "pending", Questions: []model.Question{}}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.HistoryPage(ctx, HistoryFilter{SessionID: session.ID})
	if err != nil || !page.HasMore || len(page.Outbox) != 10 || page.Outbox[0].ID != rows[3].ID || page.Outbox[9].ID != rows[12].ID || len(page.Questions) != 1 || page.Questions[0].TurnID != rows[12].TurnID {
		t.Fatalf("latest: %+v %v", page, err)
	}
	// New appends must not move the exclusive historical boundary.
	newest := historyOutbox(t, s, session, uuid.NewString(), at.Add(time.Second), nil)
	older, err := s.HistoryPage(ctx, HistoryFilter{SessionID: session.ID, BeforeTurn: page.Outbox[0].TurnID})
	if err != nil || older.HasMore || len(older.Outbox) != 3 || older.Outbox[0].ID != rows[0].ID || older.Outbox[2].ID != rows[2].ID || len(older.Questions) != 1 {
		t.Fatalf("older: %+v %v", older, err)
	}
	window, err := s.HistoryPage(ctx, HistoryFilter{SessionID: session.ID, SinceTurn: rows[11].TurnID})
	if err != nil || len(window.Outbox) != 3 || window.Outbox[0].ID != rows[11].ID || window.Outbox[2].ID != newest.ID {
		t.Fatalf("inclusive window: %+v %v", window, err)
	}
	empty, err := s.HistoryPage(ctx, HistoryFilter{SessionID: session.ID, BeforeTurn: rows[0].TurnID})
	if err != nil || empty.HasMore || len(empty.Outbox) != 0 || empty.Outbox == nil || empty.Turns == nil || empty.Questions == nil {
		t.Fatalf("empty: %+v %v", empty, err)
	}
	other := Session{ID: uuid.NewString(), Options: model.DefaultOptions(), AgentState: "idle", SyncedSeq: -1}
	if err = s.CreateSession(ctx, other); err != nil {
		t.Fatal(err)
	}
	for _, f := range []HistoryFilter{{SessionID: other.ID, BeforeTurn: rows[0].TurnID}, {SessionID: other.ID, SinceTurn: rows[0].TurnID}, {SessionID: session.ID, BeforeTurn: uuid.NewString()}} {
		if _, err = s.HistoryPage(ctx, f); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unowned keyset accepted: %v", err)
		}
	}
	if _, err = s.TurnTrace(ctx, other.ID, rows[0].TurnID, model.TraceCursor{Seq: -1}, 0, TraceFilter{}); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign trace", err)
	}
}

func TestHistorySummariesAndLosslessOneTurnTrace(t *testing.T) {
	s, session := typedStore(t)
	ctx := context.Background()
	seq := int64(1)
	first := historyOutbox(t, s, session, uuid.NewString(), time.Now().Add(-time.Second), &seq)
	seq2 := int64(11)
	second := historyOutbox(t, s, session, uuid.NewString(), time.Now(), &seq2)
	secret := strings.Repeat("private-huge-trace", 10000)
	historyRecord(t, s, session.ID, 0, "turn/start", 1000, map[string]any{"turn": 1})
	historyRecord(t, s, session.ID, 1, "user/message", 1100, map[string]any{})
	historyRecord(t, s, session.ID, 2, "assistant/message", 1200, map[string]any{"message": map[string]any{"content": "intermediate, not final"}})
	historyRecord(t, s, session.ID, 3, "tool/call", 1300, map[string]any{"callId": "root", "arguments": secret})
	historyRecord(t, s, session.ID, 4, "tool/ptc-dispatch-start", 1400, map[string]any{"subCallId": "child", "parentCallId": "root"})
	historyRecord(t, s, session.ID, 5, "tool/ptc-dispatch", 1500, map[string]any{"subCallId": "child", "content": []any{map[string]any{"type": "text", "text": "{\"exitCode\":9}"}}})
	historyRecord(t, s, session.ID, 6, "tool/result", 1600, map[string]any{"callId": "root", "error": map[string]any{"message": "failed"}})
	historyRecord(t, s, session.ID, 7, "tool/ptc-dispatch", 1700, map[string]any{"subCallId": "review", "name": "exit_plan_mode", "arguments": "{\"plan\":\"# Durable plan\"}", "isError": true, "content": "no user-questions channel is available"})
	historyRecord(t, s, session.ID, 8, "assistant/message", 1800, map[string]any{"message": map[string]any{"content": []any{map[string]any{"type": "reasoning", "text": secret}, map[string]any{"type": "text", "text": "Final answer"}}}})
	historyRecord(t, s, session.ID, 9, "turn/end", 4000, map[string]any{"turn": 1})
	historyRecord(t, s, session.ID, 10, "turn/start", 5000, map[string]any{"turn": 2})
	historyRecord(t, s, session.ID, 11, "user/message", 5100, map[string]any{})
	historyRecord(t, s, session.ID, 12, "assistant/message", 5200, map[string]any{"message": map[string]any{"content": "Second answer"}})
	for _, out := range []Outbox{first, second} {
		if err := s.CreateFrame(ctx, StreamFrame{SessionID: session.ID, TurnID: out.TurnID, Payload: model.FramePayload{Type: "tool-output", Text: secret}}); err != nil {
			t.Fatal(err)
		}
		if err := s.CreateQuestion(ctx, UserQuestion{ID: uuid.NewString(), SessionID: session.ID, TurnID: out.TurnID, Status: "pending", Questions: []model.Question{}}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.HistoryPage(ctx, HistoryFilter{SessionID: session.ID})
	if err != nil {
		t.Fatal(err)
	}
	summary := page.Turns[0]
	if summary.FinalText != "Final answer" || summary.ToolCount != 3 || summary.FailedCount != 3 || summary.Duration == nil || *summary.Duration != 3 || !summary.HasDetails || summary.PlanReviewRetry == nil || summary.PlanReviewRetry.Seq != 7 {
		t.Fatalf("summary: %+v", summary)
	}
	raw, _ := json.Marshal(page)
	if strings.Contains(string(raw), "private-huge-trace") || strings.Contains(string(raw), "intermediate, not final") || strings.Contains(string(raw), "Durable plan") {
		t.Fatal("summary leaked trace")
	}
	trace, err := s.TurnTrace(ctx, session.ID, first.TurnID, model.TraceCursor{Seq: -1}, 0, TraceFilter{})
	if err != nil || len(trace.Events) != 10 || trace.Events[0].Type != "turn/start" || trace.Events[9].Seq != 9 || len(trace.Frames) != 1 || len(trace.Questions) != 1 || trace.Questions[0].TurnID != first.TurnID || trace.Cursor.Seq != 9 || trace.Cursor.Frame != trace.Frames[0].ID {
		t.Fatalf("trace: %+v %v", trace, err)
	}
	raw, _ = json.Marshal(trace.Events[3].Payload)
	if !strings.Contains(string(raw), secret) {
		t.Fatal("explicit trace lost native arguments")
	}
	// Runtime hydration remains intentionally full and lossless.
	events, err := s.Events(ctx, EventFilter{SessionID: session.ID, After: -1})
	if err != nil || len(events) != 13 {
		t.Fatal("runtime hydration changed", len(events), err)
	}
	queued := historyOutbox(t, s, session, uuid.NewString(), time.Now().Add(time.Second), nil)
	if err = s.CreateFrame(ctx, StreamFrame{SessionID: session.ID, TurnID: queued.TurnID, Payload: model.FramePayload{Type: "fixture"}}); err != nil {
		t.Fatal(err)
	}
	trace, err = s.TurnTrace(ctx, session.ID, queued.TurnID, model.TraceCursor{Seq: -1}, 0, TraceFilter{})
	if err != nil || len(trace.Events) != 0 || len(trace.Frames) != 0 || trace.Cursor.Seq != -1 || trace.Cursor.Frame != 0 {
		t.Fatal("undelivered trace", trace, err)
	}
}

func TestTurnTraceSnapshotDoesNotMixConcurrentCommits(t *testing.T) {
	s, session := typedStore(t)
	ctx := context.Background()
	seq := int64(1)
	out := historyOutbox(t, s, session, uuid.NewString(), time.Now(), &seq)
	historyRecord(t, s, session.ID, 0, "turn/start", 1000, map[string]any{})
	historyRecord(t, s, session.ID, 1, "user/message", 1100, map[string]any{})
	if err := s.CreateFrame(ctx, StreamFrame{SessionID: session.ID, TurnID: out.TurnID, Payload: model.FramePayload{Type: "fixture"}}); err != nil {
		t.Fatal(err)
	}
	paused, resume := make(chan struct{}), make(chan struct{})
	var resumeOnce sync.Once
	unblock := func() { resumeOnce.Do(func() { close(resume) }) }
	defer unblock()
	var once sync.Once
	name := "test:pause_trace_frames"
	if err := s.db.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "stream_frames" {
			once.Do(func() { close(paused); <-resume })
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer s.db.Callback().Query().Remove(name)
	type result struct {
		trace TurnTrace
		err   error
	}
	done := make(chan result, 1)
	go func() {
		trace, err := s.TurnTrace(ctx, session.ID, out.TurnID, model.TraceCursor{Seq: -1}, 0, TraceFilter{})
		done <- result{trace, err}
	}()
	select {
	case <-paused:
	case <-time.After(5 * time.Second):
		unblock()
		t.Fatal("snapshot did not reach frame read")
	}
	historyRecord(t, s, session.ID, 2, "assistant/message", 1200, map[string]any{"message": map[string]any{"content": "concurrent"}})
	if err := s.CreateFrame(ctx, StreamFrame{SessionID: session.ID, TurnID: out.TurnID, Payload: model.FramePayload{Type: "concurrent"}}); err != nil {
		unblock()
		t.Fatal(err)
	}
	unblock()
	got := <-done
	if got.err != nil || len(got.trace.Events) != 2 || len(got.trace.Frames) != 1 || got.trace.Cursor.Seq != 1 || got.trace.Cursor.Frame != got.trace.Frames[0].ID {
		t.Fatalf("mixed snapshot: %+v %v", got.trace, got.err)
	}
	catchup, err := s.TurnTrace(ctx, session.ID, out.TurnID, got.trace.Cursor, 0, TraceFilter{})
	if err != nil || len(catchup.Events) != 1 || len(catchup.Frames) != 1 || catchup.Events[0].Seq != 2 || catchup.Frames[0].Payload.Type != "concurrent" {
		t.Fatalf("snapshot catchup gap: %+v %v", catchup, err)
	}
}

func TestHistorySnapshotKeepsSummariesAndQuestionsAtPageWatermark(t *testing.T) {
	s, session := typedStore(t)
	ctx := context.Background()
	seq := int64(1)
	out := historyOutbox(t, s, session, uuid.NewString(), time.Now(), &seq)
	historyRecord(t, s, session.ID, 0, "turn/start", 1000, map[string]any{})
	historyRecord(t, s, session.ID, 1, "user/message", 1100, map[string]any{})
	historyRecord(t, s, session.ID, 2, "assistant/message", 1200, map[string]any{"message": map[string]any{"content": "old snapshot"}})
	paused, resume := make(chan struct{}), make(chan struct{})
	var once, resumeOnce sync.Once
	unblock := func() { resumeOnce.Do(func() { close(resume) }) }
	defer unblock()
	name := "test:pause_history_outbox"
	if err := s.db.Callback().Query().After("gorm:query").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "outbox_messages" {
			once.Do(func() { close(paused); <-resume })
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer s.db.Callback().Query().Remove(name)
	type result struct {
		page HistoryPage
		err  error
	}
	done := make(chan result, 1)
	go func() {
		page, err := s.HistoryPage(ctx, HistoryFilter{SessionID: session.ID})
		done <- result{page, err}
	}()
	select {
	case <-paused:
	case <-time.After(5 * time.Second):
		t.Fatal("page did not reach snapshot boundary")
	}
	historyRecord(t, s, session.ID, 3, "assistant/message", 1300, map[string]any{"message": map[string]any{"content": "new snapshot"}})
	if err := s.CreateQuestion(ctx, UserQuestion{ID: uuid.NewString(), SessionID: session.ID, TurnID: out.TurnID, Status: "pending", Questions: []model.Question{}}); err != nil {
		t.Fatal(err)
	}
	unblock()
	got := <-done
	if got.err != nil || len(got.page.Turns) != 1 || got.page.Turns[0].FinalText != "old snapshot" || len(got.page.Questions) != 0 {
		t.Fatalf("page mixed committed snapshots: %+v %v", got.page, got.err)
	}
	next, err := s.HistoryPage(ctx, HistoryFilter{SessionID: session.ID})
	if err != nil || next.Turns[0].FinalText != "new snapshot" || len(next.Questions) != 1 {
		t.Fatalf("later snapshot did not advance: %+v %v", next, err)
	}
}

func TestHistoryLimitIsCappedAndExactBoundaryHasNoMore(t *testing.T) {
	s, session := typedStore(t)
	ctx := context.Background()
	at := time.Now()
	for i := range 51 {
		historyOutbox(t, s, session, uuid.NewString(), at.Add(time.Duration(i)*time.Second), nil)
	}
	page, err := s.HistoryPage(ctx, HistoryFilter{SessionID: session.ID, Limit: 500})
	if err != nil || len(page.Outbox) != 50 || !page.HasMore {
		t.Fatal("history maximum not applied", len(page.Outbox), page.HasMore, err)
	}
	older, err := s.HistoryPage(ctx, HistoryFilter{SessionID: session.ID, BeforeTurn: page.Outbox[0].TurnID, Limit: 1})
	if err != nil || len(older.Outbox) != 1 || older.HasMore {
		t.Fatal("exact page boundary incorrectly has more", older, err)
	}
}
