package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store/testdb"
)

type receivedEvent struct{ kind, id, data string }
type streamReader struct {
	events chan receivedEvent
	cancel context.CancelFunc
}

func openStream(t *testing.T, url, last string) *streamReader {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	request, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if last != "" {
		request.Header.Set("Last-Event-ID", last)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if response.StatusCode != 200 {
		cancel()
		response.Body.Close()
		t.Fatalf("stream status %d", response.StatusCode)
	}
	stream := &streamReader{events: make(chan receivedEvent, 4096), cancel: cancel}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer close(stream.events)
		defer response.Body.Close()
		scanner := bufio.NewScanner(response.Body)
		scanner.Buffer(make([]byte, 4096), 8<<20)
		var event receivedEvent
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				select {
				case stream.events <- event:
				case <-ctx.Done():
					return
				}
				event = receivedEvent{}
				continue
			}
			if line == ": heartbeat" {
				event.kind = "heartbeat"
			}
			if strings.HasPrefix(line, "event: ") {
				event.kind = strings.TrimPrefix(line, "event: ")
			}
			if strings.HasPrefix(line, "id: ") {
				event.id = strings.TrimPrefix(line, "id: ")
			}
			if strings.HasPrefix(line, "data: ") {
				event.data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	t.Cleanup(func() { cancel(); response.Body.Close(); <-done })
	return stream
}
func (s *streamReader) await(t *testing.T, kind string, match func(string) bool) receivedEvent {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case event, ok := <-s.events:
			if !ok {
				t.Fatalf("stream ended while waiting for %s", kind)
			}
			if event.kind == kind && (match == nil || match(event.data)) {
				return event
			}
		case <-deadline.C:
			t.Fatalf("missing SSE %s", kind)
		}
	}
}
func newStreamServer(t *testing.T) (*Server, *testdb.Probe, string) {
	t.Helper()
	db, probe := testdb.OpenWithProbe(t)
	s := &Server{S: db}
	startTestChanges(t, s)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	s.routes(router)
	httpServer := httptest.NewServer(router)
	t.Cleanup(httpServer.Close)
	return s, probe, httpServer.URL
}
func createStreamSession(t *testing.T, s *Server) model.Session {
	t.Helper()
	session := model.Session{ID: uuid.NewString(), Title: "stream fixture", AgentState: "idle", SyncedSeq: -1, Options: model.DefaultOptions()}
	if err := s.S.CreateSession(context.Background(), store.Session(session)); err != nil {
		t.Fatal(err)
	}
	return session
}
func createStreamOutbox(t *testing.T, s *Server, session model.Session) model.Outbox {
	t.Helper()
	out := model.Outbox{ID: uuid.NewString(), SessionID: session.ID, TurnID: uuid.NewString(), ClientMsgID: uuid.NewString(), Options: session.Options, Status: "queued", Images: []model.MessageImage{}}
	if err := s.S.CreateOutbox(context.Background(), store.Outbox(out)); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestStreamsStayIdleThroughHeartbeatAndRecoverListener(t *testing.T) {
	s, probe, url := newStreamServer(t)
	session := createStreamSession(t, s)
	s.running = make(map[string]*runningTurn)
	s.wg.Add(1)
	go s.scheduler()
	list := openStream(t, url+"/api/sessions/stream", "")
	selected := openStream(t, url+"/api/sessions/"+session.ID+"/stream", "")
	list.await(t, "sessions", nil)
	selected.await(t, "questions", nil)
	// Drain initial notifications that can legitimately overlap the first snapshot.
	time.Sleep(150 * time.Millisecond)
	before, err := probe.Activity(context.Background())
	if err != nil || len(before) == 0 {
		t.Fatal(before, err)
	}
	list.await(t, "heartbeat", nil)
	selected.await(t, "heartbeat", nil)
	after, err := probe.Activity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("idle SSE/heartbeat queried PostgreSQL: before=%v after=%v", before, after)
	}
	title := "pushed after commit"
	if err := s.S.UpdateSession(context.Background(), session.ID, store.SessionChanges{Title: &title}); err != nil {
		t.Fatal(err)
	}
	contains := func(text string) func(string) bool {
		return func(data string) bool { return strings.Contains(data, text) }
	}
	list.await(t, "sessions", contains(title))
	selected.await(t, "state", contains(title))
	// Ephemeral provisioning is not a DB row yet. Its explicit invalidation must
	// update the same projections without restoring a periodic polling fallback.
	s.provisioning.Store(session.ID, "starting fixture")
	s.updates.publish(session.ID, sandboxChanged)
	list.await(t, "sessions", contains("provisioning"))
	selected.await(t, "state", contains("provisioning"))
	s.provisioning.Delete(session.ID)
	s.updates.publish(session.ID, sandboxChanged)
	selected.await(t, "state", contains("not_started"))
	if err := probe.DisconnectListener(context.Background()); err != nil {
		t.Fatal(err)
	}
	title = "visible after listener reconnect"
	if err := s.S.UpdateSession(context.Background(), session.ID, store.SessionChanges{Title: &title}); err != nil {
		t.Fatal(err)
	}
	list.await(t, "sessions", contains(title))
	selected.await(t, "state", contains(title))
	if err := s.S.DeleteSession(context.Background(), session.ID); err != nil {
		t.Fatal(err)
	}
	list.await(t, "sessions", func(data string) bool { return data == "[]" })
}

func TestOneInvalidationDrainsAllEventAndFramePages(t *testing.T) {
	s, _, url := newStreamServer(t)
	session := createStreamSession(t, s)
	out := createStreamOutbox(t, s, session)
	selected := openStream(t, url+"/api/sessions/"+session.ID+"/stream", "")
	selected.await(t, "questions", nil)
	const count = streamPageSize + 5
	ctx := context.Background()
	// Each kind has only one distinct payload in this transaction, so PostgreSQL
	// coalesces >1000 notifications into one. Catch-up must drain every page.
	if err := s.S.Transaction(ctx, func(tx *store.Store) error {
		for i := range count {
			if err := tx.CreateEvent(ctx, store.Event{SessionID: session.ID, Seq: int64(i), Type: "fixture", Payload: model.NativeRecord{Seq: int64(i), Type: "fixture"}}); err != nil {
				return err
			}
			if err := tx.CreateFrame(ctx, store.StreamFrame{SessionID: session.ID, TurnID: out.TurnID, Payload: model.FramePayload{Type: "tool-output", Text: strconv.Itoa(i)}}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for i := range count {
		event := selected.await(t, "session", nil)
		if event.id != strconv.Itoa(i) {
			t.Fatalf("event cursor gap: got %s want %d", event.id, i)
		}
	}
	for i := range count {
		event := selected.await(t, "frame", nil)
		var frame model.StreamFrame
		if err := json.Unmarshal([]byte(event.data), &frame); err != nil {
			t.Fatal(err)
		}
		if frame.Payload.Text != strconv.Itoa(i) {
			t.Fatalf("frame gap: got %s want %d", frame.Payload.Text, i)
		}
	}
	selected.cancel()
	reconnected := openStream(t, url+"/api/sessions/"+session.ID+"/stream", fmt.Sprint(count-3))
	for i := count - 2; i < count; i++ {
		if got := reconnected.await(t, "session", nil); got.id != strconv.Itoa(i) {
			t.Fatal(got)
		}
	}
}
