# Nimbus agent runtime protocol

`nimbus-agent-runtime` is an independently installed TypeScript **wrapper around DeepSeek Harness**, with an explicit `@deepseek-ai/dsh` dependency. Go starts its executable, supervises the process and calls the Nimbus contract. Go never locates Harness, extracts JavaScript, or supplies a Harness path. The package owns dependency compatibility and adapts Nimbus requests to native Harness APIs.

## Contract, SDKs and generation

The source of truth is [`protocol/runtime.openrpc.json`](../protocol/runtime.openrpc.json), an OpenRPC 1.3.2 document with JSON Schema DTO definitions. [`scripts/generate-protocol.ts`](../scripts/generate-protocol.ts) runs **quicktype** for Go and TypeScript types, then emits thin, typed client/service bindings. These templates describe method signatures and dispatch only; they do not implement JSON-RPC envelopes, parsing, IDs or framing. This keeps one contract across languages without maintaining two independent DTO sets.

| Layer | Go | TypeScript |
| --- | --- | --- |
| Application boundary | `internal/executor/client.go`: typed facade, scope ownership, output decoding and domain errors | `src/service.ts` / `src/ports.ts`: typed runtime handlers and host callbacks |
| Anti-corruption adapter | `internal/runtimeprotocol/peer.go` | `src/rpc/peer.ts` |
| JSON-RPC SDK | `github.com/sourcegraph/jsonrpc2` with `VSCodeObjectCodec` | `vscode-jsonrpc` with `StreamMessageReader` / `StreamMessageWriter` |
| Validation | `github.com/santhosh-tekuri/jsonschema/v6` | AJV |
| Generated bindings | `internal/runtimeprotocol/generated/` | `runtime/src/generated/` |

Generated directories, compiled JavaScript, `test-dist/`, and tarballs are ignored by Git. **Do not commit or edit them.** `make`, `make test` and `make pack-runtime` automatically generate before compiling. npm build/test/prepack entries do the same. `make generate` prepares a source checkout for direct Go commands; `npm run protocol:check` checks local outputs against the current contract. Release CI generates on every build.

Both sides validate requests and results against generated copies of the same schemas. Some native Harness headers/records/frames, attachment references and provider schemas use opaque wire extension fields. The Go facade decodes their known projection immediately into application models; they never flow through business code as `any` or raw JSON. E2B results, MCP discovery/OAuth envelopes and review answers are named DTOs. Native history and foreign MCP content preserve unknown fields. Worker messages retain their native SDK types and discriminants until the stdio boundary. Adding or changing a method starts in OpenRPC, followed by generation and compiler-guided updates to handlers.

## Transport and lifetime

The private stdio pipe uses **JSON-RPC 2.0 with Content-Length framing**, not newline-delimited JSON. Both SDKs own wire parsing, request correlation and serialized writes. stdout is reserved for protocol data. Keys and OAuth data are scoped request inputs, not arguments or inherited environment variables.

Method parameters are named and wrapped as `params: { "request": <DTO> }`. Generated clients/services hide this wrapper. SDK errors are translated at the adapters; application code does not construct JSON-RPC messages. No batch calls are part of the Nimbus contract. IDs are SDK correlation details (Go selects `go:<sequence>`, Node uses its SDK defaults), independent in each direction.

Runtime package 0.2.0 uses protocol v2, incompatible with the older generic E2B/MCP envelopes. Upgrade the Go binary and npm package together. `runtime.check` sends `{protocolVersion: 2}` and returns `{name: "nimbus-agent-runtime", version, protocolVersion: 2}`. Go checks runtime identity and protocol compatibility. Inside the package, preflight checks the pinned Harness release and Wasm isolation. Future incompatible contract changes require a protocol version change.

## Go → runtime

| Method | Input and result |
| --- | --- |
| `runtime.check` | Protocol version → runtime identity/version; package-owned dependency and isolation checks |
| `runtime.status` | Empty request → PID, worker count, busy workers, queued turns and active operation count |
| `turn.start` | Operation/session/turn IDs, locked preset, options, native history, message/images, attachment bytes, skills, MCP definitions, private working directory and API key → `{finished: true}` after durable completion and context disposal |
| `runtime.e2b` | Operation ID, SDK operation, key, sandbox/template and arguments → an `E2BResult` containing `info`, `command`, `readFile`, `writeFile` or `ok`; Go exposes separate typed facade methods and rejects a missing operation result. Streaming commands also issue output callbacks |
| `runtime.mcp` | Operation, URL, OAuth state, callback URL and arguments → `MCPResult` with typed `ok`, `oauth_state`, `oauth_data`, tool descriptors, optional result/error/auth URL. OAuth storage is never exposed in the browser response |
| `runtime.images` | Image bytes as base64, type/name and private cache directory → normalized native reference and base64 bytes |
| `terminal.open` | Operation ID, terminal ID, sandbox ID, key and optional dimensions → pending for the PTY lifetime |
| `terminal.write` | Terminal ID plus input/resize message → `{ok: true}` |

## Runtime → Go

Each callback carries an **`operationId` UUID** allocated by Go and bound to its request context and session/turn callbacks. It is not a JSON-RPC request ID. Unknown/expired scopes are rejected; the runtime cannot choose a session by inserting one into tool arguments.

| Method | Input and acknowledgement |
| --- | --- |
| `turn.event` | Operation ID, event kind and native data → `{ok: true}` only after persistence. Kinds: `header`, `ready`, `record`, `frame`, `flushing`, `done` |
| `turn.question` | Operation ID and native question → `{answer: <validated human response>}` after an explicit browser answer |
| `tool.call` | Operation ID and `{name,args,call_id}` → `{value: <tool result>}` after authorization/scoped E2B/MCP dispatch |
| `runtime.output` | Operation ID and command/PTY data → `{ok: true}` after the database commit or terminal WebSocket write |

A waiting Plan question leaves both pipe readers free to process other requests. Go registers callbacks synchronously, then executes them concurrently; the worker preserves its event chain and awaits each acknowledgement. A callback can itself call the runtime without deadlocking. A final successful turn requires a durable `done` event matching PostgreSQL's native sequence. Cleanup closes the scope and waits for active callbacks before returning to the dispatcher.

## Cancellation, failure and recovery

Cancellation in either direction uses the SDK convention **`$/cancelRequest`**, with the ID of the specific JSON-RPC request being cancelled. TypeScript translates its cancellation tokens to AbortSignal; Go translates the notification to context cancellation. Cancelling a tool's reverse call does not cancel the owning turn or other sessions.

- Invalid parameters: `-32602`; unknown method: `-32601`; invalid result: protocol error; operation failure: `-32000`; worker lost: `-32002`; cancellation: `-32800`. Native model errors/interruption remain in Harness history when it can flush.
- A turn worker has six seconds to settle cancellation. A stuck isolate is terminated and replaced; other workers and the service remain alive. Go allows ten seconds for the cancellation response before completing callback cleanup.
- Node EOF/exit fails pending requests and cancels their callbacks. Saved frames/events remain. Affected turns and reviews become interrupted. **Accepted operations are never automatically replayed**, since an external effect might already have occurred.
- The Go supervisor restarts after active or idle crashes. Backoff starts at 250 ms, doubles to a five-second maximum, and resets after ten seconds of stable execution. New turns hydrate from PostgreSQL.
- `/api/health` exposes the supervisor's state, PID and generation without a Node round trip. Generation is scoped to the current Go lifetime.
- Normal Go shutdown closes stdin and stops supervision; Node aborts operations and exits on stdin EOF, including after a hard Go crash. Hard failure cannot guarantee cancellation of commands already accepted by E2B.

One process owns a lazy pool of up to 20 reusable worker threads. V8/module caches remain warm. Every turn still creates and disposes its own Cordis context, credential snapshot, hydrated native history and attachment cache. Durable session identity lives in PostgreSQL, not in a worker. QuickJS runs off the service thread; E2B/MCP/image/PTY adapters share the persistent asynchronous service.

## Distribution and checks

`make pack-runtime` emits `release/nimbus-agent-runtime-0.2.0.tgz`. Install that package independently with `npm install -g <tarball>`; its dependency installation supplies Harness. The package is not yet published to the public npm registry. The Go release contains only the compiled UI and Go program, and defaults to the `nimbus-agent-runtime` executable on PATH. `--runtime-command` supports alternate installation locations.

`make test` generates and compiles the TS fixtures before Go tests. Cross-language tests cover real SDK framing, 12 concurrent multi-megabyte UTF-8 responses, nested calls, output acknowledgements, cancellation, malformed inputs and active/idle crash recovery. TypeScript tests cover real package preflight, worker reuse/replacement, MCP pagination/large responses and Wasm limits. `scripts/runtime-e2e.ts` checks real Flash/DSH/E2B, Plan concurrency, Stop, SIGKILL recovery and terminal I/O; it cleans only its own test sessions.
