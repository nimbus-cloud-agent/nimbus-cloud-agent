package server

import (
	"context"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
)

func TestQuestionResourceIsExplicitAndSessionScoped(t *testing.T) {
	db := optionsDB(t)
	r := optionsRouter(db)
	sess := expectOptions(t, r, 201, "POST", "/sessions", nil)
	other := expectOptions(t, r, 201, "POST", "/sessions", nil)
	sid := sess["id"].(string)
	path := "/sessions/" + sid
	out := expectOptions(t, r, 202, "POST", path+"/messages", gin.H{"message": "review", "client_msg_id": uuid.NewString()})
	row := store.UserQuestion{ID: uuid.NewString(), SessionID: sid, TurnID: out["turn_id"].(string), Status: "pending", CreatedAt: time.Now(),
		Questions: []model.Question{{ID: "review", Question: "Review this plan", Detail: "# Exact full plan", Options: []model.QuestionOption{{Label: "Approve"}}, Intent: &model.QuestionIntent{Kind: "plan-review", Approve: "Approve"}}}}
	if err := db.CreateQuestion(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	resource := path + "/questions/" + row.ID
	full := expectOptions(t, r, 200, "GET", resource, nil)
	questions := full["questions"].([]any)
	if full["id"] != row.ID || full["status"] != "pending" || len(questions) != 1 || questions[0].(map[string]any)["detail"] != "# Exact full plan" {
		t.Fatalf("incomplete review: %+v", full)
	}
	// Fetching the document must never make a decision or change pending status.
	persisted, err := db.Question(context.Background(), sid, row.ID)
	if err != nil || persisted.Status != "pending" || persisted.Answer != nil {
		t.Fatalf("GET resolved review: %+v, %v", persisted, err)
	}
	expectOptions(t, r, 404, "GET", "/sessions/"+other["id"].(string)+"/questions/"+row.ID, nil)
	expectOptions(t, r, 404, "GET", path+"/questions/"+uuid.NewString(), nil)
	expectOptions(t, r, 400, "GET", path+"/questions/invalid", nil)
	expectOptions(t, r, 404, "GET", "/sessions/"+uuid.NewString()+"/questions/"+row.ID, nil)
}
