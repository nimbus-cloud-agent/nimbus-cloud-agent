package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
)

func usageFrame(t *testing.T, s *Store, sid, tid string, input, cacheRead, output, total int64) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"type": "chunk",
		"chunk": map[string]any{"type": "usage", "usage": map[string]any{
			"inputTokens": input, "cacheReadTokens": cacheRead,
			"cacheWriteTokens": 0, "outputTokens": output, "totalTokens": total,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload model.FramePayload
	if err = json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if err = s.CreateFrame(context.Background(), StreamFrame{SessionID: sid, TurnID: tid, Payload: payload, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
}

func TestTurnSummaryAggregatesUsageFrames(t *testing.T) {
	s, session := typedStore(t)
	seq := int64(1)
	out := historyOutbox(t, s, session, uuid.NewString(), time.Now(), &seq)
	usageFrame(t, s, session.ID, out.TurnID, 10, 88, 2, 100)
	usageFrame(t, s, session.ID, out.TurnID, 20, 0, 5, 25)
	other := historyOutbox(t, s, session, uuid.NewString(), time.Now(), nil)
	usageFrame(t, s, session.ID, other.TurnID, 999, 999, 999, 999)
	summary, err := s.turnSummary(context.Background(), out)
	if err != nil {
		t.Fatal(err)
	}
	want := model.TokenUsage{InputTokens: 30, CacheReadTokens: 88, OutputTokens: 7, TotalTokens: 125, Calls: 2}
	if summary.Usage == nil || *summary.Usage != want {
		t.Fatalf("usage = %+v, want %+v", summary.Usage, want)
	}
}
