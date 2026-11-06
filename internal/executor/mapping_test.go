package executor

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
)

// turnRequest is pure mapping: no LLM, no sandbox, no database is involved.
// The test uses in-memory inputs only, so the LLM/Sandbox/DB are mocked out
// by construction (they are never called).
func TestTurnRequestHasNoMode(t *testing.T) {
	wire := turnRequest(TurnInput{
		SessionID: "session", TurnID: "turn", MessageID: "message", Message: "hello",
		Options: model.Options{Model: "deepseek-flash", Thinking: "low", Plan: true},
		Skills:  []model.Skill{{ID: "s", Name: "skill", Files: []model.SkillFile{{Path: "SKILL.md", Content: "hi"}}}},
	})
	raw, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"mode":`) || strings.Contains(string(raw), "session_mode") {
		t.Fatalf("wire request still contains mode: %s", raw)
	}
	if wire.Options.Model != "deepseek-flash" || wire.Options.Thinking != "low" || !wire.Options.Plan {
		t.Fatalf("wire options lost fields: %+v", wire.Options)
	}
	if len(wire.Skills) != 1 || wire.Skills[0].Name != "skill" {
		t.Fatalf("wire skills lost: %+v", wire.Skills)
	}
}

func TestTurnRequestMapsAllInputs(t *testing.T) {
	var schema model.DynamicJSON
	if err := json.Unmarshal([]byte(`{"type":"object"}`), &schema); err != nil {
		t.Fatal(err)
	}
	wire := turnRequest(TurnInput{
		SessionID: "s", TurnID: "t", MessageID: "m", Message: "hi",
		Options:     model.DefaultOptions(),
		Records:     []model.NativeRecord{{Seq: 1, Type: "user/message"}},
		MCP:         []model.MCPTool{{Name: "tool", InputSchema: schema}},
		Images:      []model.MessageImage{{ID: "img", Attachment: model.ImageRef{Name: "a.png"}}},
		Attachments: []model.Attachment{{Ref: model.ImageRef{Name: "a.png"}, Data: []byte("x")}},
	})
	if len(wire.Records) != 1 || len(wire.MCP) != 1 || len(wire.Images) != 1 || len(wire.Attachments) != 1 {
		t.Fatalf("wire lost inputs: records=%d mcp=%d images=%d attachments=%d", len(wire.Records), len(wire.MCP), len(wire.Images), len(wire.Attachments))
	}
}

func TestOAuthRequestMapsFields(t *testing.T) {
	wire := oauthRequest(model.OAuthData{
		State:     "state",
		ExpiresAt: 123,
		Verifier:  "verifier",
		Client:    &model.OAuthClient{ClientID: "cid", RedirectURIs: []string{"https://example.invalid/cb"}},
		Tokens:    &model.OAuthTokens{AccessToken: "tok"},
	})
	if wire.State == nil || *wire.State != "state" || wire.ExpiresAt == nil || *wire.ExpiresAt != 123 || wire.Verifier == nil || *wire.Verifier != "verifier" {
		t.Fatalf("oauth wire lost fields: %+v", wire)
	}
	if !strings.Contains(string(wire.Client), `"client_id":"cid"`) || !strings.Contains(string(wire.Tokens), `"access_token":"tok"`) {
		t.Fatalf("oauth wire lost client/tokens: client=%s tokens=%s", wire.Client, wire.Tokens)
	}
}

func TestEncodeToolResultLibrary(t *testing.T) {
	raw, err := encodeToolResult(model.ToolResult{Library: &model.LibraryResult{ID: "file-1", Name: "notes.md", Version: 2, MimeType: "text/markdown", SizeBytes: 12, Content: "hello", Link: "nimbus://library/file-1", SHA256: "abc"}})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["id"] != "file-1" || decoded["name"] != "notes.md" || decoded["version"] != float64(2) || decoded["link"] != "nimbus://library/file-1" {
		t.Fatalf("library result lost fields: %s", raw)
	}
	// A list result keeps its files array.
	raw, err = encodeToolResult(model.ToolResult{Library: &model.LibraryResult{Files: []model.LibraryFileSummary{{ID: "f", Name: "a.txt", MimeType: "text/plain", SizeBytes: 3, Version: 1}}}})
	if err != nil || !strings.Contains(string(raw), `"files":[{"id":"f"`) {
		t.Fatalf("library list encoding: %s, %v", raw, err)
	}
}

func TestEncodeToolResultRequiresExactlyOneVariant(t *testing.T) {
	if _, err := encodeToolResult(model.ToolResult{}); err == nil {
		t.Fatal("empty result was accepted")
	}
	if _, err := encodeToolResult(model.ToolResult{Library: &model.LibraryResult{}, MCP: &model.MCPCallResult{}}); err == nil {
		t.Fatal("two variants were accepted")
	}
	for _, result := range []model.ToolResult{
		{Sandbox: &model.SandboxView{}}, {Command: &model.CommandResult{}}, {ReadFile: &model.FileReadResult{}},
		{WriteFile: &model.FileWriteResult{}}, {MCP: &model.MCPCallResult{}},
	} {
		if _, err := encodeToolResult(result); err != nil {
			t.Fatalf("single variant rejected: %v", err)
		}
	}
}

func TestLibraryArgsDecode(t *testing.T) {
	var call model.ToolCall
	if err := json.Unmarshal([]byte(`{"id":"f","name":"a.md","version":3,"offset":10,"length":100,"path":"/workspace/a.md","content":"hi","mime_type":"text/markdown"}`), &call.Library); err != nil {
		t.Fatal(err)
	}
	args := call.Library
	if args.ID != "f" || args.Name != "a.md" || args.Version != 3 || args.Offset != 10 || args.Length != 100 || args.Path != "/workspace/a.md" || args.Content != "hi" || args.MimeType != "text/markdown" {
		t.Fatalf("library args lost fields: %+v", args)
	}
}
