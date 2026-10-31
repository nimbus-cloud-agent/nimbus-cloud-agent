package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	e2b "github.com/matiasinsaurralde/go-e2b"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
)

func TestLifecycleByIDDoesNotResumeSandbox(t *testing.T) {
	var calls []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Header.Get("X-API-Key") != "test-key" {
			t.Error("missing credential")
		}
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(infoResponse{SandboxInfo: e2b.SandboxInfo{ID: "test", State: "paused"}, Region: "test-region"})
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer api.Close()
	c, err := newClient("test-key", api.URL, api.Client())
	if err != nil {
		t.Fatal(err)
	}
	info, err := c.Info(context.Background(), "test")
	if err != nil || info.State != "paused" || info.Region != "test-region" {
		t.Fatal(info, err)
	}
	if err = c.Pause(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	if err = c.Kill(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"GET /sandboxes/test", "POST /sandboxes/test/pause", "DELETE /sandboxes/test"}) {
		t.Fatal(calls)
	}
}
func TestCaptureLimitsCombinedOutputAndCancelsOnConsumerFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := capture{cancel: cancel}
	c.take("stdout", []byte(strings.Repeat("a", outputLimit-2)))
	c.take("stderr", []byte("中文"))
	if !c.result.Truncated || len(c.result.Stdout)+len(c.result.Stderr) > outputLimit || !utf8.ValidString(c.result.Stderr) {
		t.Fatal("invalid truncated output")
	}
	failure := errors.New("output consumer closed")
	c = capture{cancel: cancel, emit: func(_ model.CommandOutput) error { return failure }}
	c.take("stdout", []byte("data"))
	if !errors.Is(c.err, failure) || ctx.Err() == nil {
		t.Fatal("consumer failure did not cancel producer")
	}
}
