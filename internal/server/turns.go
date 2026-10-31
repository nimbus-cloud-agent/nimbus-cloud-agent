package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/executor"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
)

func (s *Server) scheduler() {
	defer s.wg.Done()
	t := time.NewTicker(200 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
			queue, err := s.outboxes(s.ctx, store.OutboxFilter{Status: "queued"})
			if err != nil {
				continue
			}
			for _, o := range queue {
				s.mu.Lock()
				if _, deleting := s.deleting.Load(o.SessionID); deleting {
					s.mu.Unlock()
					continue
				}
				if s.running[o.SessionID] != nil {
					s.mu.Unlock()
					continue
				}
				ctx, cancel := context.WithCancel(s.ctx)
				run := &runningTurn{cancel: cancel, done: make(chan struct{})}
				s.running[o.SessionID] = run
				s.mu.Unlock()
				s.wg.Add(1)
				go func(o model.Outbox) {
					defer s.wg.Done()
					defer cancel()
					defer close(run.done)
					s.runTurn(ctx, o)
					s.mu.Lock()
					delete(s.running, o.SessionID)
					s.mu.Unlock()
				}(o)
			}
		}
	}
}
func (s *Server) runTurn(ctx context.Context, o model.Outbox) {
	sess, err := s.getSession(ctx, o.SessionID)
	if err != nil {
		return
	}
	if err = s.S.UpdateOutbox(ctx, o.ID, store.OutboxChanges{Status: ptr("starting"), Error: ptr("")}); err != nil {
		return
	}
	_ = s.S.UpdateSession(ctx, sess.ID, store.SessionChanges{AgentState: ptr("starting")})
	outcome := "completed"
	var turnErr error
	defer func() {
		cleanup := context.WithoutCancel(ctx)
		if ctx.Err() != nil || errors.Is(turnErr, executor.ErrRuntimeLost) {
			outcome = "interrupted"
		}
		if turnErr != nil && outcome != "interrupted" {
			outcome = "error"
		}
		current, err := s.getOutbox(cleanup, o.ID, o.SessionID)
		if err != nil {
			return
		}
		changes := store.OutboxChanges{Outcome: &outcome}
		if current.Status != "delivered" {
			changes.Status = ptr("failed")
			if turnErr == nil {
				turnErr = fmt.Errorf("本轮未能送达 Harness")
			}
		}
		if turnErr != nil {
			changes.Error = ptr(s.redact(turnErr.Error()))
		}
		_ = s.S.Transaction(cleanup, func(tx *store.Store) error {
			if err := tx.UpdateOutbox(cleanup, o.ID, changes); err != nil {
				return err
			}
			return tx.UpdateSession(cleanup, sess.ID, store.SessionChanges{AgentState: ptr("idle"), LastActiveAt: ptr(time.Now())})
		})
	}()

	if sess.LockedMode == nil {
		turnErr = fmt.Errorf("session preset was not initialized")
		return
	}
	if turnErr = checkPreset(sess, o.Options); turnErr != nil {
		return
	}
	if turnErr = validOptions(o.Options); turnErr != nil {
		return
	}
	dir := filepath.Join(s.C.WorkDir, o.SessionID, o.TurnID)
	if _, e := os.Stat(dir); e == nil {
		dir = filepath.Join(dir, "retry-"+uuid.NewString())
	}
	if turnErr = os.MkdirAll(dir, 0700); turnErr != nil {
		return
	}
	dir, _ = filepath.Abs(dir)
	events, err := s.events(ctx, store.EventFilter{SessionID: o.SessionID, After: -1})
	if err != nil {
		turnErr = err
		return
	}
	records := make([]model.NativeRecord, 0, len(events))
	for _, event := range events {
		records = append(records, event.Payload)
	}
	skillRows, err := s.S.Skills(ctx, true)
	if err != nil {
		turnErr = err
		return
	}
	skills := make([]model.Skill, 0, len(skillRows))
	for _, row := range skillRows {
		skills = append(skills, skillModel(row))
	}
	mcpRows, err := s.S.MCPServers(ctx, true)
	if err != nil {
		turnErr = err
		return
	}
	mcps := make([]model.MCPServer, 0, len(mcpRows))
	for _, row := range mcpRows {
		mcps = append(mcps, model.MCPServer(row))
	}
	recovery, err := s.interruptedFragments(ctx, o.SessionID)
	if err != nil {
		turnErr = err
		return
	}
	imageRows, err := s.S.TurnImages(ctx, o.SessionID, o.ID)
	if err != nil {
		turnErr = err
		return
	}
	attachments := make([]model.Attachment, 0, len(imageRows))
	for _, row := range imageRows {
		img := model.Image(row)
		attachments = append(attachments, model.Attachment{Ref: img.Ref, Data: img.Data})
	}
	input := executor.TurnInput{SessionID: o.SessionID, TurnID: o.TurnID, MessageID: o.ClientMsgID, Message: o.Content, SessionMode: *sess.LockedMode, Options: o.Options, Header: sess.Header, Records: records, WorkDir: dir, APIKey: s.S.Get("deepseek_api_key", s.C.DeepSeekKey), Skills: skills, MCP: s.mcpTools(ctx, mcps), Images: o.Images, Attachments: attachments, Recovery: recovery}
	turnErr = s.R.Turn(ctx, input, func(event model.TurnEvent) error {
		// Native cancellation may flush history after the original request context has
		// been cancelled. Its durable acknowledgements still finish before scope exit.
		persist := context.WithoutCancel(ctx)
		switch event.Kind {
		case "header":
			return s.S.UpdateSession(persist, sess.ID, store.SessionChanges{Header: event.Header})
		case "ready":
			return s.S.UpdateSession(persist, sess.ID, store.SessionChanges{AgentState: ptr("running")})
		case "frame":
			return s.S.CreateFrame(persist, store.StreamFrame{SessionID: o.SessionID, TurnID: o.TurnID, Payload: *event.Frame, CreatedAt: time.Now()})
		case "record":
			if err := s.appendRecord(persist, o.SessionID, o.ID, *event.Record); err != nil {
				return err
			}
			if event.Record.Type == "user/message" && sess.Title == "新对话" {
				title := []rune(o.Content)
				if len(title) == 0 {
					title = []rune("图片对话")
				}
				return s.S.UpdateSession(persist, sess.ID, store.SessionChanges{Title: ptr(string(title[:min(40, len(title))]))})
			}
		case "flushing":
			return s.S.UpdateSession(persist, sess.ID, store.SessionChanges{AgentState: ptr("flushing")})
		case "done":
			latest, err := s.getSession(persist, o.SessionID)
			if err != nil {
				return err
			}
			if latest.SyncedSeq != event.Done.Seq {
				return fmt.Errorf("history synchronization did not catch up")
			}
			if event.Done.Outcome != "" {
				outcome = event.Done.Outcome
			}
			if event.Done.Error != "" {
				return fmt.Errorf("%s", event.Done.Error)
			}
		}
		return nil
	}, func(opCtx context.Context, request model.QuestionRequest) (model.QuestionAnswer, error) {
		return s.waitQuestion(opCtx, o, request)
	}, func(opCtx context.Context, call model.ToolCall) (model.ToolResult, error) {
		result, err := s.executeTool(opCtx, toolScope{o.SessionID, o.TurnID}, call)
		if err != nil {
			return result, fmt.Errorf("%s", s.redact(err.Error()))
		}
		return result, nil
	})
}

// The application combines DAO operations while Store owns row locking, SQL and
// serialization. Acknowledgement follows this complete atomic business update.
func (s *Server) appendRecord(ctx context.Context, sid, oid string, record model.NativeRecord) error {
	return s.S.Transaction(ctx, func(tx *store.Store) error {
		row, err := tx.LockSession(ctx, sid)
		if err != nil {
			return err
		}
		sess := model.Session(row)
		if record.Seq <= sess.SyncedSeq {
			return nil
		}
		if record.Seq != sess.SyncedSeq+1 {
			return fmt.Errorf("non-contiguous Harness seq: got %d, expected %d", record.Seq, sess.SyncedSeq+1)
		}
		event := model.Event{SessionID: sid, Seq: record.Seq, Type: record.Type, Payload: record, CreatedAt: time.Now()}
		if err = tx.CreateEvent(ctx, store.Event(event)); err != nil {
			return err
		}
		changes := store.SessionChanges{SyncedSeq: &record.Seq, LastActiveAt: ptr(time.Now())}
		if record.Type == "user/message" {
			out, err := tx.Outbox(ctx, oid, sid)
			if err != nil {
				return err
			}
			if out.Status == "starting" {
				if err = tx.UpdateOutbox(ctx, oid, store.OutboxChanges{Status: ptr("delivered"), DeliveredSeq: &record.Seq}); err != nil {
					return err
				}
			}
		}
		if record.Type == "plan/mode" && record.Data.Active != nil && !*record.Data.Active {
			options := sess.Options
			options.Plan = false
			changes.Options = &options
		}
		if err = tx.UpdateSession(ctx, sid, changes); err != nil {
			return err
		}
		return tx.NotifySession(ctx, sid)
	})
}

// Preserve the visible prefix of a crashed model attempt as explicit context.
// Original DSH records remain untouched; native resume still closes the dead turn.
func (s *Server) interruptedFragments(ctx context.Context, sid string) (string, error) {
	turns, err := s.outboxes(ctx, store.OutboxFilter{SessionID: sid, Outcome: "interrupted"})
	if err != nil {
		return "", err
	}
	var result string
	for _, o := range turns {
		frames, err := s.frames(ctx, store.FrameFilter{TurnID: o.TurnID})
		if err != nil {
			return "", err
		}
		texts := map[string]string{}
		closed := map[string]bool{}
		order := []string{}
		for _, f := range frames {
			v := f.Payload
			if _, ok := texts[v.AttemptID]; !ok {
				order = append(order, v.AttemptID)
				texts[v.AttemptID] = ""
			}
			if v.Type == "end" {
				closed[v.AttemptID] = true
			}
			if v.Chunk != nil && (v.Chunk.Type == "text-delta" || v.Chunk.Type == "reasoning-delta") {
				texts[v.AttemptID] += v.Chunk.Text
			}
		}
		for _, id := range order {
			if !closed[id] && texts[id] != "" {
				result += "\nInterrupted response to: " + o.Content + "\nAlready displayed before backend crash (partial, not a completed response):\n" + texts[id] + "\n"
			}
		}
	}
	return result, nil
}
