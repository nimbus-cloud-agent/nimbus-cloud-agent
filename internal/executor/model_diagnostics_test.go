package executor

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/logging"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
)

// A transport failure reaches the control plane only as a runtime diagnostic
// event; the provider's own cause chain never appears in the durable record.
func TestModelDiagnosticEventsAreLoggedWithTheirCauseChain(t *testing.T) {
	var out runtimeLogBuffer
	previous := slog.Default()
	slog.SetDefault(slog.New(logging.NewHandler(&out, slog.LevelDebug)))
	defer slog.SetDefault(previous)
	r := fixture(t)
	input := fixtureTurn()
	input.Message = "diagnostic"
	if err := r.Turn(testContext(t), input, func(model.TurnEvent) error { return nil }, nil, nil); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		`"msg":"model request failed"`,
		`"session_id":"session"`,
		`"turn_id":"turn"`,
		`"phase":"request"`,
		`"chunks_received":0`,
		`"code":"ECONNRESET"`,
		`"syscall":"read"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s in %s", want, text)
		}
	}
}
