package executor

import (
	"context"
	"testing"

	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
)

// Images the agent produces live only in the turn's work directory, so the
// runtime reports them before the turn ends; the control plane owns storage.
func TestAttachmentSavedEventsReachThePersistenceSink(t *testing.T) {
	r := fixture(t)
	input := fixtureTurn()
	input.Message = "attachment"
	var saved []model.Attachment
	input.SaveAttachment = func(_ context.Context, attachment model.Attachment) error {
		saved = append(saved, attachment)
		return nil
	}
	if err := r.Turn(testContext(t), input, func(model.TurnEvent) error { return nil }, nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 {
		t.Fatalf("saved %d attachments", len(saved))
	}
	if saved[0].Ref.Name != "agent.png" || saved[0].Ref.AttachmentID[0:7] != "sha256:" {
		t.Fatalf("unexpected reference: %+v", saved[0].Ref)
	}
	if string(saved[0].Data) != "\x01\x02\x03" {
		t.Fatalf("unexpected bytes: %v", saved[0].Data)
	}
}
