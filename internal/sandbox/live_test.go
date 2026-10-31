package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
)

// Opt-in: creates and deletes only its own real sandbox. No Node process or
// runtime is involved, including lifecycle, shell commands, files and PTY.
func TestLiveE2BControlPlane(t *testing.T) {
	key := os.Getenv("NIMBUS_TEST_E2B_KEY")
	if key == "" {
		t.Skip("set NIMBUS_TEST_E2B_KEY for real E2B")
	}
	c, err := New(key)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	info, err := c.Create(ctx, "base", "nimbus-go-adapter-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := c.Kill(ctx, info.SandboxID); err != nil {
			t.Error(err)
		}
	})
	// Confirm the SDK's autoPause configuration maps to the cloud's lifecycle.
	response, err := c.request(ctx, "GET", info.SandboxID, "")
	if err != nil {
		t.Fatal(err)
	}
	var provider infoResponse
	err = json.NewDecoder(response.Body).Decode(&provider)
	response.Body.Close()
	if err != nil || provider.Lifecycle.OnTimeout != "pause" || provider.Lifecycle.AutoResume {
		t.Fatalf("lifecycle: %+v, %v", provider.Lifecycle, err)
	}
	_, err = c.WriteFile(ctx, info.SandboxID, "/workspace/go-proof.txt", "GO_E2B_OK 中文\n")
	if err != nil {
		t.Fatal(err)
	}
	file, err := c.ReadFile(ctx, info.SandboxID, "/workspace/go-proof.txt")
	if err != nil || file.Content != "GO_E2B_OK 中文\n" {
		t.Fatal(file, err)
	}
	var output strings.Builder
	result, err := c.Exec(ctx, info.SandboxID, model.SandboxArgs{Command: "cat /workspace/go-proof.txt; printf ERR >&2; exit 7"}, func(chunk model.CommandOutput) error { output.WriteString(chunk.Text); return nil })
	if err != nil || result.ExitCode != 7 || result.Stderr != "ERR" || !strings.Contains(output.String(), "GO_E2B_OK 中文") {
		t.Fatal(result, err)
	}
	t.Log("PASS Go SDK files, streaming command, Unicode and nonzero exit")
	term, err := c.OpenTerminal(ctx, info.SandboxID)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	received := make(chan struct{}, 1)
	go func() {
		var b strings.Builder
		done <- term.Read(func(data []byte) error {
			b.Write(data)
			if strings.Contains(b.String(), "PTY_GO_OK") {
				select {
				case received <- struct{}{}:
				default:
				}
			}
			return nil
		})
	}()
	if err = term.Resize(ctx, 111, 33); err != nil {
		t.Fatal(err)
	}
	if err = term.Write(ctx, []byte("printf '\\120\\124\\131_GO_OK\\n'; stty size\r")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-received:
	case <-time.After(15 * time.Second):
		t.Fatal("PTY output missing")
	}
	if err = term.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("PTY did not close")
	}
	t.Log("PASS direct Go PTY input, resize, output and close")
	unread, err := c.OpenTerminal(ctx, info.SandboxID)
	if err != nil {
		t.Fatal(err)
	}
	if err := unread.Close(); err != nil {
		t.Fatal(err)
	}
	commandCtx, stop := context.WithCancel(ctx)
	_, err = c.Exec(commandCtx, info.SandboxID, model.SandboxArgs{Command: "echo STARTED; sleep 15; echo BAD > /workspace/late"}, func(model.CommandOutput) error { stop(); return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if err = c.Pause(ctx, info.SandboxID); err != nil {
		t.Fatal(err)
	}
	paused, err := c.Info(ctx, info.SandboxID)
	if err != nil || paused.State != "paused" {
		t.Fatal(paused, err)
	}
	resumed, err := c.Connect(ctx, info.SandboxID)
	if err != nil || resumed.SandboxID != info.SandboxID || resumed.State != "running" {
		t.Fatal(resumed, err)
	}
	file, err = c.ReadFile(ctx, info.SandboxID, "/workspace/go-proof.txt")
	if err != nil || !strings.Contains(file.Content, "GO_E2B_OK") {
		t.Fatal(file, err)
	}
	t.Log("PASS cancellation, pause without implicit wake, resume and durable file")
}
