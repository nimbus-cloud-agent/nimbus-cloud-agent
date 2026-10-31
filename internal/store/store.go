// Package store is the only production package that accesses PostgreSQL/GORM.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	db   *gorm.DB
	lock *sql.Conn
}

func Open(dsn string) (*Store, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, err
	}
	raw, err := db.DB()
	if err != nil {
		return nil, err
	}
	opened := false
	defer func() {
		if !opened {
			_ = raw.Close()
		}
	}()
	raw.SetMaxOpenConns(20)
	// One Nimbus owns each schema. Preserve the legacy public-schema lock so
	// older binaries cannot concurrently own the same production database.
	conn, err := raw.Conn(context.Background())
	if err != nil {
		return nil, err
	}
	var locked bool
	defer func() {
		if !opened {
			_ = conn.Close()
		}
	}()
	if err = conn.QueryRowContext(context.Background(), `SELECT CASE WHEN current_schema() = 'public' THEN pg_try_advisory_lock(846291710) ELSE pg_try_advisory_lock(846291710, hashtext(current_schema())) END`).Scan(&locked); err != nil {
		return nil, fmt.Errorf("acquire database ownership: %w", err)
	}
	if !locked {
		return nil, fmt.Errorf("database is already owned by another Nimbus process")
	}
	if err = db.Exec("CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY)").Error; err != nil {
		return nil, err
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		version := entry.Name()
		var n int64
		if err = db.Table("schema_migrations").Where("version = ?", version).Count(&n).Error; err != nil {
			return nil, err
		}
		// Preserve the initial migration marker used by the first local boot.
		if version == "001_initial.sql" && n == 0 {
			if err = db.Table("schema_migrations").Where("version = ?", "001").Count(&n).Error; err != nil {
				return nil, err
			}
		}
		if n == 0 {
			sql, err := migrations.ReadFile("migrations/" + version)
			if err != nil {
				return nil, err
			}
			if err = db.Transaction(func(tx *gorm.DB) error {
				if e := tx.Exec(string(sql)).Error; e != nil {
					return e
				}
				return tx.Exec("INSERT INTO schema_migrations VALUES (?)", version).Error
			}); err != nil {
				return nil, err
			}
		}
	}

	opened = true
	return &Store{db: db, lock: conn}, nil
}

func (s *Store) Close() error {
	if s.lock != nil {
		_ = s.lock.Close()
	}
	db, err := s.db.DB()
	if err != nil {
		return err
	}
	return db.Close()
}
func (s *Store) Get(key, fallback string) string {
	var row Setting
	if s.db.First(&row, "key=?", key).Error != nil {
		return fallback
	}
	return row.Value
}
func (s *Store) Set(key, value string) error {
	return s.db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&Setting{Key: key, Value: value}).Error
}
