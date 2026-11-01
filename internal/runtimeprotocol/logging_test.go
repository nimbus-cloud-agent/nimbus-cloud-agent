package runtimeprotocol

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/logging"
)

type protocolLogBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *protocolLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}
func (b *protocolLogBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }

func TestMalformedRPCProducesSafeDiagnostic(t *testing.T) {
	var out protocolLogBuffer
	previous := slog.Default()
	slog.SetDefault(slog.New(logging.NewHandler(&out, slog.LevelDebug)))
	defer slog.SetDefault(previous)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = logging.WithAttrs(ctx, "request_id", "rpc-request")
	server, client := net.Pipe()
	peer := New(ctx, server, nil)
	defer peer.Close()
	defer client.Close()
	payload := []byte(`{"jsonrpc":"2.0","method":private-rpc-content}`)
	if _, err := fmt.Fprintf(client, "Content-Length: %d\r\n\r\n%s", len(payload), payload); err != nil {
		t.Fatal(err)
	}
	for !strings.Contains(out.String(), "runtime RPC protocol error") {
		select {
		case <-ctx.Done():
			t.Fatal("missing RPC failure diagnostic")
		case <-time.After(time.Millisecond):
		}
	}
	text := out.String()
	if strings.Contains(text, "private-rpc-content") || !strings.Contains(text, "rpc-request") || !strings.Contains(text, "invalid JSON-RPC JSON") {
		t.Fatal(text)
	}
}
