package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
)

func TestHeaderQueriesStreamsAndHiddenEnvelopeFilters(t *testing.T) {
	s, _, url := newStreamServer(t)
	session := createStreamSession(t, s)
	ctx := context.Background()
	seq := int64(1)
	out := model.Outbox{ID: uuid.NewString(), SessionID: session.ID, TurnID: uuid.NewString(), ClientMsgID: uuid.NewString(), Content: "HIDDEN USER PROMPT", Options: session.Options, Status: "delivered", DeliveredSeq: &seq, Images: []model.MessageImage{}, CreatedAt: time.Now()}
	if err := s.S.CreateOutbox(ctx, store.Outbox(out)); err != nil {
		t.Fatal(err)
	}
	appendRecord := func(seq int64, kind string) {
		raw, _ := json.Marshal(map[string]any{"seq": seq, "type": kind, "time": 1000 + seq, "data": map[string]any{"content": "HIDDEN NATIVE BODY"}})
		var payload model.NativeRecord
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		if err := s.S.CreateEvent(ctx, store.Event{SessionID: session.ID, Seq: seq, Type: kind, Payload: payload}); err != nil {
			t.Fatal(err)
		}
	}
	for i, kind := range []string{"turn/start", "user/message", "assistant/message", "tool/call"} {
		appendRecord(int64(i), kind)
	}
	if err := s.S.CreateFrame(ctx, store.StreamFrame{SessionID: session.ID, TurnID: out.TurnID, Payload: model.FramePayload{Type: "tool-output", Text: "HIDDEN FRAME BODY"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.S.CreateQuestion(ctx, store.UserQuestion{ID: uuid.NewString(), SessionID: session.ID, TurnID: out.TurnID, Status: "pending", Questions: []model.Question{{ID: "q", Question: "HIDDEN QUESTION"}}}); err != nil {
		t.Fatal(err)
	}
	path := "/sessions/" + session.ID + "/turns/" + out.TurnID
	r := optionsRouter(s.S)
	page := expectOptions(t, r, 200, "GET", path+"/events?projection=headers&kind=events&limit=1", nil)
	if page["has_more"] != true || page["before"] != float64(3) || len(page["events"].([]any)) != 1 || page["events"].([]any)[0].(map[string]any)["seq"] != float64(3) {
		t.Fatal(page)
	}
	for _, bad := range []string{"projection=headers&kind=bad", "projection=headers&limit=0", "projection=headers&before=-1", "projection=headers&seq=1", "frame_id=0", "seq=1&frame_id=1"} {
		expectOptions(t, r, 400, "GET", path+"/events?"+bad, nil)
	}
	metadata := expectOptions(t, r, 200, "GET", path+"/events?projection=metadata&include_questions=false&include_outbox=false", nil)
	if len(metadata["outbox"].([]any)) != 0 || len(metadata["questions"].([]any)) != 0 {
		t.Fatal("unused envelopes transferred", metadata)
	}
	stream := openStream(t, url+"/api"+path+"/stream?projection=headers&kind=events&after_seq=-1", "2:0")
	header := stream.await(t, "event_header", nil)
	if header.id != "3:0" || strings.Contains(header.data, "HIDDEN") || strings.Contains(header.data, "payload") {
		t.Fatal("header replay/body boundary", header)
	}
	appendRecord(4, "assistant/message")
	appendRecord(5, "turn/end")
	for _, id := range []string{"4:0", "5:0"} {
		header = stream.await(t, "event_header", nil)
		if header.id != id {
			t.Fatal("header stream skipped/reordered", header, id)
		}
	}
	frames := openStream(t, url+"/api"+path+"/stream?projection=headers&kind=frames", "")
	frameHeader := frames.await(t, "frame_header", nil)
	var frame model.FrameHeader
	if err := json.Unmarshal([]byte(frameHeader.data), &frame); err != nil || frame.ID == 0 || strings.Contains(frameHeader.data, "HIDDEN") {
		t.Fatal(frameHeader, err)
	}
	body := expectOptions(t, r, 200, "GET", path+"/events?frame_id="+strings.Split(frameHeader.id, ":")[1]+"&include_questions=false&include_outbox=false", nil)
	if len(body["events"].([]any)) != 0 || len(body["frames"].([]any)) != 1 || len(body["questions"].([]any)) != 0 || body["frames"].([]any)[0].(map[string]any)["payload"].(map[string]any)["text"] != "HIDDEN FRAME BODY" {
		t.Fatal("exact frame body query", body)
	}
}
