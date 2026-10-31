package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/config"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/executor"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
	"gorm.io/gorm"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type runningTurn struct {
	cancel context.CancelFunc
	done   chan struct{}
}
type tokenScope struct {
	SessionID string
	TurnID    string
}
type Server struct {
	C             config.Config
	S             *store.Store
	R             executor.Runtime
	HTTP          *http.Server
	mu            sync.Mutex
	running       map[string]*runningTurn
	tokens        map[string]tokenScope
	locks         sync.Map
	deleting      sync.Map
	provisioning  sync.Map
	sandboxErrors sync.Map
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	capacity      sync.Mutex
	BaseURL       string
}

func New(c config.Config, st *store.Store, frontend fs.FS) (*Server, error) {
	host, _, e := net.SplitHostPort(c.Addr)
	if e != nil || !(host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()) {
		return nil, fmt.Errorf("single-user control plane must bind loopback (127.0.0.1:8080)")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{C: c, S: st, R: executor.Runtime{Dir: c.RuntimeDir}, running: map[string]*runningTurn{}, tokens: map[string]tokenScope{}, ctx: ctx, cancel: cancel, BaseURL: "http://" + c.Addr}
	if e = st.DB.Transaction(func(tx *gorm.DB) error {
		if e := tx.Model(&store.Outbox{}).Where("status = 'starting'").Updates(map[string]any{"status": "failed", "error": "后端重启，本轮尚未送达，请重发", "outcome": "interrupted"}).Error; e != nil {
			return e
		}
		if e := tx.Model(&store.Outbox{}).Where("status = 'delivered' AND outcome = ''").Update("outcome", "interrupted").Error; e != nil {
			return e
		}
		return tx.Model(&store.Session{}).Where("agent_state <> 'idle'").Update("agent_state", "idle").Error
	}); e != nil {
		return nil, e
	}
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), s.guard)
	r.GET("/api/health", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true, "harness": "0.2.1-rc.5"}) })
	s.routes(r)
	r.POST("/internal/tools", s.internalTool)
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") || strings.HasPrefix(c.Request.URL.Path, "/internal/") {
			c.JSON(404, gin.H{"error": "not found"})
			return
		}
		c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self' ws:; img-src 'self' data:; font-src 'self' data:; frame-ancestors 'none'")
		name := strings.TrimPrefix(c.Request.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		if _, err := fs.Stat(frontend, name); err != nil {
			name = "index.html"
		}
		c.Request.URL.Path = "/" + name
		if name == "index.html" {
			c.Request.URL.Path = "/"
		}
		http.FileServer(http.FS(frontend)).ServeHTTP(c.Writer, c.Request)
	})
	s.HTTP = &http.Server{Addr: c.Addr, Handler: r, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 1 << 20}
	s.wg.Add(1)
	go s.scheduler()
	s.wg.Add(1)
	go s.reaper()
	return s, nil
}
func (s *Server) guard(c *gin.Context) {
	host, _, _ := net.SplitHostPort(c.Request.Host)
	if host == "" {
		host = c.Request.Host
	}
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		c.AbortWithStatusJSON(403, gin.H{"error": "invalid Host"})
		return
	}
	if origin := c.GetHeader("Origin"); origin != "" {
		u, e := url.Parse(origin)
		if e != nil || u.Host != c.Request.Host {
			c.AbortWithStatusJSON(403, gin.H{"error": "cross-origin requests are disabled"})
			return
		}
	}
	limit := int64(2 << 20)
	if strings.HasPrefix(c.Request.URL.Path, "/api/sessions/") && strings.HasSuffix(c.Request.URL.Path, "/images") {
		limit = 8 << 20
	}
	if strings.HasPrefix(c.Request.URL.Path, "/api/skills") {
		limit = 512 << 20
	}
	if c.Request.URL.Path == "/internal/tools" {
		limit = 64 << 20
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "no-store")
	c.Next()
}
func (s *Server) redact(msg string) string {
	for _, key := range []string{s.C.E2BKey, s.C.DeepSeekKey, s.S.Get("deepseek_api_key", "")} {
		if key != "" {
			msg = strings.ReplaceAll(msg, key, "[REDACTED]")
		}
	}
	return msg
}
func (s *Server) fail(c *gin.Context, status int, e error) {
	c.JSON(status, gin.H{"error": s.redact(e.Error())})
}
func (s *Server) session(c *gin.Context) (store.Session, bool) {
	var sess store.Session
	if _, e := uuid.Parse(c.Param("id")); e != nil {
		s.fail(c, 400, fmt.Errorf("invalid session ID"))
		return sess, false
	}
	if e := s.S.DB.First(&sess, "id = ?", c.Param("id")).Error; e != nil {
		s.fail(c, 404, fmt.Errorf("会话不存在"))
		return sess, false
	}
	return sess, true
}
func (s *Server) view(sess store.Session) gin.H {
	var sb store.Sandbox
	var sandbox any = gin.H{"state": "not_started"}
	if s.S.DB.First(&sb, "session_id = ?", sess.ID).Error == nil {
		if sb.Error != "" {
			sb.State = "error"
		}
		sandbox = sb
	} else if message, ok := s.sandboxErrors.Load(sess.ID); ok {
		sandbox = gin.H{"state": "error", "error": message}
	}
	if state, ok := s.provisioning.Load(sess.ID); ok {
		sandbox = gin.H{"state": "provisioning", "message": state}
	}
	return gin.H{"id": sess.ID, "title": sess.Title, "agent_state": sess.AgentState, "synced_seq": sess.SyncedSeq, "options": sess.Options, "mode_locked": sess.LockedMode != nil, "created_at": sess.CreatedAt, "last_active_at": sess.LastActiveAt, "sandbox": sandbox}
}
func (s *Server) lock(id string) *sync.Mutex {
	x, _ := s.locks.LoadOrStore(id, &sync.Mutex{})
	return x.(*sync.Mutex)
}
func (s *Server) Shutdown(ctx context.Context) error {
	s.cancel()
	s.mu.Lock()
	for _, r := range s.running {
		r.cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
	return s.HTTP.Shutdown(ctx)
}
func decode[T any](c *gin.Context) (T, error) { var x T; e := c.ShouldBindJSON(&x); return x, e }
func validOptions(raw json.RawMessage) error {
	var x struct {
		Model    string `json:"model"`
		Thinking string `json:"thinking"`
		Mode     string `json:"mode"`
		Plan     bool   `json:"plan"`
	}
	if e := json.Unmarshal(raw, &x); e != nil {
		return e
	}
	if x.Model != "deepseek-flash" {
		return fmt.Errorf("model must be deepseek-flash")
	}
	if !strings.Contains("|off|low|high|max|", "|"+x.Thinking+"|") {
		return fmt.Errorf("thinking must be off / low / high / max")
	}
	if x.Mode != "standard" && x.Mode != "ptc" {
		return fmt.Errorf("mode must be standard or ptc")
	}
	return nil
}
