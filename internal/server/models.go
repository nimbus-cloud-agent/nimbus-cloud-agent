package server

import (
	"context"

	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
)

// These conversions belong to the application. DAO rows have storage tags and
// never become HTTP responses or runtime arguments without a model conversion.
func skillModel(row store.Skill) model.Skill {
	files := make([]model.SkillFile, 0, len(row.Files))
	for _, f := range row.Files {
		files = append(files, model.SkillFile(f))
	}
	return model.Skill{ID: row.ID, Name: row.Name, Description: row.Description, Enabled: row.Enabled, Files: files}
}
func skillRow(x model.Skill) store.Skill {
	files := make([]store.SkillFile, 0, len(x.Files))
	for _, f := range x.Files {
		files = append(files, store.SkillFile(f))
	}
	return store.Skill{ID: x.ID, Name: x.Name, Description: x.Description, Enabled: x.Enabled, Files: files}
}
func (s *Server) getSession(ctx context.Context, id string) (model.Session, error) {
	row, err := s.S.Session(ctx, id)
	return model.Session(row), err
}
func (s *Server) getSandbox(ctx context.Context, sid string) (model.Sandbox, error) {
	row, err := s.S.Sandbox(ctx, sid)
	return model.Sandbox(row), err
}
func (s *Server) getOutbox(ctx context.Context, id, sid string) (model.Outbox, error) {
	row, err := s.S.Outbox(ctx, id, sid)
	return model.Outbox(row), err
}
func (s *Server) outboxes(ctx context.Context, f store.OutboxFilter) ([]model.Outbox, error) {
	rows, err := s.S.Outboxes(ctx, f)
	result := make([]model.Outbox, 0, len(rows))
	for _, r := range rows {
		result = append(result, model.Outbox(r))
	}
	return result, err
}
func (s *Server) events(ctx context.Context, f store.EventFilter) ([]model.Event, error) {
	rows, err := s.S.Events(ctx, f)
	result := make([]model.Event, 0, len(rows))
	for _, r := range rows {
		result = append(result, model.Event(r))
	}
	return result, err
}
func (s *Server) frames(ctx context.Context, f store.FrameFilter) ([]model.StreamFrame, error) {
	rows, err := s.S.Frames(ctx, f)
	result := make([]model.StreamFrame, 0, len(rows))
	for _, r := range rows {
		result = append(result, model.StreamFrame(r))
	}
	return result, err
}
func (s *Server) questions(ctx context.Context, sid string) ([]model.UserQuestion, error) {
	rows, err := s.S.Questions(ctx, sid)
	result := make([]model.UserQuestion, 0, len(rows))
	for _, r := range rows {
		result = append(result, model.UserQuestion(r))
	}
	return result, err
}
func sandboxView(sb model.Sandbox) model.SandboxView {
	return model.SandboxView{ID: sb.ID, SessionID: sb.SessionID, E2bSandboxID: sb.E2bSandboxID, State: sb.State, Info: &sb.Info, Error: sb.Error, CreatedAt: sb.CreatedAt, UpdatedAt: sb.UpdatedAt, StartedAt: sb.StartedAt, LastActiveAt: sb.LastActiveAt}
}
