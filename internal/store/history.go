package store

import (
	"context"
	"database/sql"

	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"gorm.io/gorm"
)

// HistoryPage is a conversation projection, never a runtime hydration source.
// BeforeTurn and SinceTurn are exclusive/inclusive session-owned keyset anchors.
type HistoryFilter struct {
	SessionID, BeforeTurn, SinceTurn string
	Limit                            int
	OmitQuestionContent              bool
}
type HistoryPage struct {
	Outbox    []Outbox
	Turns     []model.TurnSummary
	Questions []UserQuestion
	HasMore   bool
}

func (s *Store) readSnapshot(ctx context.Context, fn func(*Store) error) error {
	return dbError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&Store{db: tx})
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}))
}

func (s *Store) TurnOutbox(ctx context.Context, sid, tid string, includeContent bool) (Outbox, error) {
	var row Outbox
	q := s.db.WithContext(ctx)
	if !includeContent {
		q = q.Select("id", "session_id", "turn_id", "status", "delivered_seq", "created_at")
	}
	err := q.First(&row, "session_id=? AND turn_id=?", sid, tid).Error
	return row, dbError(err)
}

func (s *Store) HistoryPage(ctx context.Context, f HistoryFilter) (HistoryPage, error) {
	page := HistoryPage{Outbox: []Outbox{}, Turns: []model.TurnSummary{}, Questions: []UserQuestion{}}
	err := s.readSnapshot(ctx, func(tx *Store) error {
		q := tx.db.WithContext(ctx).Where("session_id=?", f.SessionID)
		anchor := f.BeforeTurn
		if f.SinceTurn != "" {
			anchor = f.SinceTurn
		}
		if anchor != "" {
			row, err := tx.TurnOutbox(ctx, f.SessionID, anchor, false)
			if err != nil {
				return err
			}
			op := "<"
			if f.SinceTurn != "" {
				op = ">="
			}
			q = q.Where("(created_at,id) "+op+" (?,?)", row.CreatedAt, row.ID)
		}
		if f.SinceTurn != "" {
			q = q.Order("created_at,id")
		} else {
			limit := f.Limit
			if limit <= 0 {
				limit = 10
			}
			if limit > 50 {
				limit = 50
			}
			q = q.Order("created_at DESC,id DESC").Limit(limit + 1)
		}
		if err := q.Find(&page.Outbox).Error; err != nil {
			return err
		}
		if f.SinceTurn == "" {
			limit := f.Limit
			if limit <= 0 {
				limit = 10
			}
			if limit > 50 {
				limit = 50
			}
			if len(page.Outbox) > limit {
				page.HasMore = true
				page.Outbox = page.Outbox[:limit]
			}
			for i, j := 0, len(page.Outbox)-1; i < j; i, j = i+1, j-1 {
				page.Outbox[i], page.Outbox[j] = page.Outbox[j], page.Outbox[i]
			}
		}
		ids := make([]string, 0, len(page.Outbox))
		for _, out := range page.Outbox {
			ids = append(ids, out.TurnID)
			summary, err := tx.turnSummary(ctx, out)
			if err != nil {
				return err
			}
			page.Turns = append(page.Turns, summary)
		}
		if len(ids) > 0 {
			if f.OmitQuestionContent {
				var err error
				page.Questions, err = tx.QuestionMetadataForTurns(ctx, f.SessionID, ids)
				return err
			}
			return tx.db.WithContext(ctx).Where("session_id=? AND turn_id IN ?", f.SessionID, ids).Order("created_at,id").Find(&page.Questions).Error
		}
		return nil
	})
	return page, err
}

// turnBounds includes the native turn/start, which precedes user/message.
// Never derive the upper bound from page neighbors: they may not be adjacent.
func (s *Store) turnBounds(ctx context.Context, out Outbox) (int64, *int64, error) {
	if out.DeliveredSeq == nil {
		return -1, nil, nil
	}
	var bounds struct {
		Start int64
		End   *int64
	}
	err := s.db.WithContext(ctx).Raw(`WITH start AS (
 SELECT COALESCE(MAX(seq), ?) AS seq FROM session_events WHERE session_id=? AND type='turn/start' AND seq<=?
) SELECT start.seq AS start, (SELECT MIN(e.seq) FROM session_events e WHERE e.session_id=? AND e.type='turn/start' AND e.seq>start.seq) AS "end" FROM start`, *out.DeliveredSeq, out.SessionID, *out.DeliveredSeq, out.SessionID).Scan(&bounds).Error
	return bounds.Start, bounds.End, err
}

// Only scalar/text projections leave PostgreSQL here: no native records, tool
// arguments, reasoning blocks, result bodies, or non-terminal stream frames are
// read. Usage frames contribute counters only.
func (s *Store) turnSummary(ctx context.Context, out Outbox) (model.TurnSummary, error) {
	summary := model.TurnSummary{TurnID: out.TurnID, HasDetails: out.DeliveredSeq != nil || out.Status == "starting"}
	if out.DeliveredSeq == nil {
		return summary, nil
	}
	start, end, err := s.turnBounds(ctx, out)
	if err != nil {
		return summary, err
	}
	var row struct {
		FinalText    string
		ToolCount    int
		FailedCount  int
		Duration     *float64
		RetrySeq     *int64
		HasReasoning bool
	}
	err = s.db.WithContext(ctx).Raw(`WITH trace AS MATERIALIZED (
 SELECT seq,type,payload->'data' AS d,(payload->>'time')::double precision AS time
 FROM session_events WHERE session_id=? AND seq>=? AND (?::bigint IS NULL OR seq<?)
), tools AS (
 SELECT seq,type,d,nimbus_content_text(COALESCE(NULLIF(d#>'{message,content}','null'::jsonb),NULLIF(d->'content','null'::jsonb),d#>'{result,content}')) AS content
 FROM trace WHERE type IN ('tool/call','tool/result','tool/ptc-dispatch-start','tool/ptc-dispatch')
), calls AS (
 SELECT nimbus_tool_id(d) AS id, bool_or(nimbus_tool_failed(d)) AS failed
 FROM tools GROUP BY 1
), answers AS (
 SELECT seq,nimbus_content_text(d#>'{message,content}') AS text
 FROM trace WHERE type='assistant/message' AND seq>COALESCE((SELECT MAX(seq) FROM tools WHERE type IN ('tool/call','tool/ptc-dispatch-start','tool/ptc-dispatch')), -1)
), retry AS (
 SELECT seq FROM tools
 WHERE type='tool/ptc-dispatch' AND d->>'name'='exit_plan_mode' AND d->>'isError'='true'
 AND content LIKE '%no user-questions channel is available%' AND nimbus_argument_text(d->'arguments','plan',1) IS NOT NULL ORDER BY seq DESC LIMIT 1
)
SELECT COALESCE((SELECT text FROM answers WHERE text<>'' ORDER BY seq DESC LIMIT 1),'') AS final_text,
 (SELECT count(*) FROM calls WHERE id IS NOT NULL) AS tool_count,
 (SELECT count(*) FROM calls WHERE id IS NOT NULL AND failed) AS failed_count,
 ((SELECT time FROM trace WHERE type='turn/end' ORDER BY seq LIMIT 1)-(SELECT time FROM trace WHERE type='turn/start' ORDER BY seq LIMIT 1))/1000 AS duration,
 (SELECT seq FROM retry) AS retry_seq,
 (SELECT EXISTS(SELECT 1 FROM trace WHERE type='assistant/message' AND (d#>'{message,content}') @> '[{"type":"reasoning"}]'::jsonb)) AS has_reasoning`, out.SessionID, start, end, end).Scan(&row).Error
	if err != nil {
		return summary, dbError(err)
	}
	summary.FinalText, summary.ToolCount, summary.FailedCount, summary.Duration = row.FinalText, row.ToolCount, row.FailedCount, row.Duration
	summary.HasReasoning = row.HasReasoning
	usage, err := s.turnUsage(ctx, out.SessionID, out.TurnID)
	if err != nil {
		return summary, err
	}
	summary.Usage = usage
	if row.RetrySeq != nil {
		summary.PlanReviewRetry = &model.PlanReviewRetry{Seq: *row.RetrySeq}
	}
	return summary, nil
}

func (s *Store) turnUsage(ctx context.Context, sid, tid string) (*model.TokenUsage, error) {
	var row struct {
		Calls            int
		InputTokens      int64
		CacheReadTokens  int64
		CacheWriteTokens int64
		OutputTokens     int64
		TotalTokens      int64
	}
	err := s.db.WithContext(ctx).Raw(`SELECT count(*) AS calls,
 COALESCE(SUM((payload#>>'{chunk,usage,inputTokens}')::bigint),0) AS input_tokens,
 COALESCE(SUM((payload#>>'{chunk,usage,cacheReadTokens}')::bigint),0) AS cache_read_tokens,
 COALESCE(SUM((payload#>>'{chunk,usage,cacheWriteTokens}')::bigint),0) AS cache_write_tokens,
 COALESCE(SUM((payload#>>'{chunk,usage,outputTokens}')::bigint),0) AS output_tokens,
 COALESCE(SUM((payload#>>'{chunk,usage,totalTokens}')::bigint),0) AS total_tokens
 FROM stream_frames WHERE session_id=? AND turn_id=? AND payload#>>'{chunk,type}'='usage'`, sid, tid).Scan(&row).Error
	if err != nil {
		return nil, dbError(err)
	}
	if row.Calls == 0 {
		return nil, nil
	}
	if row.TotalTokens == 0 {
		row.TotalTokens = row.InputTokens + row.CacheReadTokens + row.CacheWriteTokens + row.OutputTokens
	}
	return &model.TokenUsage{InputTokens: row.InputTokens, CacheReadTokens: row.CacheReadTokens, CacheWriteTokens: row.CacheWriteTokens, OutputTokens: row.OutputTokens, TotalTokens: row.TotalTokens, Calls: row.Calls}, nil
}

type TurnTrace struct {
	Outbox    Outbox
	Events    []Event
	Frames    []StreamFrame
	Questions []UserQuestion
	Cursor    model.TraceCursor
}

type TraceFilter struct {
	CallID, AttemptID         string
	Seq                       *int64
	FrameID                   *int64
	ChunkTypes                []string
	FrameTypes                []string
	OmitQuestionContent       bool
	OmitQuestions, OmitOutbox bool
}

// TurnTrace returns a consistent snapshot, optionally keyset-limited for SSE.
// Cursor contains only watermarks actually included in this result.
func (s *Store) TurnTrace(ctx context.Context, sid, tid string, after model.TraceCursor, limit int, filter TraceFilter) (TurnTrace, error) {
	trace := TurnTrace{Events: []Event{}, Frames: []StreamFrame{}, Questions: []UserQuestion{}, Cursor: after}
	err := s.readSnapshot(ctx, func(tx *Store) error {
		out, err := tx.TurnOutbox(ctx, sid, tid, !filter.OmitOutbox)
		if err != nil {
			return err
		}
		trace.Outbox = out
		if out.DeliveredSeq != nil && filter.FrameID == nil && filter.AttemptID == "" && len(filter.ChunkTypes) == 0 {
			start, end, err := tx.turnBounds(ctx, out)
			if err != nil {
				return err
			}
			q := tx.db.WithContext(ctx).Where("session_id=? AND seq>=? AND seq>?", sid, start, after.Seq)
			if end != nil {
				q = q.Where("seq<?", *end)
			}
			if filter.Seq != nil {
				q = q.Where("seq=?", *filter.Seq)
			}
			if filter.CallID != "" {
				q = q.Where("type IN ? AND nimbus_tool_id(payload->'data')=?", []string{"tool/call", "tool/result", "tool/ptc-dispatch-start", "tool/ptc-dispatch"}, filter.CallID)
			}
			if limit > 0 {
				q = q.Limit(limit)
			}
			if err = q.Order("seq").Find(&trace.Events).Error; err != nil {
				return err
			}
		}
		if filter.Seq == nil && (out.DeliveredSeq != nil || out.Status == "starting") {
			q := tx.db.WithContext(ctx).Where("session_id=? AND turn_id=? AND id>?", sid, tid, after.Frame)
			if filter.FrameID != nil {
				q = q.Where("id=?", *filter.FrameID)
			}
			if filter.CallID != "" {
				q = q.Where("(payload->>'callId'=? OR payload#>>'{chunk,id}'=?)", filter.CallID, filter.CallID)
			}
			if filter.AttemptID != "" {
				q = q.Where("payload->>'attemptId'=?", filter.AttemptID)
			}
			if len(filter.ChunkTypes) > 0 {
				q = q.Where("payload#>>'{chunk,type}' IN ?", filter.ChunkTypes)
			}
			if len(filter.FrameTypes) > 0 {
				q = q.Where("payload->>'type' IN ?", filter.FrameTypes)
			}
			if limit > 0 {
				q = q.Limit(limit)
			}
			if err = q.Order("id").Find(&trace.Frames).Error; err != nil {
				return err
			}
		}
		if !filter.OmitQuestions {
			if filter.OmitQuestionContent {
				trace.Questions, err = tx.QuestionMetadataForTurns(ctx, sid, []string{tid})
				if err != nil {
					return err
				}
			} else if err = tx.db.WithContext(ctx).Where("session_id=? AND turn_id=?", sid, tid).Order("created_at,id").Find(&trace.Questions).Error; err != nil {
				return err
			}
		}
		if len(trace.Events) > 0 {
			trace.Cursor.Seq = trace.Events[len(trace.Events)-1].Seq
		}
		if len(trace.Frames) > 0 {
			trace.Cursor.Frame = trace.Frames[len(trace.Frames)-1].ID
		}
		return nil
	})
	return trace, err
}
