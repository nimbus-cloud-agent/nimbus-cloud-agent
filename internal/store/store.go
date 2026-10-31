package store

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"time"
)

//go:embed migrations/*.sql
var migrations embed.FS

type JSON = json.RawMessage
type Session struct {
	ID           string    `json:"id" gorm:"type:uuid;primaryKey"`
	Title        string    `json:"title"`
	AgentState   string    `json:"agent_state"`
	SyncedSeq    int64     `json:"synced_seq"`
	Options      JSON      `json:"options" gorm:"type:jsonb"`
	LockedMode   *string   `json:"-"`
	Header       JSON      `json:"-" gorm:"type:jsonb"`
	CreatedAt    time.Time `json:"created_at"`
	LastActiveAt time.Time `json:"last_active_at"`
}
type Event struct {
	SessionID string    `json:"session_id" gorm:"primaryKey;type:uuid"`
	Seq       int64     `json:"seq" gorm:"primaryKey"`
	Type      string    `json:"type"`
	Payload   JSON      `json:"payload" gorm:"type:jsonb"`
	CreatedAt time.Time `json:"created_at"`
}

func (Event) TableName() string { return "session_events" }

type Outbox struct {
	ID           string    `json:"id" gorm:"primaryKey;type:uuid"`
	SessionID    string    `json:"session_id"`
	TurnID       string    `json:"turn_id"`
	ClientMsgID  string    `json:"client_msg_id"`
	Content      string    `json:"content"`
	Images       JSON      `json:"images" gorm:"type:jsonb"`
	Options      JSON      `json:"options" gorm:"type:jsonb"`
	Status       string    `json:"status"`
	DeliveredSeq *int64    `json:"delivered_seq"`
	Error        string    `json:"error"`
	Outcome      string    `json:"outcome"`
	CreatedAt    time.Time `json:"created_at"`
}

func (Outbox) TableName() string { return "outbox_messages" }

type Image struct {
	ID        string `gorm:"primaryKey;type:uuid"`
	SessionID string
	Ref       JSON `gorm:"type:jsonb"`
	Data      []byte
}

type UserQuestion struct {
	ID         string     `json:"id" gorm:"primaryKey;type:uuid"`
	SessionID  string     `json:"session_id"`
	TurnID     string     `json:"turn_id"`
	Questions  JSON       `json:"questions" gorm:"type:jsonb"`
	Answer     JSON       `json:"answer,omitempty" gorm:"type:jsonb"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
}

type Sandbox struct {
	ID           string    `json:"id" gorm:"primaryKey;type:uuid"`
	SessionID    string    `json:"session_id" gorm:"uniqueIndex"`
	E2bSandboxID string    `json:"e2b_sandbox_id"`
	State        string    `json:"state"`
	Info         JSON      `json:"info" gorm:"type:jsonb"`
	Error        string    `json:"error"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	LastActiveAt time.Time `json:"last_active_at"`
	StartedAt    time.Time `json:"started_at"`
}
type Setting struct {
	Key   string `gorm:"primaryKey"`
	Value JSON   `gorm:"type:jsonb"`
}
type Skill struct {
	ID          string      `json:"id" gorm:"primaryKey;type:uuid"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Enabled     bool        `json:"enabled"`
	Files       []SkillFile `json:"files" gorm:"foreignKey:SkillID"`
}
type SkillFile struct {
	SkillID string `json:"-" gorm:"primaryKey"`
	Path    string `json:"path" gorm:"primaryKey"`
	Content string `json:"content"`
}
type MCPServer struct {
	ID         string `json:"id" gorm:"primaryKey;type:uuid"`
	Name       string `json:"name"`
	URL        string `json:"url"`
	Enabled    bool   `json:"enabled"`
	OAuthState string `json:"oauth_state" gorm:"column:oauth_state"`
	OAuthData  JSON   `json:"-" gorm:"column:oauth_data;type:jsonb"`
}

func (MCPServer) TableName() string { return "mcp_servers" }

type Store struct{ DB *gorm.DB }

func Open(dsn string) (*Store, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, err
	}
	raw, _ := db.DB()
	raw.SetMaxOpenConns(20)
	// Only one Nimbus may own this database. Keep the advisory lock connection open.
	conn, err := raw.Conn(context.Background())
	if err != nil {
		return nil, err
	}
	var locked bool
	if err = conn.QueryRowContext(context.Background(), "SELECT pg_try_advisory_lock(846291710)").Scan(&locked); err != nil || !locked {
		conn.Close()
		return nil, fmt.Errorf("database is already owned by another Nimbus process")
	}
	if err = db.Exec("CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY)").Error; err != nil {
		return nil, err
	}
	entries, _ := migrations.ReadDir("migrations")
	for _, entry := range entries {
		version := entry.Name()
		var n int64
		db.Table("schema_migrations").Where("version = ?", version).Count(&n)
		// Preserve the initial migration marker used by the first local boot.
		if version == "001_initial.sql" && n == 0 {
			db.Table("schema_migrations").Where("version = ?", "001").Count(&n)
		}
		if n == 0 {
			sql, _ := migrations.ReadFile("migrations/" + version)
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

	return &Store{db}, nil
}
func Raw(v any) JSON { b, _ := json.Marshal(v); return b }
func (s *Store) Get(key string, fallback string) string {
	var x Setting
	if s.DB.First(&x, "key = ?", key).Error != nil {
		return fallback
	}
	var v string
	if json.Unmarshal(x.Value, &v) != nil {
		return fallback
	}
	return v
}
func (s *Store) Set(key, value string) error {
	return s.DB.Clauses(clause.OnConflict{UpdateAll: true}).Create(&Setting{Key: key, Value: Raw(value)}).Error
}
func (s *Store) Append(sid, oid string, raw JSON) error {
	var rec struct {
		Seq  int64  `json:"seq"`
		Type string `json:"type"`
		Data struct {
			Active *bool `json:"active"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		return err
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var sess Session
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sess, "id = ?", sid).Error; e != nil {
			return e
		}
		if rec.Seq <= sess.SyncedSeq {
			return nil
		}
		if rec.Seq != sess.SyncedSeq+1 {
			return fmt.Errorf("non-contiguous Harness seq: got %d, expected %d", rec.Seq, sess.SyncedSeq+1)
		}
		if e := tx.Create(&Event{SessionID: sid, Seq: rec.Seq, Type: rec.Type, Payload: raw, CreatedAt: time.Now()}).Error; e != nil {
			return e
		}
		if e := tx.Model(&sess).Updates(map[string]any{"synced_seq": rec.Seq, "last_active_at": time.Now()}).Error; e != nil {
			return e
		}
		if rec.Type == "user/message" {
			if e := tx.Model(&Outbox{}).Where("id = ? AND status = 'starting'", oid).Updates(map[string]any{"status": "delivered", "delivered_seq": rec.Seq}).Error; e != nil {
				return e
			}
		}
		// The native approval transition is also the default for subsequent turns.
		if rec.Type == "plan/mode" && rec.Data.Active != nil && !*rec.Data.Active {
			if e := tx.Model(&sess).Update("options", gorm.Expr("jsonb_set(options, '{plan}', 'false')")).Error; e != nil {
				return e
			}
		}
		return tx.Exec("SELECT pg_notify('session_events', ?)", sid).Error
	})
}

type StreamFrame struct {
	ID        int64     `json:"id" gorm:"primaryKey"`
	SessionID string    `json:"session_id"`
	TurnID    string    `json:"turn_id"`
	Payload   JSON      `json:"payload" gorm:"type:jsonb"`
	CreatedAt time.Time `json:"created_at"`
}
