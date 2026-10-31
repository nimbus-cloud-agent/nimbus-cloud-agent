package store

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testJSON(v any) []byte {
	b, e := json.Marshal(v)
	if e != nil {
		panic(e)
	}
	return b
}
func migrationDB(t *testing.T, through string) (*gorm.DB, func(string)) {
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
		sql, err := os.ReadFile(filepath.Join("migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(string(sql)).Error; err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	files, err := filepath.Glob(filepath.Join("migrations", "*.sql"))
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

func TestLegacyPresetAndModelMigration(t *testing.T) {
	db, apply := migrationDB(t, "003~")
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
		opts := testJSON(gin.H{"model": "deepseek-chat", "thinking": "off", "plan": false, "mode": mode})
		if err := db.Exec("INSERT INTO outbox_messages(id,session_id,turn_id,client_msg_id,content,options,status,delivered_seq) VALUES (?,?,?,?,?,?,?,?)", uuid.NewString(), id, uuid.NewString(), uuid.NewString(), "old", opts, status, seq).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec("INSERT INTO session_events(session_id,seq,type,payload) VALUES (?,0,'fixture','{\"original\":true}')", id).Error; err != nil {
		t.Fatal(err)
	}
	apply("004_session_preset.sql")
	apply("005_deepseek_flash.sql")
	var s Session
	db.First(&s, "id=?", id)
	if s.LockedMode == nil || *s.LockedMode != "ptc" || s.Options.Model != "deepseek-flash" {
		t.Fatal(s)
	}
	var b Session
	db.First(&b, "id=?", blank)
	if b.LockedMode != nil {
		t.Fatal("migration locked blank session")
	}
	var queued Outbox
	db.First(&queued, "session_id=? AND delivered_seq IS NULL", id)
	if queued.Status != "failed" || queued.Error == "" {
		t.Fatal(queued)
	}

	var old Outbox
	db.First(&old, "session_id=? AND delivered_seq=0", id)
	if old.Options.Model != "deepseek-chat" {
		t.Fatal("rewrote historical model")
	}
	var ev Event
	db.First(&ev, "session_id=?", id)
	encoded, _ := json.Marshal(ev.Payload)
	if !bytes.Contains(encoded, []byte("original")) {
		t.Fatal("rewrote native event", string(encoded))
	}
}
