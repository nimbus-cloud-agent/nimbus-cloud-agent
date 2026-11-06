package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/logging"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
)

// Caller holds the per-session lock across provisioning and the tool operation.
func (s *Server) ensureSandbox(ctx context.Context, sid string) (model.Sandbox, error) {
	if _, deleting := s.deleting.Load(sid); deleting {
		return model.Sandbox{}, fmt.Errorf("Chat is being deleted.")
	}
	if _, err := s.getSession(ctx, sid); err != nil {
		return model.Sandbox{}, err
	}
	sb, err := s.getSandbox(ctx, sid)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return sb, err
	}
	if err == nil && sb.State == "running" {
		return sb, nil
	}
	s.provisioning.Store(sid, "Creating or resuming sandbox…")
	s.updates.publish(sid, sandboxChanged)
	defer func() { s.provisioning.Delete(sid); s.updates.publish(sid, sandboxChanged) }()
	deadline := time.Now().Add(60 * time.Second)
	for {
		s.capacity.Lock()
		count, err := s.S.RunningSandboxCount(ctx)
		if err != nil {
			s.capacity.Unlock()
			return sb, err
		}
		if count < 20 {
			break
		}
		s.capacity.Unlock()
		if time.Now().After(deadline) {
			return sb, fmt.Errorf("Timed out waiting for sandbox capacity. All 20 slots are in use; pause an idle sandbox and try again.")
		}
		select {
		case <-ctx.Done():
			return sb, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	defer s.capacity.Unlock()
	opCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	logger := logging.FromContext(ctx).With("session_id", sid, "sandbox_id", sb.E2bSandboxID)
	started := time.Now()
	logger.DebugContext(ctx, "sandbox provisioning started", "resume", sb.ID != "" && sb.State != "killed")
	var info model.SandboxInfo
	if sb.ID != "" && sb.State != "killed" {
		info, err = s.Sandboxes.Connect(opCtx, sb.E2bSandboxID)
	} else {
		info, err = s.Sandboxes.Create(opCtx, s.S.Get("e2b_template", s.C.E2BTemplate), sid)
	}
	if err != nil {
		message := s.redact(err.Error())
		logger.ErrorContext(ctx, "sandbox provisioning failed", "duration_ms", time.Since(started).Milliseconds(), "error", message)
		s.sandboxErrors.Store(sid, message)
		s.updates.publish(sid, sandboxChanged)
		if sb.ID != "" {
			_ = s.S.UpdateSandbox(context.WithoutCancel(ctx), sb.ID, store.SandboxChanges{Error: &message})
		}
		return sb, err
	}
	now := time.Now()
	if sb.ID == "" {
		sb = model.Sandbox{ID: uuid.NewString(), SessionID: sid, E2bSandboxID: info.SandboxID, State: "running", Info: info, CreatedAt: now, UpdatedAt: now, LastActiveAt: now, StartedAt: now}
		err = s.S.CreateSandbox(ctx, store.Sandbox(sb))
	} else {
		err = s.S.UpdateSandbox(ctx, sb.ID, store.SandboxChanges{E2bSandboxID: &info.SandboxID, State: ptr("running"), Info: &info, Error: ptr(""), StartedAt: &now, LastActiveAt: &now})
		if err == nil {
			sb, err = s.getSandbox(ctx, sid)
		}
	}
	if err != nil {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
		defer stop()
		_ = s.Sandboxes.Kill(cleanup, info.SandboxID)
		return sb, err
	}
	s.sandboxErrors.Delete(sid)
	logger.DebugContext(ctx, "sandbox provisioning completed", "duration_ms", time.Since(started).Milliseconds(), "e2b_sandbox_id", info.SandboxID)
	s.updates.publish(sid, sandboxChanged)
	return sb, nil
}
func (s *Server) pause(ctx context.Context, sid string) error {
	sb, err := s.getSandbox(ctx, sid)
	if err != nil {
		return fmt.Errorf("Sandbox has not been created: %w", err)
	}
	if sb.State == "paused" {
		return nil
	}
	opCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err = s.Sandboxes.Pause(opCtx, sb.E2bSandboxID); err != nil {
		return err
	}
	return s.S.UpdateSandbox(ctx, sb.ID, store.SandboxChanges{State: ptr("paused"), Error: ptr("")})
}
func (s *Server) touchSandbox(ctx context.Context, id string) error {
	return s.S.UpdateSandbox(ctx, id, store.SandboxChanges{LastActiveAt: ptr(time.Now())})
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
			rows, err := s.S.RunningSandboxes(s.ctx)
			if err != nil {
				slog.Warn("sandbox reaper database read failed", "error", s.redact(err.Error()))
				continue
			}
			for _, row := range rows {
				sb := model.Sandbox(row)
				lock := s.lock(sb.SessionID)
				if !lock.TryLock() {
					continue
				}
				func() {
					defer lock.Unlock()
					sess, err := s.getSession(s.ctx, sb.SessionID)
					if err != nil {
						return
					}
					idle := time.Since(sb.LastActiveAt) > 15*time.Minute && time.Since(sess.LastActiveAt) > 15*time.Minute && sess.AgentState == "idle"
					expired := time.Since(sb.StartedAt) > 58*time.Minute
					if idle || expired {
						_ = s.pause(s.ctx, sb.SessionID)
						return
					}
					ctx, cancel := context.WithTimeout(s.ctx, 60*time.Second)
					defer cancel()
					info, err := s.Sandboxes.Info(ctx, sb.E2bSandboxID)
					if err != nil && s.ctx.Err() == nil {
						slog.Warn("sandbox reaper provider request failed", "session_id", sb.SessionID, "sandbox_id", sb.E2bSandboxID, "error", s.redact(err.Error()))
					}
					if err == nil && (info.State == "paused" || info.State == "running" || info.State == "killed") {
						_ = s.S.UpdateSandbox(ctx, sb.ID, store.SandboxChanges{State: &info.State, Info: &info})
					}
				}()
			}
		}
	}
}

// Tool parameters have already been decoded and validated by the RPC adapter.
func (s *Server) executeTool(ctx context.Context, scope toolScope, in model.ToolCall) (result model.ToolResult, err error) {
	ctx = logging.WithAttrs(ctx, "session_id", scope.SessionID, "turn_id", scope.TurnID, "call_id", in.CallID, "tool", in.Name)
	logger := logging.FromContext(ctx)
	started := time.Now()
	logger.DebugContext(ctx, "tool call started")
	defer func() {
		if err != nil {
			level := slog.LevelError
			if errors.Is(err, context.Canceled) {
				level = slog.LevelDebug
			}
			logger.Log(ctx, level, "tool call failed", "duration_ms", time.Since(started).Milliseconds(), "error", s.redact(err.Error()))
		} else {
			logger.DebugContext(ctx, "tool call completed", "duration_ms", time.Since(started).Milliseconds())
		}
	}()
	if err := ctx.Err(); err != nil {
		return model.ToolResult{}, err
	}
	if strings.HasPrefix(in.Name, "mcp__") {
		result, err := s.callMCP(ctx, in.Name, in.MCP)
		return model.ToolResult{MCP: &result}, err
	}
	switch in.Name {
	case "sandbox_status", "sandbox_start", "sandbox_exec", "sandbox_read_file", "sandbox_write_file",
		"library_list", "library_read", "library_write", "library_versions", "library_copy_to_sandbox", "library_copy_from_sandbox":
	default:
		return model.ToolResult{}, fmt.Errorf("unknown or forbidden tool")
	}
	if in.Name == "sandbox_status" {
		sb, err := s.getSandbox(ctx, scope.SessionID)
		if errors.Is(err, store.ErrNotFound) {
			return model.ToolResult{Sandbox: &model.SandboxView{State: "not_started"}}, nil
		}
		if err != nil {
			return model.ToolResult{}, err
		}
		view := sandboxView(sb)
		return model.ToolResult{Sandbox: &view}, nil
	}
	// Library tools that need no sandbox answer without provisioning one.
	switch in.Name {
	case "library_list":
		result, err := s.libraryList(ctx)
		return model.ToolResult{Library: &result}, err
	case "library_read":
		result, err := s.libraryRead(ctx, in.Library)
		return model.ToolResult{Library: &result}, err
	case "library_write":
		result, err := s.libraryWrite(ctx, in.Library)
		return model.ToolResult{Library: &result}, err
	case "library_versions":
		result, err := s.libraryVersions(ctx, in.Library)
		return model.ToolResult{Library: &result}, err
	}
	lock := s.lock(scope.SessionID)
	lock.Lock()
	defer lock.Unlock()
	if err := ctx.Err(); err != nil {
		return model.ToolResult{}, err
	}
	sb, err := s.ensureSandbox(ctx, scope.SessionID)
	if err != nil {
		return model.ToolResult{}, err
	}
	defer s.touchSandbox(context.WithoutCancel(ctx), sb.ID)
	if in.Name == "sandbox_start" {
		view := sandboxView(sb)
		return model.ToolResult{Sandbox: &view}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	switch in.Name {
	case "sandbox_exec":
		result, err := s.Sandboxes.Exec(ctx, sb.E2bSandboxID, in.Sandbox, func(output model.CommandOutput) error {
			frame := model.StreamFrame{SessionID: scope.SessionID, TurnID: scope.TurnID, Payload: model.FramePayload{Type: "tool-output", CallID: in.CallID, Channel: output.Channel, Text: output.Text}, CreatedAt: time.Now()}
			return s.S.CreateFrame(ctx, store.StreamFrame(frame))
		})
		return model.ToolResult{Command: &result}, err
	case "sandbox_read_file":
		result, err := s.Sandboxes.ReadFile(ctx, sb.E2bSandboxID, in.Sandbox.Path)
		return model.ToolResult{ReadFile: &result}, err
	case "sandbox_write_file":
		result, err := s.Sandboxes.WriteFile(ctx, sb.E2bSandboxID, in.Sandbox.Path, in.Sandbox.Content)
		return model.ToolResult{WriteFile: &result}, err
	case "library_copy_to_sandbox":
		result, err := s.libraryCopyToSandbox(ctx, sb, in.Library)
		return model.ToolResult{Library: &result}, err
	case "library_copy_from_sandbox":
		result, err := s.libraryCopyFromSandbox(ctx, sb, in.Library)
		return model.ToolResult{Library: &result}, err
	default:
		return model.ToolResult{}, fmt.Errorf("unknown sandbox operation")
	}
}
