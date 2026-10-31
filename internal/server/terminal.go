package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
)

func (s *Server) terminal(c *gin.Context) {
	sess, ok := s.session(c)
	if !ok {
		return
	}
	sb, err := s.getSandbox(c.Request.Context(), sess.ID)
	if err != nil || sb.State != "running" {
		c.JSON(409, gin.H{"error": "请先启动沙箱"})
		return
	}
	// Shutdown cancels before taking this mutex; no terminal can register after
	// shutdown starts waiting for its active handlers.
	s.mu.Lock()
	if s.ctx.Err() != nil {
		s.mu.Unlock()
		return
	}
	s.wg.Add(1)
	s.mu.Unlock()
	defer s.wg.Done()
	up := websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 4096}
	ws, err := up.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	ws.SetReadLimit(65536)
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { _ = ws.Close() })
	defer stop()
	pty, err := s.Sandboxes.OpenTerminal(ctx, sb.E2bSandboxID)
	if err != nil {
		_ = ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
		_ = ws.WriteJSON(gin.H{"error": s.redact(err.Error())})
		return
	}
	_ = ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err = ws.WriteJSON(model.TerminalOutput{Ready: true}); err != nil {
		_ = pty.Close()
		return
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		err := pty.Read(func(data []byte) error {
			_ = ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			return ws.WriteJSON(model.TerminalOutput{Data: base64.StdEncoding.EncodeToString(data)})
		})
		if err != nil && ctx.Err() == nil {
			_ = ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			_ = ws.WriteJSON(gin.H{"error": s.redact(err.Error())})
		}
	}()
	defer func() { cancel(); _ = ws.Close(); _ = pty.Close(); <-done }()
	var lastTouch time.Time
	for {
		_, raw, err := ws.ReadMessage()
		if err != nil {
			return
		}
		var message model.TerminalMessage
		if json.Unmarshal(raw, &message) != nil {
			continue
		}
		switch message.Type {
		case "input":
			data, err := base64.StdEncoding.DecodeString(message.Data)
			if err != nil || len(data) == 0 {
				continue
			}
			if err = pty.Write(ctx, data); err != nil {
				return
			}
			// Activity is metadata, not part of the terminal byte stream. Throttle
			// it across input events so typing does not write a row per keystroke.
			if time.Since(lastTouch) >= 5*time.Second {
				if err = s.touchSandbox(ctx, sb.ID); err == nil {
					lastTouch = time.Now()
				}
			}
		case "resize":
			if err = pty.Resize(ctx, message.Cols, message.Rows); err != nil {
				return
			}
		}
	}
}

func ptr[T any](value T) *T { return &value }
