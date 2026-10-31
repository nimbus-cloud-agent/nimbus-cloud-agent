// Package sandbox is the control plane's E2B anti-corruption layer. Provider
// SDK objects, credentials and wire errors never leave this package.
package sandbox

import (
	"context"

	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
)

type Service interface {
	Create(ctx context.Context, template, sessionID string) (model.SandboxInfo, error)
	Connect(ctx context.Context, sandboxID string) (model.SandboxInfo, error)
	Info(ctx context.Context, sandboxID string) (model.SandboxInfo, error)
	Pause(ctx context.Context, sandboxID string) error
	Kill(ctx context.Context, sandboxID string) error
	Exec(ctx context.Context, sandboxID string, args model.SandboxArgs, output func(model.CommandOutput) error) (model.CommandResult, error)
	ReadFile(ctx context.Context, sandboxID, path string) (model.FileReadResult, error)
	WriteFile(ctx context.Context, sandboxID, path, content string) (model.FileWriteResult, error)
	OpenTerminal(ctx context.Context, sandboxID string) (Terminal, error)
}

// Terminal owns one remote PTY. Read drives output synchronously, so a slow
// consumer applies backpressure rather than growing an unbounded output queue.
type Terminal interface {
	Read(output func([]byte) error) error
	Write(ctx context.Context, data []byte) error
	Resize(ctx context.Context, cols, rows int64) error
	Close() error
}
