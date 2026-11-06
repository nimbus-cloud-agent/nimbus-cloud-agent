package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSkillProjectionAndPatchPreserveUnloadedFiles(t *testing.T) {
	db := optionsDB(t)
	r := gin.New()
	(&Server{S: db}).settingsRoutes(r)
	skill := expectOptions(t, r, 200, "POST", "/skills", gin.H{
		"name": "lazy-files", "enabled": true,
		"files": []gin.H{{"path": "SKILL.md", "content": "instructions"}, {"path": "reference.md", "content": "unopened reference"}},
	})
	id := skill["id"].(string)
	status, raw, _ := libraryRequest(r, "GET", "/skills?include_content=false")
	var index []struct {
		ID    string                       `json:"id"`
		Files []map[string]json.RawMessage `json:"files"`
	}
	if err := json.Unmarshal(raw, &index); err != nil || status != 200 || len(index) != 1 || len(index[0].Files) != 2 {
		t.Fatalf("metadata query: %d %s %v", status, raw, err)
	}
	for _, file := range index[0].Files {
		if _, ok := file["content"]; ok {
			t.Fatal("metadata projection returned hidden file content")
		}
	}
	filePath := "/skills/" + id + "/files?path=reference.md"
	if got := expectOptions(t, r, 200, "GET", filePath, nil)["content"]; got != "unopened reference" {
		t.Fatalf("file content: %v", got)
	}
	expectOptions(t, r, 200, "PATCH", "/skills/"+id, gin.H{"enabled": false})
	expectOptions(t, r, 200, "PATCH", "/skills/"+id, gin.H{
		"name":  "renamed",
		"files": []gin.H{{"path": "SKILL.md", "content": "edited instructions"}, {"path": "reference.md"}},
	})
	if got := expectOptions(t, r, 200, "GET", filePath, nil)["content"]; got != "unopened reference" {
		t.Fatalf("unloaded content was overwritten: %v", got)
	}
	if got := expectOptions(t, r, 200, "GET", "/skills/"+id+"/files?path=SKILL.md", nil)["content"]; got != "edited instructions" {
		t.Fatalf("edit was not saved: %v", got)
	}
	expectOptions(t, r, 200, "PATCH", "/skills/"+id, gin.H{
		"files": []gin.H{{"path": "reference.md", "content": ""}, {"path": "new.md", "content": "new file"}},
	})
	expectOptions(t, r, 404, "GET", "/skills/"+id+"/files?path=SKILL.md", nil)
	if got := expectOptions(t, r, 200, "GET", filePath, nil)["content"]; got != "" {
		t.Fatalf("explicit empty content did not clear file: %v", got)
	}
	if got := expectOptions(t, r, 200, "GET", "/skills/"+id+"/files?path=new.md", nil)["content"]; got != "new file" {
		t.Fatalf("new file was not saved: %v", got)
	}
}

func TestSkillPatchRejectsInvalidManifestAtomically(t *testing.T) {
	db := optionsDB(t)
	r := gin.New()
	(&Server{S: db}).settingsRoutes(r)
	skill := expectOptions(t, r, 200, "POST", "/skills", gin.H{
		"name": "original", "enabled": true,
		"files": []gin.H{{"path": "SKILL.md", "content": "must survive"}},
	})
	id := skill["id"].(string)
	for _, files := range [][]gin.H{
		{{"path": "new.md"}},
		{{"path": "../escape.md", "content": "bad"}},
		{{"path": "SKILL.md"}, {"path": "SKILL.md", "content": "duplicate"}},
		{{"path": "SKILL.md", "content": strings.Repeat("x", 5*1024*1024+1)}},
		{},
	} {
		expectOptions(t, r, 400, "PATCH", "/skills/"+id, gin.H{"name": "not committed", "files": files})
		if got := expectOptions(t, r, 200, "GET", "/skills/"+id+"/files?path=SKILL.md", nil)["content"]; got != "must survive" {
			t.Fatalf("failed patch changed content: %v", got)
		}
	}
	expectOptions(t, r, 404, "GET", "/skills/00000000-0000-0000-0000-000000000000/files?path=SKILL.md", nil)
	status, raw, _ := libraryRequest(r, "GET", "/skills?include_content=false")
	var index []skillMetadata
	if err := json.Unmarshal(raw, &index); err != nil || status != 200 || len(index) != 1 || index[0].Name != "original" {
		t.Fatalf("failed patch changed metadata: %d %s %v", status, raw, err)
	}
}
