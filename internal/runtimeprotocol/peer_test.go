package runtimeprotocol

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/runtimeprotocol/generated"
	"github.com/sourcegraph/jsonrpc2"
)

func TestInboundSchemaAndSDKErrorMapping(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server, client := net.Pipe()
	calls := 0
	peer := New(ctx, server, func(ctx context.Context, method string, raw json.RawMessage) (any, error) {
		calls++
		return generated.SuccessResult{Ok: true}, nil
	})
	defer peer.Close()
	sdk := jsonrpc2.NewConn(ctx, jsonrpc2.NewBufferedStream(client, jsonrpc2.VSCodeObjectCodec{}), nil)
	defer sdk.Close()
	for _, tc := range []struct {
		method string
		input  any
		code   int64
	}{
		{"missing", map[string]any{}, -32601},
		{"turn.event", map[string]any{}, -32602},
		{"turn.event", map[string]any{"request": map[string]any{"operationId": "test", "event": "invented", "data": nil}}, -32602},
	} {
		err := sdk.Call(ctx, tc.method, tc.input, nil)
		var fault *jsonrpc2.Error
		if !errors.As(err, &fault) || fault.Code != tc.code {
			t.Fatalf("%s: %v", tc.method, err)
		}
	}
	if calls != 0 {
		t.Fatal("invalid request reached the application")
	}
	var result generated.SuccessResult
	if err := sdk.Call(ctx, "turn.event", map[string]any{"request": generated.EventRequest{OperationID: "test", Event: "done", Data: json.RawMessage(`null`)}}, &result); err != nil || !result.Ok {
		t.Fatalf("valid request: %v", err)
	}
}

func TestDisconnectCancelsIncomingCallbacks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server, client := net.Pipe()
	started := make(chan struct{})
	stopped := make(chan struct{})
	peer := New(ctx, server, func(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return nil, ctx.Err()
	})
	defer peer.Close()
	sdk := jsonrpc2.NewConn(ctx, jsonrpc2.NewBufferedStream(client, jsonrpc2.VSCodeObjectCodec{}), nil)
	go func() {
		_ = sdk.Call(ctx, "turn.question", map[string]any{"request": generated.QuestionRequest{OperationID: "test", Data: json.RawMessage(`null`)}}, nil)
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("callback never started")
	}
	_ = sdk.Close()
	select {
	case <-stopped:
	case <-ctx.Done():
		t.Fatal("callback leaked after disconnect")
	}
}

func TestResultSchemaIsCheckedBeforeDecoding(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server, client := net.Pipe()
	peer := New(ctx, client, nil)
	defer peer.Close()
	sdk := jsonrpc2.NewConn(ctx, jsonrpc2.NewBufferedStream(server, jsonrpc2.VSCodeObjectCodec{}), jsonrpc2.HandlerWithError(func(context.Context, *jsonrpc2.Conn, *jsonrpc2.Request) (any, error) {
		// Missing version must not become the zero-value string during decoding.
		return map[string]any{"name": "nimbus-agent-runtime", "protocolVersion": 2}, nil
	}))
	defer sdk.Close()
	_, err := (generated.RuntimeClient{Peer: peer}).Check(ctx, generated.CheckRequest{ProtocolVersion: 2})
	var fault *Fault
	if !errors.As(err, &fault) || fault.Code != -32603 {
		t.Fatalf("invalid result accepted: %v", err)
	}
}
