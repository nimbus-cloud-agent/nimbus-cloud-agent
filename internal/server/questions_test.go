package server

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
)

func TestPlanReviewLifecycle(t *testing.T) {
	db := optionsDB(t)
	r := optionsRouter(db)
	s := &Server{S: db}
	startTestChanges(t, s)
	a := expectOptions(t, r, 201, "POST", "/sessions", nil)
	b := expectOptions(t, r, 201, "POST", "/sessions", nil)
	sid := a["id"].(string)
	path := "/sessions/" + sid
	opts := a["options"].(map[string]any)
	opts["plan"] = true
	expectOptions(t, r, 200, "PATCH", path, gin.H{"options": opts})
	out := expectOptions(t, r, 202, "POST", path+"/messages", gin.H{"message": "plan test", "client_msg_id": uuid.NewString()})
	row, err := db.Outbox(context.Background(), out["id"].(string), sid)
	if err != nil {
		t.Fatal(err)
	}
	o := model.Outbox(row)
	request := model.QuestionRequest{Questions: []model.Question{{ID: "plan-review", Options: []model.QuestionOption{{Label: "Approve"}, {Label: "Keep planning"}}, Detail: "# Plan\nTest", Intent: &model.QuestionIntent{Kind: "plan-review", Approve: "Approve"}}}}
	answer := gin.H{"answers": []gin.H{{"id": "plan-review", "selected": []string{"Approve"}}}}
	start := func() (model.UserQuestion, context.CancelFunc, <-chan error) {
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		done := make(chan error, 1)
		go func() { _, e := s.waitQuestion(ctx, o, request); done <- e }()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			questions, err := s.questions(ctx, sid)
			if err != nil {
				t.Fatal(err)
			}
			for _, q := range questions {
				if q.Status == "pending" {
					return q, cancel, done
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("question never became visible")
		return model.UserQuestion{}, cancel, done
	}
	await := func(done <-chan error, wantError bool) {
		t.Helper()
		select {
		case e := <-done:
			if (e != nil) != wantError {
				t.Fatalf("unexpected question result: %v", e)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("question did not settle")
		}
	}
	q, _, done := start()
	endpoint := path + "/questions/" + q.ID + "/answer"
	// A fresh browser/router can recover and answer the durable pending review.
	r = optionsRouter(db)
	snapshot := expectOptions(t, r, 200, "GET", path+"/events", nil)
	if len(snapshot["questions"].([]any)) != 1 {
		t.Fatal("review missing from snapshot")
	}
	expectOptions(t, r, 404, "POST", "/sessions/"+b["id"].(string)+"/questions/"+q.ID+"/answer", answer)
	for _, bad := range []any{gin.H{}, gin.H{"answers": []gin.H{{"id": "wrong", "selected": []string{"Approve"}}}}, gin.H{"answers": []gin.H{{"id": "plan-review", "selected": []string{"Unknown"}}}}, gin.H{"answers": []gin.H{{"id": "plan-review", "selected": []string{"Approve", "Keep planning"}}}}, gin.H{"dismissed": true, "answers": []gin.H{{"id": "plan-review", "selected": []string{"Approve"}}}}} {
		expectOptions(t, r, 400, "POST", endpoint, bad)
	}
	expectOptions(t, r, 200, "POST", endpoint, answer)
	expectOptions(t, r, 200, "POST", endpoint, answer)
	expectOptions(t, r, 409, "POST", endpoint, gin.H{"dismissed": true})
	await(done, false)
	// Only the native plan event, not submission of an answer, changes mode.
	sess := expectOptions(t, r, 200, "GET", path, nil)
	if sess["options"].(map[string]any)["plan"] != true {
		t.Fatal("approval skipped native transition")
	}
	if e := s.appendRecord(context.Background(), sid, o.ID, model.NativeRecord{Seq: 0, Type: "plan/mode", Data: model.EventData{Active: ptr(false)}}); e != nil {
		t.Fatal(e)
	}
	sess = expectOptions(t, r, 200, "GET", path, nil)
	if sess["options"].(map[string]any)["plan"] != false {
		t.Fatal("native transition did not update session")
	}
	q, cancel, done := start()
	cancel()
	await(done, true)
	expectOptions(t, r, 409, "POST", path+"/questions/"+q.ID+"/answer", answer)
	q, _, done = start()
	if e := db.InterruptQuestions(context.Background(), ""); e != nil {
		t.Fatal(e)
	}
	await(done, true)
	expectOptions(t, r, 409, "POST", path+"/questions/"+q.ID+"/answer", answer)
	// Two tabs cannot make conflicting decisions; the first committed one wins.
	q, _, done = start()
	var wg sync.WaitGroup
	statuses := make([]int, 2)
	for i, body := range []any{answer, gin.H{"dismissed": true}} {
		wg.Add(1)
		go func(i int, body any) {
			defer wg.Done()
			statuses[i], _ = optionRequest(r, "POST", path+"/questions/"+q.ID+"/answer", body)
		}(i, body)
	}
	wg.Wait()
	if !(statuses[0] == 200 && statuses[1] == 409 || statuses[0] == 409 && statuses[1] == 200) {
		t.Fatal(statuses)
	}
	await(done, false)
	persisted, err := db.Question(context.Background(), q.ID)
	if err != nil || persisted.Answer == nil || persisted.ResolvedAt == nil {
		t.Fatal("decision not durable", err)
	}
}
