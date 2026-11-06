package server

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
)

// Only the initial snapshot and native-record changes recompute the aggregate.
// Token notifications read forward scalar projections, never all prior frames.
func (s *Server) turnMetadataStream(c *gin.Context, sid, tid string, filter store.MetadataFilter) {
	mask := eventsChanged | outboxChanged
	root := filter.ParentCallID == "" || filter.ParentCallID == "root"
	if root {
		mask |= framesChanged
	}
	if !filter.OmitQuestions {
		mask |= questionsChanged
	}
	updates := s.updates.subscribe(sid, mask)
	defer updates.close()
	beginStream(c)
	var state *metadataAccumulator
	lastPayload := ""
	emit := func() error {
		id := fmt.Sprintf("%d:%d", state.data.Cursor.Seq, state.data.Cursor.Frame)
		payload, err := json.Marshal(metadataModel(state.data, !filter.OmitOutbox))
		if err != nil {
			return err
		}
		if string(payload) == lastPayload {
			return nil
		}
		if err := writeStream(c, "metadata", &id, payload); err != nil {
			return err
		}
		lastPayload = string(payload)
		c.Writer.Flush()
		return nil
	}
	for signal := range updates.signals(c.Request.Context(), streamHeartbeatInterval) {
		if signal.Heartbeat {
			if writeHeartbeat(c) != nil {
				return
			}
			continue
		}
		if state == nil || signal.Changed & ^framesChanged != 0 {
			data, err := s.S.TurnMetadata(c.Request.Context(), sid, tid, filter)
			if err != nil {
				return
			}
			state = newMetadataAccumulator(data)
			if emit() != nil {
				return
			}
			continue
		}
		for {
			frames, err := s.S.MetadataFrames(c.Request.Context(), sid, tid, state.data.Cursor.Frame, streamPageSize)
			if err != nil {
				return
			}
			if state.apply(frames) {
				if emit() != nil {
					return
				}
			}
			if len(frames) < streamPageSize {
				break
			}
		}
	}
}

type metadataAccumulator struct {
	data        store.TurnMetadata
	attempts    map[string]int
	text        map[string]*strings.Builder
	pending     map[string]int
	landed      map[string]bool
	settledSeq  map[int64]bool
	settledStep map[[2]int64]bool
}

func newMetadataAccumulator(data store.TurnMetadata) *metadataAccumulator {
	s := &metadataAccumulator{data: data, attempts: map[string]int{}, text: map[string]*strings.Builder{}, pending: map[string]int{}, landed: map[string]bool{}, settledSeq: map[int64]bool{}, settledStep: map[[2]int64]bool{}}
	for i, attempt := range data.Attempts {
		s.attempts[attempt.ID] = i
		builder := new(strings.Builder)
		builder.WriteString(attempt.Text)
		s.text[attempt.ID] = builder
	}
	for i, tool := range data.PendingTools {
		s.pending[tool.ID] = i
	}
	for _, tool := range data.Tools {
		s.landed[tool.ID] = true
	}
	for _, message := range data.Messages {
		s.settledSeq[message.Seq] = true
		if message.Step != nil && message.Turn != nil {
			s.settledStep[[2]int64{*message.Step, *message.Turn}] = true
		}
	}
	return s
}

func (s *metadataAccumulator) resolved(attempt model.AttemptMetadata) bool {
	return (attempt.EndedSeq != nil && s.settledSeq[*attempt.EndedSeq]) || (attempt.Step != nil && attempt.Turn != nil && s.settledStep[[2]int64{*attempt.Step, *attempt.Turn}])
}

func (s *metadataAccumulator) apply(frames []model.MetadataFrame) bool {
	changed := false
	for _, frame := range frames {
		s.data.Cursor.Frame = frame.ID
		if frame.ChunkType == "tool-call-delta" && frame.CallID != "" && !s.landed[frame.CallID] {
			index, exists := s.pending[frame.CallID]
			if !exists {
				name := frame.Name
				if name == "" {
					name = "tool"
				}
				s.pending[frame.CallID] = len(s.data.PendingTools)
				s.data.PendingTools = append(s.data.PendingTools, model.PendingToolMetadata{ID: frame.CallID, Name: name})
				changed = true
			} else if frame.Name != "" && s.data.PendingTools[index].Name != frame.Name {
				s.data.PendingTools[index].Name = frame.Name
				changed = true
			}
		}
		if frame.AttemptID == "" {
			continue
		}
		index, exists := s.attempts[frame.AttemptID]
		if !exists {
			attempt := model.AttemptMetadata{ID: frame.AttemptID, Step: frame.Step, Turn: frame.Turn, EndedSeq: frame.EndedSeq}
			if s.resolved(attempt) {
				continue
			}
			index = len(s.data.Attempts)
			s.attempts[frame.AttemptID] = index
			s.data.Attempts = append(s.data.Attempts, attempt)
			s.text[frame.AttemptID] = new(strings.Builder)
			changed = true
		}
		attempt := &s.data.Attempts[index]
		if frame.Step != nil {
			attempt.Step = frame.Step
		}
		if frame.Turn != nil {
			attempt.Turn = frame.Turn
		}
		if frame.EndedSeq != nil {
			attempt.EndedSeq = frame.EndedSeq
			changed = true
		}
		if frame.ChunkType == "text-delta" && frame.Text != "" {
			builder := s.text[frame.AttemptID]
			builder.WriteString(frame.Text)
			attempt.Text = builder.String()
			changed = true
		}
		if frame.ChunkType == "reasoning-delta" && !attempt.HasReasoning {
			attempt.HasReasoning = true
			changed = true
		}
		if s.resolved(*attempt) {
			delete(s.attempts, frame.AttemptID)
			delete(s.text, frame.AttemptID)
			s.data.Attempts = append(s.data.Attempts[:index], s.data.Attempts[index+1:]...)
			for i := index; i < len(s.data.Attempts); i++ {
				s.attempts[s.data.Attempts[i].ID] = i
			}
			changed = true
		}
	}
	return changed
}
