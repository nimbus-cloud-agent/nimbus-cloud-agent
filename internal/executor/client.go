package executor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/runtimeprotocol/generated"
)

// Application-facing inputs intentionally do not alias generated wire types.
type TurnInput struct {
	SessionID, TurnID, MessageID, Message, SessionMode, WorkDir, APIKey, Recovery string
	Options                                                                       model.Options
	Header                                                                        *model.NativeHeader
	Records                                                                       []model.NativeRecord
	Skills                                                                        []model.Skill
	MCP                                                                           []model.MCPTool
	Images                                                                        []model.MessageImage
	Attachments                                                                   []model.Attachment
}
type MCPInput struct {
	Op, URL, CallbackURL string
	OAuthData            model.OAuthData
	Args                 model.MCPArguments
}
type ImageRequest struct{ WorkDir, Name, MediaType, Data string }
type ImageOutput struct {
	Ref  model.ImageRef
	Data []byte
}
type TerminalInput struct{ TerminalID, APIKey, SandboxID string }
type callbacks struct {
	event    func(model.TurnEvent) error
	question func(context.Context, model.QuestionRequest) (model.QuestionAnswer, error)
	tool     func(context.Context, model.ToolCall) (model.ToolResult, error)
	command  func(model.CommandOutput) error
	terminal func(model.TerminalOutput) error
}
type scope struct {
	ctx       context.Context
	callbacks callbacks
	wg        sync.WaitGroup
}

func (r *Runtime) bind(ctx context.Context, c *connection, callbacks callbacks) (string, func()) {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(c.ctx, cancel)
	id := uuid.NewString()
	s := &scope{ctx: ctx, callbacks: callbacks}
	r.mu.Lock()
	r.scopes[id] = s
	r.mu.Unlock()
	return id, func() {
		r.mu.Lock()
		delete(r.scopes, id)
		r.mu.Unlock()
		cancel()
		stop()
		s.wg.Wait()
	}
}

func (r *Runtime) Check(ctx context.Context) error {
	c, err := r.acquire(ctx)
	if err != nil {
		return err
	}
	info, err := c.client.Check(ctx, generated.CheckRequest{ProtocolVersion: 2})
	if err == nil && (info.ProtocolVersion != 2 || info.Name != "nimbus-agent-runtime") {
		return errors.New("incompatible nimbus-agent-runtime; install a protocol v2 release")
	}
	return err
}
func (r *Runtime) Turn(ctx context.Context, input TurnInput, onEvent func(model.TurnEvent) error, onQuestion func(context.Context, model.QuestionRequest) (model.QuestionAnswer, error), onTool func(context.Context, model.ToolCall) (model.ToolResult, error)) error {
	wire, err := turnRequest(input)
	if err != nil {
		return err
	}
	c, err := r.acquire(ctx)
	if err != nil {
		return err
	}
	var finished atomic.Bool
	id, release := r.bind(ctx, c, callbacks{event: func(event model.TurnEvent) error {
		err := onEvent(event)
		if event.Kind == "done" && err == nil {
			finished.Store(true)
		}
		return err
	}, question: onQuestion, tool: onTool})
	wire.OperationID = id
	_, err = c.client.Turn(ctx, wire)
	release()
	if err != nil {
		return err
	}
	if !finished.Load() {
		return errors.New("runtime returned without a durable turn end")
	}
	return nil
}
func (r *Runtime) MCP(ctx context.Context, input MCPInput) (model.MCPResult, error) {
	c, err := r.acquire(ctx)
	if err != nil {
		return model.MCPResult{}, err
	}
	oauth, err := oauthRequest(input.OAuthData)
	if err != nil {
		return model.MCPResult{}, err
	}
	arguments, err := json.Marshal(input.Args.Arguments)
	if err != nil {
		return model.MCPResult{}, err
	}
	wire, err := c.client.Mcp(ctx, generated.MCPRequest{Op: generated.MCPRequestOp(input.Op), URL: input.URL, CallbackURL: input.CallbackURL, OauthData: &oauth, Args: &generated.MCPArguments{Name: optional(input.Args.Name), Code: optional(input.Args.Code), Arguments: arguments}})
	if err != nil {
		return model.MCPResult{}, err
	}
	return mcpResult(wire)
}
func (r *Runtime) Images(ctx context.Context, input ImageRequest) (ImageOutput, error) {
	c, err := r.acquire(ctx)
	if err != nil {
		return ImageOutput{}, err
	}
	result, err := c.client.Images(ctx, generated.ImageRequest{WorkDir: input.WorkDir, Name: input.Name, MediaType: input.MediaType, Data: input.Data})
	if err != nil {
		return ImageOutput{}, err
	}
	var ref model.ImageRef
	if err = json.Unmarshal(result.Ref, &ref); err != nil {
		return ImageOutput{}, fmt.Errorf("invalid runtime image reference: %w", err)
	}
	data, err := base64.StdEncoding.DecodeString(result.Data)
	return ImageOutput{Ref: ref, Data: data}, err
}
func (r *Runtime) Terminal(ctx context.Context, input TerminalInput, onOutput func(model.TerminalOutput) error) error {
	c, err := r.acquire(ctx)
	if err != nil {
		return err
	}
	id, release := r.bind(ctx, c, callbacks{terminal: onOutput})
	defer release()
	_, err = c.client.Terminal(ctx, generated.TerminalRequest{OperationID: id, TerminalID: input.TerminalID, APIKey: input.APIKey, SandboxID: input.SandboxID})
	return err
}
func (r *Runtime) TerminalWrite(ctx context.Context, id string, message model.TerminalMessage) error {
	c, err := r.acquire(ctx)
	if err != nil {
		return err
	}
	_, err = c.client.TerminalWrite(ctx, generated.TerminalWriteRequest{TerminalID: id, Message: generated.TerminalMessage{Type: generated.Type(message.Type), Data: optional(message.Data), Rows: &message.Rows, Cols: &message.Cols}})
	return err
}

type controlReceiver struct{ r *Runtime }

func withScope[T any](c controlReceiver, ctx context.Context, id string, run func(context.Context, callbacks) (T, error)) (T, error) {
	var zero T
	c.r.mu.Lock()
	s := c.r.scopes[id]
	if s != nil {
		s.wg.Add(1)
	}
	c.r.mu.Unlock()
	if s == nil {
		return zero, errors.New("runtime operation is no longer active")
	}
	defer s.wg.Done()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	defer cancel()
	defer stop()
	if s.ctx.Err() != nil {
		cancel()
	}
	return run(ctx, s.callbacks)
}
func (c controlReceiver) Event(ctx context.Context, input generated.EventRequest) (generated.SuccessResult, error) {
	event := model.TurnEvent{Kind: string(input.Event)}
	var err error
	switch input.Event {
	case "header":
		event.Header = &model.NativeHeader{}
		err = json.Unmarshal(input.Data, event.Header)
	case "record":
		event.Record = &model.NativeRecord{}
		err = json.Unmarshal(input.Data, event.Record)
	case "frame":
		event.Frame = &model.FramePayload{}
		err = json.Unmarshal(input.Data, event.Frame)
	case "done":
		event.Done = &model.TurnDone{}
		err = json.Unmarshal(input.Data, event.Done)
	case "ready", "flushing":
	default:
		err = errors.New("unsupported runtime event")
	}
	if err != nil {
		return generated.SuccessResult{}, err
	}
	return withScope(c, ctx, input.OperationID, func(_ context.Context, cb callbacks) (generated.SuccessResult, error) {
		if cb.event == nil {
			return generated.SuccessResult{}, errors.New("unexpected turn event")
		}
		err := cb.event(event)
		return generated.SuccessResult{Ok: err == nil}, err
	})
}
func (c controlReceiver) Question(ctx context.Context, input generated.QuestionRequest) (generated.QuestionResult, error) {
	var request model.QuestionRequest
	if len(input.Data) > 512<<10 {
		return generated.QuestionResult{}, errors.New("question batch exceeds 512 KiB")
	}
	if err := json.Unmarshal(input.Data, &request); err != nil {
		return generated.QuestionResult{}, err
	}
	return withScope(c, ctx, input.OperationID, func(ctx context.Context, cb callbacks) (generated.QuestionResult, error) {
		if cb.question == nil {
			return generated.QuestionResult{}, errors.New("unexpected question")
		}
		answer, err := cb.question(ctx, request)
		if err != nil {
			return generated.QuestionResult{}, err
		}
		wire := generated.ReviewAnswer{Dismissed: &answer.Dismissed, Answers: []generated.ReviewAnswerItem{}}
		for _, item := range answer.Answers {
			wire.Answers = append(wire.Answers, generated.ReviewAnswerItem{ID: item.ID, Selected: item.Selected, Custom: item.Custom})
		}
		return generated.QuestionResult{Answer: wire}, nil
	})
}
func (c controlReceiver) Tool(ctx context.Context, input generated.ToolRequest) (generated.ExternalResult, error) {
	call := model.ToolCall{Name: input.Call.Name, CallID: input.Call.CallID}
	if call.CallID == "" {
		return generated.ExternalResult{}, errors.New("missing tool call ID")
	}
	var err error
	if strings.HasPrefix(call.Name, "mcp__") {
		err = json.Unmarshal(input.Call.Args, &call.MCP)
	} else {
		err = json.Unmarshal(input.Call.Args, &call.Sandbox)
	}
	if err != nil {
		return generated.ExternalResult{}, err
	}
	return withScope(c, ctx, input.OperationID, func(ctx context.Context, cb callbacks) (generated.ExternalResult, error) {
		if cb.tool == nil {
			return generated.ExternalResult{}, errors.New("unexpected tool call")
		}
		result, err := cb.tool(ctx, call)
		if err != nil {
			return generated.ExternalResult{}, err
		}
		raw, err := encodeToolResult(result)
		return generated.ExternalResult{Value: raw}, err
	})
}
func (c controlReceiver) Output(ctx context.Context, input generated.OutputRequest) (generated.SuccessResult, error) {
	return withScope(c, ctx, input.OperationID, func(_ context.Context, cb callbacks) (generated.SuccessResult, error) {
		var err error
		switch {
		case cb.command != nil:
			var chunk model.CommandOutput
			err = json.Unmarshal(input.Data, &chunk)
			if err == nil {
				err = cb.command(chunk)
			}
		case cb.terminal != nil:
			var chunk model.TerminalOutput
			err = json.Unmarshal(input.Data, &chunk)
			if err == nil {
				err = cb.terminal(chunk)
			}
		default:
			err = errors.New("unexpected runtime output")
		}
		return generated.SuccessResult{Ok: err == nil}, err
	})
}
