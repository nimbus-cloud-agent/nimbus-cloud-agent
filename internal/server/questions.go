package server

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
)

func validateAnswer(questions []model.Question, answer model.QuestionAnswer) error {
	if answer.Dismissed {
		if len(answer.Answers) != 0 {
			return fmt.Errorf("A review cannot be dismissed and answered at the same time.")
		}
		return nil
	}

	if len(answer.Answers) != len(questions) {
		return fmt.Errorf("Answer every question.")
	}
	seen := map[string]bool{}
	for _, a := range answer.Answers {
		var q *model.Question
		for i := range questions {
			if questions[i].ID == a.ID {
				q = &questions[i]
				break
			}
		}
		if q == nil || seen[a.ID] {
			return fmt.Errorf("Question is missing or duplicated.")
		}
		seen[a.ID] = true
		if !q.MultiSelect && len(a.Selected) > 1 {
			return fmt.Errorf("Select exactly one option for this question.")
		}
		if len(a.Selected) == 0 && (a.Custom == nil || strings.TrimSpace(*a.Custom) == "") {
			return fmt.Errorf("Select an option or provide an answer.")
		}
		if a.Custom != nil && len(*a.Custom) > 20000 {
			return fmt.Errorf("Feedback cannot exceed 20 KB.")
		}
		labels := map[string]bool{}
		for _, o := range q.Options {
			labels[o.Label] = true
		}
		for _, label := range a.Selected {
			if !labels[label] {
				return fmt.Errorf("Option is missing or duplicated.")
			}
			delete(labels, label)
		}
	}
	return nil
}

func (s *Server) questionRoutes(r *gin.Engine) {
	r.GET("/api/sessions/:id/questions/:question", func(c *gin.Context) {
		sess, ok := s.session(c)
		if !ok {
			return
		}
		if _, err := uuid.Parse(c.Param("question")); err != nil {
			s.fail(c, 400, fmt.Errorf("invalid question ID"))
			return
		}
		row, err := s.S.Question(c.Request.Context(), sess.ID, c.Param("question"))
		if err != nil {
			s.fail(c, 404, fmt.Errorf("Review not found."))
			return
		}
		c.JSON(200, model.UserQuestion(row))
	})
	r.POST("/api/sessions/:id/questions/:question/answer", func(c *gin.Context) {
		sess, ok := s.session(c)
		if !ok {
			return
		}
		if _, e := uuid.Parse(c.Param("question")); e != nil {
			s.fail(c, 400, fmt.Errorf("invalid question ID"))
			return
		}
		var answer model.QuestionAnswer
		if e := c.ShouldBindJSON(&answer); e != nil {
			s.fail(c, 400, e)
			return
		}
		status := 500
		err := s.S.Transaction(c.Request.Context(), func(tx *store.Store) error {
			row, e := tx.LockQuestion(c.Request.Context(), sess.ID, c.Param("question"))
			if e != nil {
				status = 404
				return fmt.Errorf("Review not found.")
			}
			q := model.UserQuestion(row)
			if e := validateAnswer(q.Questions, answer); e != nil {
				status = 400
				return e
			}
			if q.Status != "pending" {
				if (q.Status == "answered" || q.Status == "dismissed") && q.Answer != nil && reflect.DeepEqual(*q.Answer, answer) {
					return nil
				}
				status = 409
				return fmt.Errorf("This review has ended. Check its latest status.")
			}
			next := "answered"
			if answer.Dismissed {
				next = "dismissed"
			}
			return tx.ResolveQuestion(c.Request.Context(), q.ID, next, &answer, time.Now())
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
func (s *Server) waitQuestion(ctx context.Context, out model.Outbox, request model.QuestionRequest) (model.QuestionAnswer, error) {
	if len(request.Questions) == 0 || len(request.Questions) > 10 {
		return model.QuestionAnswer{}, fmt.Errorf("invalid user question batch")
	}
	updates := s.updates.subscribe(out.SessionID, questionsChanged|sessionChanged)
	defer updates.close()
	q := model.UserQuestion{ID: uuid.NewString(), SessionID: out.SessionID, TurnID: out.TurnID, Questions: request.Questions, Status: "pending", CreatedAt: time.Now()}
	if err := s.S.CreateQuestion(ctx, store.UserQuestion(q)); err != nil {
		return model.QuestionAnswer{}, err
	}
	defer s.S.InterruptQuestions(context.WithoutCancel(ctx), q.ID)
	for range updates.signals(ctx, 0) {
		row, err := s.S.Question(ctx, q.SessionID, q.ID)
		if err != nil {
			return model.QuestionAnswer{}, err
		}
		q = model.UserQuestion(row)
		if (q.Status == "answered" || q.Status == "dismissed") && q.Answer != nil {
			return *q.Answer, nil
		}
		if q.Status != "pending" {
			return model.QuestionAnswer{}, fmt.Errorf("user question interrupted")
		}
	}
	if ctx.Err() != nil {
		return model.QuestionAnswer{}, ctx.Err()
	}
	return model.QuestionAnswer{}, fmt.Errorf("user question interrupted")
}
