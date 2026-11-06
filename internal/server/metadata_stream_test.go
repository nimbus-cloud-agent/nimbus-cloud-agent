package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
)

func TestMetadataStreamAppendsVisibleTextWithoutReplayingHiddenFrames(t *testing.T) {
	s, _, url := newStreamServer(t)
	session := createStreamSession(t, s)
	out := createStreamOutbox(t, s, session)
	ctx := context.Background()
	if err := s.S.UpdateOutbox(ctx, out.ID, store.OutboxChanges{Status: ptr("delivered"), DeliveredSeq: ptr(int64(1))}); err != nil {
		t.Fatal(err)
	}
	for i, kind := range []string{"turn/start", "user/message"} {
		if err := s.S.CreateEvent(ctx, store.Event{SessionID: session.ID, Seq: int64(i), Type: kind, Payload: model.NativeRecord{Seq: int64(i), Type: kind}}); err != nil {
			t.Fatal(err)
		}
	}
	stream := openStream(t, url+"/api/sessions/"+session.ID+"/turns/"+out.TurnID+"/stream?projection=metadata&parent_call_id=root&include_questions=false&include_outbox=false", "")
	stream.await(t, "metadata", nil)
	appendFrames := func(values ...map[string]any) {
		err := s.S.Transaction(ctx, func(tx *store.Store) error {
			for _, value := range values {
				raw, _ := json.Marshal(value)
				var payload model.FramePayload
				if err := json.Unmarshal(raw, &payload); err != nil {
					return err
				}
				if err := tx.CreateFrame(ctx, store.StreamFrame{SessionID: session.ID, TurnID: out.TurnID, Payload: payload}); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	secret := strings.Repeat("HIDDEN-ARGUMENTS-REASONING", 10000)
	appendFrames(
		map[string]any{"type": "start", "attemptId": "live", "step": 1, "turn": 1},
		map[string]any{"type": "delta", "attemptId": "live", "chunk": map[string]any{"type": "text-delta", "text": "Visible chunk"}},
		map[string]any{"type": "delta", "attemptId": "live", "chunk": map[string]any{"type": "reasoning-delta", "text": secret}},
		map[string]any{"type": "delta", "attemptId": "live", "chunk": map[string]any{"type": "tool-call-delta", "id": "pending", "name": "run_code", "argumentsDelta": secret}},
	)
	event := stream.await(t, "metadata", func(data string) bool { return strings.Contains(data, "Visible chunk") })
	var current model.TurnMetadata
	if err := json.Unmarshal([]byte(event.data), &current); err != nil || len(current.Attempts) != 1 || current.Attempts[0].Text != "Visible chunk" || !current.Attempts[0].HasReasoning || len(current.PendingTools) != 1 || current.PendingTools[0].ID != "pending" || strings.Contains(event.data, "HIDDEN-") {
		t.Fatal("metadata projection", current, err)
	}
	before := current.Cursor.Frame
	appendFrames(
		map[string]any{"type": "delta", "attemptId": "live", "chunk": map[string]any{"type": "tool-call-delta", "id": "pending", "argumentsDelta": secret}},
		map[string]any{"type": "delta", "attemptId": "live", "chunk": map[string]any{"type": "reasoning-delta", "text": secret}},
	)
	// A complete heartbeat interval observes the notification processing without
	// accepting cursor-only snapshots for invisible argument/reasoning bytes.
	for done := false; !done; {
		select {
		case event := <-stream.events:
			if event.kind == "metadata" {
				t.Fatal("hidden deltas resent unchanged visible metadata", event)
			}
			done = event.kind == "heartbeat"
		case <-time.After(7 * time.Second):
			t.Fatal("missing metadata heartbeat")
		}
	}
	appendFrames(map[string]any{"type": "delta", "attemptId": "live", "chunk": map[string]any{"type": "text-delta", "text": " next"}})
	event = stream.await(t, "metadata", nil)
	if err := json.Unmarshal([]byte(event.data), &current); err != nil || current.Attempts[0].Text != "Visible chunk next" || current.Cursor.Frame <= before+2 || strings.Contains(event.data, "HIDDEN-") {
		t.Fatal("forward frame accumulation repeated/skipped text", current, err)
	}
	// A settled native message removes the partial attempt, rather than showing
	// the same assistant output twice after the native lane overtakes the frame lane.
	raw, _ := json.Marshal(map[string]any{"seq": 2, "type": "assistant/message", "data": map[string]any{"step": 1, "turn": 1, "message": map[string]any{"content": "Settled reply"}}})
	var payload model.NativeRecord
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if err := s.S.CreateEvent(ctx, store.Event{SessionID: session.ID, Seq: 2, Type: "assistant/message", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	event = stream.await(t, "metadata", func(data string) bool { return strings.Contains(data, "Settled reply") })
	if err := json.Unmarshal([]byte(event.data), &current); err != nil || len(current.Attempts) != 0 || len(current.Messages) != 1 || current.Messages[0].Text != "Settled reply" {
		t.Fatal("settled message duplicated stream", current, err)
	}
}
