package store

import (
	"context"
	"errors"
	"time"

	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrNotFound = errors.New("record not found")

func dbError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}

// Transactions expose only DAO operations. ORM handles and SQL never leave Store.
func (s *Store) Transaction(ctx context.Context, fn func(*Store) error) error {
	return dbError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return fn(&Store{db: tx}) }))
}
func (s *Store) Session(ctx context.Context, id string) (Session, error) {
	var row Session
	err := s.db.WithContext(ctx).First(&row, "id=?", id).Error
	return row, dbError(err)
}
func (s *Store) LockSession(ctx context.Context, id string) (Session, error) {
	var row Session
	err := s.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id=?", id).Error
	return row, dbError(err)
}
func (s *Store) Sessions(ctx context.Context) ([]Session, error) {
	rows := []Session{}
	err := s.db.WithContext(ctx).Order("last_active_at DESC, id").Find(&rows).Error
	return rows, dbError(err)
}
func (s *Store) CreateSession(ctx context.Context, row Session) error {
	return dbError(s.db.WithContext(ctx).Create(&row).Error)
}
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	return dbError(s.db.WithContext(ctx).Delete(&Session{}, "id=?", id).Error)
}

type SessionChanges struct {
	Title        *string
	Options      *model.Options
	AgentState   *string
	Header       *model.NativeHeader
	LastActiveAt *time.Time
	SyncedSeq    *int64
}

func (s *Store) UpdateSession(ctx context.Context, id string, c SessionChanges) error {
	row := Session{ID: id}
	columns := []string{}
	if c.Title != nil {
		row.Title = *c.Title
		columns = append(columns, "title")
	}
	if c.Options != nil {
		row.Options = *c.Options
		columns = append(columns, "options")
	}
	if c.AgentState != nil {
		row.AgentState = *c.AgentState
		columns = append(columns, "agent_state")
	}
	if c.Header != nil {
		row.Header = c.Header
		columns = append(columns, "header")
	}
	if c.LastActiveAt != nil {
		row.LastActiveAt = *c.LastActiveAt
		columns = append(columns, "last_active_at")
	}
	if c.SyncedSeq != nil {
		row.SyncedSeq = *c.SyncedSeq
		columns = append(columns, "synced_seq")
	}
	if len(columns) == 0 {
		return nil
	}
	return dbError(s.db.WithContext(ctx).Model(&row).Select(columns).Updates(&row).Error)
}
func (s *Store) Outbox(ctx context.Context, id, sid string) (Outbox, error) {
	var row Outbox
	q := s.db.WithContext(ctx).Where("id=?", id)
	if sid != "" {
		q = q.Where("session_id=?", sid)
	}
	err := q.First(&row).Error
	return row, dbError(err)
}
func (s *Store) OutboxByClient(ctx context.Context, sid, cid string) (Outbox, error) {
	var row Outbox
	err := s.db.WithContext(ctx).First(&row, "session_id=? AND client_msg_id=?", sid, cid).Error
	return row, dbError(err)
}

type OutboxFilter struct{ SessionID, Status, Outcome string }

func (s *Store) Outboxes(ctx context.Context, f OutboxFilter) ([]Outbox, error) {
	rows := []Outbox{}
	q := s.db.WithContext(ctx)
	if f.SessionID != "" {
		q = q.Where("session_id=?", f.SessionID)
	}
	if f.Status != "" {
		q = q.Where("status=?", f.Status)
	}
	if f.Outcome != "" {
		q = q.Where("outcome=?", f.Outcome)
	}
	err := q.Order("created_at, id").Find(&rows).Error
	return rows, dbError(err)
}
func (s *Store) CreateOutbox(ctx context.Context, row Outbox) error {
	return dbError(s.db.WithContext(ctx).Create(&row).Error)
}

type OutboxChanges struct {
	Status, Error, Outcome *string
	CreatedAt              *time.Time
	DeliveredSeq           *int64
}

func (s *Store) UpdateOutbox(ctx context.Context, id string, c OutboxChanges) error {
	row := Outbox{ID: id}
	columns := []string{}
	if c.Status != nil {
		row.Status = *c.Status
		columns = append(columns, "status")
	}
	if c.Error != nil {
		row.Error = *c.Error
		columns = append(columns, "error")
	}
	if c.Outcome != nil {
		row.Outcome = *c.Outcome
		columns = append(columns, "outcome")
	}
	if c.CreatedAt != nil {
		row.CreatedAt = *c.CreatedAt
		columns = append(columns, "created_at")
	}
	if c.DeliveredSeq != nil {
		row.DeliveredSeq = c.DeliveredSeq
		columns = append(columns, "delivered_seq")
	}
	return dbError(s.db.WithContext(ctx).Model(&row).Select(columns).Updates(&row).Error)
}

type EventFilter struct {
	SessionID string
	After     int64
	Types     []string
	Limit     int
}

func (s *Store) Events(ctx context.Context, f EventFilter) ([]Event, error) {
	rows := []Event{}
	q := s.db.WithContext(ctx).Where("session_id=? AND seq>?", f.SessionID, f.After)
	if len(f.Types) > 0 {
		q = q.Where("type IN ?", f.Types)
	}
	if f.Limit > 0 {
		q = q.Limit(f.Limit)
	}
	err := q.Order("seq").Find(&rows).Error
	return rows, dbError(err)
}
func (s *Store) CreateEvent(ctx context.Context, row Event) error {
	return dbError(s.db.WithContext(ctx).Create(&row).Error)
}

type FrameFilter struct {
	SessionID, TurnID string
	After             int64
	Limit             int
}

func (s *Store) Frames(ctx context.Context, f FrameFilter) ([]StreamFrame, error) {
	rows := []StreamFrame{}
	q := s.db.WithContext(ctx).Where("id>?", f.After)
	if f.SessionID != "" {
		q = q.Where("session_id=?", f.SessionID)
	}
	if f.TurnID != "" {
		q = q.Where("turn_id=?", f.TurnID)
	}
	if f.Limit > 0 {
		q = q.Limit(f.Limit)
	}
	err := q.Order("id").Find(&rows).Error
	return rows, dbError(err)
}
func (s *Store) CreateFrame(ctx context.Context, row StreamFrame) error {
	// Allocate the frame ID only after taking the session lock. Otherwise two
	// concurrent frame writers could commit IDs out of order and a cursor-based
	// reader could permanently skip the later commit with the smaller ID.
	return s.Transaction(ctx, func(tx *Store) error {
		var parent Session
		if err := tx.db.WithContext(ctx).Select("id").Clauses(clause.Locking{Strength: "UPDATE"}).First(&parent, "id=?", row.SessionID).Error; err != nil {
			return dbError(err)
		}
		return dbError(tx.db.WithContext(ctx).Create(&row).Error)
	})
}
func (s *Store) Sandbox(ctx context.Context, sid string) (Sandbox, error) {
	var row Sandbox
	err := s.db.WithContext(ctx).First(&row, "session_id=?", sid).Error
	return row, dbError(err)
}
func (s *Store) RunningSandboxes(ctx context.Context) ([]Sandbox, error) {
	rows := []Sandbox{}
	err := s.db.WithContext(ctx).Where("state='running'").Find(&rows).Error
	return rows, dbError(err)
}
func (s *Store) RunningSandboxCount(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&Sandbox{}).Where("state='running'").Count(&count).Error
	return count, dbError(err)
}
func (s *Store) CreateSandbox(ctx context.Context, row Sandbox) error {
	return dbError(s.db.WithContext(ctx).Create(&row).Error)
}

type SandboxChanges struct {
	State, Error, E2bSandboxID *string
	Info                       *model.SandboxInfo
	StartedAt, LastActiveAt    *time.Time
}

func (s *Store) UpdateSandbox(ctx context.Context, id string, c SandboxChanges) error {
	row := Sandbox{ID: id}
	columns := []string{}
	if c.State != nil {
		row.State = *c.State
		columns = append(columns, "state")
	}
	if c.Error != nil {
		row.Error = *c.Error
		columns = append(columns, "error")
	}
	if c.E2bSandboxID != nil {
		row.E2bSandboxID = *c.E2bSandboxID
		columns = append(columns, "e2b_sandbox_id")
	}
	if c.Info != nil {
		row.Info = *c.Info
		columns = append(columns, "info")
	}
	if c.StartedAt != nil {
		row.StartedAt = *c.StartedAt
		columns = append(columns, "started_at")
	}
	if c.LastActiveAt != nil {
		row.LastActiveAt = *c.LastActiveAt
		columns = append(columns, "last_active_at")
	}
	return dbError(s.db.WithContext(ctx).Model(&row).Select(columns).Updates(&row).Error)
}
func (s *Store) Image(ctx context.Context, sid, id string) (Image, error) {
	var row Image
	err := s.db.WithContext(ctx).First(&row, "id=? AND session_id=?", id, sid).Error
	return row, dbError(err)
}
func (s *Store) ImageMetadata(ctx context.Context, sid, id string) (Image, error) {
	var row Image
	err := s.db.WithContext(ctx).Select("id", "session_id", "ref").First(&row, "id=? AND session_id=?", id, sid).Error
	return row, dbError(err)
}
func (s *Store) CreateImage(ctx context.Context, row Image) error {
	return dbError(s.db.WithContext(ctx).Create(&row).Error)
}
func (s *Store) TurnImages(ctx context.Context, sid, oid string) ([]Image, error) {
	rows := []Image{}
	err := s.db.WithContext(ctx).Where("session_id=? AND id IN (SELECT (jsonb_array_elements(images)->>'id')::uuid FROM outbox_messages WHERE session_id=? AND (status='delivered' OR id=?))", sid, sid, oid).Find(&rows).Error
	return rows, dbError(err)
}
func (s *Store) Questions(ctx context.Context, sid string) ([]UserQuestion, error) {
	rows := []UserQuestion{}
	err := s.db.WithContext(ctx).Where("session_id=?", sid).Order("created_at,id").Find(&rows).Error
	return rows, dbError(err)
}
func (s *Store) Question(ctx context.Context, id string) (UserQuestion, error) {
	var row UserQuestion
	err := s.db.WithContext(ctx).First(&row, "id=?", id).Error
	return row, dbError(err)
}
func (s *Store) LockQuestion(ctx context.Context, sid, id string) (UserQuestion, error) {
	var row UserQuestion
	err := s.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id=? AND session_id=?", id, sid).Error
	return row, dbError(err)
}
func (s *Store) CreateQuestion(ctx context.Context, row UserQuestion) error {
	return dbError(s.db.WithContext(ctx).Create(&row).Error)
}
func (s *Store) ResolveQuestion(ctx context.Context, id, status string, answer *model.QuestionAnswer, at time.Time) error {
	row := UserQuestion{ID: id, Status: status, Answer: answer, ResolvedAt: &at}
	return dbError(s.db.WithContext(ctx).Model(&row).Select("answer", "status", "resolved_at").Updates(&row).Error)
}
func (s *Store) InterruptQuestions(ctx context.Context, id string) error {
	q := s.db.WithContext(ctx).Model(&UserQuestion{}).Where("status='pending'")
	if id != "" {
		q = q.Where("id=?", id)
	}
	return dbError(q.Select("status", "resolved_at").Updates(&UserQuestion{Status: "interrupted", ResolvedAt: timePtr(time.Now())}).Error)
}
func (s *Store) Skills(ctx context.Context, enabledOnly bool) ([]Skill, error) {
	rows := []Skill{}
	q := s.db.WithContext(ctx).Preload("Files")
	if enabledOnly {
		q = q.Where("enabled=true")
	}
	err := q.Order("name").Find(&rows).Error
	return rows, dbError(err)
}
func (s *Store) CreateSkill(ctx context.Context, row Skill) error {
	return dbError(s.db.WithContext(ctx).Omit("Files").Create(&row).Error)
}
func (s *Store) UpdateSkill(ctx context.Context, row Skill) error {
	r := s.db.WithContext(ctx).Model(&row).Select("name", "description", "enabled").Updates(&row)
	if r.Error != nil {
		return dbError(r.Error)
	}
	if r.RowsAffected != 1 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) ReplaceSkillFiles(ctx context.Context, id string, files []SkillFile) error {
	if err := s.db.WithContext(ctx).Where("skill_id=?", id).Delete(&SkillFile{}).Error; err != nil {
		return dbError(err)
	}
	if len(files) == 0 {
		return nil
	}
	return dbError(s.db.WithContext(ctx).Create(&files).Error)
}
func (s *Store) DeleteSkill(ctx context.Context, id string) error {
	return dbError(s.db.WithContext(ctx).Delete(&Skill{}, "id=?", id).Error)
}
func (s *Store) MCPServers(ctx context.Context, enabledOnly bool) ([]MCPServer, error) {
	rows := []MCPServer{}
	q := s.db.WithContext(ctx)
	if enabledOnly {
		q = q.Where("enabled=true")
	}
	err := q.Order("name").Find(&rows).Error
	return rows, dbError(err)
}
func (s *Store) MCPServer(ctx context.Context, id string) (MCPServer, error) {
	var row MCPServer
	err := s.db.WithContext(ctx).First(&row, "id=?", id).Error
	return row, dbError(err)
}
func (s *Store) CreateMCPServer(ctx context.Context, row MCPServer) error {
	return dbError(s.db.WithContext(ctx).Create(&row).Error)
}
func (s *Store) UpdateMCPServer(ctx context.Context, row MCPServer) error {
	return dbError(s.db.WithContext(ctx).Model(&row).Select("name", "url", "enabled", "oauth_state", "oauth_data").Updates(&row).Error)
}
func (s *Store) SaveOAuth(ctx context.Context, id, state string, data model.OAuthData) error {
	row := MCPServer{ID: id, OAuthState: state, OAuthData: data}
	return dbError(s.db.WithContext(ctx).Model(&row).Select("oauth_state", "oauth_data").Updates(&row).Error)
}
func (s *Store) DeleteMCPServer(ctx context.Context, id string) error {
	return dbError(s.db.WithContext(ctx).Delete(&MCPServer{}, "id=?", id).Error)
}
func (s *Store) RecoverInterrupted(ctx context.Context) error {
	return s.Transaction(ctx, func(tx *Store) error {
		if err := tx.InterruptQuestions(ctx, ""); err != nil {
			return err
		}
		if err := tx.db.Model(&Outbox{}).Where("status='starting'").Select("status", "error", "outcome").Updates(&Outbox{Status: "failed", Error: "后端重启，本轮尚未送达，请重发", Outcome: "interrupted"}).Error; err != nil {
			return err
		}
		if err := tx.db.Model(&Outbox{}).Where("status='delivered' AND outcome=''").Update("outcome", "interrupted").Error; err != nil {
			return err
		}
		return tx.db.Model(&Session{}).Where("agent_state<>'idle'").Update("agent_state", "idle").Error
	})
}

func timePtr(value time.Time) *time.Time { return &value }
