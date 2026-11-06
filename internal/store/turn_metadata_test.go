package store

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
)

func metadataFrame(t *testing.T, s *Store, out Outbox, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var payload model.FramePayload
	if err = json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if err = s.CreateFrame(context.Background(), StreamFrame{SessionID: out.SessionID, TurnID: out.TurnID, Payload: payload}); err != nil {
		t.Fatal(err)
	}
}

func TestMetadataProjectsRequestedTreeLevelWithoutHiddenBodies(t *testing.T) {
	s, session := typedStore(t)
	ctx := context.Background()
	seq := int64(1)
	out := historyOutbox(t, s, session, uuid.NewString(), time.Now(), &seq)
	secret := strings.Repeat("HIDDEN-RAW-BODY", 10000)
	historyRecord(t, s, session.ID, 0, "turn/start", 1000, map[string]any{"turn": 1})
	historyRecord(t, s, session.ID, 1, "user/message", 1100, map[string]any{})
	historyRecord(t, s, session.ID, 2, "assistant/message", 1200, map[string]any{"step": 1, "turn": 1, "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "Visible message"}, map[string]any{"type": "reasoning", "text": secret}}}})
	historyRecord(t, s, session.ID, 3, "tool/call", 1300, map[string]any{"callId": "r", "name": "exec", "arguments": map[string]any{"description": "Visible description", "path": "private-path", "command": secret, "content": secret}})
	historyRecord(t, s, session.ID, 4, "tool/ptc-dispatch-start", 1400, map[string]any{"subCallId": "c", "parentCallId": "r", "name": "child", "arguments": "{\"description\":\"Child description\"}"})
	historyRecord(t, s, session.ID, 5, "tool/ptc-dispatch", 1500, map[string]any{"subCallId": "g", "parentCallId": "c", "name": "grandchild", "content": secret})
	historyRecord(t, s, session.ID, 6, "tool/ptc-dispatch", 1600, map[string]any{"subCallId": "c", "parentCallId": "r", "name": "child", "content": "{\"exitCode\":4}"})
	historyRecord(t, s, session.ID, 7, "tool/result", 1700, map[string]any{"callId": "r", "message": map[string]any{"content": secret}})
	for _, value := range []any{
		map[string]any{"type": "start", "attemptId": "landed", "step": 1, "turn": 1},
		map[string]any{"type": "delta", "attemptId": "landed", "chunk": map[string]any{"type": "text-delta", "text": "replaced-by-native-message"}},
		map[string]any{"type": "end", "attemptId": "landed", "outcome": map[string]any{"seq": 2}},
		map[string]any{"type": "start", "attemptId": "live", "step": 2, "turn": 1},
		map[string]any{"type": "delta", "attemptId": "live", "chunk": map[string]any{"type": "text-delta", "text": "Visible stream"}},
		map[string]any{"type": "delta", "attemptId": "live", "chunk": map[string]any{"type": "reasoning-delta", "reasoning": secret}},
		map[string]any{"type": "delta", "attemptId": "live", "chunk": map[string]any{"type": "tool-call-delta", "id": "p", "name": "pending", "argumentsDelta": secret}},
		map[string]any{"type": "delta", "attemptId": "landed", "chunk": map[string]any{"type": "tool-call-delta", "id": "r", "argumentsDelta": secret}},
		map[string]any{"type": "tool-output", "callId": "r", "text": secret},
		map[string]any{"type": "tool-output", "callId": "c", "text": secret},
	} {
		metadataFrame(t, s, out, value)
	}
	metadata, err := s.TurnMetadata(ctx, session.ID, out.TurnID, MetadataFilter{ParentCallID: "root", ArgumentKeys: []string{"description"}, MaxFieldChars: 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(metadata.Tools) != 1 || metadata.Tools[0].ID != "r" || metadata.Tools[0].ChildCount != 1 || !metadata.Tools[0].Complete || metadata.Tools[0].Args["description"] != "Visible " || len(metadata.Tools[0].Args) != 1 {
		t.Fatalf("root headers: %+v", metadata.Tools)
	}
	if len(metadata.Messages) != 1 || metadata.Messages[0].Text != "Visible message" || !metadata.Messages[0].HasReasoning || len(metadata.Attempts) != 1 || metadata.Attempts[0].ID != "live" || metadata.Attempts[0].Text != "Visible stream" || !metadata.Attempts[0].HasReasoning || len(metadata.PendingTools) != 1 || metadata.PendingTools[0].ID != "p" || metadata.Cursor.Seq != 7 || metadata.Cursor.Frame == 0 {
		t.Fatalf("visible projection: %+v", metadata)
	}
	raw, _ := json.Marshal(metadata)
	if strings.Contains(string(raw), "HIDDEN-RAW-BODY") || strings.Contains(string(raw), "replaced-by-native-message") || strings.Contains(string(raw), "private-path") || strings.Contains(string(raw), "argumentsDelta") {
		t.Fatal("metadata leaked unopened detail", string(raw))
	}
	children, err := s.TurnMetadata(ctx, session.ID, out.TurnID, MetadataFilter{ParentCallID: "r", ArgumentKeys: []string{"description"}, MaxFieldChars: 256})
	if err != nil || len(children.Tools) != 1 || children.Tools[0].ID != "c" || children.Tools[0].ChildCount != 1 || !children.Tools[0].Failed || children.Tools[0].Args["description"] != "Child description" || len(children.Messages) != 0 || len(children.Attempts) != 0 || len(children.PendingTools) != 0 || children.Cursor != metadata.Cursor {
		t.Fatalf("direct children: %+v %v", children, err)
	}
	for _, fixture := range []struct {
		filter         TraceFilter
		events, frames int
		seq            int64
	}{
		{TraceFilter{CallID: "c"}, 2, 1, 4},
		{TraceFilter{CallID: "r"}, 2, 2, 3},
		{TraceFilter{CallID: "r", FrameTypes: []string{"tool-output"}}, 2, 1, 3},
		{TraceFilter{CallID: "p"}, 0, 1, -1},
		{TraceFilter{Seq: ptrHistorySeq(2)}, 1, 0, 2},
		{TraceFilter{AttemptID: "live", ChunkTypes: []string{"reasoning-delta"}}, 0, 1, -1},
		{TraceFilter{AttemptID: "live", ChunkTypes: []string{"text-delta", "tool-call-delta"}}, 0, 2, -1},
	} {
		trace, err := s.TurnTrace(ctx, session.ID, out.TurnID, model.TraceCursor{Seq: -1}, 0, fixture.filter)
		if err != nil || len(trace.Events) != fixture.events || len(trace.Frames) != fixture.frames {
			t.Fatalf("scoped raw query %+v: %+v %v", fixture.filter, trace, err)
		}
		if fixture.seq >= 0 && trace.Events[0].Seq != fixture.seq {
			t.Fatal("wrong native record", trace.Events)
		}
	}
}

func ptrHistorySeq(value int64) *int64 { return &value }

func TestUndeliveredMetadataDoesNotReadNeighboringTurn(t *testing.T) {
	s, session := typedStore(t)
	seq := int64(1)
	historyOutbox(t, s, session, uuid.NewString(), time.Now().Add(-time.Second), &seq)
	historyRecord(t, s, session.ID, 0, "turn/start", 1000, map[string]any{})
	historyRecord(t, s, session.ID, 1, "user/message", 1100, map[string]any{})
	historyRecord(t, s, session.ID, 2, "tool/call", 1200, map[string]any{"callId": "p", "name": "old"})
	out := historyOutbox(t, s, session, uuid.NewString(), time.Now(), nil)
	if err := s.UpdateOutbox(context.Background(), out.ID, OutboxChanges{Status: ptrHistoryString("starting")}); err != nil {
		t.Fatal(err)
	}
	metadataFrame(t, s, out, map[string]any{"type": "delta", "chunk": map[string]any{"type": "tool-call-delta", "id": "p", "name": "new"}})
	metadata, err := s.TurnMetadata(context.Background(), session.ID, out.TurnID, MetadataFilter{ParentCallID: "root"})
	if err != nil || len(metadata.Tools) != 0 || len(metadata.Messages) != 0 || len(metadata.PendingTools) != 1 || metadata.PendingTools[0].Name != "new" || metadata.Cursor.Seq != -1 {
		t.Fatalf("undelivered metadata leaked neighbors: %+v %v", metadata, err)
	}
}
func ptrHistoryString(value string) *string { return &value }

func TestMetadataMalformedArgumentStringsAndEmptyArrays(t *testing.T) {
	s, session := typedStore(t)
	ctx := context.Background()
	seq := int64(1)
	out := historyOutbox(t, s, session, uuid.NewString(), time.Now(), &seq)
	historyRecord(t, s, session.ID, 0, "turn/start", 1000, map[string]any{})
	historyRecord(t, s, session.ID, 1, "user/message", 1100, map[string]any{})
	historyRecord(t, s, session.ID, 2, "tool/call", 1200, map[string]any{"callId": "parent", "name": "exec", "arguments": "{malformed provider JSON"})
	historyRecord(t, s, session.ID, 3, "tool/ptc-dispatch-start", 1300, map[string]any{"subCallId": "child", "rootCallId": "parent", "name": "child", "arguments": "{also malformed"})
	historyRecord(t, s, session.ID, 4, "tool/ptc-dispatch", 1400, map[string]any{"subCallId": "child", "rootCallId": "parent", "name": "child", "content": "not JSON"})
	metadata, err := s.TurnMetadata(ctx, session.ID, out.TurnID, MetadataFilter{ParentCallID: "root", ArgumentKeys: []string{"description", "command"}, MaxFieldChars: 256})
	if err != nil || len(metadata.Tools) != 1 || metadata.Tools[0].ID != "parent" || metadata.Tools[0].ChildCount != 1 || len(metadata.Tools[0].Args) != 0 {
		t.Fatalf("malformed provider arguments failed metadata: %+v %v", metadata, err)
	}
	if metadata.Messages == nil || metadata.Attempts == nil || metadata.PendingTools == nil || metadata.Questions == nil {
		t.Fatalf("metadata returned nil arrays: %+v", metadata)
	}
	trace, err := s.TurnTrace(ctx, session.ID, out.TurnID, model.TraceCursor{Seq: -1}, 0, TraceFilter{CallID: "parent"})
	if err != nil || len(trace.Events) != 1 || trace.Events[0].Seq != 2 {
		t.Fatal("root call filter incorrectly included rootCallId descendants", trace, err)
	}
	children, err := s.TurnMetadata(ctx, session.ID, out.TurnID, MetadataFilter{ParentCallID: "parent"})
	if err != nil || len(children.Tools) != 1 || children.Tools[0].ID != "child" || !children.Tools[0].Complete {
		t.Fatal("PTC start/end not grouped as one child", children, err)
	}
}
