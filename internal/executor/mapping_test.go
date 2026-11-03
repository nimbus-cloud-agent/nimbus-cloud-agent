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
	wire, err := turnRequest(TurnInput{
		SessionID: "session", TurnID: "turn", MessageID: "message", Message: "hello",
		Options: model.Options{Model: "deepseek-flash", Thinking: "low", Plan: true},
		Skills:  []model.Skill{{ID: "s", Name: "skill", Files: []model.SkillFile{{Path: "SKILL.md", Content: "hi"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
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
	wire, err := turnRequest(TurnInput{
		SessionID: "s", TurnID: "t", MessageID: "m", Message: "hi",
		Options:     model.DefaultOptions(),
		Records:     []model.NativeRecord{{Seq: 1, Type: "user/message"}},
		MCP:         []model.MCPTool{{Name: "tool", InputSchema: schema}},
		Images:      []model.MessageImage{{ID: "img", Attachment: model.ImageRef{Name: "a.png"}}},
		Attachments: []model.Attachment{{Ref: model.ImageRef{Name: "a.png"}, Data: []byte("x")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(wire.Records) != 1 || len(wire.MCP) != 1 || len(wire.Images) != 1 || len(wire.Attachments) != 1 {
		t.Fatalf("wire lost inputs: records=%d mcp=%d images=%d attachments=%d", len(wire.Records), len(wire.MCP), len(wire.Images), len(wire.Attachments))
	}
}
