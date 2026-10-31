package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (s *Server) routes(r *gin.Engine) {
	r.GET("/api/sessions/stream", s.sessionsStream)
	r.GET("/api/sessions", func(c *gin.Context) {
		var rows []store.Session
		if e := s.S.DB.Order("last_active_at DESC").Find(&rows).Error; e != nil {
			s.fail(c, 500, e)
			return
		}
		out := []gin.H{}
		for _, x := range rows {
			out = append(out, s.view(x))
		}
		c.JSON(200, out)
	})
	r.POST("/api/sessions", func(c *gin.Context) {
		sess := store.Session{ID: uuid.NewString(), Title: "新对话", AgentState: "idle", SyncedSeq: -1, Options: store.Raw(gin.H{"model": "deepseek-flash", "thinking": "off", "plan": false, "mode": "standard"}), CreatedAt: time.Now(), LastActiveAt: time.Now()}
		if e := s.S.DB.Create(&sess).Error; e != nil {
			s.fail(c, 500, e)
			return
		}
		c.JSON(201, s.view(sess))
	})
	r.GET("/api/sessions/:id", func(c *gin.Context) {
		if x, ok := s.session(c); ok {
			c.JSON(200, s.view(x))
		}
	})
	r.PATCH("/api/sessions/:id", func(c *gin.Context) {
		x, ok := s.session(c)
		if !ok {
			return
		}
		var in struct {
			Title   *string         `json:"title"`
			Options json.RawMessage `json:"options"`
		}
		if e := c.ShouldBindJSON(&in); e != nil {
			s.fail(c, 400, e)
			return
		}
		m := map[string]any{}
		if in.Title != nil {
			t := strings.TrimSpace(*in.Title)
			if t == "" {
				s.fail(c, 400, fmt.Errorf("标题不能为空"))
				return
			}
			m["title"] = string([]rune(t)[:min(len([]rune(t)), 100)])
		}
		if in.Options != nil {
			if e := validOptions(in.Options); e != nil {
				s.fail(c, 400, e)
				return
			}
			m["options"] = in.Options
		}
		status := 500
		if e := s.S.DB.Transaction(func(tx *gorm.DB) error {
			if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&x, "id = ?", x.ID).Error; e != nil {
				return e
			}
			if in.Options != nil {
				if e := checkPreset(x, in.Options); e != nil {
					status = 409
					return e
				}
			}
			return tx.Model(&x).Updates(m).Error
		}); e != nil {
			s.fail(c, status, e)
			return
		}
		s.S.DB.First(&x, "id = ?", x.ID)
		c.JSON(200, s.view(x))
	})
	r.DELETE("/api/sessions/:id", s.deleteSession)
	r.POST("/api/sessions/:id/messages", s.message)
	r.POST("/api/sessions/:id/messages/:outbox/retry", func(c *gin.Context) {
		x, ok := s.session(c)
		if !ok {
			return
		}
		if e := s.S.DB.Transaction(func(tx *gorm.DB) error {
			if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&x, "id = ?", x.ID).Error; e != nil {
				return e
			}
			var out store.Outbox
			if e := tx.First(&out, "id = ? AND session_id = ? AND status = 'failed'", c.Param("outbox"), x.ID).Error; e != nil {
				return fmt.Errorf("只有未送达的失败消息可以重发")
			}
			if e := checkPreset(x, out.Options); e != nil {
				return e
			}
			if e := validOptions(out.Options); e != nil {
				return e
			}
			return tx.Model(&out).Updates(map[string]any{"status": "queued", "error": "", "outcome": "", "created_at": time.Now()}).Error
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
		events := []store.Event{}
		q := s.S.DB.Where("session_id = ? AND seq > ?", x.ID, after)
		if t := c.Query("types"); t != "" {
			q = q.Where("type IN ?", strings.Split(t, ","))
		}
		if e := q.Order("seq").Limit(1000).Find(&events).Error; e != nil {
			s.fail(c, 500, e)
			return
		}
		outbox := []store.Outbox{}
		s.S.DB.Where("session_id = ?", x.ID).Order("created_at").Find(&outbox)
		c.JSON(200, gin.H{"events": events, "outbox": outbox, "has_more": len(events) == 1000, "frames": s.frames(x.ID), "questions": s.questions(x.ID)})
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
		s.S.DB.First(&x, "id = ?", x.ID)
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
	var out store.Outbox
	status := 500
	if e := s.S.DB.Transaction(func(tx *gorm.DB) error {
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&x, "id = ?", x.ID).Error; e != nil {
			return e
		}
		// Idempotent retries return the original snapshot even after controls change.
		e := tx.First(&out, "session_id = ? AND client_msg_id = ?", x.ID, in.ClientMsgID).Error
		if e == nil {
			return nil
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		var opts map[string]any
		if e := json.Unmarshal(x.Options, &opts); e != nil {
			return e
		}
		if in.Model != nil {
			opts["model"] = *in.Model
		}
		if in.Thinking != nil {
			opts["thinking"] = *in.Thinking
		}
		if in.Plan != nil {
			opts["plan"] = *in.Plan
		}
		if in.Mode != nil {
			opts["mode"] = *in.Mode
		}
		options := store.Raw(opts)
		if e := validOptions(options); e != nil {
			status = 400
			return e
		}
		if e := checkPreset(x, options); e != nil {
			status = 409
			return e
		}
		images, e := messageImages(tx, x.ID, in.ImageIDs)
		if e != nil {
			status = 400
			return e
		}
		out = store.Outbox{ID: uuid.NewString(), SessionID: x.ID, TurnID: uuid.NewString(), ClientMsgID: in.ClientMsgID, Content: in.Message, Images: images, Options: options, Status: "queued", CreatedAt: time.Now()}
		// The outbox trigger freezes the session preset in this transaction.
		if e := tx.Create(&out).Error; e != nil {
			return e
		}
		return tx.Model(&x).Update("last_active_at", time.Now()).Error
	}); e != nil {
		s.fail(c, status, e)
		return
	}
	c.JSON(202, out)
}

func checkPreset(sess store.Session, options json.RawMessage) error {
	var opts struct {
		Mode string `json:"mode"`
	}
	if e := json.Unmarshal(options, &opts); e != nil {
		return e
	}
	if sess.LockedMode != nil && opts.Mode != *sess.LockedMode {
		return fmt.Errorf("此会话的工具模式已固定；切换 Standard / PTC 请新建会话")
	}
	return nil
}
func (s *Server) stream(c *gin.Context) {
	x, ok := s.session(c)
	if !ok {
		return
	}
	after := int64(-1)
	v := c.GetHeader("Last-Event-ID")
	if v == "" {
		v = c.Query("after_seq")
	}
	if n, e := strconv.ParseInt(v, 10, 64); e == nil {
		after = n
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Writer.Flush()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	heartbeat := time.NewTicker(streamHeartbeatInterval)
	defer heartbeat.Stop()
	lastState, lastOutbox, lastQuestions := "", "", ""
	var frameAfter int64
	for {
		events := []store.Event{}
		if e := s.S.DB.Where("session_id = ? AND seq > ?", x.ID, after).Order("seq").Limit(1000).Find(&events).Error; e != nil {
			return
		}
		for _, ev := range events {
			data, _ := json.Marshal(ev)
			if _, e := fmt.Fprintf(c.Writer, "id: %d\nevent: session\ndata: %s\n\n", ev.Seq, data); e != nil {
				return
			}
			after = ev.Seq
		}
		if s.S.DB.First(&x, "id = ?", x.ID).Error != nil {
			return
		}
		st, _ := json.Marshal(s.view(x))
		if string(st) != lastState {
			fmt.Fprintf(c.Writer, "event: state\ndata: %s\n\n", st)
			lastState = string(st)
		}
		out := []store.Outbox{}
		s.S.DB.Where("session_id = ?", x.ID).Order("created_at").Find(&out)
		ob, _ := json.Marshal(out)
		if string(ob) != lastOutbox {
			fmt.Fprintf(c.Writer, "event: outbox\ndata: %s\n\n", ob)
			lastOutbox = string(ob)
		}
		questions, _ := json.Marshal(s.questions(x.ID))
		if string(questions) != lastQuestions {
			fmt.Fprintf(c.Writer, "event: questions\ndata: %s\n\n", questions)
			lastQuestions = string(questions)
		}
		frames := []store.StreamFrame{}
		s.S.DB.Where("session_id = ? AND id > ?", x.ID, frameAfter).Order("id").Find(&frames)
		for _, f := range frames {
			raw, _ := json.Marshal(f)
			fmt.Fprintf(c.Writer, "event: frame\ndata: %s\n\n", raw)
			frameAfter = f.ID
		}
		c.Writer.Flush()
		select {
		case <-c.Request.Context().Done():
			return
		case <-s.ctx.Done():
			return
		case <-heartbeat.C:
			if _, e := fmt.Fprint(c.Writer, ": heartbeat\n\n"); e != nil {
				return
			}
			c.Writer.Flush()
		case <-tick.C:
		}
	}
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
	var sb store.Sandbox
	if s.S.DB.First(&sb, "session_id = ?", x.ID).Error == nil && sb.State != "killed" {
		if _, e := s.e2b(c.Request.Context(), "kill", &sb, nil); e != nil {
			s.fail(c, 502, e)
			return
		}
	}
	if e := os.RemoveAll(filepath.Join(s.C.WorkDir, x.ID)); e != nil {
		s.fail(c, 500, e)
		return
	}
	if e := s.S.DB.Delete(&x).Error; e != nil {
		s.fail(c, 500, e)
		return
	}
	s.sandboxErrors.Delete(x.ID)
	c.Status(204)
}

func (s *Server) frames(id string) []store.StreamFrame {
	f := []store.StreamFrame{}
	s.S.DB.Where("session_id = ?", id).Order("id").Find(&f)
	return f
}
