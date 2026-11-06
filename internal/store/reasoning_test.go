package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func assistantRecord(id string, blocks ...any) map[string]any {
	return map[string]any{
		"message": map[string]any{
			"id":   id,
			"role": "assistant",
			"source": map[string]any{
				"kind": "model", "provider": "deepseek-official", "model": "deepseek-flash",
			},
			"content": blocks,
		},
	}
}

func TestTurnSummaryReportsReasoning(t *testing.T) {
	s, session := typedStore(t)
	ctx := context.Background()
	// Each turn ends before the next turn/start, so their summaries stay apart.
	first := int64(2)
	reasoning := historyOutbox(t, s, session, uuid.NewString(), time.Now(), &first)
	historyRecord(t, s, session.ID, 1, "turn/start", 1000, map[string]any{})
	historyRecord(t, s, session.ID, 2, "assistant/message", 2000, assistantRecord(uuid.NewString(),
		map[string]any{"type": "reasoning", "text": "thinking"},
		map[string]any{"type": "text", "text": "answer"},
	))
	second := int64(4)
	plain := historyOutbox(t, s, session, uuid.NewString(), time.Now().Add(time.Second), &second)
	historyRecord(t, s, session.ID, 3, "turn/start", 3000, map[string]any{})
	historyRecord(t, s, session.ID, 4, "assistant/message", 4000, assistantRecord(uuid.NewString(),
		map[string]any{"type": "text", "text": "answer"},
	))
	summary, err := s.turnSummary(ctx, reasoning)
	if err != nil {
		t.Fatal(err)
	}
	if !summary.HasReasoning {
		t.Fatalf("reasoning was not reported: %+v", summary)
	}
	summary, err = s.turnSummary(ctx, plain)
	if err != nil {
		t.Fatal(err)
	}
	if summary.HasReasoning {
		t.Fatalf("text-only turn claimed reasoning: %+v", summary)
	}
}
