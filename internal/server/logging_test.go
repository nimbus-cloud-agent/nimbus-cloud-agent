package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
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

func TestPanicLogIncludesSafeCauseAndRequestIdentity(t *testing.T) {
	var out bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(logging.NewHandler(&out, slog.LevelDebug, "panic-test-key")))
	defer slog.SetDefault(previous)
	r := gin.New()
	r.Use(requestLogger(), recoveryLogger())
	r.GET("/panic", func(*gin.Context) {
		panic(fmt.Errorf("fixture failure: panic-test-key https://user:private-password@example.test/path?token=private-query"))
	})
	response := httptest.NewRecorder()
	r.ServeHTTP(response, httptest.NewRequest("GET", "/panic", nil))
	if response.Code != 500 {
		t.Fatal(response.Code)
	}
	var found bool
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatal(err)
		}
		if entry["msg"] == "http handler panic" {
			found = true
			if entry["request_id"] != response.Header().Get("X-Request-ID") || !strings.Contains(fmt.Sprint(entry["error"]), "fixture failure") {
				t.Fatal(entry)
			}
		}
	}
	if !found {
		t.Fatal("missing panic diagnostic")
	}
	for _, secret := range []string{"panic-test-key", "private-password", "private-query"} {
		if strings.Contains(out.String(), secret) || strings.Contains(response.Body.String(), secret) {
			t.Fatal("panic leaked credentials")
		}
	}
}

func TestTerminalFailuresKeepIdentityAndSeparateNormalClosures(t *testing.T) {
	for _, tc := range []struct {
		op    string
		err   error
		level string
	}{
		{"open", errors.New("provider https://example.test/pty?token=private-terminal-token"), "ERROR"},
		{"read", errors.New("stream failed"), "ERROR"},
		{"write", errors.New("input rejected"), "ERROR"},
		{"resize", errors.New("resize rejected"), "ERROR"},
		{"websocket_read", &websocket.CloseError{Code: websocket.CloseNormalClosure}, "DEBUG"},
		{"close", context.Canceled, "DEBUG"},
	} {
		t.Run(tc.op, func(t *testing.T) {
			var out bytes.Buffer
			request := logging.WithLogger(context.Background(), slog.New(logging.NewHandler(&out, slog.LevelDebug)).With("request_id", "terminal-request", "session_id", "terminal-session"))
			ctx := logging.WithLogger(context.Background(), logging.FromContext(request))
			logTerminalFailure(ctx, tc.op, tc.err)
			var entry map[string]any
			if err := json.Unmarshal(out.Bytes(), &entry); err != nil {
				t.Fatal(err)
			}
			if entry["level"] != tc.level || entry["operation"] != tc.op || entry["request_id"] != "terminal-request" || entry["session_id"] != "terminal-session" || strings.Contains(out.String(), "private-terminal-token") {
				t.Fatal(entry)
			}
		})
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
