package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

// SaveLibraryDraft keeps one open draft per file: the user's conflicting text
// stays here until the agent (or the user) merges it into a new version.
func (s *Store) SaveLibraryDraft(ctx context.Context, fileID string, baseVersion int, mimeType, content string) (LibraryDraft, error) {
	now := time.Now()
	row := LibraryDraft{
		ID:          uuid.NewString(),
		FileID:      fileID,
		BaseVersion: baseVersion,
		MimeType:    mimeType,
		SizeBytes:   int64(len(content)),
		Content:     content,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "file_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"base_version", "mime_type", "size_bytes", "content", "updated_at"}),
	}).Create(&row).Error
	if err != nil {
		return LibraryDraft{}, dbError(err)
	}
	return s.LibraryDraftByFile(ctx, fileID)
}

func (s *Store) LibraryDraftByFile(ctx context.Context, fileID string) (LibraryDraft, error) {
	var row LibraryDraft
	err := s.db.WithContext(ctx).First(&row, "file_id=?", fileID).Error
	return row, dbError(err)
}

func (s *Store) LibraryDraft(ctx context.Context, id string) (LibraryDraft, error) {
	var row LibraryDraft
	err := s.db.WithContext(ctx).First(&row, "id=?", id).Error
	return row, dbError(err)
}

func (s *Store) LibraryDrafts(ctx context.Context) ([]LibraryDraft, error) {
	rows := []LibraryDraft{}
	err := s.db.WithContext(ctx).Order("updated_at DESC, id").Find(&rows).Error
	return rows, dbError(err)
}

func (s *Store) DeleteLibraryDraft(ctx context.Context, id string) error {
	return dbError(s.db.WithContext(ctx).Delete(&LibraryDraft{}, "id=?", id).Error)
}
