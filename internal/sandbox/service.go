// Package sandbox is the control plane's E2B anti-corruption layer. Provider
// SDK objects, credentials and wire errors never leave this package.
package sandbox

import (
	"context"
	"io"

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
	// WriteFileStream uploads path from a stream instead of an in-memory
	// string, so a file of any size up to the caller's cap can be written
	// without holding it in memory.
	WriteFileStream(ctx context.Context, sandboxID, path string, r io.Reader) (model.FileWriteResult, error)
	// ReadFileStream returns the file's bytes as a stream plus its size.
	// The caller must close the stream; the size is advisory and the
	// caller enforces its own cap while reading.
	ReadFileStream(ctx context.Context, sandboxID, path string) (io.ReadCloser, int64, error)
	// ListDir returns one directory level. It never recurses: the explorer
	// expands directories lazily, so a deep tree costs one call per open node.
	ListDir(ctx context.Context, sandboxID, path string) ([]model.FileEntry, error)
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
