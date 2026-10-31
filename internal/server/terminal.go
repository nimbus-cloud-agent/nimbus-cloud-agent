package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/executor"
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
	up := websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 4096}
	ws, e := up.Upgrade(c.Writer, c.Request, nil)
	if e != nil {
		return
	}
	defer ws.Close()
	ws.SetReadLimit(65536)
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	id := uuid.NewString()
	ready, done := make(chan struct{}), make(chan struct{})
	var once sync.Once
	go func() {
		defer close(done)
		defer cancel()
		defer ws.Close()
		err := s.R.Terminal(ctx, executor.TerminalInput{TerminalID: id, APIKey: s.C.E2BKey, SandboxID: sb.E2bSandboxID}, func(data model.TerminalOutput) error {
			once.Do(func() { close(ready) })
			_ = ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			return ws.WriteJSON(data)
		})
		if err != nil && ctx.Err() == nil {
			_ = ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			_ = ws.WriteJSON(gin.H{"error": s.redact(err.Error())})
		}
	}()
	for {
		_, b, err := ws.ReadMessage()
		if err != nil {
			break
		}
		var m model.TerminalMessage
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		if m.Type == "input" {
			if _, err = base64.StdEncoding.DecodeString(m.Data); err != nil {
				continue
			}
			s.touchSandbox(ctx, sb.ID)
		} else if m.Type != "resize" {
			continue
		}
		select {
		case <-ready:
		case <-ctx.Done():
			return
		}
		if err = s.R.TerminalWrite(ctx, id, m); err != nil {
			break
		}
	}
	cancel()
	<-done
}

func ptr[T any](value T) *T { return &value }
