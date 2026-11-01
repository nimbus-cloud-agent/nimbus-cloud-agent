package executor

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/logging"
)

type runtimeLogBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *runtimeLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}
func (b *runtimeLogBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }

func TestRuntimeCrashLogsExitStatusWithoutRawStderr(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime-fixture")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf 'TypeError: private-runtime-payload\\n' >&2\nexit 23\n"), 0700); err != nil {
		t.Fatal(err)
	}
	var out runtimeLogBuffer
	previous := slog.Default()
	slog.SetDefault(slog.New(logging.NewHandler(&out, slog.LevelDebug)))
	defer slog.SetDefault(previous)
	r := New(path)
	defer r.Close()
	deadline := time.After(5 * time.Second)
	for !strings.Contains(out.String(), "agent runtime disconnected; restarting") {
		select {
		case <-deadline:
			t.Fatal("missing runtime exit diagnostic")
		case <-time.After(time.Millisecond):
		}
	}
	r.Close()
	text := out.String()
	if !strings.Contains(text, `"exit_code":23`) || !strings.Contains(text, `"stderr_kind":"TypeError"`) || strings.Contains(text, "private-runtime-payload") {
		t.Fatal(text)
	}
}

func TestRuntimeStderrBufferRemainsBoundedAfterPreflight(t *testing.T) {
	d := &runtimeDiagnostics{}
	_, _ = d.Write([]byte("old startup text"))
	d.Reset()
	raw := strings.Repeat("private-runtime-output", 1000) + "\nError [ERR_MODULE_NOT_FOUND]: private-module-name\n"
	_, _ = d.Write([]byte(raw))
	if len(d.String()) > maxRuntimeDiagnosticBytes {
		t.Fatal("unbounded stderr")
	}
	attrs := fmt.Sprint(d.attrs())
	if !strings.Contains(attrs, "ERR_MODULE_NOT_FOUND") || strings.Contains(attrs, "private-") {
		t.Fatal(attrs)
	}
}
