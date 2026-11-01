package server

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/logging"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
)

func turnErrorLevel(ctx context.Context) slog.Level {
	if ctx.Err() != nil {
		return slog.LevelDebug
	}
	return slog.LevelError
}

func (s *Server) finishTurn(ctx context.Context, o model.Outbox, outcome string, turnErr error) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	log := logging.FromContext(cleanup).With("session_id", o.SessionID, "turn_id", o.TurnID)
	cleanup = logging.WithLogger(cleanup, log)
	current, err := s.getOutbox(cleanup, o.ID, o.SessionID)
	if err != nil {
		log.ErrorContext(cleanup, "turn final state read failed", "error", logging.Sanitize(err.Error()))
		return err
	}
	changes := store.OutboxChanges{}
	if current.Status != "delivered" {
		changes.Status = ptr("failed")
		if turnErr == nil {
			turnErr = fmt.Errorf("本轮未能送达 Harness")
		}
		if outcome != "interrupted" {
			outcome = "error"
		}
	}
	changes.Outcome = &outcome
	if turnErr != nil {
		changes.Error = ptr(logging.Sanitize(turnErr.Error(), s.C.E2BKey, s.C.DeepSeekKey))
	}
	err = s.S.Transaction(cleanup, func(tx *store.Store) error {
		if err := tx.UpdateOutbox(cleanup, o.ID, changes); err != nil {
			return err
		}
		return tx.UpdateSession(cleanup, o.SessionID, store.SessionChanges{AgentState: ptr("idle"), LastActiveAt: ptr(time.Now())})
	})
	if err != nil {
		log.ErrorContext(cleanup, "turn result persistence failed", "outcome", outcome, "error", logging.Sanitize(err.Error()))
		return err
	}
	// Success is observable only after the transaction has committed.
	switch {
	case turnErr != nil:
		level := slog.LevelError
		if outcome == "interrupted" {
			level = slog.LevelInfo
		}
		log.Log(cleanup, level, "turn ended with error", "outcome", outcome, "error", *changes.Error)
	case outcome == "interrupted":
		log.InfoContext(cleanup, "turn interrupted", "outcome", outcome)
	default:
		log.DebugContext(cleanup, "turn completed", "outcome", outcome)
	}
	return nil
}
