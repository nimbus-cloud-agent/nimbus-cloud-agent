package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type Runtime struct{ Dir string }

func (r Runtime) Command(ctx context.Context, name string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "node", filepath.Join(r.Dir, name+".mjs"))
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "LANG=en_US.UTF-8", "NODE_NO_WARNINGS=1"}
	return cmd
}
func (r Runtime) Call(ctx context.Context, name string, input any) (json.RawMessage, error) {
	cmd := r.Command(ctx, name)
	data, _ := json.Marshal(input)
	cmd.Stdin = bytes.NewReader(append(data, '\n'))
	var out, errout bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errout
	if e := cmd.Run(); e != nil {
		return nil, fmt.Errorf("%s: %s", name, strings.TrimSpace(errout.String()))
	}
	var result struct {
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	}
	if e := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &result); e != nil {
		return nil, fmt.Errorf("invalid %s runtime response", name)
	}
	if result.Error != "" {
		return nil, fmt.Errorf("%s", result.Error)
	}
	return result.Result, nil
}

// Stream messages are acknowledged after their database transaction commits.
func (r Runtime) Turn(ctx context.Context, input any, onEvent func(string, json.RawMessage) error) error {
	cmd := r.Command(ctx, "turn")
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 10 * time.Second
	stdin, e := cmd.StdinPipe()
	if e != nil {
		return e
	}
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		return e
	}
	var errout bytes.Buffer
	cmd.Stderr = &errout
	if e = cmd.Start(); e != nil {
		return e
	}
	enc := json.NewEncoder(stdin)
	if e = enc.Encode(input); e != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return e
	}
	scan := bufio.NewScanner(stdout)
	scan.Buffer(make([]byte, 65536), 32<<20)
	var failure error
	finished := false
	for scan.Scan() {
		var msg struct {
			Event string          `json:"event"`
			Data  json.RawMessage `json:"data"`
			Ack   int             `json:"ack"`
		}
		if e = json.Unmarshal(scan.Bytes(), &msg); e != nil {
			failure = e
			break
		}
		if e = onEvent(msg.Event, msg.Data); e != nil {
			failure = e
			break
		}
		if msg.Event == "done" {
			finished = true
		}
		if msg.Ack > 0 {
			if e = enc.Encode(map[string]any{"ack": msg.Ack}); e != nil {
				failure = e
				break
			}
		}
	}
	stdin.Close()
	if failure != nil {
		_ = cmd.Process.Kill()
	}
	e = cmd.Wait()
	if failure != nil {
		return failure
	}
	if scan.Err() != nil {
		return scan.Err()
	}
	if e != nil {
		return fmt.Errorf("Harness process: %s", strings.TrimSpace(errout.String()))
	}
	if !finished {
		return fmt.Errorf("Harness exited without a durable turn end")
	}
	return nil
}
func Encode(w io.Writer, v any) error { return json.NewEncoder(w).Encode(v) }

func (r Runtime) CallStream(ctx context.Context, name string, input any, onOutput func(json.RawMessage) error) (json.RawMessage, error) {
	cmd := r.Command(ctx, name)
	data, _ := json.Marshal(input)
	cmd.Stdin = bytes.NewReader(append(data, '\n'))
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		return nil, e
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if e = cmd.Start(); e != nil {
		return nil, e
	}
	scan := bufio.NewScanner(stdout)
	scan.Buffer(make([]byte, 65536), 4<<20)
	var result json.RawMessage
	var failure error
	for scan.Scan() {
		var m struct {
			Output json.RawMessage `json:"output"`
			Result json.RawMessage `json:"result"`
			Error  string          `json:"error"`
		}
		if e = json.Unmarshal(scan.Bytes(), &m); e != nil {
			failure = e
			break
		}
		if m.Error != "" {
			failure = fmt.Errorf("%s", m.Error)
			break
		}
		if m.Output != nil {
			if e = onOutput(m.Output); e != nil {
				failure = e
				break
			}
		}
		if m.Result != nil {
			result = m.Result
		}
	}
	if failure != nil {
		cmd.Process.Kill()
	}
	e = cmd.Wait()
	if failure != nil {
		return nil, failure
	}
	if e != nil {
		return nil, fmt.Errorf("%s: %s", name, stderr.String())
	}
	if scan.Err() != nil {
		return nil, scan.Err()
	}
	if result == nil {
		return nil, fmt.Errorf("runtime returned no result")
	}
	return result, nil
}
