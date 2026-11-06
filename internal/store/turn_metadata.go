package store

import (
	"context"

	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
)

type MetadataFilter struct {
	ParentCallID              string
	ArgumentKeys              []string
	MaxFieldChars             int
	OmitQuestionContent       bool
	OmitQuestions, OmitOutbox bool
}
type TurnMetadata struct {
	Tools        []model.ToolMetadata
	Messages     []model.MessageMetadata
	PendingTools []model.PendingToolMetadata
	Attempts     []model.AttemptMetadata
	Outbox       Outbox
	Questions    []UserQuestion
	Cursor       model.TraceCursor
}

const metadataTrace = `WITH trace AS MATERIALIZED (
 SELECT seq,type,payload->'data' AS d,(payload->>'time')::bigint AS time
 FROM session_events WHERE session_id=? AND seq>=? AND (?::bigint IS NULL OR seq<?)
) `

// TurnMetadata projects only requested scalar argument fields and visible
// assistant text. Native tool inputs/results, reasoning and token frames never
// cross the DAO boundary in this query. ParentCallID is a query filter, not an
// instruction about which disclosures the UI should open.
func (s *Store) TurnMetadata(ctx context.Context, sid, tid string, filter MetadataFilter) (TurnMetadata, error) {
	result := TurnMetadata{Tools: []model.ToolMetadata{}, Messages: []model.MessageMetadata{}, PendingTools: []model.PendingToolMetadata{}, Attempts: []model.AttemptMetadata{}, Questions: []UserQuestion{}, Cursor: model.TraceCursor{Seq: -1}}
	err := s.readSnapshot(ctx, func(tx *Store) error {
		out, err := tx.TurnOutbox(ctx, sid, tid, !filter.OmitOutbox)
		if err != nil {
			return err
		}
		result.Outbox = out
		start, end, err := tx.turnBounds(ctx, out)
		if out.DeliveredSeq == nil {
			start = 1<<63 - 1
		}
		if err != nil {
			return err
		}
		if out.DeliveredSeq != nil {
			if err = tx.db.WithContext(ctx).Raw(metadataTrace+`SELECT COALESCE(MAX(seq),-1) AS seq FROM trace`, sid, start, end, end).Scan(&result.Cursor.Seq).Error; err != nil {
				return err
			}
			result.Tools, err = tx.metadataTools(ctx, sid, start, end, filter)
			if err != nil {
				return err
			}
			if filter.ParentCallID == "" || filter.ParentCallID == "root" {
				err = tx.db.WithContext(ctx).Raw(metadataTrace+`SELECT seq,time,(d->>'step')::bigint AS step,(d->>'turn')::bigint AS turn,
 COALESCE(nimbus_content_text(d#>'{message,content}'),'') AS text,
 COALESCE(jsonb_path_exists(d#>'{message,content}','$[*] ? (@.type == "reasoning")'),false) AS has_reasoning
 FROM trace WHERE type='assistant/message' ORDER BY seq`, sid, start, end, end).Scan(&result.Messages).Error
				if err != nil {
					return err
				}
			}
		}
		if out.DeliveredSeq != nil || out.Status == "starting" {
			if err = tx.db.WithContext(ctx).Raw(`SELECT COALESCE(MAX(id),0) FROM stream_frames WHERE session_id=? AND turn_id=?`, sid, tid).Scan(&result.Cursor.Frame).Error; err != nil {
				return err
			}
			if filter.ParentCallID == "" || filter.ParentCallID == "root" {
				err = tx.db.WithContext(ctx).Raw(metadataTrace+`, attempts AS (
 SELECT payload->>'attemptId' AS id, MIN(id) AS first_frame, MAX((payload->>'step')::bigint) AS step,MAX((payload->>'turn')::bigint) AS turn,
 MAX((payload#>>'{outcome,seq}')::bigint) AS ended_seq,
 COALESCE(string_agg(payload#>>'{chunk,text}','' ORDER BY id) FILTER (WHERE payload#>>'{chunk,type}'='text-delta'),'') AS text,
 bool_or(payload#>>'{chunk,type}'='reasoning-delta') AS has_reasoning
 FROM stream_frames WHERE session_id=? AND turn_id=? AND payload->>'attemptId' IS NOT NULL GROUP BY payload->>'attemptId'
) SELECT attempts.id,step,turn,ended_seq,text,COALESCE(has_reasoning,false) AS has_reasoning FROM attempts
 WHERE NOT EXISTS (SELECT 1 FROM trace WHERE type='assistant/message' AND (trace.seq=attempts.ended_seq OR ((d->>'step')::bigint=attempts.step AND (d->>'turn')::bigint=attempts.turn)))
 ORDER BY attempts.first_frame`, sid, start, end, end, sid, tid).Scan(&result.Attempts).Error
				if err != nil {
					return err
				}
				err = tx.db.WithContext(ctx).Raw(metadataTrace+`SELECT payload#>>'{chunk,id}' AS id,
 COALESCE((array_agg(NULLIF(payload#>>'{chunk,name}','') ORDER BY id DESC) FILTER (WHERE NULLIF(payload#>>'{chunk,name}','') IS NOT NULL))[1],'tool') AS name
 FROM stream_frames WHERE session_id=? AND turn_id=? AND payload#>>'{chunk,type}'='tool-call-delta' AND payload#>>'{chunk,id}' IS NOT NULL
 AND NOT EXISTS (SELECT 1 FROM trace WHERE type IN ('tool/call','tool/result','tool/ptc-dispatch-start','tool/ptc-dispatch') AND nimbus_tool_id(d)=payload#>>'{chunk,id}')
 GROUP BY payload#>>'{chunk,id}' ORDER BY MIN(id)`, sid, start, end, end, sid, tid).Scan(&result.PendingTools).Error
				if err != nil {
					return err
				}
			}
		}
		if filter.OmitQuestions {
			return nil
		}
		if filter.OmitQuestionContent {
			result.Questions, err = tx.QuestionMetadataForTurns(ctx, sid, []string{tid})
			return err
		}
		return tx.db.WithContext(ctx).Where("session_id=? AND turn_id=?", sid, tid).Order("created_at,id").Find(&result.Questions).Error
	})
	return result, err
}

func (s *Store) metadataTools(ctx context.Context, sid string, start int64, end *int64, filter MetadataFilter) ([]model.ToolMetadata, error) {
	keys := map[string]bool{}
	for _, key := range filter.ArgumentKeys {
		keys[key] = true
	}
	chars := filter.MaxFieldChars
	if chars <= 0 {
		chars = 256
	}
	var rows []struct {
		ID, Name                                        string
		Seq                                             int64
		Parent                                          *string
		Time, Ended                                     *int64
		Complete, Failed                                bool
		ChildCount                                      int
		Description, Path, Command, Query, ArgumentName *string
	}
	err := s.db.WithContext(ctx).Raw(metadataTrace+`, projected AS (
 SELECT seq,type,time,nimbus_tool_id(d) AS id,COALESCE(d->>'name',d->>'toolName') AS name,
 NULLIF(COALESCE(d->>'parentCallId',d->>'rootCallId'),'') AS parent,
 CASE WHEN type IN ('tool/result','tool/ptc-dispatch') THEN nimbus_tool_failed(d) ELSE false END AS failed,
 CASE WHEN ? THEN nimbus_argument_text(d->'arguments','description',?) END AS description,
 CASE WHEN ? THEN nimbus_argument_text(d->'arguments','path',?) END AS path,
 CASE WHEN ? THEN nimbus_argument_text(d->'arguments','command',?) END AS command,
 CASE WHEN ? THEN nimbus_argument_text(d->'arguments','query',?) END AS query,
 CASE WHEN ? THEN nimbus_argument_text(d->'arguments','name',?) END AS argument_name
 FROM trace WHERE type IN ('tool/call','tool/result','tool/ptc-dispatch-start','tool/ptc-dispatch')
), grouped AS (
 SELECT id,COALESCE((array_agg(name ORDER BY seq DESC) FILTER (WHERE name IS NOT NULL))[1],'tool') AS name,
 MIN(seq) AS seq,MIN(time) AS time,MAX(time) FILTER (WHERE type IN ('tool/result','tool/ptc-dispatch')) AS ended,
 (array_agg(parent ORDER BY seq) FILTER (WHERE parent IS NOT NULL))[1] AS parent,
 bool_or(type IN ('tool/result','tool/ptc-dispatch')) AS complete,COALESCE(bool_or(failed),false) AS failed,
 (array_agg(description ORDER BY seq DESC) FILTER (WHERE description IS NOT NULL))[1] AS description,
 (array_agg(path ORDER BY seq DESC) FILTER (WHERE path IS NOT NULL))[1] AS path,
 (array_agg(command ORDER BY seq DESC) FILTER (WHERE command IS NOT NULL))[1] AS command,
 (array_agg(query ORDER BY seq DESC) FILTER (WHERE query IS NOT NULL))[1] AS query,
 (array_agg(argument_name ORDER BY seq DESC) FILTER (WHERE argument_name IS NOT NULL))[1] AS argument_name
 FROM projected WHERE id IS NOT NULL GROUP BY id
), tools AS (
 SELECT child.id,child.name,child.seq,child.time,child.ended,child.complete,child.failed,parent.id AS parent,
 child.description,child.path,child.command,child.query,child.argument_name
 FROM grouped child LEFT JOIN grouped parent ON child.parent=parent.id AND parent.seq<child.seq
) SELECT tools.*, (SELECT count(*) FROM tools child WHERE child.parent=tools.id) AS child_count FROM tools
 WHERE (?='' OR (?='root' AND parent IS NULL) OR (?<>'root' AND parent=?)) ORDER BY seq`,
		sid, start, end, end, keys["description"], chars, keys["path"], chars, keys["command"], chars, keys["query"], chars, keys["name"], chars,
		filter.ParentCallID, filter.ParentCallID, filter.ParentCallID, filter.ParentCallID).Scan(&rows).Error
	if err != nil {
		return nil, dbError(err)
	}
	tools := make([]model.ToolMetadata, 0, len(rows))
	for _, row := range rows {
		args := map[string]string{}
		for key, value := range map[string]*string{"description": row.Description, "path": row.Path, "command": row.Command, "query": row.Query, "name": row.ArgumentName} {
			if value != nil {
				args[key] = *value
			}
		}
		tools = append(tools, model.ToolMetadata{ID: row.ID, Name: row.Name, Seq: row.Seq, Parent: row.Parent, Time: row.Time, Ended: row.Ended, Complete: row.Complete, Failed: row.Failed, ChildCount: row.ChildCount, Args: args})
	}
	return tools, nil
}
