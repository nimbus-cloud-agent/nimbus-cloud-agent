package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/sandbox"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store/testdb"
)

// fakeSandboxService is an in-memory sandbox.Service for the copy tools:
// no E2B, no network. WriteFileStream captures uploads; ReadFileStream
// serves files the test put there.
type fakeSandboxService struct {
	files map[string][]byte
	sizes map[string]int64
}

func newFakeSandboxService() *fakeSandboxService {
	return &fakeSandboxService{files: map[string][]byte{}, sizes: map[string]int64{}}
}
func (f *fakeSandboxService) Create(context.Context, string, string) (model.SandboxInfo, error) {
	return model.SandboxInfo{}, errors.New("fake sandbox cannot create")
}
func (f *fakeSandboxService) Connect(context.Context, string) (model.SandboxInfo, error) {
	return model.SandboxInfo{}, errors.New("fake sandbox cannot connect")
}
func (f *fakeSandboxService) Info(context.Context, string) (model.SandboxInfo, error) {
	return model.SandboxInfo{}, errors.New("fake sandbox cannot info")
}
func (f *fakeSandboxService) Pause(context.Context, string) error {
	return errors.New("fake sandbox cannot pause")
}
func (f *fakeSandboxService) Kill(context.Context, string) error {
	return errors.New("fake sandbox cannot kill")
}
func (f *fakeSandboxService) Exec(context.Context, string, model.SandboxArgs, func(model.CommandOutput) error) (model.CommandResult, error) {
	return model.CommandResult{}, errors.New("fake sandbox cannot exec")
}
func (f *fakeSandboxService) ReadFile(context.Context, string, string) (model.FileReadResult, error) {
	return model.FileReadResult{}, errors.New("fake sandbox cannot read file")
}
func (f *fakeSandboxService) WriteFile(context.Context, string, string, string) (model.FileWriteResult, error) {
	return model.FileWriteResult{}, errors.New("fake sandbox cannot write file")
}
func (f *fakeSandboxService) WriteFileStream(_ context.Context, _, path string, r io.Reader) (model.FileWriteResult, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return model.FileWriteResult{}, err
	}
	f.files[path] = data
	return model.FileWriteResult{Path: path, Written: true}, nil
}
func (f *fakeSandboxService) ReadFileStream(_ context.Context, _, path string) (io.ReadCloser, int64, error) {
	data, ok := f.files[path]
	if !ok {
		return nil, 0, errors.New("file not found in fake sandbox")
	}
	size := int64(len(data))
	if override, ok := f.sizes[path]; ok {
		size = override
	}
	return io.NopCloser(bytes.NewReader(data)), size, nil
}
func (f *fakeSandboxService) OpenTerminal(context.Context, string) (sandbox.Terminal, error) {
	return nil, errors.New("fake sandbox cannot open a terminal")
}

var _ sandbox.Service = (*fakeSandboxService)(nil)

func libraryServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	db := testdb.Open(t)
	return &Server{S: db, Sandboxes: newFakeSandboxService()}, db
}

func libraryRequest(r *gin.Engine, method, path string) (int, []byte, map[string]string) {
	req := httptest.NewRequest(method, "/api"+path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	headers := map[string]string{}
	for _, key := range []string{"Content-Type", "Content-Disposition", "Content-Security-Policy", "X-Content-Type-Options"} {
		if value := w.Header().Get(key); value != "" {
			headers[key] = value
		}
	}
	return w.Code, w.Body.Bytes(), headers
}

func TestLibraryHTTPListDetailContent(t *testing.T) {
	db := optionsDB(t)
	r := optionsRouter(db)
	ctx := context.Background()
	code, body, _ := libraryRequest(r, "GET", "/library")
	if code != 200 {
		t.Fatalf("empty list: %d %s", code, body)
	}
	var list struct {
		Files []model.LibraryFileSummary `json:"files"`
	}
	if err := json.Unmarshal(body, &list); err != nil || list.Files == nil || len(list.Files) != 0 {
		t.Fatalf("empty list body: %s, %v", body, err)
	}
	content := "<h1>hello</h1>"
	file, version, err := db.WriteLibraryFile(ctx, "page.html", "", strings.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	code, body, _ = libraryRequest(r, "GET", "/library")
	if code != 200 {
		t.Fatalf("list: %d %s", code, body)
	}
	if err := json.Unmarshal(body, &list); err != nil || len(list.Files) != 1 || list.Files[0].Name != "page.html" || list.Files[0].Version != 1 || list.Files[0].MimeType != "text/html" {
		t.Fatalf("list body: %s, %v", body, err)
	}
	code, body, _ = libraryRequest(r, "GET", "/library/"+file.ID)
	if code != 200 {
		t.Fatalf("detail: %d %s", code, body)
	}
	var detail libraryDetailView
	if err := json.Unmarshal(body, &detail); err != nil || detail.Name != "page.html" || detail.Version != 1 || detail.SizeBytes != int64(len(content)) || len(detail.Versions) != 1 || detail.Versions[0].SHA256 != version.SHA256 {
		t.Fatalf("detail body: %s, %v", body, err)
	}
	code, body, headers := libraryRequest(r, "GET", "/library/"+file.ID+"/content")
	if code != 200 || string(body) != content || headers["Content-Type"] != "text/html" || headers["Content-Disposition"] != "inline" || headers["Content-Security-Policy"] != "sandbox" {
		t.Fatalf("content: %d %q headers %v", code, body, headers)
	}
	code, body, headers = libraryRequest(r, "GET", "/library/"+file.ID+"/content?version=1")
	if code != 200 || string(body) != content {
		t.Fatalf("versioned content: %d %s", code, body)
	}
	for _, bad := range []string{"/library/" + file.ID + "/content?version=2", "/library/" + file.ID + "/content?version=nope", "/library/" + uuid.NewString(), "/library/" + uuid.NewString() + "/content", "/library/not-a-uuid", "/library/not-a-uuid/content"} {
		if code, _, _ := libraryRequest(r, "GET", bad); code != 404 {
			t.Fatalf("%s: got %d, want 404", bad, code)
		}
	}
	// A non-HTML file gets no CSP sandbox header.
	if _, _, err := db.WriteLibraryFile(ctx, "notes.md", "", strings.NewReader("# hi")); err != nil {
		t.Fatal(err)
	}
	rows, err := db.LibraryFiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Name == "notes.md" {
			_, _, headers := libraryRequest(r, "GET", "/library/"+row.ID+"/content")
			if _, ok := headers["Content-Security-Policy"]; ok {
				t.Fatal("markdown content got a CSP sandbox header")
			}
		}
	}
}

func TestLibraryToolReadWriteVersions(t *testing.T) {
	s, db := libraryServer(t)
	ctx := context.Background()
	if _, err := s.libraryRead(ctx, model.LibraryArgs{}); err == nil {
		t.Fatal("read without id or name was accepted")
	}
	written, err := s.libraryWrite(ctx, model.LibraryArgs{Name: "notes.md", Content: "line1\nline2\nline3\n"})
	if err != nil {
		t.Fatal(err)
	}
	if written.Version != 1 || written.Link != "nimbus://library/"+written.ID || written.MimeType != "text/markdown" || written.SHA256 == "" {
		t.Fatalf("write result: %+v", written)
	}
	read, err := s.libraryRead(ctx, model.LibraryArgs{Name: "notes.md"})
	if err != nil || read.Content != "line1\nline2\nline3\n" || read.Truncated || read.Version != 1 {
		t.Fatalf("read: %+v, %v", read, err)
	}
	paged, err := s.libraryRead(ctx, model.LibraryArgs{Name: "notes.md", Offset: 6, Length: 5})
	if err != nil || paged.Content != "line2" || !paged.Truncated {
		t.Fatalf("paged read: %+v, %v", paged, err)
	}
	tail, err := s.libraryRead(ctx, model.LibraryArgs{Name: "notes.md", Offset: 12})
	if err != nil || tail.Content != "line3\n" || tail.Truncated {
		t.Fatalf("tail read: %+v, %v", tail, err)
	}
	if _, err := s.libraryWrite(ctx, model.LibraryArgs{Name: "notes.md", Content: "v2"}); err != nil {
		t.Fatal(err)
	}
	old, err := s.libraryRead(ctx, model.LibraryArgs{Name: "notes.md", Version: 1})
	if err != nil || old.Content != "line1\nline2\nline3\n" || old.Version != 1 {
		t.Fatalf("old version read: %+v, %v", old, err)
	}
	if _, err := s.libraryRead(ctx, model.LibraryArgs{Name: "notes.md", Version: 9}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing version: %v", err)
	}
	versions, err := s.libraryVersions(ctx, model.LibraryArgs{ID: written.ID})
	if err != nil || len(versions.Versions) != 2 || versions.Versions[0].Version != 1 || versions.Versions[1].Version != 2 {
		t.Fatalf("versions: %+v, %v", versions, err)
	}
	list, err := s.libraryList(ctx)
	if err != nil || len(list.Files) != 1 || list.Files[0].Version != 2 {
		t.Fatalf("list: %+v, %v", list, err)
	}
	// Validation errors.
	for _, args := range []model.LibraryArgs{
		{Name: "a/b.md", Content: "x"},
		{Name: "", Content: "x"},
		{Name: "bad.md", Content: "bad \xff utf8"},
	} {
		if _, err := s.libraryWrite(ctx, args); err == nil {
			t.Fatalf("write accepted: %+v", args)
		}
	}
	if _, err := s.libraryRead(ctx, model.LibraryArgs{Name: "notes.md", Offset: -1}); err == nil {
		t.Fatal("negative offset accepted")
	}
	// An image file reads back as base64; a binary file refuses a text read.
	png := []byte{0x89, 'P', 'N', 'G', 1, 2, 3}
	if _, _, err := db.WriteLibraryFile(ctx, "pic.png", "", bytes.NewReader(png)); err != nil {
		t.Fatal(err)
	}
	img, err := s.libraryRead(ctx, model.LibraryArgs{Name: "pic.png"})
	if err != nil || img.Image == nil || img.Image.MediaType != "image/png" || img.Image.Data != base64.StdEncoding.EncodeToString(png) {
		t.Fatalf("image read: %+v, %v", img, err)
	}
	// An image over the read cap refuses a direct read and points at the
	// sandbox copy instead of sending a huge base64 payload to the agent.
	bigPNG := make([]byte, store.LibraryMaxImageRead+1)
	bigPNG[0] = 0x89
	if _, _, err := db.WriteLibraryFile(ctx, "big.png", "", bytes.NewReader(bigPNG)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.libraryRead(ctx, model.LibraryArgs{Name: "big.png"}); err == nil || !strings.Contains(err.Error(), "library_copy_to_sandbox") {
		t.Fatalf("oversize image read: %v", err)
	}
	if _, _, err := db.WriteLibraryFile(ctx, "blob.bin", "", bytes.NewReader([]byte{1, 2, 3})); err != nil {
		t.Fatal(err)
	}
	if _, err := s.libraryRead(ctx, model.LibraryArgs{Name: "blob.bin"}); err == nil || !strings.Contains(err.Error(), "library_copy_to_sandbox") {
		t.Fatalf("binary read: %v", err)
	}
}

func TestLibraryCopyToolsStreamThroughFakeSandbox(t *testing.T) {
	s, db := libraryServer(t)
	ctx := context.Background()
	fake := s.Sandboxes.(*fakeSandboxService)
	sb := model.Sandbox{E2bSandboxID: "sb-1"}
	// A multi-chunk file proves the copy streams instead of buffering one
	// whole read: 2 full chunks plus a tail.
	big := bytes.Repeat([]byte("b"), 2*store.LibraryChunkSize+5)
	file, _, err := db.WriteLibraryFile(ctx, "big.bin", "", bytes.NewReader(big))
	if err != nil {
		t.Fatal(err)
	}
	copied, err := s.libraryCopyToSandbox(ctx, sb, model.LibraryArgs{ID: file.ID, Path: "/workspace/big.bin"})
	if err != nil {
		t.Fatal(err)
	}
	if copied.Path != "/workspace/big.bin" || copied.Bytes != int64(len(big)) || copied.Version != 1 {
		t.Fatalf("copy to sandbox: %+v", copied)
	}
	if !bytes.Equal(fake.files["/workspace/big.bin"], big) {
		t.Fatal("copied bytes differ")
	}
	if _, err := s.libraryCopyToSandbox(ctx, sb, model.LibraryArgs{ID: file.ID}); err == nil {
		t.Fatal("copy without a path was accepted")
	}
	// Copy back under an explicit name, then under the path basename.
	fake.files["/workspace/report.md"] = []byte("# from sandbox")
	back, err := s.libraryCopyFromSandbox(ctx, sb, model.LibraryArgs{Path: "/workspace/report.md", Name: "report-copy.md"})
	if err != nil {
		t.Fatal(err)
	}
	if back.Name != "report-copy.md" || back.Version != 1 || back.Link != "nimbus://library/"+back.ID {
		t.Fatalf("copy from sandbox: %+v", back)
	}
	// Read the stored version back through the file row.
	row, err := db.LibraryFile(ctx, back.ID)
	if err != nil || row.CurrentVersionID == nil {
		t.Fatal(row, err)
	}
	stored, _, err := db.ReadVersionRange(ctx, *row.CurrentVersionID, 0, 100)
	if err != nil || string(stored) != "# from sandbox" {
		t.Fatalf("copied-back bytes: %q, %v", stored, err)
	}
	basename, err := s.libraryCopyFromSandbox(ctx, sb, model.LibraryArgs{Path: "/workspace/report.md"})
	if err != nil || basename.Name != "report.md" || basename.Version != 1 {
		t.Fatalf("basename copy: %+v, %v", basename, err)
	}
	// A sandbox file larger than the cap is refused before any write.
	fake.sizes["/workspace/huge.bin"] = store.LibraryMaxFileSize + 1
	fake.files["/workspace/huge.bin"] = []byte("small")
	if _, err := s.libraryCopyFromSandbox(ctx, sb, model.LibraryArgs{Path: "/workspace/huge.bin"}); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversize copy accepted: %v", err)
	}
	if _, err := s.libraryCopyFromSandbox(ctx, sb, model.LibraryArgs{Path: "/workspace/missing"}); err == nil {
		t.Fatal("missing sandbox file was accepted")
	}
}

func TestLibraryToolsViaExecuteTool(t *testing.T) {
	s, db := libraryServer(t)
	ctx := context.Background()
	sess := model.Session{ID: uuid.NewString(), Title: "library", AgentState: "idle", SyncedSeq: -1, Options: model.DefaultOptions(), CreatedAt: time.Now(), LastActiveAt: time.Now()}
	if err := db.CreateSession(ctx, store.Session(sess)); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	sbRow := store.Sandbox{ID: uuid.NewString(), SessionID: sess.ID, E2bSandboxID: "sb-1", State: "running", CreatedAt: now, UpdatedAt: now, LastActiveAt: now, StartedAt: now}
	if err := db.CreateSandbox(ctx, sbRow); err != nil {
		t.Fatal(err)
	}
	scope := toolScope{SessionID: sess.ID, TurnID: uuid.NewString()}
	fake := s.Sandboxes.(*fakeSandboxService)
	// library_list needs no sandbox: provisioning must not happen, and the
	// fake's Create is unreachable by construction (it errors).
	result, err := s.executeTool(ctx, scope, model.ToolCall{Name: "library_list", CallID: "c1"})
	if err != nil || result.Library == nil {
		t.Fatalf("executeTool library_list: %+v, %v", result, err)
	}
	// A write through executeTool, then a copy to the (already running)
	// sandbox through the same entry point.
	if _, err := s.executeTool(ctx, scope, model.ToolCall{Name: "library_write", CallID: "c2", Library: model.LibraryArgs{Name: "via-tool.md", Content: "tool"}}); err != nil {
		t.Fatal(err)
	}
	copied, err := s.executeTool(ctx, scope, model.ToolCall{Name: "library_copy_to_sandbox", CallID: "c3", Library: model.LibraryArgs{Name: "via-tool.md", Path: "/workspace/via-tool.md"}})
	if err != nil || copied.Library == nil || copied.Library.Path != "/workspace/via-tool.md" {
		t.Fatalf("executeTool copy: %+v, %v", copied, err)
	}
	if string(fake.files["/workspace/via-tool.md"]) != "tool" {
		t.Fatal("executeTool copy bytes differ")
	}
	if _, err := s.executeTool(ctx, scope, model.ToolCall{Name: "library_nope", CallID: "c4"}); err == nil {
		t.Fatal("unknown library tool was accepted")
	}
}
