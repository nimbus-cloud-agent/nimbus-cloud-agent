// Package store is the only production package that accesses PostgreSQL/GORM.
package store

import (
	"context"
	"embed"
	"log/slog"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	db            *gorm.DB
	lock          *ownership
	notifications *notificationSource
}

func Open(dsn string) (*Store, error) {
	return open(context.Background(), dsn, false, false)
}

// OpenWithHandoff waits for the existing owner to drain before touching schema
// or recovering tasks. Only deployments opting into handoff relinquish ownership.
func OpenWithHandoff(ctx context.Context, dsn string) (*Store, error) {
	return open(ctx, dsn, true, true)
}

// TryOpenWithHandoff restores a retired owner only if its successor has exited.
// It never requests another handoff from a healthy successor.
func TryOpenWithHandoff(ctx context.Context, dsn string) (*Store, error) {
	return open(ctx, dsn, true, false)
}

func open(ctx context.Context, dsn string, handoff, wait bool) (*Store, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), DisableAutomaticPing: true})
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
	lock, err := acquireOwnership(ctx, raw, handoff, wait)
	if err != nil {
		return nil, err
	}
	defer func() {
		if !opened {
			lock.close()
		}
	}()
	if err = migrate(ctx, db); err != nil {
		return nil, err
	}
	notifications, err := newNotificationSource(dsn)
	if err != nil {
		return nil, err
	}
	opened = true
	return &Store{db: db, lock: lock, notifications: notifications}, nil
}

func migrate(ctx context.Context, db *gorm.DB) error {
	db = db.WithContext(ctx)
	if err := db.Exec("CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY)").Error; err != nil {
		return err
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		version := entry.Name()
		var n int64
		if err = db.Table("schema_migrations").Where("version = ?", version).Count(&n).Error; err != nil {
			return err
		}
		// Preserve the initial migration marker used by the first local boot.
		if version == "001_initial.sql" && n == 0 {
			if err = db.Table("schema_migrations").Where("version = ?", "001").Count(&n).Error; err != nil {
				return err
			}
		}
		if n == 0 {
			started := time.Now()
			sql, err := migrations.ReadFile("migrations/" + version)
			if err != nil {
				return err
			}
			if err = db.Transaction(func(tx *gorm.DB) error {
				if e := tx.Exec(string(sql)).Error; e != nil {
					return e
				}
				return tx.Exec("INSERT INTO schema_migrations VALUES (?)", version).Error
			}); err != nil {
				return err
			}
			slog.InfoContext(ctx, "database migration applied", "version", version, "duration_ms", time.Since(started).Milliseconds())
		} else {
			slog.DebugContext(ctx, "database migration already applied", "version", version)
		}
	}
	return nil
}

// OwnershipDone yields nil for a cooperative handoff or an error if the lock
// connection is lost. The owner must stop all work before closing the Store.
func (s *Store) OwnershipDone() <-chan error {
	if s.lock == nil {
		return nil
	}
	return s.lock.events
}

func (s *Store) Close() error {
	if s.notifications != nil {
		s.notifications.close()
	}
	if s.lock != nil {
		s.lock.close()
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
