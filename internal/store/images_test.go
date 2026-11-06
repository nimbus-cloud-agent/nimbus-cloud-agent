package store

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
)

func attachment(name string, data []byte) model.Attachment {
	return model.Attachment{
		Ref:  model.ImageRef{AttachmentID: "sha256:" + name, MediaType: "image/png", Bytes: int64(len(data)), Width: 1, Height: 1, Name: name},
		Data: data,
	}
}

func TestSaveAttachmentIsIdempotentAndSessionScoped(t *testing.T) {
	s, session := typedStore(t)
	ctx := context.Background()
	other := Session{ID: uuid.NewString(), Title: "other", AgentState: "idle", SyncedSeq: -1, Options: model.DefaultOptions()}
	if err := s.CreateSession(ctx, other); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		sid  string
		data model.Attachment
	}{{session.ID, attachment("agent", []byte("one"))}, {session.ID, attachment("agent", []byte("one"))}, {other.ID, attachment("agent", []byte("one"))}} {
		if err := s.SaveAttachment(ctx, row.sid, row.data); err != nil {
			t.Fatal(err)
		}
	}
	own, err := s.SessionImages(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(own) != 1 || string(own[0].Data) != "one" {
		t.Fatalf("duplicate or foreign rows: %+v", own)
	}
	if foreign, err := s.SessionImages(ctx, other.ID); err != nil || len(foreign) != 1 {
		t.Fatalf("session scope: %d %v", len(foreign), err)
	}
}
