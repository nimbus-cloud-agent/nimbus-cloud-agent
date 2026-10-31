package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/config"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store/testdb"
)

// Each test gets a disposable schema and no scheduler: no real model calls or
// changes to the developer's sessions. Exercise the actual SQL migrations/routes.
func optionsDB(t *testing.T) *store.Store {
	t.Helper()
	return testdb.Open(t)
}
func optionsRouter(db *store.Store) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	s := &Server{S: db, C: config.Config{DeepSeekKey: "test-only-no-network"}}
	s.routes(r)
	return r
}

func optionRequest(r *gin.Engine, method, path string, body any) (int, map[string]any) {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, "/api"+path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var data map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &data)
	return w.Code, data
}

func expectOptions(t *testing.T, r *gin.Engine, status int, method, path string, body any) map[string]any {
	t.Helper()
	code, data := optionRequest(r, method, path, body)
	if code != status {
		t.Fatalf("%s %s: got %d, want %d: %v", method, path, code, status, data)
	}
	return data
}

func TestSessionPresetLifecycle(t *testing.T) {
	db := optionsDB(t)
	r := optionsRouter(db)
	s := expectOptions(t, r, 201, "POST", "/sessions", nil)
	path := "/sessions/" + s["id"].(string)
	if s["mode_locked"] != false {
		t.Fatal("blank session is locked")
	}
	opts := s["options"].(map[string]any)
	if opts["model"] != "deepseek-flash" {
		t.Fatal(opts)
	}
	opts["mode"] = "ptc"
	expectOptions(t, r, 200, "PATCH", path, gin.H{"options": opts})
	opts["mode"] = "standard"
	expectOptions(t, r, 200, "PATCH", path, gin.H{"options": opts})
	cid := uuid.NewString()
	// Partial overrides must not be ignored when model is absent.
	first := expectOptions(t, r, 202, "POST", path+"/messages", gin.H{"message": "hello", "client_msg_id": cid, "mode": "ptc"})
	s = expectOptions(t, r, 200, "GET", path, nil)
	if s["mode_locked"] != true || s["options"].(map[string]any)["mode"] != "ptc" {
		t.Fatal(s)
	}
	duplicate := expectOptions(t, r, 202, "POST", path+"/messages", gin.H{"message": "retry", "client_msg_id": cid, "mode": "standard"})
	if first["id"] != duplicate["id"] {
		t.Fatal("duplicate lost idempotency")
	}
	expectOptions(t, r, 409, "PATCH", path, gin.H{"options": opts})
	expectOptions(t, r, 409, "POST", path+"/messages", gin.H{"message": "hello", "client_msg_id": uuid.NewString(), "mode": "standard"})
	opts["mode"], opts["thinking"], opts["plan"] = "ptc", "max", true
	expectOptions(t, r, 200, "PATCH", path, gin.H{"options": opts})
	next := expectOptions(t, r, 202, "POST", path+"/messages", gin.H{"message": "next", "client_msg_id": uuid.NewString(), "plan": false})
	got := next["options"].(map[string]any)
	if got["plan"] != false || got["thinking"] != "max" || got["mode"] != "ptc" {
		t.Fatal(got)
	}
	if err := db.UpdateOutbox(context.Background(), first["id"].(string), store.OutboxChanges{Status: ptr("failed")}); err != nil {
		t.Fatal(err)
	}
	// Construct a new router to demonstrate the lock is durable, not process state.
	r = optionsRouter(db)
	expectOptions(t, r, 409, "POST", path+"/messages", gin.H{"message": "hello", "client_msg_id": uuid.NewString(), "mode": "standard"})
	expectOptions(t, r, 202, "POST", path+"/messages/"+first["id"].(string)+"/retry", nil)
}

func TestConcurrentFirstMessagePreset(t *testing.T) {
	db := optionsDB(t)
	r := optionsRouter(db)
	for i := 0; i < 12; i++ {
		s := expectOptions(t, r, 201, "POST", "/sessions", nil)
		path := "/sessions/" + s["id"].(string)
		var wg sync.WaitGroup
		statuses := make([]int, 2)
		for j, mode := range []string{"standard", "ptc"} {
			wg.Add(1)
			go func(j int, mode string) {
				defer wg.Done()
				statuses[j], _ = optionRequest(r, "POST", path+"/messages", gin.H{"message": "hello", "client_msg_id": uuid.NewString(), "mode": mode})
			}(j, mode)
		}
		wg.Wait()
		if !(statuses[0] == 202 && statuses[1] == 409 || statuses[0] == 409 && statuses[1] == 202) {
			t.Fatal(statuses)
		}
		s = expectOptions(t, r, 200, "GET", path, nil)
		if s["mode_locked"] != true {
			t.Fatal("race left session unlocked")
		}
		// A concurrent PATCH cannot change a preset already accepted by the queue.
		opts := s["options"].(map[string]any)
		if opts["mode"] == "standard" {
			opts["mode"] = "ptc"
		} else {
			opts["mode"] = "standard"
		}
		expectOptions(t, r, 409, "PATCH", path, gin.H{"options": opts})
	}
}

func TestImageMessageOwnershipAndAdmission(t *testing.T) {
	db := optionsDB(t)
	r := optionsRouter(db)
	a := expectOptions(t, r, 201, "POST", "/sessions", nil)
	b := expectOptions(t, r, 201, "POST", "/sessions", nil)
	sid := a["id"].(string)
	path := "/sessions/" + sid
	img := store.Image{ID: uuid.NewString(), SessionID: sid, Ref: model.ImageRef{Name: "test.png", MediaType: "image/png", Bytes: 12, Width: 1, Height: 1}, Data: []byte("fixture")}
	if e := db.CreateImage(context.Background(), img); e != nil {
		t.Fatal(e)
	}
	// Text pretending to be a raster is refused before starting a runtime.
	expectOptions(t, r, 400, "POST", path+"/images", gin.H{"name": "fake.png", "media_type": "image/png", "data": "bm90IGFuIGltYWdl"})
	expectOptions(t, r, 404, "GET", "/sessions/"+b["id"].(string)+"/images/"+img.ID, nil)
	expectOptions(t, r, 400, "POST", "/sessions/"+b["id"].(string)+"/messages", gin.H{"message": "", "client_msg_id": uuid.NewString(), "image_ids": []string{img.ID}})
	for _, ids := range [][]string{{img.ID, img.ID}, {uuid.NewString()}, {"bad"}, {img.ID, img.ID, img.ID, img.ID, img.ID}} {
		expectOptions(t, r, 400, "POST", path+"/messages", gin.H{"message": "", "client_msg_id": uuid.NewString(), "image_ids": ids})
	}
	state := expectOptions(t, r, 200, "GET", path, nil)
	if state["mode_locked"] != false {
		t.Fatal("invalid attachment locked preset")
	}
	result := expectOptions(t, r, 202, "POST", path+"/messages", gin.H{"message": "", "client_msg_id": uuid.NewString(), "image_ids": []string{img.ID}})
	if result["content"] != "" || len(result["images"].([]any)) != 1 {
		t.Fatal("image-only message lost attachment")
	}
	if e := db.DeleteSession(context.Background(), sid); e != nil {
		t.Fatal(e)
	}
	if _, err := db.Image(context.Background(), sid, img.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("session deletion retained image data", err)
	}
}
