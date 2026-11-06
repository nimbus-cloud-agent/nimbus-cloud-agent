package store

import (
	"context"

	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
)

type HeaderFilter struct {
	Kind   string
	Limit  int
	Before *int64
	After  *model.TraceCursor
}

// TurnHeaders reads only identities and scalar types, never native/frame bodies.
// Snapshot pages are newest-first keysets; After is the forward SSE catch-up.
func (s *Store) TurnHeaders(ctx context.Context, sid, tid string, filter HeaderFilter) (model.TraceHeaders, error) {
	page := model.TraceHeaders{Events: []model.EventHeader{}, Frames: []model.FrameHeader{}, Cursor: model.TraceCursor{Seq: -1}}
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	if filter.After != nil {
		page.Cursor = *filter.After
	}
	err := s.readSnapshot(ctx, func(tx *Store) error {
		out, err := tx.TurnOutbox(ctx, sid, tid, false)
		if err != nil {
			return err
		}
		if filter.Kind == "frames" {
			q := tx.db.WithContext(ctx).Table("stream_frames").Select("id, payload->>'type' AS type, payload#>>'{chunk,type}' AS chunk_type, payload->>'attemptId' AS attempt_id, COALESCE(payload->>'callId',payload#>>'{chunk,id}') AS call_id").Where("session_id=? AND turn_id=?", sid, tid)
			if filter.After != nil {
				q = q.Where("id>?", filter.After.Frame).Order("id")
			} else {
				q = q.Order("id DESC")
				if filter.Before != nil {
					q = q.Where("id<?", *filter.Before)
				}
			}
			if err = q.Limit(limit + 1).Scan(&page.Frames).Error; err != nil {
				return err
			}
			page.HasMore = len(page.Frames) > limit
			if page.HasMore {
				page.Frames = page.Frames[:limit]
			}
			if filter.After == nil {
				for i, j := 0, len(page.Frames)-1; i < j; i, j = i+1, j-1 {
					page.Frames[i], page.Frames[j] = page.Frames[j], page.Frames[i]
				}
			}
			if len(page.Frames) > 0 {
				page.Cursor.Frame = page.Frames[len(page.Frames)-1].ID
				if page.HasMore && filter.After == nil {
					before := page.Frames[0].ID
					page.Before = &before
				}
			}
			return nil
		}
		if out.DeliveredSeq == nil {
			return nil
		}
		start, end, err := tx.turnBounds(ctx, out)
		if err != nil {
			return err
		}
		q := tx.db.WithContext(ctx).Table("session_events").Select("seq, type, CASE WHEN jsonb_typeof(payload->'time')='number' THEN (payload->>'time')::bigint END AS time").Where("session_id=? AND seq>=?", sid, start)
		if end != nil {
			q = q.Where("seq<?", *end)
		}
		if filter.After != nil {
			q = q.Where("seq>?", filter.After.Seq).Order("seq")
		} else {
			q = q.Order("seq DESC")
			if filter.Before != nil {
				q = q.Where("seq<?", *filter.Before)
			}
		}
		if err = q.Limit(limit + 1).Scan(&page.Events).Error; err != nil {
			return err
		}
		page.HasMore = len(page.Events) > limit
		if page.HasMore {
			page.Events = page.Events[:limit]
		}
		if filter.After == nil {
			for i, j := 0, len(page.Events)-1; i < j; i, j = i+1, j-1 {
				page.Events[i], page.Events[j] = page.Events[j], page.Events[i]
			}
		}
		if len(page.Events) > 0 {
			page.Cursor.Seq = page.Events[len(page.Events)-1].Seq
			if page.HasMore && filter.After == nil {
				before := page.Events[0].Seq
				page.Before = &before
			}
		}
		return nil
	})
	return page, err
}
