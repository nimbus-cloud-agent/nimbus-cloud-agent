package server

import (
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
	"strings"
	"testing"
)

func TestSkillValidation(t *testing.T) {
	valid := store.Skill{Name: "data-cleaning", Files: []store.SkillFile{{Path: "SKILL.md", Content: "hello 世界"}}}
	if e := ValidateSkill(valid); e != nil {
		t.Fatal(e)
	}
	cases := []struct {
		name  string
		files []store.SkillFile
	}{{"101 files", make([]store.SkillFile, 101)}, {"oversized", []store.SkillFile{{Path: "a.txt", Content: strings.Repeat("x", 5*1024*1024+1)}}}, {"binary content", []store.SkillFile{{Path: "a.txt", Content: "a\x00b"}}}, {"binary name", []store.SkillFile{{Path: "a.png", Content: "text"}}}, {"escape", []store.SkillFile{{Path: "../key", Content: "text"}}}, {"duplicate", []store.SkillFile{{Path: "a.md", Content: "1"}, {Path: "a.md", Content: "2"}}}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := valid
			s.Files = c.files
			if ValidateSkill(s) == nil {
				t.Fatal("accepted invalid skill")
			}
		})
	}
}
func TestOptions(t *testing.T) {
	for _, level := range []string{"off", "low", "high", "max"} {
		if e := validOptions(store.Raw(map[string]any{"model": "deepseek-flash", "thinking": level, "mode": "ptc"})); e != nil {
			t.Fatal(e)
		}
	}
	if validOptions(store.Raw(map[string]any{"model": "deepseek-flash", "thinking": "medium", "mode": "standard"})) == nil {
		t.Fatal("accepted non-native thinking level")
	}
	for _, model := range []string{"deepseek-chat", "deepseek-reasoner", "deepseek-v4-pro", ""} {
		if validOptions(store.Raw(map[string]any{"model": model, "thinking": "off", "mode": "standard"})) == nil {
			t.Fatalf("accepted unsupported model %q", model)
		}
	}
}
