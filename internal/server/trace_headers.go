package server

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
)

func parseHeaderQuery(c *gin.Context) (store.HeaderFilter, error) {
	filter := store.HeaderFilter{Kind: c.DefaultQuery("kind", "events"), Limit: 100}
	if filter.Kind != "events" && filter.Kind != "frames" {
		return filter, fmt.Errorf("Invalid trace header kind.")
	}
	if raw, exists := c.GetQuery("limit"); exists {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 {
			return filter, fmt.Errorf("Invalid header limit.")
		}
		if limit > 500 {
			limit = 500
		}
		filter.Limit = limit
	}
	if raw, exists := c.GetQuery("before"); exists {
		before, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || before < 0 {
			return filter, fmt.Errorf("Invalid header cursor.")
		}
		filter.Before = &before
	}
	return filter, nil
}

func (s *Server) turnHeaderStream(c *gin.Context, sid, tid string, filter store.HeaderFilter, cursor model.TraceCursor) {
	if filter.Before != nil {
		s.fail(c, 400, fmt.Errorf("Header streams use after_seq and after_frame, not before."))
		return
	}
	mask := eventsChanged | outboxChanged
	if filter.Kind == "frames" {
		mask = framesChanged | outboxChanged
	}
	updates := s.updates.subscribe(sid, mask)
	defer updates.close()
	beginStream(c)
	filter.Limit = streamPageSize
	for signal := range updates.signals(c.Request.Context(), streamHeartbeatInterval) {
		if signal.Heartbeat {
			if writeHeartbeat(c) != nil {
				return
			}
			continue
		}
		for {
			filter.After = &cursor
			page, err := s.S.TurnHeaders(c.Request.Context(), sid, tid, filter)
			if err != nil {
				return
			}
			for _, event := range page.Events {
				cursor.Seq = event.Seq
				id := fmt.Sprintf("%d:%d", cursor.Seq, cursor.Frame)
				if writeEvent(c, "event_header", &id, event) != nil {
					return
				}
			}
			for _, frame := range page.Frames {
				cursor.Frame = frame.ID
				id := fmt.Sprintf("%d:%d", cursor.Seq, cursor.Frame)
				if writeEvent(c, "frame_header", &id, frame) != nil {
					return
				}
			}
			c.Writer.Flush()
			if !page.HasMore {
				break
			}
		}
	}
}
