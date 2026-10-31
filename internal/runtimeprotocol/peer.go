// Package runtimeprotocol is the anti-corruption layer between Nimbus and the
// JSON-RPC SDK. Application code never handles wire envelopes or SDK errors.
package runtimeprotocol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/runtimeprotocol/generated"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/sourcegraph/jsonrpc2"
)

var ErrDisconnected = errors.New("agent runtime disconnected; saved output is preserved")

type Fault struct {
	Code    int64
	Message string
}

func (e *Fault) Error() string        { return e.Message }
func (e *Fault) Is(target error) bool { return target == ErrDisconnected && e.Code == -32002 }

type Dispatcher func(context.Context, string, json.RawMessage) (any, error)

type Peer struct {
	conn     *jsonrpc2.Conn
	ctx      context.Context
	cancel   context.CancelFunc
	dispatch Dispatcher
	mu       sync.Mutex
	incoming map[jsonrpc2.ID]context.CancelFunc
	sequence atomic.Uint64
}

var schemaOnce sync.Once
var schemas map[string]*jsonschema.Schema
var schemaErr error

func validate(name string, value any) error {
	schemaOnce.Do(func() {
		doc, err := jsonschema.UnmarshalJSON(strings.NewReader(generated.SchemaJSON))
		if err != nil {
			schemaErr = err
			return
		}
		compiler := jsonschema.NewCompiler()
		if err = compiler.AddResource("nimbus://protocol", doc); err != nil {
			schemaErr = err
			return
		}
		schemas = map[string]*jsonschema.Schema{}
		for _, m := range generated.Methods {
			for _, n := range []string{m.Request, m.Result} {
				if schemas[n] != nil {
					continue
				}
				s, e := compiler.Compile("nimbus://protocol#/definitions/" + n)
				if e != nil {
					schemaErr = e
					return
				}
				schemas[n] = s
			}
		}
	})
	if schemaErr != nil {
		return schemaErr
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	instance, err := jsonschema.UnmarshalJSON(strings.NewReader(string(raw)))
	if err != nil {
		return err
	}
	if err = schemas[name].Validate(instance); err != nil {
		return fmt.Errorf("invalid %s payload", name)
	}
	return nil
}
func New(ctx context.Context, stream io.ReadWriteCloser, dispatch Dispatcher) *Peer {
	ctx, cancel := context.WithCancel(ctx)
	p := &Peer{ctx: ctx, cancel: cancel, dispatch: dispatch, incoming: map[jsonrpc2.ID]context.CancelFunc{}}
	p.conn = jsonrpc2.NewConn(ctx, jsonrpc2.NewBufferedStream(stream, jsonrpc2.VSCodeObjectCodec{}), p, jsonrpc2.SetLogger(log.New(io.Discard, "", 0)))
	go func() { <-p.conn.DisconnectNotify(); cancel() }()
	return p
}
func (p *Peer) Done() <-chan struct{} { return p.conn.DisconnectNotify() }
func (p *Peer) Close()                { p.cancel(); _ = p.conn.Close() }
func (p *Peer) Call(ctx context.Context, method string, input, output any) error {
	meta, ok := generated.Methods[method]
	if !ok {
		return &Fault{-32601, "unknown runtime method"}
	}
	if err := validate(meta.Request, input); err != nil {
		return &Fault{-32602, err.Error()}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	callCtx, cancel := context.WithCancel(p.ctx)
	defer cancel()
	id := jsonrpc2.ID{Str: fmt.Sprintf("go:%d", p.sequence.Add(1)), IsString: true}
	request := struct {
		Request any `json:"request"`
	}{input}
	waiter, err := p.conn.DispatchCall(callCtx, method, request, jsonrpc2.PickID(id))
	if err != nil {
		return translate(err)
	}
	var result json.RawMessage
	err = waiter.Wait(ctx, &result)
	if ctx.Err() != nil {
		_ = p.conn.Notify(p.ctx, "$/cancelRequest", struct {
			ID jsonrpc2.ID `json:"id"`
		}{id})
		grace, stop := context.WithTimeout(p.ctx, 10*time.Second)
		defer stop()
		_ = waiter.Wait(grace, nil)
		return ctx.Err()
	}
	if err != nil {
		return translate(err)
	}
	// Validate the actual payload before decoding: decoding to a Go struct first
	// would turn missing fields into zero values and discard unknown fields.
	if err = validate(meta.Result, result); err != nil {
		return &Fault{-32603, err.Error()}
	}
	return json.Unmarshal(result, output)
}
func translate(err error) error {
	if errors.Is(err, jsonrpc2.ErrClosed) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) {
		return ErrDisconnected
	}
	var rpcErr *jsonrpc2.Error
	if errors.As(err, &rpcErr) {
		return &Fault{rpcErr.Code, rpcErr.Message}
	}
	return err
}

// Registration happens on the SDK reader, so a following cancellation cannot
// overtake its request. Only the actual application handler runs asynchronously.
func (p *Peer) Handle(ctx context.Context, conn *jsonrpc2.Conn, req *jsonrpc2.Request) {
	if req.Method == "$/cancelRequest" {
		var params struct {
			ID jsonrpc2.ID `json:"id"`
		}
		if req.Params != nil && json.Unmarshal(*req.Params, &params) == nil {
			p.mu.Lock()
			cancel := p.incoming[params.ID]
			p.mu.Unlock()
			if cancel != nil {
				cancel()
			}
		}
		return
	}
	if req.Notif {
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	p.mu.Lock()
	p.incoming[req.ID] = cancel
	p.mu.Unlock()
	go func() {
		defer func() { cancel(); p.mu.Lock(); delete(p.incoming, req.ID); p.mu.Unlock() }()
		result, err := p.handle(ctx, req)
		if err != nil {
			code := int64(-32000)
			if errors.Is(err, context.Canceled) {
				code = -32800
			}
			var fault *Fault
			if errors.As(err, &fault) {
				code = fault.Code
			}
			_ = conn.ReplyWithError(p.ctx, req.ID, &jsonrpc2.Error{Code: code, Message: err.Error()})
		} else {
			_ = conn.Reply(p.ctx, req.ID, result)
		}
	}()
}
func (p *Peer) handle(ctx context.Context, req *jsonrpc2.Request) (any, error) {
	meta, ok := generated.Methods[req.Method]
	if !ok || p.dispatch == nil {
		return nil, &Fault{-32601, "method not found"}
	}
	var params struct {
		Request json.RawMessage `json:"request"`
	}
	if req.Params == nil || json.Unmarshal(*req.Params, &params) != nil || len(params.Request) == 0 {
		return nil, &Fault{-32602, "named request parameters required"}
	}
	if err := validate(meta.Request, params.Request); err != nil {
		return nil, &Fault{-32602, err.Error()}
	}
	result, err := p.dispatch(ctx, req.Method, params.Request)
	if err != nil {
		return nil, err
	}
	if err = validate(meta.Result, result); err != nil {
		return nil, &Fault{-32603, err.Error()}
	}
	return result, nil
}
