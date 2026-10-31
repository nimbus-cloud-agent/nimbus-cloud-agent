package server

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
)

const streamHeartbeatInterval = 5 * time.Second

// Send a full snapshot on connect/reconnect, then only when the list changes.
// This includes inactive sessions and sandbox state, not just the open chat.
func (s *Server) sessionsStream(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Writer.Flush()
	changes := time.NewTicker(250 * time.Millisecond)
	heartbeat := time.NewTicker(streamHeartbeatInterval)
	defer changes.Stop()
	defer heartbeat.Stop()
	last := ""
	for {
		sessions, err := s.S.Sessions(c.Request.Context())
		if err != nil {
			return
		}
		views := make([]model.SessionView, 0, len(sessions))
		for _, session := range sessions {
			view, err := s.view(c.Request.Context(), model.Session(session))
			if err != nil {
				return
			}
			views = append(views, view)
		}
		data, err := json.Marshal(views)
		if err != nil {
			return
		}
		if string(data) != last {
			if _, err := fmt.Fprintf(c.Writer, "event: sessions\ndata: %s\n\n", data); err != nil {
				return
			}
			c.Writer.Flush()
			last = string(data)
		}
		select {
		case <-c.Request.Context().Done():
			return
		case <-s.ctx.Done():
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(c.Writer, ": heartbeat\n\n"); err != nil {
				return
			}
			c.Writer.Flush()
		case <-changes.C:
		}
	}
}
