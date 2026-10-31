package server

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/executor"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
)

func (s *Server) imageRoutes(r *gin.Engine) {
	r.POST("/api/sessions/:id/images", func(c *gin.Context) {
		sess, ok := s.session(c)
		if !ok {
			return
		}
		var in struct {
			Name      string `json:"name"`
			MediaType string `json:"media_type"`
			Data      string `json:"data"`
		}
		if err := c.ShouldBindJSON(&in); err != nil {
			s.fail(c, 400, err)
			return
		}
		data, err := base64.StdEncoding.Strict().DecodeString(in.Data)
		if err != nil || len(data) == 0 || len(data) > 5<<20 {
			s.fail(c, 400, fmt.Errorf("图片编码无效或超过 5 MB"))
			return
		}
		media := http.DetectContentType(data)
		if !strings.Contains("|image/png|image/jpeg|image/gif|image/webp|", "|"+media+"|") || media != in.MediaType {
			s.fail(c, 400, fmt.Errorf("请选择有效的 PNG、JPEG、WebP 或 GIF 图片"))
			return
		}
		name := filepath.Base(strings.ReplaceAll(in.Name, "\\", "/"))
		if name == "." || name == "/" || strings.TrimSpace(name) == "" {
			name = "图片"
		}
		name = string([]rune(name)[:min(len([]rune(name)), 120)])
		// Normalize using the same native attachment service the provider consumes.
		// PostgreSQL owns the resulting bytes; the temporary host cache is removed.
		dir, err := os.MkdirTemp("", "nimbus-image-")
		if err != nil {
			s.fail(c, 500, err)
			return
		}
		defer os.RemoveAll(dir)
		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()
		result, err := s.R.Images(ctx, executor.ImageRequest{WorkDir: dir, Name: name, MediaType: media, Data: in.Data})
		if err != nil {
			s.fail(c, 400, fmt.Errorf("图片无法读取：%s", err))
			return
		}
		if len(result.Data) == 0 {
			s.fail(c, 500, fmt.Errorf("图片处理失败"))
			return
		}
		img := model.Image{ID: uuid.NewString(), SessionID: sess.ID, Ref: result.Ref, Data: result.Data}
		if err := s.S.CreateImage(c.Request.Context(), store.Image(img)); err != nil {
			s.fail(c, 500, err)
			return
		}
		c.JSON(201, imageView(img))
	})
	r.GET("/api/sessions/:id/images/:image", func(c *gin.Context) {
		sess, ok := s.session(c)
		if !ok {
			return
		}
		if _, err := uuid.Parse(c.Param("image")); err != nil {
			c.Status(404)
			return
		}
		row, err := s.S.Image(c.Request.Context(), sess.ID, c.Param("image"))
		if err != nil {
			c.Status(404)
			return
		}
		c.Data(200, row.Ref.MediaType, row.Data)
	})
}

func imageView(img model.Image) model.MessageImage {
	return model.MessageImage{ID: img.ID, Name: img.Ref.Name, URL: "/api/sessions/" + img.SessionID + "/images/" + img.ID, Attachment: img.Ref}
}
func messageImages(ctx context.Context, tx *store.Store, sid string, ids []string) ([]model.MessageImage, error) {
	if len(ids) > 4 {
		return nil, fmt.Errorf("每条消息最多 4 张图片")
	}
	images := make([]model.MessageImage, 0, len(ids))
	seen := map[string]bool{}
	var total int64
	for _, id := range ids {
		if _, err := uuid.Parse(id); err != nil || seen[id] {
			return nil, fmt.Errorf("图片 ID 无效或重复")
		}
		seen[id] = true
		row, err := tx.ImageMetadata(ctx, sid, id)
		if err != nil {
			return nil, fmt.Errorf("图片不属于当前会话，请重新上传: %w", err)
		}
		img := model.Image(row)
		total += img.Ref.Bytes
		images = append(images, imageView(img))
	}
	if total > 10<<20 {
		return nil, fmt.Errorf("每条消息图片总大小不能超过 10 MB")
	}
	return images, nil
}
