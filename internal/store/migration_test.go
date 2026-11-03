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

func TestModeRemovedMigration(t *testing.T) {
	db, apply := migrationDB(t, "008~")
	id, blank := uuid.NewString(), uuid.NewString()
	for _, sid := range []string{id, blank} {
		if err := db.Exec("INSERT INTO sessions(id) VALUES (?)", sid).Error; err != nil {
			t.Fatal(err)
		}
	}
	opts := testJSON(gin.H{"model": "deepseek-flash", "thinking": "off", "plan": false, "mode": "ptc"})
	if err := db.Exec("INSERT INTO outbox_messages(id,session_id,turn_id,client_msg_id,content,options,status,delivered_seq) VALUES (?,?,?,?,?,?,?,?)", uuid.NewString(), id, uuid.NewString(), uuid.NewString(), "old", opts, "delivered", 0).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO session_events(session_id,seq,type,payload) VALUES (?,0,'fixture','{\"original\":true}')", id).Error; err != nil {
		t.Fatal(err)
	}
	apply("009_remove_mode.sql")
	var s Session
	db.First(&s, "id=?", id)
	if s.Options.Model != "deepseek-flash" || s.Options.Thinking != "off" {
		t.Fatal(s)
	}
	var raw string
	if err := db.Raw("SELECT options::text FROM sessions WHERE id=?", id).Scan(&raw).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, `"mode":`) {
		t.Fatal("session options still contain mode", raw)
	}
	var out Outbox
	db.First(&out, "session_id=?", id)
	if err := db.Raw("SELECT options::text FROM outbox_messages WHERE id=?", out.ID).Scan(&raw).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, `"mode":`) {
		t.Fatal("outbox options still contain mode", raw)
	}
	var columnCount int64
	if err := db.Raw("SELECT count(*) FROM information_schema.columns WHERE table_name='sessions' AND column_name='locked_mode'").Scan(&columnCount).Error; err != nil {
		t.Fatal(err)
	}
	if columnCount != 0 {
		t.Fatal("locked_mode column still exists")
	}
	var ev Event
	db.First(&ev, "session_id=?", id)
	encoded, _ := json.Marshal(ev.Payload)
	if !bytes.Contains(encoded, []byte("original")) {
		t.Fatal("rewrote native event", string(encoded))
	}
	var b Session
	db.First(&b, "id=?", blank)
	if b.Options.Model != "deepseek-flash" {
		t.Fatal("blank session lost defaults", b)
	}
}
