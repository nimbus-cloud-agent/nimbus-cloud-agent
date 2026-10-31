package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
)

func fixtureCommand(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs("../../runtime/test-dist/test/fixtures/service.js")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("build the TypeScript fixture first: make test-runtime")
	}
	return path
}
func echo(r *Runtime, ctx context.Context, text string) (model.FileReadResult, error) {
	return r.ReadSandboxFile(ctx, "", "fixture", text)
}
func fixtureTurn() TurnInput {
	return TurnInput{SessionID: "session", TurnID: "turn", MessageID: "message", Message: "review", SessionMode: "standard", Options: model.DefaultOptions()}
}
func fixture(t *testing.T) *Runtime {
	t.Helper()
	r := New(fixtureCommand(t))
	t.Cleanup(r.Close)
	return r
}
func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func TestRPCMultiplexAndBackpressure(t *testing.T) {
	r := fixture(t)
	ctx := testContext(t)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			text := fmt.Sprint(i) + strings.Repeat("大", 600000)
			result, err := echo(r, ctx, text)
			if err != nil {
				t.Error(err)
				return
			}
			if result.Content != text {
				t.Error("response crossed requests or was truncated")
			}
		}(i)
	}
	wg.Wait()
	pid := r.Status().PID
	// A callback may call the same runtime without blocking the stdout reader.
	command := "nested"
	_, err := r.ExecSandbox(ctx, "", "fixture", model.SandboxArgs{Command: command}, func(chunk model.CommandOutput) error {
		_, err := echo(r, ctx, chunk.Text)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	next := 0
	_, err = r.ExecSandbox(ctx, "", "fixture", model.SandboxArgs{Command: "stream"}, func(chunk model.CommandOutput) error {
		if chunk.Text != fmt.Sprint(next) {
			t.Errorf("out of order: %s != %d", chunk.Text, next)
		}
		next++
		time.Sleep(10 * time.Millisecond)
		return nil
	})
	if err != nil || next != 3 {
		t.Fatalf("stream: %d %v", next, err)
	}
	if r.Status().PID != pid {
		t.Fatal("runtime restarted between requests")
	}
}
func TestCancelRequestKeepsOtherRequestsAlive(t *testing.T) {
	r := fixture(t)
	ctx := testContext(t)
	_, err := echo(r, ctx, "echo")
	if err != nil {
		t.Fatal(err)
	}
	pid := r.Status().PID
	waitCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	waiting := make(chan struct{})
	go func() {
		command := "wait"
		_, err := r.ExecSandbox(waitCtx, "", "fixture", model.SandboxArgs{Command: command}, func(model.CommandOutput) error { close(waiting); return nil })
		done <- err
	}()
	select {
	case <-waiting:
	case <-ctx.Done():
		t.Fatal("request not started")
	}
	cancel()
	if _, err = echo(r, ctx, "echo"); err != nil {
		t.Fatal(err)
	}
	if err = <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if r.Status().PID != pid {
		t.Fatal("cancellation killed the shared runtime")
	}
}
func TestCrashCancelsCallbacksAndRestartsWithoutReplay(t *testing.T) {
	r := fixture(t)
	ctx := testContext(t)
	waiting := make(chan struct{})
	cancelled := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		err := r.Turn(ctx, fixtureTurn(), func(model.TurnEvent) error { return nil }, func(c context.Context, _ model.QuestionRequest) (model.QuestionAnswer, error) {
			close(waiting)
			<-c.Done()
			close(cancelled)
			return model.QuestionAnswer{}, c.Err()
		}, nil)
		done <- err
	}()
	select {
	case <-waiting:
	case <-ctx.Done():
		t.Fatal("callback not delivered")
	}
	old := r.Status()
	proc, _ := os.FindProcess(old.PID)
	if err := proc.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrRuntimeLost) {
		t.Fatalf("expected interrupted runtime: %v", err)
	}
	select {
	case <-cancelled:
	case <-ctx.Done():
		t.Fatal("question callback leaked")
	}
	// Restart is proactive: no client request is needed to trigger it.
	for r.Status().Generation == old.Generation {
		select {
		case <-ctx.Done():
			t.Fatal("no automatic restart")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if r.Status().PID == old.PID {
		t.Fatal("PID did not change")
	}
	if _, err := echo(r, ctx, "echo"); err != nil {
		t.Fatal(err)
	}
	// Also restart after an idle crash.
	old = r.Status()
	proc, _ = os.FindProcess(old.PID)
	_ = proc.Kill()
	for r.Status().Generation == old.Generation {
		select {
		case <-ctx.Done():
			t.Fatal("no idle restart")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
func TestCloseReapsChildAndNeverRestarts(t *testing.T) {
	r := New(fixtureCommand(t))
	ctx := testContext(t)
	if _, err := echo(r, ctx, "echo"); err != nil {
		r.Close()
		t.Fatal(err)
	}
	before := r.Status()
	r.Close()
	r.Close()
	time.Sleep(300 * time.Millisecond)
	if after := r.Status(); after.State != "stopped" || after.Generation != before.Generation {
		t.Fatalf("restarted after shutdown: %+v", after)
	}
	if _, err := echo(r, ctx, "echo"); err == nil {
		t.Fatal("accepted request after close")
	}
}

func TestToolCancelDoesNotCancelOwningTurn(t *testing.T) {
	r := fixture(t)
	ctx := testContext(t)
	cancelled := false
	input := fixtureTurn()
	input.Message = "cancel-tool"
	err := r.Turn(ctx, input, func(model.TurnEvent) error { return nil }, nil, func(c context.Context, _ model.ToolCall) (model.ToolResult, error) {
		<-c.Done()
		cancelled = true
		return model.ToolResult{}, c.Err()
	})
	if err != nil || !cancelled {
		t.Fatalf("tool cancellation: %v %v", cancelled, err)
	}
	if ctx.Err() != nil {
		t.Fatal("tool cancellation cancelled the parent turn")
	}
	if _, err = echo(r, ctx, "echo"); err != nil {
		t.Fatal(err)
	}
}
