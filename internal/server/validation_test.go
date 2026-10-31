package server

import (
	"strings"
	"testing"

	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
)

func TestSkillValidation(t *testing.T) {
	valid := model.Skill{Name: "data-cleaning", Files: []model.SkillFile{{Path: "SKILL.md", Content: "hello 世界"}}}
	if e := ValidateSkill(valid); e != nil {
		t.Fatal(e)
	}
	cases := []struct {
		name  string
		files []model.SkillFile
	}{{"101 files", make([]model.SkillFile, 101)}, {"oversized", []model.SkillFile{{Path: "a.txt", Content: strings.Repeat("x", 5*1024*1024+1)}}}, {"binary content", []model.SkillFile{{Path: "a.txt", Content: "a\x00b"}}}, {"binary name", []model.SkillFile{{Path: "a.png", Content: "text"}}}, {"escape", []model.SkillFile{{Path: "../key", Content: "text"}}}, {"duplicate", []model.SkillFile{{Path: "a.md", Content: "1"}, {Path: "a.md", Content: "2"}}}}
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
		if e := validOptions(model.Options{Model: "deepseek-flash", Thinking: level, Mode: "ptc"}); e != nil {
			t.Fatal(e)
		}
	}
	if validOptions(model.Options{Model: "deepseek-flash", Thinking: "medium", Mode: "standard"}) == nil {
		t.Fatal("accepted non-native thinking level")
	}
	for _, name := range []string{"deepseek-chat", "deepseek-reasoner", "deepseek-v4-pro", ""} {
		if validOptions(model.Options{Model: name, Thinking: "off", Mode: "standard"}) == nil {
			t.Fatalf("accepted unsupported model %q", name)
		}
	}
}
