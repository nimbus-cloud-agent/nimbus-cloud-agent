package store

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SkillFilter selects data, independently of any editor or display state.
type SkillFilter struct {
	ID             string
	IncludeContent bool
	ForUpdate      bool
}

func (s *Store) QuerySkills(ctx context.Context, filter SkillFilter) ([]Skill, error) {
	q := s.db.WithContext(ctx)
	if filter.IncludeContent {
		q = q.Preload("Files", func(q *gorm.DB) *gorm.DB { return q.Order("path") })
	} else {
		q = q.Preload("Files", func(q *gorm.DB) *gorm.DB {
			return q.Select("skill_id", "path").Order("path")
		})
	}
	if filter.ID != "" {
		q = q.Where("id = ?", filter.ID)
	}
	if filter.ForUpdate {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	rows := []Skill{}
	err := q.Order("name").Find(&rows).Error
	return rows, dbError(err)
}

func (s *Store) SkillFile(ctx context.Context, id, path string) (SkillFile, error) {
	var file SkillFile
	err := s.db.WithContext(ctx).Where("skill_id = ? AND path = ?", id, path).First(&file).Error
	return file, dbError(err)
}

// PatchSkillFiles applies a complete path manifest while preserving the bytes of
// retained files whose contents were not supplied. Call within a transaction.
func (s *Store) PatchSkillFiles(ctx context.Context, id string, paths []string, changed []SkillFile) error {
	if err := s.db.WithContext(ctx).Where("skill_id = ? AND path NOT IN ?", id, paths).Delete(&SkillFile{}).Error; err != nil {
		return dbError(err)
	}
	if len(changed) == 0 {
		return nil
	}
	return dbError(s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "skill_id"}, {Name: "path"}},
		DoUpdates: clause.AssignmentColumns([]string{"content"}),
	}).Create(&changed).Error)
}
