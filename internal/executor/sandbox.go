package executor

import (
	"context"
	"errors"

	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/runtimeprotocol/generated"
)

func (r *Runtime) e2b(ctx context.Context, input generated.E2BRequest, onOutput func(model.CommandOutput) error) (generated.E2BResult, error) {
	c, err := r.acquire(ctx)
	if err != nil {
		return generated.E2BResult{}, err
	}
	id, release := r.bind(ctx, c, callbacks{command: onOutput})
	defer release()
	input.OperationID = id
	return c.client.E2b(ctx, input)
}
func sandboxInfo(result generated.E2BResult, err error) (model.SandboxInfo, error) {
	if err != nil {
		return model.SandboxInfo{}, err
	}
	if result.Info == nil || result.Info.SandboxID == "" {
		return model.SandboxInfo{}, errors.New("runtime returned no sandbox identity")
	}
	x := result.Info
	return model.SandboxInfo{SandboxID: x.SandboxID, TemplateID: x.TemplateID, State: x.State, Region: x.Region, CPUCount: x.CPUCount, MemoryMB: x.MemoryMB, StartedAt: x.StartedAt, EndAt: x.EndAt, Name: x.Name, Metadata: x.Metadata, EnvdVersion: x.EnvdVersion}, nil
}
func (r *Runtime) CreateSandbox(ctx context.Context, key, template, sid string) (model.SandboxInfo, error) {
	return sandboxInfo(r.e2b(ctx, generated.E2BRequest{Op: "create", APIKey: key, Template: &template, Args: &generated.E2BArgs{SessionID: &sid}}, nil))
}
func (r *Runtime) ConnectSandbox(ctx context.Context, key, id string) (model.SandboxInfo, error) {
	return sandboxInfo(r.e2b(ctx, generated.E2BRequest{Op: "connect", APIKey: key, SandboxID: &id}, nil))
}
func (r *Runtime) SandboxInfo(ctx context.Context, key, id string) (model.SandboxInfo, error) {
	return sandboxInfo(r.e2b(ctx, generated.E2BRequest{Op: "info", APIKey: key, SandboxID: &id}, nil))
}
func sandboxAck(result generated.E2BResult, err error) error {
	if err != nil {
		return err
	}
	if result.Ok == nil || !*result.Ok {
		return errors.New("runtime did not acknowledge sandbox operation")
	}
	return nil
}
func (r *Runtime) KillSandbox(ctx context.Context, key, id string) error {
	return sandboxAck(r.e2b(ctx, generated.E2BRequest{Op: "kill", APIKey: key, SandboxID: &id}, nil))
}
func (r *Runtime) PauseSandbox(ctx context.Context, key, id string) error {
	return sandboxAck(r.e2b(ctx, generated.E2BRequest{Op: "pause", APIKey: key, SandboxID: &id}, nil))
}
func (r *Runtime) ExecSandbox(ctx context.Context, key, id string, args model.SandboxArgs, output func(model.CommandOutput) error) (model.CommandResult, error) {
	stream := output != nil
	result, err := r.e2b(ctx, generated.E2BRequest{Op: "exec", APIKey: key, SandboxID: &id, Stream: &stream, Args: &generated.E2BArgs{Command: &args.Command, Cwd: optional(args.Cwd), TimeoutMS: optional(args.TimeoutMS)}}, output)
	if err != nil {
		return model.CommandResult{}, err
	}
	if result.Command == nil {
		return model.CommandResult{}, errors.New("runtime returned no command result")
	}
	return model.CommandResult{Stdout: result.Command.Stdout, Stderr: result.Command.Stderr, ExitCode: result.Command.ExitCode, Truncated: result.Command.Truncated}, nil
}
func (r *Runtime) ReadSandboxFile(ctx context.Context, key, id, path string) (model.FileReadResult, error) {
	result, err := r.e2b(ctx, generated.E2BRequest{Op: "read_file", APIKey: key, SandboxID: &id, Args: &generated.E2BArgs{Path: &path}}, nil)
	if err != nil {
		return model.FileReadResult{}, err
	}
	if result.ReadFile == nil {
		return model.FileReadResult{}, errors.New("runtime returned no file content")
	}
	return model.FileReadResult(*result.ReadFile), nil
}
func (r *Runtime) WriteSandboxFile(ctx context.Context, key, id, path, content string) (model.FileWriteResult, error) {
	result, err := r.e2b(ctx, generated.E2BRequest{Op: "write_file", APIKey: key, SandboxID: &id, Args: &generated.E2BArgs{Path: &path, Content: &content}}, nil)
	if err != nil {
		return model.FileWriteResult{}, err
	}
	if result.WriteFile == nil {
		return model.FileWriteResult{}, errors.New("runtime returned no file result")
	}
	return model.FileWriteResult(*result.WriteFile), nil
}
