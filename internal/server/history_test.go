package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
)

func TestHistoryHTTPRejectsLegacyAndForeignCursors(t *testing.T) {
	db := optionsDB(t)
	r := optionsRouter(db)
	s := &Server{S: db}
	a, b := createStreamSession(t, s), createStreamSession(t, s)
	out := createStreamOutbox(t, s, a)
	path := "/sessions/" + a.ID
	page := expectOptions(t, r, 200, "GET", path+"/events", nil)
	for _, key := range []string{"events", "frames"} {
		if _, ok := page[key]; ok {
			t.Fatal("history exposes raw trace", page)
		}
	}
	if len(page["outbox"].([]any)) != 1 || len(page["turns"].([]any)) != 1 || page["before_turn"] != nil || page["has_more"] != false || page["questions"] == nil {
		t.Fatal(page)
	}
	for _, query := range []string{"?after_seq=-1", "?types=assistant/message", "?before_turn=no", "?limit=0", "?limit=x"} {
		expectOptions(t, r, 400, "GET", path+"/events"+query, nil)
	}
	other := "/sessions/" + b.ID
	expectOptions(t, r, 404, "GET", other+"/events?before_turn="+out.TurnID, nil)
	expectOptions(t, r, 404, "GET", other+"/stream?since_turn="+out.TurnID, nil)
	for _, suffix := range []string{"/events", "/stream"} {
		expectOptions(t, r, 404, "GET", other+"/turns/"+out.TurnID+suffix, nil)
		expectOptions(t, r, 404, "GET", path+"/turns/"+uuid.NewString()+suffix, nil)
		expectOptions(t, r, 400, "GET", path+"/turns/invalid"+suffix, nil)
	}
	for _, query := range []string{"?after_seq=x", "?after_frame=-1", "?after_seq=-2"} {
		expectOptions(t, r, 400, "GET", path+"/turns/"+out.TurnID+"/stream"+query, nil)
	}
	trace := expectOptions(t, r, 200, "GET", path+"/turns/"+out.TurnID+"/events", nil)
	if len(trace["events"].([]any)) != 0 || len(trace["frames"].([]any)) != 0 || trace["cursor"].(map[string]any)["seq"] != float64(-1) {
		t.Fatal(trace)
	}
}

func TestMainStreamWindowNeverBackfillsOrReadsFrameInvalidations(t *testing.T) {
	s, probe, url := newStreamServer(t)
	session := createStreamSession(t, s)
	rows := make([]model.Outbox, 13)
	for i := range rows {
		rows[i] = createStreamOutbox(t, s, session)
	}
	stream := openStream(t, url+"/api/sessions/"+session.ID+"/stream?since_turn="+rows[11].TurnID, "")
	initial := stream.await(t, "outbox", nil)
	var got []model.Outbox
	if err := json.Unmarshal([]byte(initial.data), &got); err != nil || len(got) != 2 || got[0].ID != rows[11].ID {
		t.Fatal(initial, err)
	}
	stream.await(t, "questions", nil)
	newRows := make([]model.Outbox, 12)
	if err := s.S.Transaction(context.Background(), func(tx *store.Store) error {
		for i := range newRows {
			out := model.Outbox{ID: uuid.NewString(), SessionID: session.ID, TurnID: uuid.NewString(), ClientMsgID: uuid.NewString(), Options: session.Options, Status: "queued", Images: []model.MessageImage{}, CreatedAt: time.Now().Add(time.Duration(i) * time.Microsecond)}
			newRows[i] = out
			if err := tx.CreateOutbox(context.Background(), store.Outbox(out)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	update := stream.await(t, "outbox", func(data string) bool { return strings.Contains(data, newRows[11].TurnID) })
	if err := json.Unmarshal([]byte(update.data), &got); err != nil || len(got) != 14 || got[0].ID != rows[11].ID || got[13].ID != newRows[11].ID {
		t.Fatal("window omitted new or backfilled old turns", update, err)
	}
	turns := stream.await(t, "turns", nil)
	if strings.Contains(turns.data, "payload") {
		t.Fatal("main stream leaked native trace", turns)
	}
	// The projection flush must be a complete no-op for raw frame notifications.
	time.Sleep(150 * time.Millisecond)
	before, err := probe.Activity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)
	if err = s.flushSession(c, session.ID, &streamCursor{}, framesChanged); err != nil {
		t.Fatal(err)
	}
	after, err := probe.Activity(context.Background())
	if err != nil || !reflect.DeepEqual(before, after) || w.Body.Len() != 0 {
		t.Fatal("frame notification queried main history", before, after, err, w.Body.String())
	}
}

func TestDetailSnapshotToStreamCatchupAndTurnIsolation(t *testing.T) {
	s, _, url := newStreamServer(t)
	session := createStreamSession(t, s)
	first, second := createStreamOutbox(t, s, session), createStreamOutbox(t, s, session)
	ctx := context.Background()
	if err := s.S.UpdateOutbox(ctx, first.ID, store.OutboxChanges{Status: ptr("delivered"), DeliveredSeq: ptr(int64(1))}); err != nil {
		t.Fatal(err)
	}
	if err := s.S.UpdateOutbox(ctx, second.ID, store.OutboxChanges{Status: ptr("delivered"), DeliveredSeq: ptr(int64(4))}); err != nil {
		t.Fatal(err)
	}
	for i, kind := range []string{"turn/start", "user/message", "assistant/message", "turn/start", "user/message", "assistant/message"} {
		if err := s.S.CreateEvent(ctx, store.Event{SessionID: session.ID, Seq: int64(i), Type: kind, Payload: model.NativeRecord{Seq: int64(i), Type: kind}}); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := s.S.TurnTrace(ctx, session.ID, first.TurnID, model.TraceCursor{Seq: -1}, 0, store.TraceFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, out := range []model.Outbox{first, second} {
		if err = s.S.CreateFrame(ctx, store.StreamFrame{SessionID: session.ID, TurnID: out.TurnID, Payload: model.FramePayload{Type: "tool-output", Text: out.TurnID}}); err != nil {
			t.Fatal(err)
		}
	}
	stream := openStream(t, url+"/api/sessions/"+session.ID+"/turns/"+first.TurnID+"/stream?after_seq=2&after_frame=0", "")
	frameEvent := stream.await(t, "frame", nil)
	var frame model.StreamFrame
	if err = json.Unmarshal([]byte(frameEvent.data), &frame); err != nil || frame.TurnID != first.TurnID || !strings.HasPrefix(frameEvent.id, "2:") || snapshot.Cursor.Seq != 2 {
		t.Fatal("snapshot/stream gap or foreign trace", frameEvent, snapshot.Cursor, err)
	}
	stream.await(t, "outbox", nil)
	// A new frame for a neighboring turn must not leak on this lane.
	if err = s.S.CreateFrame(ctx, store.StreamFrame{SessionID: session.ID, TurnID: second.TurnID, Payload: model.FramePayload{Type: "foreign"}}); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case event := <-stream.events:
			if event.kind == "frame" || event.kind == "session" {
				t.Fatal("replayed foreign turn", event)
			}
			if event.kind == "heartbeat" {
				return
			}
		case <-time.After(7 * time.Second):
			t.Fatal("missing idle heartbeat")
		}
	}
}

func TestDetailCompositeCursorValidation(t *testing.T) {
	for _, fixture := range []struct {
		last, query string
		want        model.TraceCursor
		valid       bool
	}{
		{"4:12", "/?after_seq=1&after_frame=2", model.TraceCursor{Seq: 4, Frame: 12}, true},
		{"", "/?after_seq=3&after_frame=8", model.TraceCursor{Seq: 3, Frame: 8}, true},
		{"-1:0", "/", model.TraceCursor{Seq: -1}, true},
		{"4", "/", model.TraceCursor{}, false},
		{"4:-1", "/", model.TraceCursor{}, false},
		{"4:12:13", "/", model.TraceCursor{}, false},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", fixture.query, nil)
		c.Request.Header.Set("Last-Event-ID", fixture.last)
		got, err := detailCursor(c)
		if (err == nil) != fixture.valid || (fixture.valid && got != fixture.want) {
			t.Fatal(fixture, got, err)
		}
	}
}

func TestMetadataHTTPAndScopedDetailStreams(t *testing.T) {
	s, _, url := newStreamServer(t)
	session := createStreamSession(t, s)
	out := createStreamOutbox(t, s, session)
	ctx := context.Background()
	if err := s.S.UpdateOutbox(ctx, out.ID, store.OutboxChanges{Status: ptr("delivered"), DeliveredSeq: ptr(int64(1))}); err != nil {
		t.Fatal(err)
	}
	secret := "UNOPENED-DETAIL-BODY"
	for i, fixture := range []struct {
		kind string
		data any
	}{
		{"turn/start", map[string]any{}},
		{"user/message", map[string]any{}},
		{"tool/call", map[string]any{"callId": "r", "name": "exec", "arguments": map[string]any{"description": "Visible label", "command": secret}}},
		{"tool/ptc-dispatch", map[string]any{"subCallId": "c", "parentCallId": "r", "name": "child", "content": secret}},
	} {
		raw, _ := json.Marshal(map[string]any{"seq": i, "type": fixture.kind, "time": 1000 + i, "data": fixture.data})
		var payload model.NativeRecord
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		if err := s.S.CreateEvent(ctx, store.Event{SessionID: session.ID, Seq: int64(i), Type: fixture.kind, Payload: payload}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.S.CreateFrame(ctx, store.StreamFrame{SessionID: session.ID, TurnID: out.TurnID, Payload: model.FramePayload{Type: "tool-output", CallID: "r", Text: secret}}); err != nil {
		t.Fatal(err)
	}
	r := optionsRouter(s.S)
	path := "/sessions/" + session.ID + "/turns/" + out.TurnID
	metadata := expectOptions(t, r, 200, "GET", path+"/events?projection=metadata&parent_call_id=root&argument_keys=description&max_field_chars=3", nil)
	raw, _ := json.Marshal(metadata)
	if strings.Contains(string(raw), secret) || len(metadata["tools"].([]any)) != 1 || metadata["tools"].([]any)[0].(map[string]any)["args"].(map[string]any)["description"] != "Vis" {
		t.Fatal("metadata leaked or missed projection", metadata)
	}
	metadataStream := openStream(t, url+"/api"+path+"/stream?projection=metadata&parent_call_id=root&argument_keys=description&max_field_chars=3", "")
	initial := metadataStream.await(t, "metadata", nil)
	if !strings.HasPrefix(initial.id, "3:") || strings.Contains(initial.data, secret) {
		t.Fatal(initial)
	}
	// Raw details query only one explicit tool; siblings remain unopened.
	tool := expectOptions(t, r, 200, "GET", path+"/events?call_id=r", nil)
	if len(tool["events"].([]any)) != 1 || len(tool["frames"].([]any)) != 1 {
		t.Fatal("tool scope leaked child", tool)
	}
	toolStream := openStream(t, url+"/api"+path+"/stream?call_id=r", "")
	landed := toolStream.await(t, "session", nil)
	if landed.id != "2:0" || !strings.Contains(landed.data, secret) {
		t.Fatal("explicit raw detail unavailable", landed)
	}
	toolStream.await(t, "frame", nil)
	toolStream.await(t, "outbox", nil)
	if err := s.S.CreateFrame(ctx, store.StreamFrame{SessionID: session.ID, TurnID: out.TurnID, Payload: model.FramePayload{Type: "tool-output", CallID: "c", Text: "FOREIGN-CHILD-OUTPUT"}}); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case event := <-toolStream.events:
			if event.kind == "session" || event.kind == "frame" {
				t.Fatal("filtered stream leaked sibling", event)
			}
			if event.kind == "heartbeat" {
				return
			}
		case <-time.After(7 * time.Second):
			t.Fatal("missing filtered stream heartbeat")
		}
	}
}

func TestQuestionContentProjectionAcrossHistoryAndStreams(t *testing.T) {
	s, _, url := newStreamServer(t)
	session := createStreamSession(t, s)
	out := createStreamOutbox(t, s, session)
	secret := "UNOPENED-PLAN-CONTENT"
	q := store.UserQuestion{ID: uuid.NewString(), SessionID: session.ID, TurnID: out.TurnID, Status: "pending", Questions: []model.Question{{ID: "review", Question: secret, Detail: secret, Intent: &model.QuestionIntent{Kind: "plan-review", Approve: "Approve"}}}}
	if err := s.S.CreateQuestion(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	r := optionsRouter(s.S)
	path := "/sessions/" + session.ID
	for _, suffix := range []string{"/events?include_question_content=false", "/turns/" + out.TurnID + "/events?projection=metadata&include_question_content=false", "/turns/" + out.TurnID + "/events?include_question_content=false"} {
		data := expectOptions(t, r, 200, "GET", path+suffix, nil)
		raw, _ := json.Marshal(data)
		if strings.Contains(string(raw), secret) || !strings.Contains(string(raw), "plan-review") || len(data["questions"].([]any)) != 1 {
			t.Fatal("question projection lost status/intent or leaked content", data)
		}
	}
	full := expectOptions(t, r, 200, "GET", path+"/events", nil)
	raw, _ := json.Marshal(full)
	if !strings.Contains(string(raw), secret) {
		t.Fatal("explicit full query lost question content")
	}
	main := openStream(t, url+"/api"+path+"/stream?include_question_content=false", "")
	questions := main.await(t, "questions", nil)
	if strings.Contains(questions.data, secret) || !strings.Contains(questions.data, "plan-review") {
		t.Fatal(questions)
	}
	metadata := openStream(t, url+"/api"+path+"/turns/"+out.TurnID+"/stream?projection=metadata&include_question_content=false", "")
	event := metadata.await(t, "metadata", nil)
	if strings.Contains(event.data, secret) || !strings.Contains(event.data, "plan-review") {
		t.Fatal(event)
	}
}
