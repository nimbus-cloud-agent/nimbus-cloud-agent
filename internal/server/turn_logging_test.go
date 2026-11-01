package server

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/logging"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store/testdb"
)

func TestTurnCompletionLogsOnlyAfterSuccessfulCommit(t *testing.T) {
	for _, reject := range []bool{false, true} {
		name := "committed"
		if reject {
			name = "rolled_back"
		}
		t.Run(name, func(t *testing.T) {
			db, probe := testdb.OpenWithProbe(t)
			ctx := context.Background()
			session := store.Session{ID: uuid.NewString(), Title: "fixture", AgentState: "running", SyncedSeq: -1, Options: model.DefaultOptions()}
			if err := db.CreateSession(ctx, session); err != nil {
				t.Fatal(err)
			}
			row := store.Outbox{ID: uuid.NewString(), SessionID: session.ID, TurnID: uuid.NewString(), ClientMsgID: uuid.NewString(), Content: "private-turn-input", Options: session.Options, Images: []model.MessageImage{}, Status: "delivered", DeliveredSeq: ptr(int64(0))}
			if err := db.CreateOutbox(ctx, row); err != nil {
				t.Fatal(err)
			}
			if reject {
				if err := probe.RejectCompletedTurns(ctx); err != nil {
					t.Fatal(err)
				}
			}
			var out bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(logging.NewHandler(&out, slog.LevelDebug)))
			defer slog.SetDefault(previous)
			s := &Server{S: db}
			err := s.finishTurn(ctx, model.Outbox(row), "completed", nil)
			text := out.String()
			records, readErr := db.Outboxes(ctx, store.OutboxFilter{SessionID: session.ID})
			if readErr != nil || len(records) != 1 {
				t.Fatal(records, readErr)
			}
			if reject {
				if err == nil || strings.Contains(text, "turn completed") || !strings.Contains(text, "turn result persistence failed") || records[0].Outcome != "" {
					t.Fatal(text, err, records)
				}
			} else {
				if err != nil || !strings.Contains(text, "turn completed") || records[0].Outcome != "completed" {
					t.Fatal(text, err, records)
				}
			}
			if strings.Contains(text, "private-turn-input") {
				t.Fatal("logged turn content")
			}
		})
	}
}
