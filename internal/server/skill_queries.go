package server

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
)

type skillFileMetadata struct {
	Path string `json:"path"`
}
type skillMetadata struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Enabled     bool                `json:"enabled"`
	Files       []skillFileMetadata `json:"files"`
}

func skillMetadataModel(row store.Skill) skillMetadata {
	files := make([]skillFileMetadata, 0, len(row.Files))
	for _, file := range row.Files {
		files = append(files, skillFileMetadata{Path: file.Path})
	}
	return skillMetadata{row.ID, row.Name, row.Description, row.Enabled, files}
}

func (s *Server) listSkills(c *gin.Context) {
	includeContent := true
	if raw, ok := c.GetQuery("include_content"); ok {
		var err error
		includeContent, err = strconv.ParseBool(raw)
		if err != nil {
			s.fail(c, 400, fmt.Errorf("include_content must be a boolean"))
			return
		}
	}
	rows, err := s.S.QuerySkills(c.Request.Context(), store.SkillFilter{IncludeContent: includeContent})
	if err != nil {
		s.fail(c, 500, err)
		return
	}
	if !includeContent {
		result := make([]skillMetadata, 0, len(rows))
		for _, row := range rows {
			result = append(result, skillMetadataModel(row))
		}
		c.JSON(200, result)
		return
	}
	result := make([]model.Skill, 0, len(rows))
	for _, row := range rows {
		result = append(result, skillModel(row))
	}
	c.JSON(200, result)
}

func (s *Server) skillQueryRoutes(r *gin.Engine) {
	r.GET("/api/skills/:id/files", func(c *gin.Context) {
		filePath := c.Query("path")
		if filePath == "" {
			s.fail(c, 400, fmt.Errorf("path is required"))
			return
		}
		file, err := s.S.SkillFile(c.Request.Context(), c.Param("id"), filePath)
		if err != nil {
			status := 500
			if errors.Is(err, store.ErrNotFound) {
				status = 404
			}
			s.fail(c, status, err)
			return
		}
		c.JSON(200, model.SkillFile(file))
	})
	r.PATCH("/api/skills/:id", s.patchSkill)
}

type skillFilePatch struct {
	Path    string  `json:"path"`
	Content *string `json:"content"`
}
type skillPatch struct {
	Name        *string           `json:"name"`
	Description *string           `json:"description"`
	Enabled     *bool             `json:"enabled"`
	Files       *[]skillFilePatch `json:"files"`
}

func (s *Server) patchSkill(c *gin.Context) {
	in, err := decode[skillPatch](c)
	if err != nil {
		s.fail(c, 400, err)
		return
	}
	var result skillMetadata
	err = s.S.Transaction(c.Request.Context(), func(tx *store.Store) error {
		rows, err := tx.QuerySkills(c.Request.Context(), store.SkillFilter{ID: c.Param("id"), ForUpdate: true})
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return store.ErrNotFound
		}
		row := rows[0]
		if in.Name != nil {
			row.Name = *in.Name
		}
		if in.Description != nil {
			row.Description = *in.Description
		}
		if in.Enabled != nil {
			row.Enabled = *in.Enabled
		}
		var changed []store.SkillFile
		var paths []string
		if in.Files != nil {
			existing := make(map[string]bool, len(row.Files))
			for _, file := range row.Files {
				existing[file.Path] = true
			}
			row.Files = make([]store.SkillFile, 0, len(*in.Files))
			for _, file := range *in.Files {
				value := store.SkillFile{SkillID: row.ID, Path: file.Path}
				if file.Content != nil {
					value.Content = *file.Content
					changed = append(changed, value)
				} else if !existing[file.Path] {
					return fmt.Errorf("content is required for new file: %s", file.Path)
				}
				paths = append(paths, file.Path)
				row.Files = append(row.Files, value)
			}
		}
		if err := ValidateSkill(skillModel(row)); err != nil {
			return err
		}
		if err := tx.UpdateSkill(c.Request.Context(), row); err != nil {
			return err
		}
		if in.Files != nil {
			if err := tx.PatchSkillFiles(c.Request.Context(), row.ID, paths, changed); err != nil {
				return err
			}
		}
		result = skillMetadataModel(row)
		return nil
	})
	if err != nil {
		status := 400
		if errors.Is(err, store.ErrNotFound) {
			status = 404
		}
		s.fail(c, status, err)
		return
	}
	c.JSON(200, result)
}
