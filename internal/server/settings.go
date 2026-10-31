package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
	"gorm.io/gorm"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode/utf8"
)

func ValidateSkill(x store.Skill) error {
	if strings.TrimSpace(x.Name) == "" {
		return fmt.Errorf("Skill 名称不能为空")
	}
	if len(x.Files) == 0 || len(x.Files) > 100 {
		return fmt.Errorf("每个 Skill 必须包含 1–100 个文本文件")
	}
	seen := map[string]bool{}
	for _, f := range x.Files {
		if f.Path == "" || path.IsAbs(f.Path) || path.Clean(f.Path) != f.Path || strings.Contains(f.Path, "\\") || f.Path == ".." || strings.HasPrefix(f.Path, "../") || seen[f.Path] {
			return fmt.Errorf("文件路径不合法或重复：%s", f.Path)
		}
		seen[f.Path] = true
		if len(f.Content) > 5*1024*1024 {
			return fmt.Errorf("单个文件不得超过 5 MB：%s", f.Path)
		}
		if !utf8.ValidString(f.Content) || strings.ContainsRune(f.Content, 0) {
			return fmt.Errorf("仅支持 UTF-8 文本文件：%s", f.Path)
		}
		for _, r := range f.Content {
			if r < 32 && r != '\n' && r != '\r' && r != '\t' {
				return fmt.Errorf("拒绝二进制文件：%s", f.Path)
			}
		}
		switch strings.ToLower(path.Ext(f.Path)) {
		case ".png", ".jpg", ".jpeg", ".gif", ".pdf", ".zip", ".exe", ".wasm", ".mp3", ".mp4", ".woff", ".ttf":
			return fmt.Errorf("仅支持文本文件：%s", f.Path)
		}
	}
	return nil
}
func (s *Server) settingsRoutes(r *gin.Engine) {
	r.GET("/api/settings", func(c *gin.Context) {
		mask := func(k string) string {
			if k == "" {
				return ""
			}
			return "••••••••"
		}
		c.JSON(200, gin.H{"deepseek_api_key": mask(s.S.Get("deepseek_api_key", s.C.DeepSeekKey)), "e2b_api_key": mask(s.C.E2BKey), "e2b_template": s.S.Get("e2b_template", s.C.E2BTemplate), "idle_timeout_minutes": 15})
	})
	r.PUT("/api/settings", func(c *gin.Context) {
		var in map[string]string
		if e := c.ShouldBindJSON(&in); e != nil {
			s.fail(c, 400, e)
			return
		}
		for _, k := range []string{"deepseek_api_key", "e2b_template"} {
			if v, ok := in[k]; ok && v != "••••••••" {
				if strings.TrimSpace(v) == "" {
					s.fail(c, 400, fmt.Errorf("设置值不能为空"))
					return
				}
				if e := s.S.Set(k, strings.TrimSpace(v)); e != nil {
					s.fail(c, 500, e)
					return
				}
			}
		}
		c.JSON(200, gin.H{"ok": true})
	})
	r.GET("/api/skills", func(c *gin.Context) {
		x := []store.Skill{}
		s.S.DB.Preload("Files").Order("name").Find(&x)
		c.JSON(200, x)
	})
	save := func(c *gin.Context) {
		x, e := decode[store.Skill](c)
		if e != nil {
			s.fail(c, 400, e)
			return
		}
		if e = ValidateSkill(x); e != nil {
			s.fail(c, 400, e)
			return
		}
		if c.Param("id") != "" {
			x.ID = c.Param("id")
		} else {
			x.ID = uuid.NewString()
		}
		e = s.S.DB.Transaction(func(tx *gorm.DB) error {
			if c.Param("id") == "" {
				if e := tx.Omit("Files").Create(&x).Error; e != nil {
					return e
				}
			} else {
				res := tx.Model(&store.Skill{}).Where("id = ?", x.ID).Updates(map[string]any{"name": x.Name, "description": x.Description, "enabled": x.Enabled})
				if res.Error != nil {
					return res.Error
				}
				if res.RowsAffected != 1 {
					return fmt.Errorf("Skill 不存在")
				}
				if e := tx.Where("skill_id = ?", x.ID).Delete(&store.SkillFile{}).Error; e != nil {
					return e
				}
			}
			for i := range x.Files {
				x.Files[i].SkillID = x.ID
			}
			return tx.Create(&x.Files).Error
		})
		if e != nil {
			s.fail(c, 400, e)
			return
		}
		c.JSON(200, x)
	}
	r.POST("/api/skills", save)
	r.PUT("/api/skills/:id", save)
	r.PUT("/api/skills/:id/files", save)
	r.DELETE("/api/skills/:id", func(c *gin.Context) {
		if e := s.S.DB.Delete(&store.Skill{}, "id = ?", c.Param("id")).Error; e != nil {
			s.fail(c, 400, e)
			return
		}
		c.Status(204)
	})
	r.GET("/api/mcp/servers", func(c *gin.Context) { x := []store.MCPServer{}; s.S.DB.Order("name").Find(&x); c.JSON(200, x) })
	saveMCP := func(c *gin.Context) {
		x, e := decode[store.MCPServer](c)
		if e != nil {
			s.fail(c, 400, e)
			return
		}
		u, e := url.Parse(x.URL)
		if e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || x.Name == "" {
			s.fail(c, 400, fmt.Errorf("仅支持 streamable-http 的 HTTP(S) URL，不支持 STDIO 命令"))
			return
		}
		if c.Param("id") == "" {
			x.ID = uuid.NewString()
			x.OAuthState = "none"
			x.OAuthData = store.Raw(gin.H{})
			e = s.S.DB.Create(&x).Error
		} else {
			x.ID = c.Param("id")
			var old store.MCPServer
			s.S.DB.First(&old, "id = ?", x.ID)
			m := map[string]any{"name": x.Name, "url": x.URL, "enabled": x.Enabled}
			if old.URL != x.URL {
				m["oauth_data"] = store.Raw(gin.H{})
				m["oauth_state"] = "none"
			}
			e = s.S.DB.Model(&old).Updates(m).Error
		}
		if e != nil {
			s.fail(c, 400, e)
			return
		}
		c.JSON(200, x)
	}
	r.POST("/api/mcp/servers", saveMCP)
	r.PUT("/api/mcp/servers/:id", saveMCP)
	r.DELETE("/api/mcp/servers/:id", func(c *gin.Context) {
		if e := s.S.DB.Delete(&store.MCPServer{}, "id = ?", c.Param("id")).Error; e != nil {
			s.fail(c, 400, e)
			return
		}
		c.Status(204)
	})
	r.POST("/api/mcp/servers/:id/test", func(c *gin.Context) {
		var x store.MCPServer
		if s.S.DB.First(&x, "id = ?", c.Param("id")).Error != nil {
			c.Status(404)
			return
		}
		d, e := s.mcp(c.Request.Context(), x, "list", nil)
		if e != nil {
			s.fail(c, 502, e)
			return
		}
		c.JSON(200, d)
	})
	r.POST("/api/mcp/servers/:id/oauth/start", func(c *gin.Context) {
		var x store.MCPServer
		if s.S.DB.First(&x, "id = ?", c.Param("id")).Error != nil {
			c.Status(404)
			return
		}
		state := x.ID + "." + uuid.NewString()
		var data map[string]any
		json.Unmarshal(x.OAuthData, &data)
		if data == nil {
			data = map[string]any{}
		}
		data["state"] = state
		data["expires_at"] = time.Now().Add(10 * time.Minute).Unix()
		x.OAuthData = store.Raw(data)
		d, e := s.mcp(c.Request.Context(), x, "authorize", nil)
		if e != nil {
			s.fail(c, 502, e)
			return
		}
		c.JSON(200, d)
	})
	r.GET("/oauth/callback", func(c *gin.Context) {
		state := c.Query("state")
		parts := strings.Split(state, ".")
		if len(parts) != 2 {
			c.String(400, "Invalid OAuth state")
			return
		}
		var x store.MCPServer
		if s.S.DB.First(&x, "id = ?", parts[0]).Error != nil {
			c.Status(400)
			return
		}
		var d map[string]any
		json.Unmarshal(x.OAuthData, &d)
		exp, _ := d["expires_at"].(float64)
		if d["state"] != state || int64(exp) < time.Now().Unix() || c.Query("code") == "" {
			c.String(400, "OAuth 授权过期或被拒绝，请返回设置重新授权。")
			return
		}
		if _, e := s.mcp(c.Request.Context(), x, "callback", map[string]any{"code": c.Query("code")}); e != nil {
			s.fail(c, 502, e)
			return
		}
		c.Redirect(303, "/?settings=mcp")
	})
}
func (s *Server) mcp(ctx context.Context, x store.MCPServer, op string, args any) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	raw, e := s.R.Call(ctx, "mcp", map[string]any{"op": op, "url": x.URL, "oauth_data": x.OAuthData, "callback_url": s.BaseURL + "/oauth/callback", "args": args})
	if e != nil {
		return nil, e
	}
	var d map[string]any
	if e = json.Unmarshal(raw, &d); e != nil {
		return nil, e
	}
	if data, ok := d["oauth_data"]; ok {
		s.S.DB.Model(&x).Updates(map[string]any{"oauth_data": store.Raw(data), "oauth_state": d["oauth_state"]})
		delete(d, "oauth_data")
	}
	if err, ok := d["error"].(string); ok && err != "" {
		return d, fmt.Errorf("%s", err)
	}
	return d, nil
}
func (s *Server) mcpTools(ctx context.Context, servers []store.MCPServer) []map[string]any {
	out := []map[string]any{}
	for _, x := range servers {
		d, e := s.mcp(ctx, x, "list", nil)
		if e != nil {
			out = append(out, map[string]any{"server": x.Name, "error": s.redact(e.Error())})
			continue
		}
		tools, _ := d["tools"].([]any)
		for _, v := range tools {
			t := v.(map[string]any)
			t["server"] = x.Name
			t["name"] = "mcp__" + strings.ReplaceAll(x.ID, "-", "") + "__" + t["name"].(string)
			out = append(out, t)
		}
	}
	return out
}
func (s *Server) callMCP(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	parts := strings.SplitN(name, "__", 3)
	if len(parts) != 3 || len(parts[1]) != 32 {
		return nil, fmt.Errorf("invalid MCP tool")
	}
	id, e := uuid.Parse(parts[1])
	if e != nil {
		return nil, e
	}
	var x store.MCPServer
	if s.S.DB.First(&x, "id = ? AND enabled = true", id.String()).Error != nil {
		return nil, fmt.Errorf("MCP 服务未启用")
	}
	d, e := s.mcp(ctx, x, "call", map[string]any{"name": parts[2], "arguments": args})
	if e != nil {
		return nil, e
	}
	return store.Raw(d["result"]), nil
}
