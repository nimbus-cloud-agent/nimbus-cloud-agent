package store

import (
	"context"

	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
)

// MetadataFrames is a bounded forward query. Argument deltas, tool output and
// reasoning bodies are not selected, even when those frames advance the cursor.
func (s *Store) MetadataFrames(ctx context.Context, sid, tid string, after int64, limit int) ([]model.MetadataFrame, error) {
	rows := []model.MetadataFrame{}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	err := s.db.WithContext(ctx).Table("stream_frames").Select(`id,
 payload->>'type' AS type, payload->>'attemptId' AS attempt_id,
 (payload->>'step')::bigint AS step, (payload->>'turn')::bigint AS turn,
 (payload#>>'{outcome,seq}')::bigint AS ended_seq,
 payload#>>'{chunk,type}' AS chunk_type,
 CASE WHEN payload#>>'{chunk,type}'='text-delta' THEN payload#>>'{chunk,text}' END AS text,
 payload#>>'{chunk,id}' AS call_id, payload#>>'{chunk,name}' AS name`).
		Where("session_id=? AND turn_id=? AND id>?", sid, tid, after).Order("id").Limit(limit).Scan(&rows).Error
	return rows, dbError(err)
}
