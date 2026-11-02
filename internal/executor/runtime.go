package executor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/runtimeprotocol"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/runtimeprotocol/generated"
)

var ErrRuntimeLost = runtimeprotocol.ErrDisconnected

type connection struct {
	cmd         *exec.Cmd
	in          io.WriteCloser
	ctx         context.Context
	done        chan struct{}
	peer        *runtimeprotocol.Peer
	client      generated.RuntimeClient
	diagnostics *runtimeDiagnostics
}

const maxRuntimeDiagnosticBytes = 4096

// Retain a bounded stderr tail. Only startup preflight may return raw text;
// runtime crashes emit counts and exception categories, never arbitrary stderr
// that may contain a model prompt, command output or credentials.
type runtimeDiagnostics struct {
	mu    sync.Mutex
	data  []byte
	bytes int64
}

func (d *runtimeDiagnostics) Write(p []byte) (int, error) {
	n := len(p)
	d.mu.Lock()
	defer d.mu.Unlock()
	d.bytes += int64(n)
	if len(p) >= maxRuntimeDiagnosticBytes {
		d.data = append(d.data[:0], p[len(p)-maxRuntimeDiagnosticBytes:]...)
		return n, nil
	}
	if excess := len(d.data) + len(p) - maxRuntimeDiagnosticBytes; excess > 0 {
		copy(d.data, d.data[excess:])
		d.data = d.data[:len(d.data)-excess]
	}
	d.data = append(d.data, p...)
	return n, nil
}

func (d *runtimeDiagnostics) String() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return strings.TrimSpace(string(d.data))
}

func (d *runtimeDiagnostics) Reset() {
	d.mu.Lock()
	d.data = nil
	d.bytes = 0
	d.mu.Unlock()
}

var nodeErrorCode = regexp.MustCompile(`(?m)^Error \[(ERR_[A-Z0-9_]{1,64})\]:`)
var nodeErrorType = regexp.MustCompile(`(?m)^(TypeError|SyntaxError|RangeError|ReferenceError|URIError|EvalError|Error):`)

func (d *runtimeDiagnostics) attrs() []any {
	d.mu.Lock()
	defer d.mu.Unlock()
	var kind []byte
	if match := nodeErrorCode.FindSubmatch(d.data); len(match) > 1 {
		kind = match[1]
	}
	if len(kind) == 0 {
		if match := nodeErrorType.FindSubmatch(d.data); len(match) > 1 {
			kind = match[1]
		}
	}
	if strings.Contains(string(d.data), "heap out of memory") {
		kind = []byte("out_of_memory")
	}
	return []any{"stderr_bytes", d.bytes, "stderr_truncated", d.bytes > int64(len(d.data)), "stderr_kind", string(kind)}
}

type pipes struct {
	io.Reader
	io.Writer
	in  io.Closer
	out io.Closer
}

func (p *pipes) Close() error { _ = p.in.Close(); return p.out.Close() }

// Runtime owns one long-lived Node child and all multiplexed JSON-RPC requests.
// It never replays an accepted request after a crash (tools may have side effects).
type Runtime struct {
	command    string
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
	mu         sync.Mutex
	current    *connection
	changed    chan struct{}
	generation uint64
	scopes     map[string]*scope
}
type Status struct {
	State      string `json:"state"`
	PID        int    `json:"pid"`
	Generation uint64 `json:"generation"`
}

func New(command string) *Runtime {
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runtime{command: command, scopes: map[string]*scope{}, ctx: ctx, cancel: cancel, done: make(chan struct{}), changed: make(chan struct{})}
	go r.supervise()
	return r
}
func (r *Runtime) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	status := Status{State: "restarting", Generation: r.generation}
	if r.ctx.Err() != nil {
		status.State = "stopped"
	} else if r.current != nil {
		status.State = "running"
		status.PID = r.current.cmd.Process.Pid
	}
	return status
}
func (r *Runtime) Close() { r.cancel(); <-r.done }
func (r *Runtime) supervise() {
	defer close(r.done)
	backoff := 250 * time.Millisecond
	for r.ctx.Err() == nil {
		started := time.Now()
		cmd := exec.Command(r.command)
		diagnostics := &runtimeDiagnostics{}
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "LANG=en_US.UTF-8", "NODE_NO_WARNINGS=1"}
		cmd.Stderr = diagnostics
		in, err := cmd.StdinPipe()
		if err != nil {
			slog.Error("agent runtime stdin pipe failed", "error", err)
			return
		}
		out, err := cmd.StdoutPipe()
		if err != nil {
			in.Close()
			slog.Error("agent runtime stdout pipe failed", "error", err)
			return
		}
		if err = cmd.Start(); err == nil {
			ctx, cancel := context.WithCancel(r.ctx)
			c := &connection{cmd: cmd, in: in, ctx: ctx, done: make(chan struct{}), diagnostics: diagnostics}
			c.peer = runtimeprotocol.New(ctx, &pipes{Reader: out, Writer: in, in: in, out: out}, func(callbackCtx context.Context, method string, params json.RawMessage) (any, error) {
				return generated.DispatchControl(callbackCtx, controlReceiver{r}, method, params)
			})
			c.client = generated.RuntimeClient{Peer: c.peer}
			r.mu.Lock()
			r.current = c
			r.generation++
			generation := r.generation
			close(r.changed)
			r.changed = make(chan struct{})
			r.mu.Unlock()
			slog.Info("agent runtime started", "pid", cmd.Process.Pid, "generation", generation)
			go func() {
				select {
				case <-r.ctx.Done():
					_ = in.Close()
					select {
					case <-c.done:
					case <-time.After(3 * time.Second):
						_ = cmd.Process.Kill()
					}
				case <-c.done:
				}
			}()
			<-c.peer.Done()
			cancel()
			close(c.done)
			r.mu.Lock()
			r.current = nil
			close(r.changed)
			r.changed = make(chan struct{})
			r.mu.Unlock()
			_ = in.Close()
			_ = cmd.Process.Kill()
			waitErr := cmd.Wait()
			if r.ctx.Err() == nil {
				attrs := []any{"pid", cmd.Process.Pid, "generation", generation, "uptime_ms", time.Since(started).Milliseconds(), "error", waitErr}
				if cmd.ProcessState != nil {
					attrs = append(attrs, "exit_code", cmd.ProcessState.ExitCode(), "exit_reason", cmd.ProcessState.String())
				}
				slog.Warn("agent runtime disconnected; restarting", append(attrs, diagnostics.attrs()...)...)
			}
		} else {
			in.Close()
			out.Close()
			slog.Error("agent runtime could not start; retrying", "error", err)
		}
		if time.Since(started) > 10*time.Second {
			backoff = 250 * time.Millisecond
		}
		select {
		case <-r.ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(2*backoff, 5*time.Second)
	}
}
func (r *Runtime) acquire(ctx context.Context) (*connection, error) {
	for {
		r.mu.Lock()
		c, changed := r.current, r.changed
		r.mu.Unlock()
		if r.ctx.Err() != nil {
			return nil, errors.New("Agent runtime is stopped")
		}
		if c != nil && c.ctx.Err() == nil {
			return c, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-r.ctx.Done():
			return nil, errors.New("Agent runtime is stopped")
		case <-changed:
		}
	}
}
