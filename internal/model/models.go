package model

import "time"

type Session struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	AgentState   string        `json:"agent_state"`
	SyncedSeq    int64         `json:"synced_seq"`
	Options      Options       `json:"options"`
	LockedMode   *string       `json:"-"`
	Header       *NativeHeader `json:"-"`
	CreatedAt    time.Time     `json:"created_at"`
	LastActiveAt time.Time     `json:"last_active_at"`
}
type Event struct {
	SessionID string       `json:"session_id"`
	Seq       int64        `json:"seq"`
	Type      string       `json:"type"`
	Payload   NativeRecord `json:"payload"`
	CreatedAt time.Time    `json:"created_at"`
}

type Outbox struct {
	ID           string         `json:"id"`
	SessionID    string         `json:"session_id"`
	TurnID       string         `json:"turn_id"`
	ClientMsgID  string         `json:"client_msg_id"`
	Content      string         `json:"content"`
	Images       []MessageImage `json:"images"`
	Options      Options        `json:"options"`
	Status       string         `json:"status"`
	DeliveredSeq *int64         `json:"delivered_seq"`
	Error        string         `json:"error"`
	Outcome      string         `json:"outcome"`
	CreatedAt    time.Time      `json:"created_at"`
}

type Image struct {
	ID        string
	SessionID string
	Ref       ImageRef
	Data      []byte
}

type UserQuestion struct {
	ID         string          `json:"id"`
	SessionID  string          `json:"session_id"`
	TurnID     string          `json:"turn_id"`
	Questions  []Question      `json:"questions"`
	Answer     *QuestionAnswer `json:"answer,omitempty"`
	Status     string          `json:"status"`
	CreatedAt  time.Time       `json:"created_at"`
	ResolvedAt *time.Time      `json:"resolved_at,omitempty"`
}

type Sandbox struct {
	ID           string      `json:"id"`
	SessionID    string      `json:"session_id"`
	E2bSandboxID string      `json:"e2b_sandbox_id"`
	State        string      `json:"state"`
	Info         SandboxInfo `json:"info"`
	Error        string      `json:"error"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
	LastActiveAt time.Time   `json:"last_active_at"`
	StartedAt    time.Time   `json:"started_at"`
}
type Skill struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Enabled     bool        `json:"enabled"`
	Files       []SkillFile `json:"files"`
}
type SkillFile struct {
	SkillID string `json:"-"`
	Path    string `json:"path"`
	Content string `json:"content"`
}
type MCPServer struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	URL        string    `json:"url"`
	Enabled    bool      `json:"enabled"`
	OAuthState string    `json:"oauth_state"`
	OAuthData  OAuthData `json:"-"`
}

type StreamFrame struct {
	ID        int64        `json:"id"`
	SessionID string       `json:"session_id"`
	TurnID    string       `json:"turn_id"`
	Payload   FramePayload `json:"payload"`
	CreatedAt time.Time    `json:"created_at"`
}
type SandboxView struct {
	ID           string       `json:"id,omitempty"`
	SessionID    string       `json:"session_id,omitempty"`
	E2bSandboxID string       `json:"e2b_sandbox_id,omitempty"`
	State        string       `json:"state"`
	Info         *SandboxInfo `json:"info,omitempty"`
	Error        string       `json:"error,omitempty"`
	Message      string       `json:"message,omitempty"`
	CreatedAt    time.Time    `json:"created_at,omitempty,omitzero"`
	UpdatedAt    time.Time    `json:"updated_at,omitempty,omitzero"`
	LastActiveAt time.Time    `json:"last_active_at,omitempty,omitzero"`
	StartedAt    time.Time    `json:"started_at,omitempty,omitzero"`
}
type SessionView struct {
	ID           string      `json:"id"`
	Title        string      `json:"title"`
	AgentState   string      `json:"agent_state"`
	SyncedSeq    int64       `json:"synced_seq"`
	Options      Options     `json:"options"`
	ModeLocked   bool        `json:"mode_locked"`
	CreatedAt    time.Time   `json:"created_at"`
	LastActiveAt time.Time   `json:"last_active_at"`
	Sandbox      SandboxView `json:"sandbox"`
}
type History struct {
	Events    []Event        `json:"events"`
	Outbox    []Outbox       `json:"outbox"`
	Frames    []StreamFrame  `json:"frames"`
	Questions []UserQuestion `json:"questions"`
	HasMore   bool           `json:"has_more"`
}
