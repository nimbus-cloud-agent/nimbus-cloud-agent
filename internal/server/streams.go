package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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
func writeStream(c *gin.Context, event string, id *string, data []byte) error {
	// Bound a slow client's write without imposing a lifetime on the SSE request.
	_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Now().Add(10 * time.Second))
	if id != nil {
		if _, err := fmt.Fprintf(c.Writer, "id: %s\n", *id); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, data)
	return err
}
func writeEvent[T any](c *gin.Context, event string, id *string, value T) error {
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
	since                           string
	state, outbox, turns, questions string
	omitQuestionContent             bool
}

func (s *Server) stream(c *gin.Context) {
	session, ok := s.session(c)
	if !ok {
		return
	}
	cursor := streamCursor{since: c.Query("since_turn"), omitQuestionContent: c.Query("include_question_content") == "false"}
	if cursor.since != "" {
		if _, valid := s.validHistoryAnchor(c, session.ID, cursor.since); !valid {
			return
		}
	}
	// Raw frames never invalidate the conversation projection.
	updates := s.updates.subscribe(session.ID, sessionChanged|sandboxChanged|outboxChanged|questionsChanged|eventsChanged)
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
	if changed&(outboxChanged|questionsChanged|eventsChanged) != 0 {
		page, err := s.S.HistoryPage(ctx, store.HistoryFilter{SessionID: sid, SinceTurn: cursor.since, Limit: 10, OmitQuestionContent: cursor.omitQuestionContent})
		if err != nil {
			return err
		}
		if cursor.since == "" && len(page.Outbox) > 0 {
			cursor.since = page.Outbox[0].TurnID
		}
		history := historyModel(page)
		if err := writeChanged(c, "outbox", history.Outbox, &cursor.outbox); err != nil {
			return err
		}
		if err := writeChanged(c, "turns", history.Turns, &cursor.turns); err != nil {
			return err
		}
		if err := writeChanged(c, "questions", history.Questions, &cursor.questions); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) validHistoryAnchor(c *gin.Context, sid, tid string) (store.Outbox, bool) {
	if _, err := uuid.Parse(tid); err != nil {
		s.fail(c, 400, fmt.Errorf("Invalid since_turn."))
		return store.Outbox{}, false
	}
	row, err := s.S.TurnOutbox(c.Request.Context(), sid, tid, false)
	if err != nil {
		s.historyError(c, err)
		return store.Outbox{}, false
	}
	return row, true
}

func detailCursor(c *gin.Context) (model.TraceCursor, error) {
	cursor := model.TraceCursor{Seq: -1}
	seq, frame := c.Query("after_seq"), c.Query("after_frame")
	if last := c.GetHeader("Last-Event-ID"); last != "" {
		parts := strings.Split(last, ":")
		if len(parts) != 2 {
			return cursor, fmt.Errorf("Invalid detail stream cursor.")
		}
		seq, frame = parts[0], parts[1]
	}
	if seq != "" {
		value, err := strconv.ParseInt(seq, 10, 64)
		if err != nil || value < -1 {
			return cursor, fmt.Errorf("Invalid event cursor.")
		}
		cursor.Seq = value
	}
	if frame != "" {
		value, err := strconv.ParseInt(frame, 10, 64)
		if err != nil || value < 0 {
			return cursor, fmt.Errorf("Invalid frame cursor.")
		}
		cursor.Frame = value
	}
	return cursor, nil
}

func (s *Server) turnStream(c *gin.Context) {
	session, ok := s.session(c)
	if !ok {
		return
	}
	out, ok := s.scopedTurn(c, session.ID)
	if !ok {
		return
	}
	query, err := parseTurnQuery(c)
	if err != nil {
		s.fail(c, 400, err)
		return
	}
	cursor, err := detailCursor(c)
	if err != nil {
		s.fail(c, 400, err)
		return
	}
	if query.headers {
		s.turnHeaderStream(c, session.ID, out.TurnID, query.header, cursor)
		return
	}
	if query.metadata {
		s.turnMetadataStream(c, session.ID, out.TurnID, query.fields)
		return
	}
	// Subscribe before the first snapshot so writes overlapping it are replayed.
	mask := eventsChanged | framesChanged | outboxChanged
	if !query.trace.OmitQuestions {
		mask |= questionsChanged
	}
	updates := s.updates.subscribe(session.ID, mask)
	defer updates.close()
	beginStream(c)
	lastQuestions, lastOutbox := "", ""
	for signal := range updates.signals(c.Request.Context(), streamHeartbeatInterval) {
		if signal.Heartbeat {
			if writeHeartbeat(c) != nil {
				return
			}
			continue
		}
		for {
			trace, err := s.S.TurnTrace(c.Request.Context(), session.ID, out.TurnID, cursor, streamPageSize, query.trace)
			if err != nil {
				return
			}
			for _, row := range trace.Events {
				cursor.Seq = row.Seq
				id := fmt.Sprintf("%d:%d", cursor.Seq, cursor.Frame)
				if writeEvent(c, "session", &id, model.Event(row)) != nil {
					return
				}
			}
			for _, row := range trace.Frames {
				cursor.Frame = row.ID
				id := fmt.Sprintf("%d:%d", cursor.Seq, cursor.Frame)
				if writeEvent(c, "frame", &id, model.StreamFrame(row)) != nil {
					return
				}
			}
			c.Writer.Flush()
			if len(trace.Events) == streamPageSize || len(trace.Frames) == streamPageSize {
				continue
			}
			if !query.trace.OmitQuestions {
				questions := make([]model.UserQuestion, 0, len(trace.Questions))
				for _, row := range trace.Questions {
					questions = append(questions, model.UserQuestion(row))
				}
				if writeChanged(c, "questions", questions, &lastQuestions) != nil {
					return
				}
			}
			if !query.trace.OmitOutbox && writeChanged(c, "outbox", []model.Outbox{model.Outbox(trace.Outbox)}, &lastOutbox) != nil {
				return
			}
			break
		}
		c.Writer.Flush()
	}
}
