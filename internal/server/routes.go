package server

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
)

func (s *Server) routes(r *gin.Engine) {
	r.GET("/api/sessions/stream", s.sessionsStream)
	r.GET("/api/sessions", func(c *gin.Context) {
		rows, e := s.S.Sessions(c.Request.Context())
		if e != nil {
			s.fail(c, 500, e)
			return
		}
		out := []model.SessionView{}
		for _, x := range rows {
			view, err := s.view(c.Request.Context(), model.Session(x))
			if err != nil {
				s.fail(c, 500, err)
				return
			}
			out = append(out, view)
		}
		c.JSON(200, out)
	})
	r.POST("/api/sessions", func(c *gin.Context) {
		sess := model.Session{ID: uuid.NewString(), Title: "新对话", AgentState: "idle", SyncedSeq: -1, Options: model.DefaultOptions(), CreatedAt: time.Now(), LastActiveAt: time.Now()}
		if e := s.S.CreateSession(c.Request.Context(), store.Session(sess)); e != nil {
			s.fail(c, 500, e)
			return
		}
		s.respondSession(c, 201, sess)
	})
	r.GET("/api/sessions/:id", func(c *gin.Context) {
		if x, ok := s.session(c); ok {
			s.respondSession(c, 200, x)
		}
	})
	r.PATCH("/api/sessions/:id", func(c *gin.Context) {
		x, ok := s.session(c)
		if !ok {
			return
		}
		var in struct {
			Title   *string        `json:"title"`
			Options *model.Options `json:"options"`
		}
		if e := c.ShouldBindJSON(&in); e != nil {
			s.fail(c, 400, e)
			return
		}
		changes := store.SessionChanges{}
		if in.Title != nil {
			t := strings.TrimSpace(*in.Title)
			if t == "" {
				s.fail(c, 400, fmt.Errorf("标题不能为空"))
				return
			}
			title := string([]rune(t)[:min(len([]rune(t)), 100)])
			changes.Title = &title
		}
		if in.Options != nil {
			if e := validOptions(*in.Options); e != nil {
				s.fail(c, 400, e)
				return
			}
			changes.Options = in.Options
		}
		status := 500
		if e := s.S.Transaction(c.Request.Context(), func(tx *store.Store) error {
			row, e := tx.LockSession(c.Request.Context(), x.ID)
			if e != nil {
				return e
			}
			x = model.Session(row)
			if in.Options != nil {
				if e := checkPreset(x, *in.Options); e != nil {
					status = 409
					return e
				}
			}
			return tx.UpdateSession(c.Request.Context(), x.ID, changes)
		}); e != nil {
			s.fail(c, status, e)
			return
		}
		var reloadErr error
		x, reloadErr = s.getSession(c.Request.Context(), x.ID)
		if reloadErr != nil {
			s.fail(c, 500, reloadErr)
			return
		}
		s.respondSession(c, 200, x)
	})
	r.DELETE("/api/sessions/:id", s.deleteSession)
	r.POST("/api/sessions/:id/messages", s.message)
	r.POST("/api/sessions/:id/messages/:outbox/retry", func(c *gin.Context) {
		x, ok := s.session(c)
		if !ok {
			return
		}
		if e := s.S.Transaction(c.Request.Context(), func(tx *store.Store) error {
			row, e := tx.LockSession(c.Request.Context(), x.ID)
			if e != nil {
				return e
			}
			x = model.Session(row)
			out, e := tx.Outbox(c.Request.Context(), c.Param("outbox"), x.ID)
			if e != nil || out.Status != "failed" {
				return fmt.Errorf("只有未送达的失败消息可以重发")
			}
			if e := checkPreset(x, out.Options); e != nil {
				return e
			}
			if e := validOptions(out.Options); e != nil {
				return e
			}
			return tx.UpdateOutbox(c.Request.Context(), out.ID, store.OutboxChanges{Status: ptr("queued"), Error: ptr(""), Outcome: ptr(""), CreatedAt: ptr(time.Now())})
		}); e != nil {
			s.fail(c, 409, e)
			return
		}
		c.JSON(202, gin.H{"status": "queued"})
	})
	r.GET("/api/sessions/:id/events", func(c *gin.Context) {
		x, ok := s.session(c)
		if !ok {
			return
		}
		after := int64(-1)
		if v, e := strconv.ParseInt(c.Query("after_seq"), 10, 64); e == nil {
			after = v
		}
		filter := store.EventFilter{SessionID: x.ID, After: after, Limit: 1000}
		if types := c.Query("types"); types != "" {
			filter.Types = strings.Split(types, ",")
		}
		events, err := s.events(c.Request.Context(), filter)
		if err != nil {
			s.fail(c, 500, err)
			return
		}
		outbox, err := s.outboxes(c.Request.Context(), store.OutboxFilter{SessionID: x.ID})
		if err != nil {
			s.fail(c, 500, err)
			return
		}
		frames, err := s.frames(c.Request.Context(), store.FrameFilter{SessionID: x.ID})
		if err != nil {
			s.fail(c, 500, err)
			return
		}
		questions, err := s.questions(c.Request.Context(), x.ID)
		if err != nil {
			s.fail(c, 500, err)
			return
		}
		c.JSON(200, model.History{Events: events, Outbox: outbox, Frames: frames, Questions: questions, HasMore: len(events) == 1000})
	})
	r.GET("/api/sessions/:id/stream", s.stream)
	r.POST("/api/sessions/:id/stop", func(c *gin.Context) {
		x, ok := s.session(c)
		if !ok {
			return
		}
		s.mu.Lock()
		run := s.running[x.ID]
		if run != nil {
			run.cancel()
		}
		s.mu.Unlock()
		c.JSON(200, gin.H{"ok": true})
	})
	r.POST("/api/sessions/:id/sandbox/start", func(c *gin.Context) {
		x, ok := s.session(c)
		if !ok {
			return
		}
		l := s.lock(x.ID)
		l.Lock()
		defer l.Unlock()
		sb, e := s.ensureSandbox(c.Request.Context(), x.ID)
		if e != nil {
			s.fail(c, 502, e)
			return
		}
		c.JSON(200, sb)
	})
	r.POST("/api/sessions/:id/sandbox/pause", func(c *gin.Context) {
		x, ok := s.session(c)
		if !ok {
			return
		}
		l := s.lock(x.ID)
		if !l.TryLock() {
			s.fail(c, 409, fmt.Errorf("沙箱正在执行操作，请稍后暂停"))
			return
		}
		defer l.Unlock()
		var reloadErr error
		x, reloadErr = s.getSession(c.Request.Context(), x.ID)
		if reloadErr != nil {
			s.fail(c, 500, reloadErr)
			return
		}
		if x.AgentState != "idle" {
			s.fail(c, 409, fmt.Errorf("请先停止当前轮次"))
			return
		}
		if e := s.pause(c.Request.Context(), x.ID); e != nil {
			s.fail(c, 409, e)
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})
	r.GET("/api/sessions/:id/terminal", s.terminal)
	s.settingsRoutes(r)
	s.imageRoutes(r)
	s.questionRoutes(r)
}
func (s *Server) message(c *gin.Context) {
	x, ok := s.session(c)
	if !ok {
		return
	}
	if _, deleting := s.deleting.Load(x.ID); deleting {
		s.fail(c, 409, fmt.Errorf("会话正在删除"))
		return
	}
	var in struct {
		Message     string   `json:"message"`
		ClientMsgID string   `json:"client_msg_id"`
		Model       *string  `json:"model"`
		Thinking    *string  `json:"thinking"`
		Plan        *bool    `json:"plan"`
		Mode        *string  `json:"mode"`
		ImageIDs    []string `json:"image_ids"`
	}
	if e := c.ShouldBindJSON(&in); e != nil {
		s.fail(c, 400, e)
		return
	}
	if (strings.TrimSpace(in.Message) == "" && len(in.ImageIDs) == 0) || len(in.Message) > 100000 || in.ClientMsgID == "" || len(in.ClientMsgID) > 100 {
		s.fail(c, 400, fmt.Errorf("请填写文字或添加图片；文字最多 100 KB，并需要 client_msg_id"))
		return
	}
	if s.S.Get("deepseek_api_key", s.C.DeepSeekKey) == "" {
		s.fail(c, 400, fmt.Errorf("请先在全局设置中填写 DeepSeek API key"))
		return
	}
	var out model.Outbox
	status := 500
	if e := s.S.Transaction(c.Request.Context(), func(tx *store.Store) error {
		row, e := tx.LockSession(c.Request.Context(), x.ID)
		if e != nil {
			return e
		}
		x = model.Session(row)
		// Idempotent retries return the original snapshot even after controls change.
		stored, e := tx.OutboxByClient(c.Request.Context(), x.ID, in.ClientMsgID)
		out = model.Outbox(stored)
		if e == nil {
			return nil
		}
		if !errors.Is(e, store.ErrNotFound) {
			return e
		}
		opts := x.Options
		if in.Model != nil {
			opts.Model = *in.Model
		}
		if in.Thinking != nil {
			opts.Thinking = *in.Thinking
		}
		if in.Plan != nil {
			opts.Plan = *in.Plan
		}
		if in.Mode != nil {
			opts.Mode = *in.Mode
		}
		options := opts
		if e := validOptions(options); e != nil {
			status = 400
			return e
		}
		if e := checkPreset(x, options); e != nil {
			status = 409
			return e
		}
		images, e := messageImages(c.Request.Context(), tx, x.ID, in.ImageIDs)
		if e != nil {
			status = 400
			return e
		}
		out = model.Outbox{ID: uuid.NewString(), SessionID: x.ID, TurnID: uuid.NewString(), ClientMsgID: in.ClientMsgID, Content: in.Message, Images: images, Options: options, Status: "queued", CreatedAt: time.Now()}
		// The outbox trigger freezes the session preset in this transaction.
		if e := tx.CreateOutbox(c.Request.Context(), store.Outbox(out)); e != nil {
			return e
		}
		return tx.UpdateSession(c.Request.Context(), x.ID, store.SessionChanges{LastActiveAt: ptr(time.Now())})
	}); e != nil {
		s.fail(c, status, e)
		return
	}
	c.JSON(202, out)
}

func checkPreset(sess model.Session, opts model.Options) error {
	if sess.LockedMode != nil && opts.Mode != *sess.LockedMode {
		return fmt.Errorf("此会话的工具模式已固定；切换 Standard / PTC 请新建会话")
	}
	return nil
}
func (s *Server) deleteSession(c *gin.Context) {
	x, ok := s.session(c)
	if !ok {
		return
	}
	if _, busy := s.deleting.LoadOrStore(x.ID, true); busy {
		s.fail(c, 409, fmt.Errorf("会话正在删除"))
		return
	}
	defer s.deleting.Delete(x.ID)
	s.mu.Lock()
	run := s.running[x.ID]
	if run != nil {
		run.cancel()
	}
	s.mu.Unlock()
	if run != nil {
		select {
		case <-run.done:
		case <-c.Request.Context().Done():
			return
		}
	}
	l := s.lock(x.ID)
	l.Lock()
	defer l.Unlock()
	sb, err := s.getSandbox(c.Request.Context(), x.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.fail(c, 500, err)
		return
	}
	if err == nil && sb.State != "killed" {
		if e := s.R.KillSandbox(c.Request.Context(), s.C.E2BKey, sb.E2bSandboxID); e != nil {
			s.fail(c, 502, e)
			return
		}
	}
	if e := os.RemoveAll(filepath.Join(s.C.WorkDir, x.ID)); e != nil {
		s.fail(c, 500, e)
		return
	}
	if e := s.S.DeleteSession(c.Request.Context(), x.ID); e != nil {
		s.fail(c, 500, e)
		return
	}
	s.sandboxErrors.Delete(x.ID)
	c.Status(204)
}
