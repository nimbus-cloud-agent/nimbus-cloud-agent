package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDefaultOptionsHasNoMode(t *testing.T) {
	opts := DefaultOptions()
	if opts.Model != "deepseek-flash" || opts.Thinking != "off" || opts.Plan {
		t.Fatalf("unexpected defaults: %+v", opts)
	}
	raw, err := json.Marshal(opts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"mode":`) {
		t.Fatalf("default options still contain mode: %s", raw)
	}
	var back Options
	if err := json.Unmarshal([]byte(`{"model":"deepseek-flash","thinking":"low","plan":true}`), &back); err != nil {
		t.Fatal(err)
	}
	if back.Model != "deepseek-flash" || back.Thinking != "low" || !back.Plan {
		t.Fatalf("options round-trip failed: %+v", back)
	}
}
