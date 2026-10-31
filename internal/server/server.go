package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/config"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/executor"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
)

type runningTurn struct {
	cancel context.CancelFunc
	done   chan struct{}
}
type toolScope struct {
	SessionID string
	TurnID    string
}
type Server struct {
	C             config.Config
	S             *store.Store
	R             *executor.Runtime
	HTTP          *http.Server
	mu            sync.Mutex
	running       map[string]*runningTurn
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

func New(c config.Config, st *store.Store, frontend fs.FS, runtime *executor.Runtime) (*Server, error) {
	host, _, e := net.SplitHostPort(c.Addr)
	if e != nil || !(host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()) {
		return nil, fmt.Errorf("single-user control plane must bind loopback (127.0.0.1:8080)")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{C: c, S: st, R: runtime, running: map[string]*runningTurn{}, ctx: ctx, cancel: cancel, BaseURL: "http://" + c.Addr}
	if e = st.RecoverInterrupted(ctx); e != nil {
		cancel()
		return nil, e
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), s.guard)
	r.GET("/api/health", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true, "runtime": s.R.Status()}) })
	s.routes(r)
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
func (s *Server) session(c *gin.Context) (model.Session, bool) {
	var sess model.Session
	if _, e := uuid.Parse(c.Param("id")); e != nil {
		s.fail(c, 400, fmt.Errorf("invalid session ID"))
		return sess, false
	}
	var e error
	sess, e = s.getSession(c.Request.Context(), c.Param("id"))
	if e != nil {
		if errors.Is(e, store.ErrNotFound) {
			s.fail(c, 404, fmt.Errorf("会话不存在"))
		} else {
			s.fail(c, 500, e)
		}
		return sess, false
	}
	return sess, true
}
func (s *Server) view(ctx context.Context, sess model.Session) (model.SessionView, error) {
	sandbox := model.SandboxView{State: "not_started"}
	sb, err := s.getSandbox(ctx, sess.ID)
	if err == nil {
		if sb.Error != "" {
			sb.State = "error"
		}
		sandbox = sandboxView(sb)
	} else if !errors.Is(err, store.ErrNotFound) {
		return model.SessionView{}, err
	} else if message, ok := s.sandboxErrors.Load(sess.ID); ok {
		sandbox = model.SandboxView{State: "error", Error: message.(string)}
	}
	if state, ok := s.provisioning.Load(sess.ID); ok {
		sandbox = model.SandboxView{State: "provisioning", Message: state.(string)}
	}
	return model.SessionView{ID: sess.ID, Title: sess.Title, AgentState: sess.AgentState, SyncedSeq: sess.SyncedSeq, Options: sess.Options, ModeLocked: sess.LockedMode != nil, CreatedAt: sess.CreatedAt, LastActiveAt: sess.LastActiveAt, Sandbox: sandbox}, nil
}
func (s *Server) respondSession(c *gin.Context, status int, sess model.Session) {
	view, err := s.view(c.Request.Context(), sess)
	if err != nil {
		s.fail(c, 500, err)
		return
	}
	c.JSON(status, view)
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
func validOptions(x model.Options) error {
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
