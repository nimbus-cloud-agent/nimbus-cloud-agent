package server

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/logging"
)

func TestHTTPFailureLogHasSeverityAndRequestID(t *testing.T) {
	var out bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(logging.NewHandler(&out, slog.LevelDebug)))
	defer slog.SetDefault(previous)
	r := gin.New()
	r.Use(requestLogger())
	r.GET("/failure", func(c *gin.Context) { c.Status(500) })
	req := httptest.NewRequest("GET", "/failure?key=private-query", nil)
	req.Header.Set("Authorization", "Basic private-credentials")
	response := httptest.NewRecorder()
	r.ServeHTTP(response, req)
	text := out.String()
	if !strings.Contains(text, `"level":"ERROR"`) || !strings.Contains(text, response.Header().Get("X-Request-ID")) || response.Header().Get("X-Request-ID") == "" || strings.Contains(text, "private-") {
		t.Fatal(text)
	}
}

func TestDrainWaitsForRequestsAndKeepsHealthAlive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &Server{ctx: ctx, cancel: cancel, running: map[string]*runningTurn{}, drained: make(chan struct{})}
	r := gin.New()
	r.Use(s.acceptRequest)
	entered, release := make(chan struct{}), make(chan struct{})
	r.GET("/work", func(c *gin.Context) { close(entered); <-release; c.Status(200) })
	r.GET("/api/health", func(c *gin.Context) { c.Status(200) })
	finished := make(chan struct{})
	go func() { r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/work", nil)); close(finished) }()
	<-entered
	drainCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	drained := make(chan error, 1)
	go func() { drained <- s.Drain(drainCtx) }()
	<-ctx.Done()
	select {
	case err := <-drained:
		t.Fatalf("drained with an active request: %v", err)
	default:
	}
	for path, want := range map[string]int{"/work": http.StatusServiceUnavailable, "/api/health": http.StatusOK} {
		response := httptest.NewRecorder()
		r.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != want {
			t.Fatalf("%s: %d", path, response.Code)
		}
	}
	close(release)
	<-finished
	if err := <-drained; err != nil {
		t.Fatal(err)
	}
}
