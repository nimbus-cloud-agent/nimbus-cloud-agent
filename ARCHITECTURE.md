# Architecture

Nimbus is a single-user, single-instance Go control plane. It serves a compiled React 19 / HeroUI v3 UI, controls the separately installed `nimbus-agent-runtime` npm package, and delegates Linux execution to E2B Cloud. The Mac never hosts the execution VM.

```text
Browser ─ HTTP / SSE / WebSocket ─ Go (Gin + GORM + Cobra + Viper)
                                    ├ PostgreSQL: authoritative history/config
                                    └ JSON-RPC 2.0 / stdio ─ nimbus-agent-runtime (TypeScript)
                                           ├ reusable turn worker threads
                                           │    ├ DeepSeek provider + DSH agent loop
                                           │    └ QuickJS Wasm → scoped RPC tools
                                           ├ E2B SDK → cloud sandboxes / PTYs
                                           ├ MCP SDK → HTTP services / OAuth
                                           └ DSH attachment normalization
```

## Packages

- `cmd/nimbus`: configuration, runtime/version preflight, database, embedded assets, graceful shutdown.
- `internal/model`: application aggregates and typed value objects, without ORM or RPC SDK dependencies.
- `internal/store`: the DAO; owns PostgreSQL/GORM, migrations, persistence records, typed queries/updates and transaction-scoped DAO handles. No database handle or query builder is exported.
- `internal/server`: session API, per-session dispatcher, SSE replay, request-scoped RPC tool dispatch, lifecycle serialization, idle collection, settings, OAuth, PTY gateway.
- `internal/executor`: subprocess supervision, typed application facade, operation scopes and error translation. It calls only the Nimbus protocol, never resolves or loads DeepSeek Harness. Harness event RPC responses follow their PostgreSQL commits; cancellation gives the Harness time to close and flush.
- `internal/runtimeprotocol`: Go anti-corruption adapter around `sourcegraph/jsonrpc2`, schema validation and cancellation. SDK envelopes and transport errors stay here.
- `protocol/runtime.openrpc.json`: authoritative method and DTO definitions. `scripts/generate-protocol.ts` uses quicktype for Go/TypeScript DTOs and thin templates for typed clients/service dispatch. Generated files are ignored; Make and npm generate them before compilation.
- `runtime`: independently packaged **`nimbus-agent-runtime`**, a TypeScript wrapper around its explicitly declared `@deepseek-ai/dsh@0.2.1-rc.5` dependency. `src/turn.ts` adapts Nimbus requests to public Cordis interfaces; native DSH owns the agent loop, persistence, Plan and provider behavior. `src/wasm.ts` implements the public PTC runtime service in QuickJS WebAssembly. E2B and MCP use their official SDKs. `src/rpc/peer.ts` is the only runtime module that knows `vscode-jsonrpc`.
- `web`: React 19.3.1, HeroUI 3.2.7, Tailwind 4.3.3, Vite 8.3.5, TypeScript 7.0.2; same-origin API and xterm.js.
- `scripts`: local setup, foreground/background lifecycle, browser and integration checks.

## Models and IO boundaries

Data acquires its type when it enters the application and stays typed until it leaves. `store.Session`, `store.Outbox`, `store.Sandbox`, image/review/Skill/MCP records are persistence models; `model.*` aggregates are application models. The application maps between them in `internal/server/models.go` (and explicit conversions at writes). Pure value objects such as options, attachment references and review answers are shared; GORM tags and table names live only in `store`.

JSONB columns use typed GORM JSON serializers. A `Store.Sandbox` query returns a record whose `Info.SandboxID` is already available. Options, images, OAuth state and review answers likewise need no later JSON parsing. Invalid persisted shapes fail the DAO query. Named filter/update structs replace business SQL, maps and `gorm.DB` calls. Business operations compose `Store.Transaction(ctx, func(tx *store.Store) error)`; contiguous event append, outbox delivery and Plan transitions remain one transaction without exposing ORM handles.

The RPC facade maps generated wire DTOs to `model` values before invoking business callbacks. E2B returns explicit sandbox metadata, command/file results, and MCP returns typed discovery/OAuth/result envelopes. Worker events retain native SDK types and discriminants through the pool; question responses and command/PTY output have named types. Only boundary codecs perform decoding; HTTP/SSE/WebSocket and RPC writers serialize outgoing values.

Foreign extensible data is deliberate: native DSH history/frames and OAuth/MCP documents preserve their full original representation alongside known typed projections. These snapshots are immutable after decoding; application changes create a new value rather than changing a field on a snapshot. MCP schemas, arbitrary tool arguments and structured content use an opaque `DynamicJSON` value with no late decode API. Nimbus forwards such provider-owned data; it does not invent a fixed schema or drop unknown fields. The TS MCP adapter validates OAuth documents using the official SDK schemas on entry. No database migration is needed for this refactor: existing JSONB data remains readable and native history is preserved.

The browser applies the same rule: HTTP/SSE/terminal entry points attach named models; `web/src/event-codec.ts` normalizes native tool arguments and text-encoded results before React state. Conversation/tool components use typed projections and never parse JSON during rendering. The original event payload remains available for the trace viewer.

`internal/architecture` checks dependency direction and rejects DB drivers or late business JSON decoding outside their boundaries. DAO integration tests cover typed projections, false/zero updates, malformed data, transactional rollback, preset constraints and lossless provider fields.

## Persistent runtime lifecycle

Go starts the installed `nimbus-agent-runtime` executable once, including for protocol preflight. The service accepts JSON-RPC 2.0 with SDK-managed `Content-Length` framing over its private stdin/stdout pipes; stdout carries protocol frames only. Requests have independent IDs, so tool callbacks, output acknowledgements, terminal input and human questions can coexist. A Plan review does not block the pipe reader. There is no extra local HTTP listener or `/internal/tools` route.

`turn.start` goes to a lazy pool of up to 20 reusable worker threads, one active turn per worker, with excess turns queued. Worker isolates and imported modules remain warm. Every turn still builds and disposes its own allowlisted Cordis context, credentials snapshot, session and work directory. Workers use per-context `launchEnvironment` instead of mutating `process.env`. CPU-bound Wasm executes off the Node service thread. MCP, E2B, image normalization and PTY handling use asynchronous handlers in the same Node process.

Go binds reverse RPC calls to an operation UUID and its session/turn scope. This application ID is separate from SDK-managed request IDs. `turn.event` carries native records, incremental frames and lifecycle changes; `turn.question` carries human questions; `tool.call` dispatches E2B/MCP operations; `runtime.output` carries command or PTY output. Only committed records/frames are acknowledged. The Node SDK can receive a nested request while waiting for Go's response to a tool callback. Both SDKs handle framing, request correlation and serialized writes, including large responses. AJV (TypeScript) and jsonschema (Go) validate payloads against generated copies of the same schema; Nimbus-owned DTOs are typed, while native Harness records and external tool results remain opaque at the protocol boundary.

`$/cancelRequest` cancels only the named operation. DSH can flush interrupted history; a worker that does not settle within six seconds is terminated and replaced, without killing other sessions. Go allows ten seconds for cancellation to settle. Worker crashes fail their owning request. A Node exit fails all in-flight requests, cancels pending question/tool callbacks and marks affected turns interrupted. Stored output and E2B files remain available. Accepted requests are **not automatically replayed**, because a tool may already have made changes. Queued messages continue when the runtime is available.

The Go supervisor restarts Node even if it crashed while idle: initial backoff 250 ms, exponential retries capped at 5 s, reset after 10 s of stable operation. `/api/health` reports `runtime.state`, `pid` and `generation`. Closing Go stops supervision and closes Node's stdin; stdin EOF also shuts down Node if Go was killed. Closing a browser terminal cancels its own RPC operation and closes its remote PTY. A hard process crash cannot guarantee that remote commands or PTYs already accepted by E2B have stopped.

The internal methods and cancellation/error behavior are documented in [docs/RUNTIME_RPC.md](docs/RUNTIME_RPC.md).

## Two independent lifecycles

`sessions` contains the agent lifecycle, title, options, immutable Harness header, and synced sequence. `sandboxes` contains only E2B state and metadata. `UNIQUE(sandboxes.session_id)` enforces zero or one sandbox per session. Absence derives `{state: "not_started"}`. Session creation, sending a message, and reading history never create a sandbox.

A per-session mutex covers E2B provisioning and each complete sandbox operation. An additional creation lock bounds concurrent provisioning. A real E2B ID is obtained before inserting the sandbox row; persistence failure triggers cleanup of the newly created sandbox. Deletion orders turn stop → E2B kill → supervisor-directory removal → database cascade. Polling reconciles provider state and pauses idle or nearly expired sandboxes; the E2B-side timeout also pauses if Nimbus is unavailable.

## Outbox and durability

Every message receives `client_msg_id`, `turn_id`, and an outbox row before execution. Unique keys make retries idempotent. The dispatcher starts one turn per session; other messages remain queued. The matching native `user/message` event's transaction commits delivery. Only undelivered failures can be retried. Tool failures remain tool results and do not change message delivery.

At turn start the supervisor uses public `sessionPersistence.create/append/flush/close` APIs to hydrate exact stored Harness records into a fresh `<work-dir>/<session>/<turn>/` directory. Retries preserve previous files in that directory and use a new attempt subdirectory. `ctx.agents.resume()` performs the native interrupted-turn repair. No private JSONL parser or generated replacement history is used.

Native `session/event` callbacks trigger synchronization through public persistence read handles, and `session/flush` awaits the synchronization chain. Go validates sequence continuity and commits records before acknowledging them. Final success requires the last Harness sequence to equal the database sequence. Database triggers reject record updates and direct deletion; deleting their owning session is allowed.

**Verified difference from the proposal:** DSH RC.5's public `session/event` feed contains a settled `assistant/message`, not one event per token. The supported `agent/assistant-stream` feed provides the incremental frames. Nimbus stores those separately in append-only `stream_frames` before streaming them to the browser. Commands use the same table for bounded output chunks. On a hard crash, the visible partial assistant text remains in PostgreSQL; the next turn includes it as explicitly labelled recovery context, while original Harness records remain unchanged. This avoids claiming that nonexistent native token events can be replayed as JSONL records.

SSE currently uses a bounded 250 ms database polling loop for replay and state changes; transactions also issue `NOTIFY`. It does not depend on LISTEN delivery for correctness. Native events use sequence IDs; frames use independent durable IDs and client deduplication. This is intentionally a single-instance demo; the database advisory lock refuses a second Nimbus process.

The browser subscribes once to `/api/sessions/stream` for list snapshots, including inactive sessions and sandbox state. The server emits the initial snapshot on every connection and subsequent snapshots only when something changes. The selected session retains its own replayable stream. Both streams send heartbeat comments every five seconds independently of the database change check; the browser does not poll `/api/sessions`.

Standard/PTC is a durable session preset. A PostgreSQL trigger freezes `locked_mode` in the same transaction that inserts the first outbox message. PATCH, send (including partial overrides), retry, and runtime resume all enforce it. Session row locks serialize first-send races; duplicate client IDs return the original message snapshot. A queued, failed, or interrupted first message still fixes the preset. Empty sessions remain editable. Migration 004 pins formerly mixed sessions to their last delivered mode, rejects incompatible pending messages, and preserves native history. Migration 005 updates future requests to `deepseek-flash` while preserving delivered model snapshots.

Image uploads are decoded and normalized by DSH's native attachment service in the persistent trusted service. PostgreSQL stores the normalized bytes plus immutable native attachment metadata, scoped to their owning session. Outbox messages retain ordered image references; SSE never carries base64 image bytes. The image route serves bytes only within the same session. Each turn worker hydrates its private attachment cache from PostgreSQL, verifies the content ID, and submits native `image` blocks through the official DeepSeek provider. No filesystem tool or execution-world path is exposed. Deleting a session cascades to its image rows and removes its supervisor work directory.

## Native plan review

`dsh-plan-mode` uses `dsh-user-questions` and a root-scoped `user-questions/request` answerer. The supervisor flushes preceding records and sends a question through a reverse `turn.question` JSON-RPC request. Go persists the request in `user_questions` (migration 007), publishes it through the selected session's SSE stream, and waits for an explicit browser response. It returns that response in the JSON-RPC result; native DSH owns approval, revision feedback, dismissal, and the subsequent `plan/mode` transition. Persisting native `active: false` also turns off the session's Plan default. Outbox option snapshots and native history remain unchanged.

Question IDs are scoped to a session and turn. Answer validation checks every question and option; row locking prevents conflicting decisions from multiple tabs, and retries of the same answer are idempotent. Refresh/reconnect recovers the pending card from PostgreSQL. Stop, worker failure and Node restart interrupt the wait; Go startup also invalidates pending questions left by a control-plane crash. Interrupted reviews must be submitted again in a new turn, and are never automatically approved or replayed. Historical missing-channel failures expose an explicit resubmit action that sends the existing plan in a new message.

Only time spent asynchronously waiting on the trusted human-question provider extends a Wasm deadline. The clock is not a generated SDK binding. CPU loops, unrelated unresolved promises, memory limits, and cancellation retain their execution limits. Plan instructions request investigation and review before implementation; this is native planning behavior, not a separate read-only sandbox policy.

## Isolation and secrets

Public routes bind loopback, validate Host, reject cross-origin requests, and use a same-origin WebSocket check. Internal tools are available only through the private child-process pipe. Go binds each callback to the owning pending request and session; Node cannot choose an arbitrary session ID in tool arguments.

The Harness composition does not mount local filesystem, subprocess, bash, jobs, sandbox-local, or Node PTC providers. Startup checks reject accidental local providers. A metadata-only working-directory adapter returns `/workspace` without touching the host. The official DeepSeek provider route in RC.5 is `deepseek-official` (the proposal's `deepseek` route does not exist in this release).

PTC source is stripped to JavaScript using Node's TypeScript transformer, then evaluated **only inside QuickJS Wasm**. Each run owns a separate WebAssembly instance with maximum 4096 memory pages (256 MiB). It has no mounted directories, Node globals, environment, arbitrary network, or subprocess capability. Host functions are explicitly generated from the Harness tool registry. Sandbox and MCP dispatch share the same supervised path. API keys and OAuth credentials are absent from program-visible bindings.

The UI returns masked keys and never returns OAuth storage. Credentials are passed to the trusted service through JSON-RPC parameters on stdin, not process arguments or the shared environment. Skill contents arrive from PostgreSQL into the in-memory provider, never temporary skill files. Model artifacts are written only to E2B.

The JSON-RPC SDK serializes stream writes; successful operations do not exit the process. This retains the earlier fix for pipe-buffer truncation of large MCP catalogs and results. MCP discovery/call responses, image bytes and E2B file/output responses share this transport. Multi-page MCP tool catalogs are assembled before return. Each turn's prompt reports MCP discovery failures and explains that connector authorization belongs to the control plane, not the execution sandbox. The settings connection test reports the discovered tool count separately from saved OAuth status.

## Runtime and distribution

The Go executable and `nimbus-agent-runtime` npm tarball are separate artifacts. Go embeds the compiled UI only; it neither embeds/extracts Node code nor accepts a Harness path. Install the runtime package with npm and put its executable on PATH, or set `--runtime-command`. Installing that package pulls in DeepSeek Harness and the SDKs through its direct dependencies. Runtime startup checks its own pinned Harness version and Wasm isolation; Go checks only Nimbus protocol v2 and runtime identity. Version 0.2.0 of the runtime uses protocol v2; upgrade both artifacts together because E2B/MCP result envelopes and review responses changed. Exact dependency versions and the workspace lockfile are committed.

`make` generates the protocol, compiles TypeScript, builds the UI and Go executable. `make test` and `make pack-runtime` also generate first. Generated Go/TypeScript/schema files, JavaScript build outputs and tarballs are not committed. A source checkout needs Node and Go for generation; users of the prebuilt artifacts need Node and PostgreSQL but no compilers. The release workflow emits Go executables and the npm tarball independently. The package has not been published to the public npm registry; use `npm install -g ./nimbus-agent-runtime-0.2.0.tgz` from the release artifact. Configuration follows flags > environment > YAML > defaults. Default local database port is 55432, separate from a conventional PostgreSQL installation.

The obsolete `internal/vm` and `vmrelay` directories are unused scaffolding; no QEMU/KVM code is invoked.
