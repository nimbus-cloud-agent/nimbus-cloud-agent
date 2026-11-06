package server

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
)

func historyModel(page store.HistoryPage) model.History {
	result := model.History{Outbox: make([]model.Outbox, 0, len(page.Outbox)), Turns: page.Turns, Questions: make([]model.UserQuestion, 0, len(page.Questions)), HasMore: page.HasMore}
	for _, row := range page.Outbox {
		result.Outbox = append(result.Outbox, model.Outbox(row))
	}
	for _, row := range page.Questions {
		result.Questions = append(result.Questions, model.UserQuestion(row))
	}
	if page.HasMore && len(page.Outbox) > 0 {
		result.BeforeTurn = &page.Outbox[0].TurnID
	}
	return result
}

func (s *Server) history(c *gin.Context) {
	session, ok := s.session(c)
	if !ok {
		return
	}
	if c.Query("after_seq") != "" || c.Query("types") != "" {
		s.fail(c, 400, fmt.Errorf("Use turn events for raw execution history."))
		return
	}
	before := c.Query("before_turn")
	if before != "" {
		if _, err := uuid.Parse(before); err != nil {
			s.fail(c, 400, fmt.Errorf("Invalid before_turn."))
			return
		}
	}
	limit := 10
	if value, exists := c.GetQuery("limit"); exists {
		var err error
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 {
			s.fail(c, 400, fmt.Errorf("Invalid history limit."))
			return
		}
		if limit > 50 {
			limit = 50
		}
	}
	page, err := s.S.HistoryPage(c.Request.Context(), store.HistoryFilter{SessionID: session.ID, BeforeTurn: before, Limit: limit, OmitQuestionContent: c.Query("include_question_content") == "false"})
	if err != nil {
		s.historyError(c, err)
		return
	}
	c.JSON(200, historyModel(page))
}

func (s *Server) historyError(c *gin.Context, err error) {
	status := 500
	if errors.Is(err, store.ErrNotFound) {
		status = 404
	}
	s.fail(c, status, err)
}

func (s *Server) scopedTurn(c *gin.Context, sid string) (store.Outbox, bool) {
	tid := c.Param("turn")
	if _, err := uuid.Parse(tid); err != nil {
		s.fail(c, 400, fmt.Errorf("Invalid turn ID."))
		return store.Outbox{}, false
	}
	row, err := s.S.TurnOutbox(c.Request.Context(), sid, tid, false)
	if err != nil {
		s.historyError(c, err)
		return store.Outbox{}, false
	}
	return row, true
}

func (s *Server) turnHistory(c *gin.Context) {
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
	if query.headers {
		data, err := s.S.TurnHeaders(c.Request.Context(), session.ID, out.TurnID, query.header)
		if err != nil {
			s.historyError(c, err)
			return
		}
		c.JSON(200, data)
		return
	}
	if query.metadata {
		data, err := s.S.TurnMetadata(c.Request.Context(), session.ID, out.TurnID, query.fields)
		if err != nil {
			s.historyError(c, err)
			return
		}
		c.JSON(200, metadataModel(data, !query.fields.OmitOutbox))
		return
	}
	trace, err := s.S.TurnTrace(c.Request.Context(), session.ID, out.TurnID, model.TraceCursor{Seq: -1}, 0, query.trace)
	if err != nil {
		s.historyError(c, err)
		return
	}
	c.JSON(200, traceModel(trace))
}

func traceModel(trace store.TurnTrace) model.TurnTrace {
	result := model.TurnTrace{Events: make([]model.Event, 0, len(trace.Events)), Frames: make([]model.StreamFrame, 0, len(trace.Frames)), Questions: make([]model.UserQuestion, 0, len(trace.Questions)), Cursor: trace.Cursor}
	for _, row := range trace.Events {
		result.Events = append(result.Events, model.Event(row))
	}
	for _, row := range trace.Frames {
		result.Frames = append(result.Frames, model.StreamFrame(row))
	}
	for _, row := range trace.Questions {
		result.Questions = append(result.Questions, model.UserQuestion(row))
	}
	return result
}

type turnQuery struct {
	metadata bool
	headers  bool
	header   store.HeaderFilter
	trace    store.TraceFilter
	fields   store.MetadataFilter
}

func parseTurnQuery(c *gin.Context) (turnQuery, error) {
	query := turnQuery{fields: store.MetadataFilter{ParentCallID: c.Query("parent_call_id"), MaxFieldChars: 256}, trace: store.TraceFilter{CallID: c.Query("call_id"), AttemptID: c.Query("attempt_id")}}
	query.fields.OmitQuestionContent = c.Query("include_question_content") == "false"
	query.trace.OmitQuestionContent = query.fields.OmitQuestionContent
	query.fields.OmitQuestions = c.Query("include_questions") == "false"
	query.fields.OmitOutbox = c.Query("include_outbox") == "false"
	query.trace.OmitQuestions = query.fields.OmitQuestions
	query.trace.OmitOutbox = query.fields.OmitOutbox
	switch c.Query("projection") {
	case "", "full":
	case "metadata":
		query.metadata = true
	case "headers":
		query.headers = true
		var err error
		query.header, err = parseHeaderQuery(c)
		if err != nil {
			return query, err
		}
	default:
		return query, fmt.Errorf("Invalid turn projection.")
	}
	if value, exists := c.GetQuery("seq"); exists {
		seq, err := strconv.ParseInt(value, 10, 64)
		if err != nil || seq < 0 {
			return query, fmt.Errorf("Invalid event sequence.")
		}
		query.trace.Seq = &seq
	}
	if value, exists := c.GetQuery("frame_id"); exists {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id < 1 {
			return query, fmt.Errorf("Invalid frame ID.")
		}
		query.trace.FrameID = &id
	}
	if value := c.Query("chunk_types"); value != "" {
		query.trace.ChunkTypes = strings.Split(value, ",")
	}
	if value := c.Query("frame_types"); value != "" {
		query.trace.FrameTypes = strings.Split(value, ",")
	}
	if value := c.Query("argument_keys"); value != "" {
		for _, key := range strings.Split(value, ",") {
			switch key {
			case "description", "path", "command", "query", "name":
				query.fields.ArgumentKeys = append(query.fields.ArgumentKeys, key)
			default:
				return query, fmt.Errorf("Invalid argument projection.")
			}
		}
	}
	if value, exists := c.GetQuery("max_field_chars"); exists {
		max, err := strconv.Atoi(value)
		if err != nil || max < 1 || max > 4096 {
			return query, fmt.Errorf("Invalid argument field length.")
		}
		query.fields.MaxFieldChars = max
	}
	if query.headers && (query.trace.CallID != "" || query.trace.AttemptID != "" || query.trace.Seq != nil || query.trace.FrameID != nil || len(query.trace.ChunkTypes) > 0 || len(query.trace.FrameTypes) > 0 || query.fields.ParentCallID != "" || len(query.fields.ArgumentKeys) > 0) {
		return query, fmt.Errorf("Header queries accept kind, limit and before, not body filters.")
	}
	if query.trace.Seq != nil && (query.trace.FrameID != nil || query.trace.AttemptID != "" || len(query.trace.ChunkTypes) > 0 || len(query.trace.FrameTypes) > 0) {
		return query, fmt.Errorf("A native sequence cannot be combined with frame filters.")
	}
	return query, nil
}

func metadataModel(data store.TurnMetadata, includeOutbox bool) model.TurnMetadata {
	result := model.TurnMetadata{Tools: data.Tools, Messages: data.Messages, PendingTools: data.PendingTools, Attempts: data.Attempts, Questions: make([]model.UserQuestion, 0, len(data.Questions)), Outbox: []model.Outbox{}, Cursor: data.Cursor}
	if includeOutbox {
		result.Outbox = append(result.Outbox, model.Outbox(data.Outbox))
	}
	for _, row := range data.Questions {
		result.Questions = append(result.Questions, model.UserQuestion(row))
	}
	return result
}
