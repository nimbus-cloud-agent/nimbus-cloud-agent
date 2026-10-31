# Architecture

Nimbus is a single-user, single-instance Go control plane. It serves a compiled React 19 / HeroUI v3 UI, controls a separately installed DeepSeek Harness 0.2.1-rc.5 using its public Cordis interfaces, and delegates Linux execution to E2B Cloud. The Mac never hosts the execution VM.

```text
Browser ─ HTTP / SSE / WebSocket ─ Go (Gin + GORM + Cobra + Viper)
                                    ├ PostgreSQL: authoritative history/config
                                    ├ trusted Harness supervisor (Node)
                                    │    ├ DeepSeek official provider + agent loop
                                    │    └ QuickJS Wasm → registered SDK functions
                                    ├ E2B SDK → per-session cloud sandbox / PTY
                                    └ MCP SDK → HTTP services / OAuth
```

## Packages

- `cmd/nimbus`: configuration, runtime/version preflight, database, embedded assets, graceful shutdown.
- `internal/store`: explicit versioned PostgreSQL migrations, models, contiguous append transactions, outbox delivery commit, `pg_notify`.
- `internal/server`: session API, per-session dispatcher, SSE replay, internal capability token endpoint, lifecycle serialization, idle collection, settings, OAuth, PTY gateway.
- `internal/executor`: trusted child process JSON protocol. Harness events require an acknowledgement after their PostgreSQL transaction commits; cancellation gives the Harness time to close and flush.
- `runtime`: embedded trusted JavaScript supervisors. E2B and MCP use their official SDKs. `turn.mjs` assembles an allowlisted Harness plugin composition. `wasm.mjs` implements the public PTC runtime service using QuickJS compiled to WebAssembly.
- `web`: React 19.3.1, HeroUI 3.2.7, Tailwind 4.3.3, Vite 8.3.5, TypeScript 7.0.2; same-origin API and xterm.js.
- `scripts`: local setup, foreground/background lifecycle, browser and integration checks.

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

Image uploads are decoded and normalized by DSH's native attachment service in a temporary trusted supervisor. PostgreSQL stores the normalized bytes plus immutable native attachment metadata, scoped to their owning session. Outbox messages retain ordered image references; SSE never carries base64 image bytes. The image route serves bytes only within the same session. Each resumed supervisor hydrates its private attachment cache from PostgreSQL, verifies the content ID, and submits native `image` blocks through the official DeepSeek provider. No filesystem tool or execution-world path is exposed. Deleting a session cascades to its image rows and removes its supervisor work directory.

## Isolation and secrets

Public and control routes bind loopback, validate Host, reject cross-origin requests, and use a same-origin WebSocket check. A per-turn random capability binds internal calls to one session. It is never sent to E2B or browser clients.

The Harness composition does not mount local filesystem, subprocess, bash, jobs, sandbox-local, or Node PTC providers. Startup checks reject accidental local providers. A metadata-only working-directory adapter returns `/workspace` without touching the host. The official DeepSeek provider route in RC.5 is `deepseek-official` (the proposal's `deepseek` route does not exist in this release).

PTC source is stripped to JavaScript using Node's TypeScript transformer, then evaluated **only inside QuickJS Wasm**. Each run owns a separate WebAssembly instance with maximum 4096 memory pages (256 MiB). It has no mounted directories, Node globals, environment, arbitrary network, or subprocess capability. Host functions are explicitly generated from the Harness tool registry. Sandbox and MCP dispatch share the same supervised path. API keys and OAuth credentials are absent from program-visible bindings.

The UI returns masked keys and never returns OAuth storage. Credentials are passed to trusted child processes over stdin, not arguments. Skill contents arrive from PostgreSQL into the in-memory provider, never temporary skill files. Model artifacts are written only to E2B.

## Runtime and distribution

The Go binary embeds both the UI and Nimbus supervisors. Node and separately installed npm dependencies are still needed to execute the Harness and SDKs; this is not a fully static, Node-free distribution. Exact top-level versions and lockfiles are committed. Configuration follows flags > environment > YAML > defaults. Default local database port is 55432, separate from a conventional PostgreSQL installation.

The obsolete `internal/vm` and `vmrelay` directories are unused scaffolding; no QEMU/KVM code is invoked.
