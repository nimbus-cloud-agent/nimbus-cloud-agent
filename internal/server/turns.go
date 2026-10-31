package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
	"os"
	"path/filepath"
	"time"
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
			var queue []store.Outbox
			if s.S.DB.Where("status = 'queued'").Order("created_at").Find(&queue).Error != nil {
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
				go func(o store.Outbox) {
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
func (s *Server) runTurn(ctx context.Context, o store.Outbox) {
	var sess store.Session
	if s.S.DB.First(&sess, "id = ?", o.SessionID).Error != nil {
		return
	}
	if e := s.S.DB.Model(&o).Updates(map[string]any{"status": "starting", "error": ""}).Error; e != nil {
		return
	}
	s.S.DB.Model(&sess).Update("agent_state", "starting")
	outcome := "completed"
	var turnErr error
	defer func() {
		if ctx.Err() != nil {
			outcome = "interrupted"
		}
		if turnErr != nil && outcome != "interrupted" {
			outcome = "error"
		}
		var current store.Outbox
		s.S.DB.First(&current, "id = ?", o.ID)
		updates := map[string]any{"outcome": outcome}
		if current.Status != "delivered" {
			updates["status"] = "failed"
			if turnErr == nil {
				turnErr = fmt.Errorf("本轮未能送达 Harness")
			}
		}
		if turnErr != nil {
			updates["error"] = s.redact(turnErr.Error())
		}
		s.S.DB.Model(&o).Updates(updates)
		s.S.DB.Model(&sess).Updates(map[string]any{"agent_state": "idle", "last_active_at": time.Now()})
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
	events := []store.Event{}
	if turnErr = s.S.DB.Where("session_id = ?", o.SessionID).Order("seq").Find(&events).Error; turnErr != nil {
		return
	}
	raw := []json.RawMessage{}
	for _, e := range events {
		raw = append(raw, e.Payload)
	}
	skills := []store.Skill{}
	s.S.DB.Preload("Files").Where("enabled = true").Find(&skills)
	mcps := []store.MCPServer{}
	s.S.DB.Where("enabled = true").Find(&mcps)
	token := uuid.NewString() + uuid.NewString()
	s.mu.Lock()
	s.tokens[token] = tokenScope{o.SessionID, o.TurnID}
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.tokens, token); s.mu.Unlock() }()
	recovery := s.interruptedFragments(o.SessionID)
	input := map[string]any{"recovery": recovery, "session_id": o.SessionID, "turn_id": o.TurnID, "message_id": o.ClientMsgID, "message": o.Content, "options": o.Options, "header": sess.Header, "records": raw, "work_dir": dir, "api_key": s.S.Get("deepseek_api_key", s.C.DeepSeekKey), "harness_path": s.C.HarnessPath, "skills": skills, "tools_url": s.BaseURL + "/internal/tools", "token": token, "mcp": s.mcpTools(ctx, mcps)}
	input["session_mode"] = *sess.LockedMode
	input["images"] = o.Images
	var images []store.Image
	if turnErr = s.S.DB.Where("session_id=? AND id IN (SELECT (jsonb_array_elements(images)->>'id')::uuid FROM outbox_messages WHERE session_id=? AND (status='delivered' OR id=?))", o.SessionID, o.SessionID, o.ID).Find(&images).Error; turnErr != nil {
		return
	}
	attachments := make([]map[string]any, 0, len(images))
	for _, img := range images {
		attachments = append(attachments, map[string]any{"ref": img.Ref, "data": img.Data})
	}
	input["attachments"] = attachments
	turnErr = s.R.Turn(ctx, input, func(kind string, data json.RawMessage) error {
		switch kind {
		case "header":
			return s.S.DB.Model(&sess).Update("header", data).Error
		case "ready":
			return s.S.DB.Model(&sess).Update("agent_state", "running").Error
		case "frame":
			return s.S.DB.Create(&store.StreamFrame{SessionID: o.SessionID, TurnID: o.TurnID, Payload: data, CreatedAt: time.Now()}).Error
		case "record":
			if e := s.S.Append(o.SessionID, o.ID, data); e != nil {
				return e
			}
			var ev struct {
				Type string `json:"type"`
			}
			json.Unmarshal(data, &ev)
			if ev.Type == "user/message" && sess.Title == "新对话" {
				title := []rune(o.Content)
				if len(title) == 0 {
					title = []rune("图片对话")
				}
				s.S.DB.Model(&sess).Update("title", string(title[:min(40, len(title))]))
			}
			return nil
		case "flushing":
			return s.S.DB.Model(&sess).Update("agent_state", "flushing").Error
		case "done":
			var d struct {
				Outcome string `json:"outcome"`
				Error   string `json:"error"`
				Seq     int64  `json:"seq"`
			}
			json.Unmarshal(data, &d)
			var latest store.Session
			if e := s.S.DB.First(&latest, "id = ?", o.SessionID).Error; e != nil {
				return e
			}
			if latest.SyncedSeq != d.Seq {
				return fmt.Errorf("history synchronization did not catch up")
			}
			if d.Outcome != "" {
				outcome = d.Outcome
			}
			if d.Error != "" {
				return fmt.Errorf("%s", d.Error)
			}
		case "error":
			var msg string
			json.Unmarshal(data, &msg)
			return fmt.Errorf("%s", msg)
		}
		return nil
	})
}

// Preserve the visible prefix of a crashed model attempt as explicit context.
// Original DSH records remain untouched; native resume still closes the dead turn.
func (s *Server) interruptedFragments(sid string) string {
	var turns []store.Outbox
	s.S.DB.Where("session_id = ? AND outcome = 'interrupted'", sid).Find(&turns)
	var result string
	for _, o := range turns {
		var frames []store.StreamFrame
		s.S.DB.Where("turn_id = ?", o.TurnID).Order("id").Find(&frames)
		texts := map[string]string{}
		closed := map[string]bool{}
		order := []string{}
		for _, f := range frames {
			var v struct {
				Type    string `json:"type"`
				Attempt string `json:"attemptId"`
				Chunk   struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"chunk"`
			}
			json.Unmarshal(f.Payload, &v)
			if _, ok := texts[v.Attempt]; !ok {
				order = append(order, v.Attempt)
				texts[v.Attempt] = ""
			}
			if v.Type == "end" {
				closed[v.Attempt] = true
			}
			if v.Chunk.Type == "text-delta" || v.Chunk.Type == "reasoning-delta" {
				texts[v.Attempt] += v.Chunk.Text
			}
		}
		for _, id := range order {
			if !closed[id] && texts[id] != "" {
				result += "\nInterrupted response to: " + o.Content + "\nAlready displayed before backend crash (partial, not a completed response):\n" + texts[id] + "\n"
			}
		}
	}
	return result
}
