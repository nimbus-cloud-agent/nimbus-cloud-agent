package server

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type questionItem struct {
	ID      string `json:"id"`
	Options []struct {
		Label string `json:"label"`
	} `json:"options"`
	MultiSelect bool `json:"multiSelect"`
}

func interruptQuestions(db *gorm.DB) error {
	return db.Model(&store.UserQuestion{}).Where("status = 'pending'").Updates(map[string]any{"status": "interrupted", "resolved_at": time.Now()}).Error
}

type questionAnswer struct {
	Dismissed bool `json:"dismissed,omitempty"`
	Answers   []struct {
		ID       string   `json:"id"`
		Selected []string `json:"selected"`
		Custom   *string  `json:"custom,omitempty"`
	} `json:"answers,omitempty"`
}

func validateAnswer(raw store.JSON, answer questionAnswer) error {
	if answer.Dismissed {
		if len(answer.Answers) != 0 {
			return fmt.Errorf("暂不决定不能同时提交答案")
		}
		return nil
	}
	var questions []questionItem
	if e := json.Unmarshal(raw, &questions); e != nil {
		return e
	}
	if len(answer.Answers) != len(questions) {
		return fmt.Errorf("请回答每个问题")
	}
	seen := map[string]bool{}
	for _, a := range answer.Answers {
		var q *questionItem
		for i := range questions {
			if questions[i].ID == a.ID {
				q = &questions[i]
				break
			}
		}
		if q == nil || seen[a.ID] {
			return fmt.Errorf("问题不存在或重复")
		}
		seen[a.ID] = true
		if !q.MultiSelect && len(a.Selected) > 1 {
			return fmt.Errorf("此问题只能选择一项")
		}
		if len(a.Selected) == 0 && (a.Custom == nil || strings.TrimSpace(*a.Custom) == "") {
			return fmt.Errorf("请选择或填写答案")
		}
		if a.Custom != nil && len(*a.Custom) > 20000 {
			return fmt.Errorf("修改意见不能超过 20 KB")
		}
		labels := map[string]bool{}
		for _, o := range q.Options {
			labels[o.Label] = true
		}
		for _, label := range a.Selected {
			if !labels[label] {
				return fmt.Errorf("选项不存在或重复")
			}
			delete(labels, label)
		}
	}
	return nil
}

func (s *Server) questions(sid string) []store.UserQuestion {
	rows := []store.UserQuestion{}
	s.S.DB.Where("session_id = ?", sid).Order("created_at, id").Find(&rows)
	return rows
}

func (s *Server) questionRoutes(r *gin.Engine) {
	r.POST("/api/sessions/:id/questions/:question/answer", func(c *gin.Context) {
		sess, ok := s.session(c)
		if !ok {
			return
		}
		if _, e := uuid.Parse(c.Param("question")); e != nil {
			s.fail(c, 400, fmt.Errorf("invalid question ID"))
			return
		}
		var answer questionAnswer
		if e := c.ShouldBindJSON(&answer); e != nil {
			s.fail(c, 400, e)
			return
		}
		status := 500
		err := s.S.DB.Transaction(func(tx *gorm.DB) error {
			var q store.UserQuestion
			if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&q, "id = ? AND session_id = ?", c.Param("question"), sess.ID).Error; e != nil {
				status = 404
				return fmt.Errorf("评审不存在")
			}
			if e := validateAnswer(q.Questions, answer); e != nil {
				status = 400
				return e
			}
			if q.Status != "pending" {
				var old questionAnswer
				if (q.Status == "answered" || q.Status == "dismissed") && json.Unmarshal(q.Answer, &old) == nil && reflect.DeepEqual(old, answer) {
					return nil
				}
				status = 409
				return fmt.Errorf("评审已结束，请查看最新状态")
			}
			next := "answered"
			if answer.Dismissed {
				next = "dismissed"
			}
			return tx.Model(&q).Updates(map[string]any{"answer": store.Raw(answer), "status": next, "resolved_at": time.Now()}).Error
		})
		if err != nil {
			s.fail(c, status, err)
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})
}

// A runtime question stays pending across browser disconnects; only an explicit
// answer, Stop, or backend shutdown releases the native tool call.
func (s *Server) waitQuestion(ctx context.Context, out store.Outbox, raw json.RawMessage) (json.RawMessage, error) {
	var request struct {
		Questions json.RawMessage `json:"questions"`
	}
	if e := json.Unmarshal(raw, &request); e != nil {
		return nil, e
	}
	var items []questionItem
	if e := json.Unmarshal(request.Questions, &items); e != nil {
		return nil, e
	}
	if len(items) == 0 || len(items) > 10 || len(raw) > 512<<10 {
		return nil, fmt.Errorf("invalid user question batch")
	}
	q := store.UserQuestion{ID: uuid.NewString(), SessionID: out.SessionID, TurnID: out.TurnID, Questions: request.Questions, Status: "pending", CreatedAt: time.Now()}
	if e := s.S.DB.Create(&q).Error; e != nil {
		return nil, e
	}
	defer func() {
		s.S.DB.Model(&store.UserQuestion{}).Where("id = ? AND status = 'pending'", q.ID).Updates(map[string]any{"status": "interrupted", "resolved_at": time.Now()})
	}()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-tick.C:
			if e := s.S.DB.First(&q, "id = ?", q.ID).Error; e != nil {
				return nil, e
			}
			if q.Status == "answered" || q.Status == "dismissed" {
				return q.Answer, nil
			}
			if q.Status != "pending" {
				return nil, fmt.Errorf("user question interrupted")
			}
		}
	}
}
