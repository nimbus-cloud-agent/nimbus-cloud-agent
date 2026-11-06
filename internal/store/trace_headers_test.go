package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
)

func TestTraceHeaderKeysetsAndExactFrameReads(t *testing.T) {
	s, session := typedStore(t)
	ctx := context.Background()
	first := historyOutbox(t, s, session, uuid.NewString(), time.Now(), ptrHistorySeq(1))
	second := historyOutbox(t, s, session, uuid.NewString(), time.Now().Add(time.Second), ptrHistorySeq(6))
	secret := strings.Repeat("HIDDEN-RECORD-CONTENT", 1000)
	for i, kind := range []string{"turn/start", "user/message", "assistant/message", "tool/call", "tool/result", "turn/start", "user/message", "assistant/message"} {
		historyRecord(t, s, session.ID, int64(i), kind, int64(1000+i), map[string]any{"message": map[string]any{"content": secret}, "arguments": secret})
	}
	for _, out := range []Outbox{first, second, first, first, first} {
		metadataFrame(t, s, out, map[string]any{"type": "delta", "attemptId": "attempt", "chunk": map[string]any{"type": "text-delta", "text": secret}})
	}
	page, err := s.TurnHeaders(ctx, session.ID, first.TurnID, HeaderFilter{Kind: "events", Limit: 2})
	if err != nil || len(page.Events) != 2 || page.Events[0].Seq != 3 || page.Events[1].Seq != 4 || !page.HasMore || page.Before == nil || *page.Before != 3 || page.Cursor.Seq != 4 || len(page.Frames) != 0 {
		t.Fatalf("newest event headers: %+v %v", page, err)
	}
	older, err := s.TurnHeaders(ctx, session.ID, first.TurnID, HeaderFilter{Kind: "events", Limit: 2, Before: page.Before})
	if err != nil || len(older.Events) != 2 || older.Events[0].Seq != 1 || older.Events[1].Seq != 2 || !older.HasMore {
		t.Fatalf("older headers: %+v %v", older, err)
	}
	last, err := s.TurnHeaders(ctx, session.ID, first.TurnID, HeaderFilter{Kind: "events", Limit: 2, Before: older.Before})
	if err != nil || len(last.Events) != 1 || last.Events[0].Seq != 0 || last.HasMore || last.Before != nil {
		t.Fatalf("header boundary: %+v %v", last, err)
	}
	frames, err := s.TurnHeaders(ctx, session.ID, first.TurnID, HeaderFilter{Kind: "frames", Limit: 2})
	if err != nil || len(frames.Frames) != 2 || !frames.HasMore || frames.Before == nil || frames.Frames[0].ID >= frames.Frames[1].ID || frames.Frames[0].ChunkType != "text-delta" || len(frames.Events) != 0 {
		t.Fatalf("frame headers: %+v %v", frames, err)
	}
	previous, err := s.TurnHeaders(ctx, session.ID, first.TurnID, HeaderFilter{Kind: "frames", Limit: 2, Before: frames.Before})
	if err != nil || len(previous.Frames) != 2 || previous.HasMore || previous.Frames[1].ID >= frames.Frames[0].ID {
		t.Fatalf("older frame headers: %+v %v", previous, err)
	}
	for _, snapshot := range []model.TraceHeaders{page, frames} {
		raw, _ := json.Marshal(snapshot)
		if strings.Contains(string(raw), "HIDDEN-RECORD-CONTENT") || strings.Contains(string(raw), "payload") {
			t.Fatal("headers transferred hidden bodies", string(raw))
		}
	}
	id := frames.Frames[0].ID
	trace, err := s.TurnTrace(ctx, session.ID, first.TurnID, model.TraceCursor{Seq: -1}, 0, TraceFilter{FrameID: &id, OmitQuestions: true, OmitOutbox: true})
	if err != nil || len(trace.Events) != 0 || len(trace.Frames) != 1 || trace.Frames[0].ID != id {
		t.Fatalf("exact frame read: %+v %v", trace, err)
	}
	raw, _ := json.Marshal(trace.Frames[0].Payload)
	if !strings.Contains(string(raw), secret) {
		t.Fatal("explicit frame read lost original bytes")
	}
	foreign, err := s.TurnHeaders(ctx, session.ID, second.TurnID, HeaderFilter{Kind: "frames"})
	if err != nil || len(foreign.Frames) != 1 {
		t.Fatal(foreign, err)
	}
	foreignID := foreign.Frames[0].ID
	trace, err = s.TurnTrace(ctx, session.ID, first.TurnID, model.TraceCursor{Seq: -1}, 0, TraceFilter{FrameID: &foreignID})
	if err != nil || len(trace.Frames) != 0 || len(trace.Events) != 0 {
		t.Fatal("foreign frame escaped turn filter", trace, err)
	}
	if _, err = s.TurnHeaders(ctx, uuid.NewString(), first.TurnID, HeaderFilter{Kind: "events"}); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign headers accepted", err)
	}
	// Forward SSE batches must begin at the oldest unseen header, not the tail.
	cursor := model.TraceCursor{Seq: -1}
	var sequences []int64
	for {
		batch, err := s.TurnHeaders(ctx, session.ID, first.TurnID, HeaderFilter{Kind: "events", Limit: 2, After: &cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, header := range batch.Events {
			sequences = append(sequences, header.Seq)
			cursor.Seq = header.Seq
		}
		if !batch.HasMore {
			break
		}
	}
	if len(sequences) != 5 {
		t.Fatal("forward headers skipped", sequences)
	}
	for i, seq := range sequences {
		if seq != int64(i) {
			t.Fatal("forward headers reordered", sequences)
		}
	}
}

func TestTraceQueryCanOmitUnusedEnvelopes(t *testing.T) {
	s, session := typedStore(t)
	ctx := context.Background()
	out := historyOutbox(t, s, session, uuid.NewString(), time.Now(), ptrHistorySeq(1))
	if err := s.db.WithContext(ctx).Model(&Outbox{}).Where("id=?", out.ID).Update("content", "HIDDEN USER PROMPT").Error; err != nil {
		t.Fatal(err)
	}
	historyRecord(t, s, session.ID, 0, "turn/start", 1000, map[string]any{})
	historyRecord(t, s, session.ID, 1, "user/message", 1100, map[string]any{})
	if err := s.CreateQuestion(ctx, UserQuestion{ID: uuid.NewString(), SessionID: session.ID, TurnID: out.TurnID, Status: "pending", Questions: []model.Question{{ID: "q", Question: "HIDDEN PLAN"}}}); err != nil {
		t.Fatal(err)
	}
	full, err := s.TurnTrace(ctx, session.ID, out.TurnID, model.TraceCursor{Seq: -1}, 0, TraceFilter{})
	if err != nil || full.Outbox.Content != "HIDDEN USER PROMPT" || len(full.Questions) != 1 {
		t.Fatal(full, err)
	}
	trace, err := s.TurnTrace(ctx, session.ID, out.TurnID, model.TraceCursor{Seq: -1}, 0, TraceFilter{OmitQuestions: true, OmitOutbox: true})
	if err != nil || trace.Outbox.Content != "" || len(trace.Questions) != 0 || len(trace.Events) != 2 {
		t.Fatal("trace ignored include filters", trace, err)
	}
	metadata, err := s.TurnMetadata(ctx, session.ID, out.TurnID, MetadataFilter{OmitQuestions: true, OmitOutbox: true})
	if err != nil || metadata.Outbox.Content != "" || len(metadata.Questions) != 0 {
		t.Fatal("metadata ignored include filters", metadata, err)
	}
}
