package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
)

const streamHeartbeatInterval = 5 * time.Second
const streamPageSize = 1000

func beginStream(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Writer.Flush()
}
func writeStream(c *gin.Context, event string, id *int64, data []byte) error {
	// Bound a slow client's write without imposing a lifetime on the SSE request.
	_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Now().Add(10 * time.Second))
	if id != nil {
		if _, err := fmt.Fprintf(c.Writer, "id: %d\n", *id); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, data)
	return err
}
func writeEvent[T any](c *gin.Context, event string, id *int64, value T) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return writeStream(c, event, id, data)
}
func writeChanged[T any](c *gin.Context, event string, value T, last *string) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if string(data) == *last {
		return nil
	}
	if err = writeStream(c, event, nil, data); err != nil {
		return err
	}
	*last = string(data)
	return nil
}
func writeHeartbeat(c *gin.Context) error {
	_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err := fmt.Fprint(c.Writer, ": heartbeat\n\n")
	c.Writer.Flush()
	return err
}

// Subscribe before the initial read. Notifications received during that read
// remain pending. LISTEN reconnects invalidate all subscribers for catch-up.
func (s *Server) sessionsStream(c *gin.Context) {
	updates := s.updates.subscribe("", sessionChanged|sandboxChanged)
	defer updates.close()
	beginStream(c)
	last := ""
	for signal := range updates.signals(c.Request.Context(), streamHeartbeatInterval) {
		if signal.Heartbeat {
			if writeHeartbeat(c) != nil {
				return
			}
			continue
		}
		rows, err := s.S.Sessions(c.Request.Context())
		if err != nil {
			return
		}
		views := make([]model.SessionView, 0, len(rows))
		for _, row := range rows {
			view, err := s.view(c.Request.Context(), model.Session(row))
			if err != nil {
				return
			}
			views = append(views, view)
		}
		if writeChanged(c, "sessions", views, &last) != nil {
			return
		}
		c.Writer.Flush()
	}
}

type streamCursor struct {
	after                    int64
	frameAfter               int64
	state, outbox, questions string
}

func (s *Server) stream(c *gin.Context) {
	session, ok := s.session(c)
	if !ok {
		return
	}
	cursor := streamCursor{after: -1}
	value := c.GetHeader("Last-Event-ID")
	if value == "" {
		value = c.Query("after_seq")
	}
	if after, err := strconv.ParseInt(value, 10, 64); err == nil {
		cursor.after = after
	}
	updates := s.updates.subscribe(session.ID, allChanges)
	defer updates.close()
	beginStream(c)
	for signal := range updates.signals(c.Request.Context(), streamHeartbeatInterval) {
		if signal.Heartbeat {
			if writeHeartbeat(c) != nil {
				return
			}
			continue
		}
		if s.flushSession(c, session.ID, &cursor, signal.Changed) != nil {
			return
		}
		c.Writer.Flush()
	}
}
func (s *Server) flushSession(c *gin.Context, sid string, cursor *streamCursor, changed changeMask) error {
	ctx := c.Request.Context()
	if changed&eventsChanged != 0 {
		for {
			events, err := s.events(ctx, store.EventFilter{SessionID: sid, After: cursor.after, Limit: streamPageSize})
			if err != nil {
				return err
			}
			for _, event := range events {
				if err := writeEvent(c, "session", &event.Seq, event); err != nil {
					return err
				}
				cursor.after = event.Seq
			}
			if len(events) < streamPageSize {
				break
			}
			c.Writer.Flush()
		}
	}
	if changed&(sessionChanged|sandboxChanged) != 0 {
		session, err := s.getSession(ctx, sid)
		if err != nil {
			return err
		}
		view, err := s.view(ctx, session)
		if err != nil {
			return err
		}
		if err := writeChanged(c, "state", view, &cursor.state); err != nil {
			return err
		}
	}
	if changed&outboxChanged != 0 {
		outbox, err := s.outboxes(ctx, store.OutboxFilter{SessionID: sid})
		if err != nil {
			return err
		}
		if err := writeChanged(c, "outbox", outbox, &cursor.outbox); err != nil {
			return err
		}
	}
	if changed&questionsChanged != 0 {
		questions, err := s.questions(ctx, sid)
		if err != nil {
			return err
		}
		if err := writeChanged(c, "questions", questions, &cursor.questions); err != nil {
			return err
		}
	}
	if changed&framesChanged != 0 {
		for {
			frames, err := s.frames(ctx, store.FrameFilter{SessionID: sid, After: cursor.frameAfter, Limit: streamPageSize})
			if err != nil {
				return err
			}
			for _, frame := range frames {
				if err := writeEvent(c, "frame", nil, frame); err != nil {
					return err
				}
				cursor.frameAfter = frame.ID
			}
			if len(frames) < streamPageSize {
				break
			}
			c.Writer.Flush()
		}
	}
	return nil
}
