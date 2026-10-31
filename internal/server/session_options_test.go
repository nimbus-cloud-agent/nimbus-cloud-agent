package server

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/config"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Each test gets a disposable schema and no scheduler: no real model calls or
// changes to the developer's sessions. Exercise the actual SQL migrations/routes.
func optionsDB(t *testing.T, through string) (*gorm.DB, func(string)) {
	t.Helper()
	dsn := os.Getenv("NIMBUS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set NIMBUS_TEST_DATABASE_URL for PostgreSQL integration tests")
	}
	cfg := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	admin, err := gorm.Open(postgres.Open(dsn), cfg)
	if err != nil {
		t.Fatal(err)
	}
	schema := "nimbus_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := gorm.Open(postgres.Open(u.String()), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		raw, _ := db.DB()
		raw.Close()
		if err := admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error; err != nil {
			t.Error(err)
		}
		raw, _ = admin.DB()
		raw.Close()
	})
	apply := func(name string) {
		t.Helper()
		sql, err := os.ReadFile(filepath.Join("..", "store", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(string(sql)).Error; err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	files, err := filepath.Glob(filepath.Join("..", "store", "migrations", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if filepath.Base(file) <= through {
			apply(filepath.Base(file))
		}
	}
	return db, apply
}

func optionsRouter(db *gorm.DB) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	s := &Server{S: &store.Store{DB: db}, C: config.Config{DeepSeekKey: "test-only-no-network"}}
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
	db, _ := optionsDB(t, "999")
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
	if err := db.Exec("UPDATE sessions SET locked_mode=NULL WHERE id=?", s["id"]).Error; err == nil {
		t.Fatal("SQL unlocked session")
	}
	if err := db.Exec("UPDATE sessions SET options=jsonb_set(options, '{mode}', '\"standard\"') WHERE id=?", s["id"]).Error; err == nil {
		t.Fatal("SQL changed preset")
	}
	if err := db.Exec("UPDATE outbox_messages SET options=jsonb_set(options, '{mode}', '\"standard\"') WHERE id=?", first["id"]).Error; err == nil {
		t.Fatal("SQL changed queued preset")
	}
	if err := db.Exec("UPDATE outbox_messages SET status='failed' WHERE id=?", first["id"]).Error; err != nil {
		t.Fatal(err)
	}
	// Construct a new router to demonstrate the lock is durable, not process state.
	r = optionsRouter(db)
	expectOptions(t, r, 409, "POST", path+"/messages", gin.H{"message": "hello", "client_msg_id": uuid.NewString(), "mode": "standard"})
	expectOptions(t, r, 202, "POST", path+"/messages/"+first["id"].(string)+"/retry", nil)
}

func TestConcurrentFirstMessagePreset(t *testing.T) {
	db, _ := optionsDB(t, "999")
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
	db, _ := optionsDB(t, "999")
	r := optionsRouter(db)
	a := expectOptions(t, r, 201, "POST", "/sessions", nil)
	b := expectOptions(t, r, 201, "POST", "/sessions", nil)
	sid := a["id"].(string)
	path := "/sessions/" + sid
	img := store.Image{ID: uuid.NewString(), SessionID: sid, Ref: store.Raw(gin.H{"name": "test.png", "mediaType": "image/png", "bytes": 12, "width": 1, "height": 1}), Data: []byte("fixture")}
	if e := db.Create(&img).Error; e != nil {
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
	if e := db.Delete(&store.Session{}, "id=?", sid).Error; e != nil {
		t.Fatal(e)
	}
	var count int64
	db.Model(&store.Image{}).Where("session_id=?", sid).Count(&count)
	if count != 0 {
		t.Fatal("session deletion retained image data")
	}
}

func TestLegacyPresetAndModelMigration(t *testing.T) {
	db, apply := optionsDB(t, "003~")
	id, blank := uuid.NewString(), uuid.NewString()
	for _, sid := range []string{id, blank} {
		if err := db.Exec("INSERT INTO sessions(id) VALUES (?)", sid).Error; err != nil {
			t.Fatal(err)
		}
	}
	// A former mixed session: standard -> PTC, with an incompatible queued turn.
	for i, mode := range []string{"standard", "ptc", "standard"} {
		status := "delivered"
		var seq any = i * 10
		if i == 2 {
			status = "queued"
			seq = nil
		}
		opts := store.Raw(gin.H{"model": "deepseek-chat", "thinking": "off", "plan": false, "mode": mode})
		if err := db.Exec("INSERT INTO outbox_messages(id,session_id,turn_id,client_msg_id,content,options,status,delivered_seq) VALUES (?,?,?,?,?,?,?,?)", uuid.NewString(), id, uuid.NewString(), uuid.NewString(), "old", opts, status, seq).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec("INSERT INTO session_events(session_id,seq,type,payload) VALUES (?,0,'fixture','{\"original\":true}')", id).Error; err != nil {
		t.Fatal(err)
	}
	apply("004_session_preset.sql")
	apply("005_deepseek_flash.sql")
	var s store.Session
	db.First(&s, "id=?", id)
	if s.LockedMode == nil || *s.LockedMode != "ptc" || !bytes.Contains(s.Options, []byte("deepseek-flash")) {
		t.Fatal(s)
	}
	var b store.Session
	db.First(&b, "id=?", blank)
	if b.LockedMode != nil {
		t.Fatal("migration locked blank session")
	}
	var queued store.Outbox
	db.First(&queued, "session_id=? AND delivered_seq IS NULL", id)
	if queued.Status != "failed" || queued.Error == "" {
		t.Fatal(queued)
	}
	r := optionsRouter(db)
	expectOptions(t, r, 409, "POST", "/sessions/"+id+"/messages/"+queued.ID+"/retry", nil)
	var old store.Outbox
	db.First(&old, "session_id=? AND delivered_seq=0", id)
	if !bytes.Contains(old.Options, []byte("deepseek-chat")) {
		t.Fatal("rewrote historical model")
	}
	var ev store.Event
	db.First(&ev, "session_id=?", id)
	if string(ev.Payload) != "{\"original\": true}" {
		t.Fatal("rewrote native event", string(ev.Payload))
	}
}
