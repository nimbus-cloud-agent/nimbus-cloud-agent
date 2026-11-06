package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
)

func TestLibraryWriteRequiresCurrentBaseVersion(t *testing.T) {
	s, db := libraryServer(t)
	ctx := context.Background()
	if _, err := s.libraryWrite(ctx, model.LibraryArgs{Name: "notes.md", Content: "v1"}); err != nil {
		t.Fatal(err)
	}
	_, err := s.libraryWrite(ctx, model.LibraryArgs{Name: "notes.md", Content: "stale"})
	if err == nil || !strings.Contains(err.Error(), "library_read") || !strings.Contains(err.Error(), "base_version=1") {
		t.Fatalf("conflict guidance: %v", err)
	}
	written, err := s.libraryWrite(ctx, model.LibraryArgs{Name: "notes.md", Content: "v2", BaseVersion: 1})
	if err != nil || written.Version != 2 {
		t.Fatalf("write: %+v %v", written, err)
	}
	var conflict *store.VersionConflict
	if _, _, err := db.WriteLibraryFile(ctx, "notes.md", "", strings.NewReader("again"), 1); !errors.As(err, &conflict) || conflict.Current != 2 {
		t.Fatalf("store conflict: %v", err)
	}
}

func TestLibraryDraftSaveReadPublish(t *testing.T) {
	s, db := libraryServer(t)
	ctx := context.Background()
	file, version, err := db.WriteLibraryFile(ctx, "doc.md", "", strings.NewReader("v1"), 0)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := s.S.SaveLibraryDraft(ctx, file.ID, version.VersionNumber, file.MimeType, "user edit")
	if err != nil {
		t.Fatal(err)
	}
	listed, err := s.libraryDrafts(ctx, model.LibraryArgs{})
	if err != nil || len(listed.Drafts) != 1 {
		t.Fatalf("list: %+v %v", listed, err)
	}
	if got := listed.Drafts[0]; got.BaseVersion != 1 || got.CurrentVersion != 1 || got.Content != "" || got.FileName != "doc.md" {
		t.Fatalf("summary: %+v", got)
	}
	read, err := s.libraryDrafts(ctx, model.LibraryArgs{DraftID: draft.ID})
	if err != nil || read.Draft == nil || read.Draft.Content != "user edit" {
		t.Fatalf("read: %+v %v", read, err)
	}
	if _, err = s.libraryWrite(ctx, model.LibraryArgs{Name: "doc.md", Content: "merged", BaseVersion: 1, DraftID: draft.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.S.LibraryDraftByFile(ctx, file.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("draft survived publication")
	}
}

func TestLibraryEditEndpointsReportConflicts(t *testing.T) {
	_, db := libraryServer(t)
	r := optionsRouter(db)
	ctx := context.Background()
	file, _, err := db.WriteLibraryFile(ctx, "page.html", "", strings.NewReader("<h1>v1</h1>"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if status, body := optionRequest(r, "POST", "/library/"+file.ID+"/versions", map[string]any{"base_version": 1, "content": "<h1>v2</h1>"}); status != 201 {
		t.Fatalf("edit: %d %v", status, body)
	}
	status, body := optionRequest(r, "POST", "/library/"+file.ID+"/versions", map[string]any{"base_version": 1, "content": "stale"})
	if status != 409 || body["current_version"] != float64(2) {
		t.Fatalf("conflict: %d %v", status, body)
	}
	status, body = optionRequest(r, "POST", "/library/"+file.ID+"/draft", map[string]any{"base_version": 1, "content": "draft text"})
	if status != 201 || body["base_version"] != float64(1) || body["content"] != "draft text" {
		t.Fatalf("draft: %d %v", status, body)
	}
	if status, body = optionRequest(r, "GET", "/library/"+file.ID+"/draft", nil); status != 200 || body["base_version"] != float64(1) {
		t.Fatalf("draft get: %d %v", status, body)
	}
	if status, _ = optionRequest(r, "DELETE", "/library/"+file.ID+"/draft", nil); status != 204 {
		t.Fatalf("draft delete: %d", status)
	}
}

// Every library tool the runtime exposes must pass the dispatcher's allowlist;
// a tool missing here fails at runtime as "unknown or forbidden tool".
func TestLibraryToolsReachTheDispatcher(t *testing.T) {
	s, _ := libraryServer(t)
	for _, name := range []string{"library_list", "library_read", "library_write", "library_versions", "library_drafts"} {
		_, err := s.executeTool(context.Background(), toolScope{SessionID: "session", TurnID: "turn"}, model.ToolCall{Name: name, CallID: "call"})
		if err != nil && strings.Contains(err.Error(), "unknown or forbidden tool") {
			t.Fatalf("%s is not dispatched: %v", name, err)
		}
	}
}
