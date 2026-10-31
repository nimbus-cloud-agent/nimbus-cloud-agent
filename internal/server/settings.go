package server

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/executor"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
)

func ValidateSkill(x model.Skill) error {
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
		var in struct {
			DeepSeekKey *string `json:"deepseek_api_key"`
			E2BTemplate *string `json:"e2b_template"`
		}
		if e := c.ShouldBindJSON(&in); e != nil {
			s.fail(c, 400, e)
			return
		}
		for _, field := range []struct {
			key   string
			value *string
		}{{"deepseek_api_key", in.DeepSeekKey}, {"e2b_template", in.E2BTemplate}} {
			if field.value != nil && *field.value != "••••••••" {
				v := *field.value
				if strings.TrimSpace(v) == "" {
					s.fail(c, 400, fmt.Errorf("设置值不能为空"))
					return
				}
				if e := s.S.Set(field.key, strings.TrimSpace(v)); e != nil {
					s.fail(c, 500, e)
					return
				}
			}
		}
		c.JSON(200, gin.H{"ok": true})
	})
	r.GET("/api/skills", func(c *gin.Context) {
		rows, err := s.S.Skills(c.Request.Context(), false)
		if err != nil {
			s.fail(c, 500, err)
			return
		}
		x := make([]model.Skill, 0, len(rows))
		for _, row := range rows {
			x = append(x, skillModel(row))
		}
		c.JSON(200, x)
	})
	save := func(c *gin.Context) {
		x, e := decode[model.Skill](c)
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
		e = s.S.Transaction(c.Request.Context(), func(tx *store.Store) error {
			row := skillRow(x)
			if c.Param("id") == "" {
				if err := tx.CreateSkill(c.Request.Context(), row); err != nil {
					return err
				}
			} else {
				if err := tx.UpdateSkill(c.Request.Context(), row); err != nil {
					return err
				}
			}
			for i := range row.Files {
				row.Files[i].SkillID = x.ID
			}
			return tx.ReplaceSkillFiles(c.Request.Context(), x.ID, row.Files)
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
		if e := s.S.DeleteSkill(c.Request.Context(), c.Param("id")); e != nil {
			s.fail(c, 400, e)
			return
		}
		c.Status(204)
	})
	r.GET("/api/mcp/servers", func(c *gin.Context) {
		rows, err := s.S.MCPServers(c.Request.Context(), false)
		if err != nil {
			s.fail(c, 500, err)
			return
		}
		x := make([]model.MCPServer, 0, len(rows))
		for _, row := range rows {
			x = append(x, model.MCPServer(row))
		}
		c.JSON(200, x)
	})
	saveMCP := func(c *gin.Context) {
		x, e := decode[model.MCPServer](c)
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
			x.OAuthData = model.OAuthData{}
			e = s.S.CreateMCPServer(c.Request.Context(), store.MCPServer(x))
		} else {
			x.ID = c.Param("id")
			row, err := s.S.MCPServer(c.Request.Context(), x.ID)
			if err != nil {
				s.fail(c, 404, err)
				return
			}
			old := model.MCPServer(row)
			x.OAuthData, x.OAuthState = old.OAuthData, old.OAuthState
			if old.URL != x.URL {
				x.OAuthData = model.OAuthData{}
				x.OAuthState = "none"
			}
			e = s.S.UpdateMCPServer(c.Request.Context(), store.MCPServer(x))
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
		if e := s.S.DeleteMCPServer(c.Request.Context(), c.Param("id")); e != nil {
			s.fail(c, 400, e)
			return
		}
		c.Status(204)
	})
	r.POST("/api/mcp/servers/:id/test", func(c *gin.Context) {
		row, err := s.S.MCPServer(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.Status(404)
			return
		}
		x := model.MCPServer(row)
		d, e := s.mcp(c.Request.Context(), x, "list", model.MCPArguments{})
		if e != nil {
			s.fail(c, 502, e)
			return
		}
		c.JSON(200, d)
	})
	r.POST("/api/mcp/servers/:id/oauth/start", func(c *gin.Context) {
		row, err := s.S.MCPServer(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.Status(404)
			return
		}
		x := model.MCPServer(row)
		state := x.ID + "." + uuid.NewString()
		x.OAuthData.State = state
		x.OAuthData.ExpiresAt = time.Now().Add(10 * time.Minute).Unix()
		d, e := s.mcp(c.Request.Context(), x, "authorize", model.MCPArguments{})
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
		row, err := s.S.MCPServer(c.Request.Context(), parts[0])
		if err != nil {
			c.Status(400)
			return
		}
		x := model.MCPServer(row)
		if x.OAuthData.State != state || x.OAuthData.ExpiresAt < time.Now().Unix() || c.Query("code") == "" {
			c.String(400, "OAuth 授权过期或被拒绝，请返回设置重新授权。")
			return
		}
		if _, e := s.mcp(c.Request.Context(), x, "callback", model.MCPArguments{Code: c.Query("code")}); e != nil {
			s.fail(c, 502, e)
			return
		}
		c.Redirect(303, "/?settings=mcp")
	})
}
func (s *Server) mcp(ctx context.Context, x model.MCPServer, op string, args model.MCPArguments) (model.MCPResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	result, err := s.R.MCP(ctx, executor.MCPInput{Op: op, URL: x.URL, OAuthData: x.OAuthData, CallbackURL: s.BaseURL + "/oauth/callback", Args: args})
	if err != nil {
		return result, err
	}
	if err = s.S.SaveOAuth(ctx, x.ID, result.OAuthState, result.OAuthData); err != nil {
		return result, err
	}
	if result.Error != "" {
		return result, fmt.Errorf("%s", result.Error)
	}
	return result, nil
}
func (s *Server) mcpTools(ctx context.Context, servers []model.MCPServer) []model.MCPTool {
	result := []model.MCPTool{}
	for _, server := range servers {
		discovered, err := s.mcp(ctx, server, "list", model.MCPArguments{})
		if err != nil {
			result = append(result, model.MCPTool{Server: server.Name, Error: s.redact(err.Error())})
			continue
		}
		for _, tool := range discovered.Tools {
			tool.Server = server.Name
			tool.Name = "mcp__" + strings.ReplaceAll(server.ID, "-", "") + "__" + tool.Name
			result = append(result, tool)
		}
	}
	return result
}
func (s *Server) callMCP(ctx context.Context, name string, args model.DynamicJSON) (model.MCPCallResult, error) {
	parts := strings.SplitN(name, "__", 3)
	if len(parts) != 3 || len(parts[1]) != 32 {
		return model.MCPCallResult{}, fmt.Errorf("invalid MCP tool")
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return model.MCPCallResult{}, err
	}
	row, err := s.S.MCPServer(ctx, id.String())
	if err != nil || !row.Enabled {
		return model.MCPCallResult{}, fmt.Errorf("MCP 服务未启用")
	}
	result, err := s.mcp(ctx, model.MCPServer(row), "call", model.MCPArguments{Name: parts[2], Arguments: args})
	if err != nil {
		return model.MCPCallResult{}, err
	}
	if result.Result == nil {
		return model.MCPCallResult{}, fmt.Errorf("MCP returned no tool result")
	}
	return *result.Result, nil
}
