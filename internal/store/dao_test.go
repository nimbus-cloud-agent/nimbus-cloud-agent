package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
)

func typedStore(t *testing.T) (*Store, Session) {
	t.Helper()
	db, _ := migrationDB(t, "999")
	s := &Store{db: db}
	row := Session{ID: uuid.NewString(), Title: "fixture", AgentState: "idle", SyncedSeq: -1, Options: model.DefaultOptions()}
	if err := s.CreateSession(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	return s, row
}

func TestTypedJSONAtDAOBoundary(t *testing.T) {
	s, session := typedStore(t)
	ctx := context.Background()
	options := session.Options
	options.Plan = true
	if err := s.UpdateSession(ctx, session.ID, SessionChanges{Options: &options}); err != nil {
		t.Fatal(err)
	}
	options.Plan = false
	if err := s.UpdateSession(ctx, session.ID, SessionChanges{Options: &options}); err != nil {
		t.Fatal(err)
	}
	read, err := s.Session(ctx, session.ID)
	if err != nil || read.Options != options {
		t.Fatalf("typed options / false update: %+v, %v", read.Options, err)
	}

	image := Image{ID: uuid.NewString(), SessionID: session.ID, Ref: model.ImageRef{AttachmentID: "image-1", MediaType: "image/png", Bytes: 3, Width: 1, Height: 1, Name: "proof.png"}, Data: []byte{1, 2, 3}}
	if err := s.CreateImage(ctx, image); err != nil {
		t.Fatal(err)
	}
	metadata, err := s.ImageMetadata(ctx, session.ID, image.ID)
	if err != nil || metadata.Ref != image.Ref || metadata.Data != nil {
		t.Fatalf("metadata projection: %+v, %v", metadata, err)
	}

	sb := Sandbox{ID: uuid.NewString(), SessionID: session.ID, E2bSandboxID: "e2b-fixture", State: "running", Info: model.SandboxInfo{SandboxID: "e2b-fixture", Metadata: map[string]string{"owner": "session"}}}
	if err := s.CreateSandbox(ctx, sb); err != nil {
		t.Fatal(err)
	}
	saved, err := s.Sandbox(ctx, session.ID)
	if err != nil || saved.Info.SandboxID != sb.E2bSandboxID || saved.Info.Metadata["owner"] != "session" {
		t.Fatalf("typed sandbox: %+v, %v", saved.Info, err)
	}

	question := UserQuestion{ID: uuid.NewString(), SessionID: session.ID, TurnID: uuid.NewString(), Status: "pending", Questions: []model.Question{{ID: "plan", Question: "Approve?", Intent: &model.QuestionIntent{Kind: "plan-review", Approve: "Yes"}}}}
	turn := Outbox{ID: uuid.NewString(), SessionID: session.ID, TurnID: question.TurnID, ClientMsgID: uuid.NewString(), Options: options, Status: "queued", Images: []model.MessageImage{}}
	if err := s.CreateOutbox(ctx, turn); err != nil {
		t.Fatal(err)
	}

	if err := s.CreateQuestion(ctx, question); err != nil {
		t.Fatal(err)
	}
	answer := &model.QuestionAnswer{Answers: []model.AnswerItem{{ID: "plan", Selected: []string{"Yes"}}}}
	if err := s.ResolveQuestion(ctx, question.ID, "answered", answer, time.Now()); err != nil {
		t.Fatal(err)
	}
	review, err := s.Question(ctx, question.ID)
	if err != nil || review.Answer == nil || review.Answer.Answers[0].Selected[0] != "Yes" || review.Questions[0].Intent.Kind != "plan-review" {
		t.Fatalf("typed review: %+v, %v", review, err)
	}

	// Invalid persisted JSON is rejected while scanning the row, before business
	// code receives a model. SQL is deliberately confined to the DAO test.
	if err := s.db.Exec(`UPDATE sandboxes SET info='{"sandboxId":42}' WHERE id=?`, sb.ID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.Sandbox(ctx, session.ID); err == nil {
		t.Fatal("DAO returned malformed sandbox metadata")
	}
}

func TestDAOTransactionRollbackAndPresetGuard(t *testing.T) {
	s, session := typedStore(t)
	ctx := context.Background()
	title := "rolled back"
	stop := errors.New("abort transaction")
	err := s.Transaction(ctx, func(tx *Store) error {
		if _, err := tx.LockSession(ctx, session.ID); err != nil {
			return err
		}
		if err := tx.UpdateSession(ctx, session.ID, SessionChanges{Title: &title}); err != nil {
			return err
		}
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatal(err)
	}
	row, err := s.Session(ctx, session.ID)
	if err != nil || row.Title != session.Title {
		t.Fatalf("transaction did not roll back: %+v %v", row, err)
	}
	out := Outbox{ID: uuid.NewString(), SessionID: session.ID, TurnID: uuid.NewString(), ClientMsgID: uuid.NewString(), Options: session.Options, Status: "queued", Images: []model.MessageImage{}}
	if err := s.CreateOutbox(ctx, out); err != nil {
		t.Fatal(err)
	}
	row, err = s.Session(ctx, session.ID)
	if err != nil || row.LockedMode == nil || *row.LockedMode != "standard" {
		t.Fatalf("preset not locked: %+v %v", row, err)
	}
	changed := session.Options
	changed.Mode = "ptc"
	if err := s.UpdateSession(ctx, session.ID, SessionChanges{Options: &changed}); err == nil {
		t.Fatal("DAO bypassed preset guard")
	}
	if err := s.db.Model(&Session{}).Where("id=?", session.ID).Update("locked_mode", nil).Error; err == nil {
		t.Fatal("database allowed unlocking a used session")
	}
	if err := s.db.Model(&Outbox{}).Where("id=?", out.ID).Update("options", `{"mode":"ptc"}`).Error; err == nil {
		t.Fatal("database allowed queued preset switch")
	}
}

func TestNativeAndOAuthRoundTripPreservesExtensions(t *testing.T) {
	s, session := typedStore(t)
	ctx := context.Background()
	// These are external documents, not application-authored maps. JSON fixtures
	// exercise the actual DAO codecs and PostgreSQL JSONB normalization.
	native := `{"seq":0,"type":"future/event","time":123,"data":{"active":true,"providerField":{"text":"preserve me"}},"newEnvelope":"preserve too"}`
	if err := s.db.Exec("INSERT INTO session_events(session_id,seq,type,payload) VALUES (?,0,'future/event',?::jsonb)", session.ID, native).Error; err != nil {
		t.Fatal(err)
	}
	events, err := s.Events(ctx, EventFilter{SessionID: session.ID, After: -1})
	if err != nil || len(events) != 1 || events[0].Payload.Data.Active == nil || !*events[0].Payload.Data.Active {
		t.Fatalf("typed event projection: %+v %v", events, err)
	}
	encoded, err := json.Marshal(events[0].Payload)
	if err != nil || !strings.Contains(string(encoded), "preserve me") || !strings.Contains(string(encoded), "preserve too") {
		t.Fatalf("native extension lost: %s %v", encoded, err)
	}

	id := uuid.NewString()
	oauth := `{"state":"state-1","expires_at":123,"client":{"client_id":"client-1","client_secret_expires_at":999},"tokens":{"access_token":"fixture-token","token_type":"Bearer","provider_extension":"keep"}}`
	if err := s.db.Exec("INSERT INTO mcp_servers(id,name,url,oauth_data) VALUES (?,'fixture','http://example.test',?::jsonb)", id, oauth).Error; err != nil {
		t.Fatal(err)
	}
	server, err := s.MCPServer(ctx, id)
	if err != nil || server.OAuthData.State != "state-1" || server.OAuthData.Client.ClientID != "client-1" {
		t.Fatalf("typed OAuth: %+v %v", server, err)
	}
	// Nimbus mutates owned state while preserving provider-owned credentials.
	server.OAuthData.State = "state-2"
	if err := s.SaveOAuth(ctx, id, "pending", server.OAuthData); err != nil {
		t.Fatal(err)
	}
	server, err = s.MCPServer(ctx, id)
	if err != nil || server.OAuthData.State != "state-2" {
		t.Fatal(err)
	}
	encoded, err = json.Marshal(server.OAuthData)
	if err != nil || !strings.Contains(string(encoded), "provider_extension") || !strings.Contains(string(encoded), "client_secret_expires_at") {
		t.Fatalf("OAuth extension lost: %s %v", encoded, err)
	}
}
