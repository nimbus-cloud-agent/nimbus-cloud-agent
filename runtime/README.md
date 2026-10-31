# nimbus-agent-runtime

A TypeScript wrapper around **DeepSeek Harness**, exposed to the Nimbus Go control plane through JSON-RPC 2.0 over stdio. `@deepseek-ai/dsh@0.2.1-rc.5` is an explicit, pinned dependency. Public Harness modules used by this adapter are also declared explicitly at matching versions, without relying on transitive dependency hoisting. Native Harness owns the agent loop, provider integration, session persistence and Plan workflow; this package adapts those interfaces to Nimbus and supplies isolated E2B/MCP tools and a QuickJS Wasm PTC runtime.

## Install and run

Requires Node.js 24.11+. The package is distributed independently from the Nimbus Go executable. It has not yet been published to the public npm registry; install the built tarball:

```sh
npm install -g ./nimbus-agent-runtime-0.2.0.tgz
nimbus-agent-runtime --version
```

Nimbus starts `nimbus-agent-runtime` from PATH, communicates over its private stdin/stdout pipes, and restarts it if it exits. Set Nimbus's `--runtime-command` / `NIMBUS_RUNTIME_COMMAND` for a custom executable location. No separately configured Harness installation is needed. The package resolves its own dependencies, independent of the caller's working directory.

The CLI itself is a protocol server, not an interactive terminal. stdout is reserved for SDK-framed JSON-RPC messages. API keys and OAuth data arrive in scoped requests; they are not CLI arguments. It exposes no HTTP listener. On stdin EOF it aborts operations, closes workers, and exits.

## Architecture

- `src/service.ts`: process lifetime and typed service implementation.
- `src/rpc/peer.ts`: anti-corruption adapter around `vscode-jsonrpc`, including Content-Length framing, schema validation, cancellation and error mapping.
- `src/turn.ts`: adapts Nimbus inputs to public Harness/Cordis APIs and translates callbacks. Model code is never evaluated in the Node host.
- `src/pool.ts` / `src/worker.ts`: reusable worker isolates; fresh Harness context and credentials for each turn, independent cancellation and six-second forced worker replacement.
- `src/wasm.ts`: QuickJS WebAssembly PTC provider, explicit tool bindings and execution limits.
- `src/e2b.ts`, `src/mcp.ts`, `src/images.ts`, `src/terminal.ts`: SDK adapters for external execution, connectors, native attachments and PTYs.

Version 0.2.0 requires the matching Go control plane: protocol v2 replaces generic E2B/MCP result envelopes and types review responses. Worker events retain their SDK types throughout dispatch. Protocol v2 uses `runtime.check` for compatibility and internal dependency checks. Calls are multiplexed; questions and tool callbacks do not block other sessions. Accepted requests are never automatically replayed after failure. The Go parent owns durable history, authorization and process supervision.

## Develop in the Nimbus repository

```sh
npm ci
make                # protocol generation, runtime, frontend and Go executable
make test
make pack-runtime   # release/nimbus-agent-runtime-0.2.0.tgz
```

`protocol/runtime.openrpc.json` is the shared contract. The generator uses quicktype for Go/TypeScript DTOs and produces typed service/client bindings. Generated code, copied schemas, JavaScript build output and tarballs are ignored by Git. Make and npm build/test/pack entries generate before compilation. The published tarball contains compiled JavaScript and schemas, so runtime installation requires neither Go nor a compiler.

See the repository's `ARCHITECTURE.md` and `docs/RUNTIME_RPC.md` for the full contract and lifecycle guarantees.
