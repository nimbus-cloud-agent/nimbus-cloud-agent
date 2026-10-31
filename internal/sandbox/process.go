package sandbox

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	e2b "github.com/matiasinsaurralde/go-e2b"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
)

type capture struct {
	result model.CommandResult
	size   int
	emit   func(model.CommandOutput) error
	err    error
	cancel context.CancelFunc
}

func (c *capture) take(channel string, data []byte) {
	if c.err != nil {
		return
	}
	n := min(len(data), outputLimit-c.size)
	if n < len(data) {
		c.result.Truncated = true
	}
	c.size += n
	// The SDK decodes complete UTF-8 chunks. Truncation must not split a rune.
	for n > 0 && !utf8.Valid(data[:n]) {
		n--
	}
	if n == 0 {
		return
	}
	text := string(data[:n])
	if channel == "stdout" {
		c.result.Stdout += text
	} else {
		c.result.Stderr += text
	}
	if c.emit != nil {
		if err := c.emit(model.CommandOutput{Channel: channel, Text: text}); err != nil {
			c.err = err
			c.cancel()
		}
	}
}
func (c *Client) Exec(ctx context.Context, id string, args model.SandboxArgs, output func(model.CommandOutput) error) (model.CommandResult, error) {
	if strings.TrimSpace(args.Command) == "" {
		return model.CommandResult{}, errors.New("command is required")
	}
	timeout := args.TimeoutMS
	if timeout == 0 {
		timeout = 55000
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(max(1000, min(timeout, 55000)))*time.Millisecond)
	defer cancel()
	sb, err := c.sdk.Connect(ctx, id, 60)
	if err != nil {
		return model.CommandResult{}, c.failure(ctx, err)
	}
	cwd := args.Cwd
	if cwd == "" {
		cwd = "/workspace"
	}
	captured := capture{emit: output, cancel: cancel}
	handle, err := sb.Commands.Start(ctx, args.Command, e2b.WithUser("root"), e2b.WithCwd(cwd), e2b.WithTimeout(0), e2b.WithOnStdout(func(b []byte) { captured.take("stdout", b) }), e2b.WithOnStderr(func(b []byte) { captured.take("stderr", b) }))
	if err != nil {
		return model.CommandResult{}, c.failure(ctx, err)
	}
	result, err := handle.Wait(ctx)
	var exit *e2b.CommandExitError
	if err != nil && !errors.As(err, &exit) {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_, _ = handle.Kill(cleanup)
		if captured.err != nil {
			return captured.result, captured.err
		}
		return captured.result, c.failure(ctx, err)
	}
	if result == nil {
		return captured.result, errors.New("E2B command ended without a result")
	}
	captured.result.ExitCode = int64(result.ExitCode)
	return captured.result, nil
}

type terminal struct {
	client   *Client
	sandbox  *e2b.Sandbox
	handle   *e2b.CommandHandle
	ctx      context.Context
	cancel   context.CancelFunc
	once     sync.Once
	closeErr error
}

func (c *Client) OpenTerminal(parent context.Context, id string) (Terminal, error) {
	ctx, cancel := context.WithCancel(parent)
	deadline := time.AfterFunc(60*time.Second, cancel)
	defer deadline.Stop()
	sb, err := c.sdk.Connect(ctx, id, 60)
	if err != nil {
		cancel()
		return nil, c.failure(parent, err)
	}
	handle, err := sb.Pty.Create(ctx, 80, 24, e2b.WithPtyUser("root"), e2b.WithPtyCwd("/workspace"), e2b.WithPtyTimeout(0))
	if err != nil {
		cancel()
		return nil, c.failure(parent, err)
	}
	return &terminal{client: c, sandbox: sb, handle: handle, ctx: ctx, cancel: cancel}, nil
}
func (t *terminal) Read(output func([]byte) error) error {
	var outputErr error
	_, err := t.handle.Wait(t.ctx, e2b.WithWaitOnPty(func(data []byte) {
		if outputErr != nil {
			return
		}
		if outputErr = output(data); outputErr != nil {
			t.cancel()
		}
	}))
	if outputErr != nil {
		return outputErr
	}
	var exit *e2b.CommandExitError
	if errors.As(err, &exit) {
		return nil
	} // An interactive shell exit closes the terminal.
	return t.client.failure(t.ctx, err)
}
func (t *terminal) Write(ctx context.Context, data []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return t.client.failure(ctx, t.sandbox.Pty.SendInput(ctx, t.handle.PID(), data))
}
func (t *terminal) Resize(ctx context.Context, cols, rows int64) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return t.client.failure(ctx, t.sandbox.Pty.Resize(ctx, t.handle.PID(), uint32(max(2, min(cols, 500))), uint32(max(2, min(rows, 200)))))
}
func (t *terminal) Close() error {
	t.once.Do(func() {
		t.cancel()
		// Also release the SDK response if the browser disconnected before Read
		// started. Disconnect is safe alongside an active Wait.
		_ = t.handle.Disconnect()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := t.handle.Kill(ctx)
		t.closeErr = t.client.failure(ctx, err)
	})
	return t.closeErr
}
