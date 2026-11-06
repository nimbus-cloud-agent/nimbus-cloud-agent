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

func TestQuestionQueriesProjectDocumentsAndScopeSessions(t *testing.T) {
	s, session := typedStore(t)
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Microsecond)
	out := historyOutbox(t, s, session, uuid.NewString(), at, nil)
	otherTurn := historyOutbox(t, s, session, uuid.NewString(), at, nil)
	secret := strings.Repeat("private-plan-document", 10000)
	intent := &model.QuestionIntent{Kind: "plan-review", Approve: "Approve", CallID: "native-review"}
	answer := &model.QuestionAnswer{Answers: []model.AnswerItem{{ID: "plan", Selected: []string{"Approve"}}}}
	for _, fixture := range []struct {
		turn   string
		status string
	}{
		{out.TurnID, "pending"},
		{out.TurnID, "answered"},
		{otherTurn.TurnID, "dismissed"},
	} {
		row := UserQuestion{ID: uuid.NewString(), SessionID: session.ID, TurnID: fixture.turn, Status: fixture.status,
			Questions: []model.Question{{ID: "plan", Question: secret, Detail: secret, Header: secret, Options: []model.QuestionOption{{Label: "Approve", Description: secret}}, Intent: intent}},
			Answer:    answer, CreatedAt: at, ResolvedAt: &at}
		if err := s.CreateQuestion(ctx, row); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.QuestionMetadataForTurns(ctx, session.ID, []string{out.TurnID})
	if err != nil || len(rows) != 2 {
		t.Fatalf("metadata rows: %d, %v", len(rows), err)
	}
	for _, row := range rows {
		if row.SessionID != session.ID || row.TurnID != out.TurnID || row.CreatedAt.IsZero() || row.ResolvedAt == nil || row.Answer == nil || row.Answer.Answers[0].Selected[0] != "Approve" {
			t.Fatalf("missing resolution metadata: %+v", row)
		}
		if len(row.Questions) != 1 || row.Questions[0].ID != "plan" || row.Questions[0].Intent == nil || *row.Questions[0].Intent != *intent {
			t.Fatalf("missing question identity: %+v", row.Questions)
		}
		raw, err := json.Marshal(model.UserQuestion(row))
		if err != nil {
			t.Fatal(err)
		}
		for _, hidden := range []string{"private-plan-document", `"question":`, `"detail":`, `"header":`, `"options":`} {
			if strings.Contains(string(raw), hidden) {
				t.Fatalf("metadata contains document field %s", hidden)
			}
		}
		full, err := s.Question(ctx, session.ID, row.ID)
		if err != nil || len(full.Questions) != 1 || full.Questions[0].Detail != secret || len(full.Questions[0].Options) != 1 {
			t.Fatalf("explicit question lost document: %v", err)
		}
		if _, err = s.Question(ctx, uuid.NewString(), row.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("foreign session question accepted: %v", err)
		}
	}
	for _, ids := range [][]string{nil, {}, {uuid.NewString()}} {
		rows, err := s.QuestionMetadataForTurns(ctx, session.ID, ids)
		if err != nil || rows == nil || len(rows) != 0 {
			t.Fatalf("empty projection: %+v, %v", rows, err)
		}
	}
	rows, err = s.QuestionMetadataForTurns(ctx, uuid.NewString(), []string{out.TurnID})
	if err != nil || len(rows) != 0 {
		t.Fatalf("foreign metadata returned: %+v, %v", rows, err)
	}
}
