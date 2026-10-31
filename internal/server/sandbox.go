package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
	"time"
)

func (s *Server) e2b(ctx context.Context, op string, sb *store.Sandbox, args any) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	input := map[string]any{"op": op, "api_key": s.C.E2BKey, "template": s.S.Get("e2b_template", s.C.E2BTemplate), "args": args}
	if sb != nil {
		input["sandbox_id"] = sb.E2bSandboxID
	}
	return s.R.Call(ctx, "e2b", input)
}

// Caller holds the per-session lock across creation and the complete tool operation.
func (s *Server) ensureSandbox(ctx context.Context, sid string) (store.Sandbox, error) {
	var sb store.Sandbox
	if _, deleting := s.deleting.Load(sid); deleting {
		return sb, fmt.Errorf("会话正在删除")
	}
	var sess store.Session
	if err := s.S.DB.First(&sess, "id = ?", sid).Error; err != nil {
		return sb, err
	}
	e := s.S.DB.First(&sb, "session_id = ?", sid).Error
	if e == nil && sb.State == "running" {
		return sb, nil
	}
	s.provisioning.Store(sid, "正在创建 / 唤醒沙箱…")
	defer s.provisioning.Delete(sid)
	deadline := time.Now().Add(60 * time.Second)
	for {
		s.capacity.Lock()
		var count int64
		s.S.DB.Model(&store.Sandbox{}).Where("state = 'running'").Count(&count)
		if count < 20 {
			break
		}
		s.capacity.Unlock()
		if time.Now().After(deadline) {
			return sb, fmt.Errorf("等待沙箱名额超时：已有 20 台运行中，请暂停空闲沙箱")
		}
		select {
		case <-ctx.Done():
			return sb, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	defer s.capacity.Unlock()
	op := "create"
	var target *store.Sandbox
	if e == nil && sb.State != "killed" {
		op = "connect"
		target = &sb
	}
	data, err := s.e2b(ctx, op, target, map[string]any{"session_id": sid})
	if err != nil {
		s.sandboxErrors.Store(sid, s.redact(err.Error()))
		if sb.ID != "" {
			s.S.DB.Model(&sb).Updates(map[string]any{"error": s.redact(err.Error())})
		}
		return sb, err
	}
	var info struct {
		SandboxID string `json:"sandboxId"`
	}
	if err = json.Unmarshal(data, &info); err != nil || info.SandboxID == "" {
		return sb, fmt.Errorf("E2B returned no sandbox ID")
	}
	now := time.Now()
	if sb.ID == "" {
		sb = store.Sandbox{ID: uuid.NewString(), SessionID: sid, E2bSandboxID: info.SandboxID, State: "running", Info: data, CreatedAt: now, UpdatedAt: now, LastActiveAt: now, StartedAt: now}
		err = s.S.DB.Create(&sb).Error
	} else {
		err = s.S.DB.Model(&sb).Updates(map[string]any{"e2b_sandbox_id": info.SandboxID, "state": "running", "info": data, "error": "", "started_at": now, "last_active_at": now}).Error
		s.S.DB.First(&sb, "id = ?", sb.ID)
	}
	if err != nil {
		_, _ = s.e2b(context.Background(), "kill", &store.Sandbox{E2bSandboxID: info.SandboxID}, nil)
		return sb, err
	}
	s.sandboxErrors.Delete(sid)
	return sb, nil
}
func (s *Server) pause(ctx context.Context, sid string) error {
	var sb store.Sandbox
	if s.S.DB.First(&sb, "session_id = ?", sid).Error != nil {
		return fmt.Errorf("沙箱尚未创建")
	}
	if sb.State == "paused" {
		return nil
	}
	if _, e := s.e2b(ctx, "pause", &sb, nil); e != nil {
		return e
	}
	return s.S.DB.Model(&sb).Updates(map[string]any{"state": "paused", "error": ""}).Error
}
func (s *Server) reaper() {
	defer s.wg.Done()
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-tick.C:
			var boxes []store.Sandbox
			s.S.DB.Where("state = 'running'").Find(&boxes)
			for _, sb := range boxes {
				l := s.lock(sb.SessionID)
				if !l.TryLock() {
					continue
				}
				var sess store.Session
				s.S.DB.First(&sess, "id = ?", sb.SessionID)
				idle := time.Since(sb.LastActiveAt) > 15*time.Minute && time.Since(sess.LastActiveAt) > 15*time.Minute && sess.AgentState == "idle"
				expired := time.Since(sb.StartedAt) > 58*time.Minute
				if idle || expired {
					_ = s.pause(s.ctx, sb.SessionID)
				} else {
					data, e := s.e2b(s.ctx, "info", &sb, nil)
					if e == nil {
						var inf struct {
							State string `json:"state"`
						}
						json.Unmarshal(data, &inf)
						if inf.State == "paused" || inf.State == "running" || inf.State == "killed" {
							s.S.DB.Model(&sb).Updates(map[string]any{"state": inf.State, "info": data})
						}
					}
				}
				l.Unlock()
			}
		}
	}
}
func (s *Server) internalTool(c *gin.Context) {
	token := c.GetHeader("Authorization")
	s.mu.Lock()
	scope, ok := s.tokens[token]
	s.mu.Unlock()
	if !ok {
		c.AbortWithStatus(403)
		return
	}
	var in struct {
		Name   string          `json:"name"`
		Args   json.RawMessage `json:"args"`
		CallID string          `json:"call_id"`
	}
	if e := c.ShouldBindJSON(&in); e != nil || in.CallID == "" {
		s.fail(c, 400, fmt.Errorf("invalid tool call"))
		return
	}
	if len(in.Name) > 5 && in.Name[:5] == "mcp__" {
		result, e := s.callMCP(c.Request.Context(), in.Name, in.Args)
		if e != nil {
			s.fail(c, 502, e)
			return
		}
		c.Data(200, "application/json", result)
		return
	}
	switch in.Name {
	case "sandbox_status", "sandbox_start", "sandbox_exec", "sandbox_read_file", "sandbox_write_file":
	default:
		s.fail(c, 403, fmt.Errorf("unknown or forbidden tool"))
		return
	}
	if in.Name == "sandbox_status" {
		var sb store.Sandbox
		if s.S.DB.First(&sb, "session_id = ?", scope.SessionID).Error != nil {
			c.JSON(200, gin.H{"state": "not_started"})
		} else {
			c.JSON(200, sb)
		}
		return
	}
	l := s.lock(scope.SessionID)
	l.Lock()
	defer l.Unlock()
	sb, e := s.ensureSandbox(c.Request.Context(), scope.SessionID)
	if e != nil {
		s.fail(c, 502, e)
		return
	}
	defer s.S.DB.Model(&sb).Update("last_active_at", time.Now())
	if in.Name == "sandbox_start" {
		c.JSON(200, sb)
		return
	}
	op := map[string]string{"sandbox_exec": "exec", "sandbox_read_file": "read_file", "sandbox_write_file": "write_file"}[in.Name]
	var result json.RawMessage
	if op == "exec" {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
		defer cancel()
		result, e = s.R.CallStream(ctx, "e2b", map[string]any{"op": op, "api_key": s.C.E2BKey, "sandbox_id": sb.E2bSandboxID, "args": in.Args, "stream": true}, func(output json.RawMessage) error {
			var d map[string]any
			if err := json.Unmarshal(output, &d); err != nil {
				return err
			}
			d["type"] = "tool-output"
			d["callId"] = in.CallID
			return s.S.DB.Create(&store.StreamFrame{SessionID: scope.SessionID, TurnID: scope.TurnID, Payload: store.Raw(d), CreatedAt: time.Now()}).Error
		})
	} else {
		result, e = s.e2b(c.Request.Context(), op, &sb, in.Args)
	}
	if e != nil {
		s.fail(c, 502, e)
		return
	}
	c.Data(200, "application/json", result)
}
