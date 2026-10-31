// Package runtimeassets embeds trusted supervisor scripts, never the Harness distribution.
package runtimeassets

import "embed"

//go:embed check.mjs turn.mjs e2b.mjs terminal.mjs mcp.mjs wasm.mjs images.mjs
var Files embed.FS
