package store

import "context"

// Question returns one complete review owned by the requested session.
func (s *Store) Question(ctx context.Context, sessionID, questionID string) (UserQuestion, error) {
	var row UserQuestion
	err := s.db.WithContext(ctx).First(&row, "session_id=? AND id=?", sessionID, questionID).Error
	return row, dbError(err)
}

// QuestionMetadataForTurns projects review identity and resolution metadata in
// PostgreSQL. Document bodies and options never cross the database connection.
func (s *Store) QuestionMetadataForTurns(ctx context.Context, sessionID string, turnIDs []string) ([]UserQuestion, error) {
	rows := []UserQuestion{}
	if len(turnIDs) == 0 {
		return rows, nil
	}
	err := s.db.WithContext(ctx).Model(&UserQuestion{}).
		Select(`id, session_id, turn_id, status, answer, created_at, resolved_at,
		COALESCE((SELECT jsonb_agg(jsonb_strip_nulls(jsonb_build_object(
		'id', entry->'id', 'intent', entry->'intent')) ORDER BY ordinal)
		FROM jsonb_array_elements(questions) WITH ORDINALITY AS item(entry, ordinal)), '[]'::jsonb) AS questions`).
		Where("session_id=? AND turn_id IN ?", sessionID, turnIDs).
		Order("created_at,id").Find(&rows).Error
	return rows, dbError(err)
}
