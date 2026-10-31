package server

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
	"sync"
	"time"
)

func (s *Server) terminal(c *gin.Context) {
	sess, ok := s.session(c)
	if !ok {
		return
	}
	var sb store.Sandbox
	if s.S.DB.First(&sb, "session_id = ? AND state = 'running'", sess.ID).Error != nil {
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
	cmd := s.R.Command(ctx, "terminal")
	in, e := cmd.StdinPipe()
	if e != nil {
		return
	}
	out, e := cmd.StdoutPipe()
	if e != nil {
		return
	}
	if e = cmd.Start(); e != nil {
		return
	}
	defer cmd.Wait()
	defer cmd.Process.Kill()
	defer in.Close()
	enc := json.NewEncoder(in)
	enc.Encode(map[string]any{"api_key": s.C.E2BKey, "sandbox_id": sb.E2bSandboxID})
	var writeMu sync.Mutex
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		scan := bufio.NewScanner(out)
		scan.Buffer(make([]byte, 4096), 2<<20)
		for scan.Scan() {
			writeMu.Lock()
			_ = ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			e := ws.WriteMessage(websocket.TextMessage, scan.Bytes())
			writeMu.Unlock()
			if e != nil {
				return
			}
		}
		ws.Close()
	}()
	for {
		_, b, e := ws.ReadMessage()
		if e != nil {
			break
		}
		var m struct {
			Type string `json:"type"`
			Data string `json:"data"`
			Cols int    `json:"cols"`
			Rows int    `json:"rows"`
		}
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		if m.Type == "input" {
			if _, e = base64.StdEncoding.DecodeString(m.Data); e != nil {
				continue
			}
			s.S.DB.Model(&sb).Update("last_active_at", time.Now())
		}
		if e = enc.Encode(m); e != nil {
			break
		}
	}
	in.Close()
	// Let the supervisor kill its remote PTY before terminating the process.
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		cancel()
		<-done
	}
}
