package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store/testdb"
)

func filesRouter(t *testing.T, state string) (*gin.Engine, *fakeSandboxService, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := testdb.Open(t)
	fake := newFakeSandboxService()
	s := &Server{S: db, Sandboxes: fake}
	r := gin.New()
	s.routes(r)
	ctx := context.Background()
	sess := model.Session{ID: uuid.NewString(), Title: "files", AgentState: "idle", SyncedSeq: -1, Options: model.DefaultOptions(), CreatedAt: time.Now(), LastActiveAt: time.Now()}
	if err := db.CreateSession(ctx, store.Session(sess)); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	row := store.Sandbox{ID: uuid.NewString(), SessionID: sess.ID, E2bSandboxID: "sb-1", State: state, CreatedAt: now, UpdatedAt: now, LastActiveAt: now, StartedAt: now}
	if err := db.CreateSandbox(ctx, row); err != nil {
		t.Fatal(err)
	}
	return r, fake, sess.ID
}

func filesGet(r *gin.Engine, target string) (int, []byte, map[string]string) {
	req := httptest.NewRequest("GET", target, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	headers := map[string]string{}
	for _, key := range []string{"Content-Type", "Content-Security-Policy", "X-Content-Type-Options", "Cache-Control"} {
		if value := w.Header().Get(key); value != "" {
			headers[key] = value
		}
	}
	return w.Code, w.Body.Bytes(), headers
}

func TestSandboxPathScopesToWorkspace(t *testing.T) {
	for _, test := range []struct{ in, want string }{
		{"", "/workspace"},
		{"/workspace", "/workspace"},
		{"workspace", "/workspace"},
		{"/workspace/", "/workspace"},
		{"/workspace/src", "/workspace/src"},
		{"/workspace/src/../README.md", "/workspace/README.md"},
	} {
		got, err := sandboxPath(test.in)
		if err != nil || got != test.want {
			t.Fatalf("%q: got %q, %v; want %q", test.in, got, err, test.want)
		}
	}
	for _, bad := range []string{"/", "/etc/passwd", "/workspace/../etc/passwd", "/workspaces", "a\x00b"} {
		if _, err := sandboxPath(bad); err == nil {
			t.Fatalf("%q was accepted", bad)
		}
	}
}

func TestSandboxFilesList(t *testing.T) {
	r, fake, id := filesRouter(t, "running")
	fake.dirs["/workspace"] = []model.FileEntry{
		{Name: "src", Path: "/workspace/src", Type: "directory"},
		{Name: "README.md", Path: "/workspace/README.md", Type: "file", Size: 12},
	}
	code, body, headers := filesGet(r, "/api/sessions/"+id+"/sandbox/files")
	if code != 200 || headers["Cache-Control"] != "no-store" {
		t.Fatalf("list: %d %s %v", code, body, headers)
	}
	var got struct {
		Path    string            `json:"path"`
		Entries []model.FileEntry `json:"entries"`
	}
	if err := json.Unmarshal(body, &got); err != nil || got.Path != "/workspace" || len(got.Entries) != 2 || got.Entries[1].Name != "README.md" || got.Entries[1].Size != 12 {
		t.Fatalf("list body: %s, %v", body, err)
	}
	// A path outside the workspace is a contract error, not a sandbox call.
	if code, _, _ := filesGet(r, "/api/sessions/"+id+"/sandbox/files?path=/etc"); code != 400 {
		t.Fatalf("escape path: got %d, want 400", code)
	}
	// The fake has no /workspace/src, so the sandbox error surfaces as 502.
	if code, _, _ := filesGet(r, "/api/sessions/"+id+"/sandbox/files?path=/workspace/src"); code != 502 {
		t.Fatalf("missing dir: got %d, want 502", code)
	}
}

func TestSandboxFilesRequireRunningSandbox(t *testing.T) {
	r, _, id := filesRouter(t, "paused")
	if code, body, _ := filesGet(r, "/api/sessions/"+id+"/sandbox/files"); code != 409 || !strings.Contains(string(body), "Start the sandbox") {
		t.Fatalf("paused list: %d %s", code, body)
	}
	if code, _, _ := filesGet(r, "/api/sessions/"+id+"/sandbox/raw/workspace/README.md"); code != 409 {
		t.Fatalf("paused raw: got %d, want 409", code)
	}
}

func TestSandboxRawFilePreview(t *testing.T) {
	r, fake, id := filesRouter(t, "running")
	fake.files["/workspace/page.html"] = []byte("<h1>hi</h1>")
	fake.files["/workspace/note.md"] = []byte("# note")
	code, body, headers := filesGet(r, "/api/sessions/"+id+"/sandbox/raw/workspace/page.html")
	if code != 200 || string(body) != "<h1>hi</h1>" {
		t.Fatalf("html raw: %d %s", code, body)
	}
	if headers["Content-Type"] != "text/html; charset=utf-8" || headers["Content-Security-Policy"] != "sandbox" || headers["X-Content-Type-Options"] != "nosniff" {
		t.Fatalf("html headers: %v", headers)
	}
	_, _, headers = filesGet(r, "/api/sessions/"+id+"/sandbox/raw/workspace/note.md")
	if headers["Content-Type"] != "text/plain; charset=utf-8" || headers["Content-Security-Policy"] != "sandbox" {
		t.Fatalf("text headers: %v", headers)
	}
	// Image types are pinned here rather than read from the host mime database,
	// so the preview type is the same on every platform.
	fake.files["/workspace/logo.png"] = []byte("\x89PNG")
	if code, _, headers := filesGet(r, "/api/sessions/"+id+"/sandbox/raw/workspace/logo.png"); code != 200 || headers["Content-Type"] != "image/png" {
		t.Fatalf("image raw: %d %v", code, headers)
	}
	// A path outside the workspace never reaches the sandbox.
	if code, _, _ := filesGet(r, "/api/sessions/"+id+"/sandbox/raw/etc/passwd"); code != 400 {
		t.Fatalf("escape raw: got %d, want 400", code)
	}
	// Missing files surface the sandbox error.
	if code, _, _ := filesGet(r, "/api/sessions/"+id+"/sandbox/raw/workspace/missing.txt"); code != 502 {
		t.Fatalf("missing raw: got %d, want 502", code)
	}
}

func TestSandboxRawFileSizeCap(t *testing.T) {
	r, fake, id := filesRouter(t, "running")
	fake.files["/workspace/big.bin"] = []byte("x")
	fake.sizes["/workspace/big.bin"] = maxPreviewSize + 1
	code, body, _ := filesGet(r, "/api/sessions/"+id+"/sandbox/raw/workspace/big.bin")
	if code != 413 || !strings.Contains(string(body), "Previews stop at") {
		t.Fatalf("oversize raw: %d %s", code, body)
	}
}
