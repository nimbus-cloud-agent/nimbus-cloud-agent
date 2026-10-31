package store

import (
	"time"

	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
)

type Session struct {
	ID           string `gorm:"type:uuid;primaryKey"`
	Title        string
	AgentState   string
	SyncedSeq    int64
	Options      model.Options `gorm:"type:jsonb;serializer:json"`
	LockedMode   *string
	Header       *model.NativeHeader `gorm:"type:jsonb;serializer:json"`
	CreatedAt    time.Time
	LastActiveAt time.Time
}
type Event struct {
	SessionID string `gorm:"primaryKey;type:uuid"`
	Seq       int64  `gorm:"primaryKey"`
	Type      string
	Payload   model.NativeRecord `gorm:"type:jsonb;serializer:json"`
	CreatedAt time.Time
}

func (Event) TableName() string { return "session_events" }

type Outbox struct {
	ID           string `gorm:"primaryKey;type:uuid"`
	SessionID    string
	TurnID       string
	ClientMsgID  string
	Content      string
	Images       []model.MessageImage `gorm:"type:jsonb;serializer:json"`
	Options      model.Options        `gorm:"type:jsonb;serializer:json"`
	Status       string
	DeliveredSeq *int64
	Error        string
	Outcome      string
	CreatedAt    time.Time
}

func (Outbox) TableName() string { return "outbox_messages" }

type Image struct {
	ID        string `gorm:"primaryKey;type:uuid"`
	SessionID string
	Ref       model.ImageRef `gorm:"type:jsonb;serializer:json"`
	Data      []byte
}

type UserQuestion struct {
	ID         string `gorm:"primaryKey;type:uuid"`
	SessionID  string
	TurnID     string
	Questions  []model.Question      `gorm:"type:jsonb;serializer:json"`
	Answer     *model.QuestionAnswer `gorm:"type:jsonb;serializer:json"`
	Status     string
	CreatedAt  time.Time
	ResolvedAt *time.Time
}

type Sandbox struct {
	ID           string `gorm:"primaryKey;type:uuid"`
	SessionID    string `gorm:"uniqueIndex"`
	E2bSandboxID string
	State        string
	Info         model.SandboxInfo `gorm:"type:jsonb;serializer:json"`
	Error        string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	LastActiveAt time.Time
	StartedAt    time.Time
}
type Setting struct {
	Key   string `gorm:"primaryKey"`
	Value string `gorm:"type:jsonb;serializer:json"`
}
type Skill struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	Name        string
	Description string
	Enabled     bool
	Files       []SkillFile `gorm:"foreignKey:SkillID"`
}
type SkillFile struct {
	SkillID string `gorm:"primaryKey"`
	Path    string `gorm:"primaryKey"`
	Content string
}
type MCPServer struct {
	ID         string `gorm:"primaryKey;type:uuid"`
	Name       string
	URL        string
	Enabled    bool
	OAuthState string          `gorm:"column:oauth_state"`
	OAuthData  model.OAuthData `gorm:"column:oauth_data;type:jsonb;serializer:json"`
}

func (MCPServer) TableName() string { return "mcp_servers" }

type StreamFrame struct {
	ID        int64 `gorm:"primaryKey"`
	SessionID string
	TurnID    string
	Payload   model.FramePayload `gorm:"type:jsonb;serializer:json"`
	CreatedAt time.Time
}
