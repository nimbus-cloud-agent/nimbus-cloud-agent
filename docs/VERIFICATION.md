# Verification Record

This record covers runs performed on this macOS machine from 2026-10-31 through 2026-11-01. The tests used the real DeepSeek and E2B credentials available in the current environment; credentials were not written to code or test output.

| Area | Results |
| --- | --- |
| Standalone runtime package | `make pack-runtime` produced the 0.3.0 tarball. The artifact uses protocol v3 and includes the generated SDK/schema, with no E2B npm dependency or leftover `e2b.js` / `terminal.js`. Local service checks, a real persistent process, and image end-to-end verification passed. |
| On-demand generation | `make` succeeded after removing generated directories on both sides and TypeScript build artifacts. `make test` generated and ran 19 JS/TS tests plus Go race and PostgreSQL integration tests. Git did not track generated code, `dist`, or the tarball. |
| Build and static checks | Frontend TypeScript/Vite build, Go build, `go test ./...`, and `go vet ./...` passed. |
| Wasm isolation | Nine automated checks passed: TypeScript and SDK integration, infinite-loop interruption, blocked Node imports, async errors, dangling-Promise timeouts, a 256 MiB memory limit, the `ToolCallError` type, and confirmation that waiting for review does not consume execution time while CPU limits and cancellation remain effective. |
| Frontend type boundaries | HTTP, SSE, and WebSocket use explicit event, frame, settings, and MCP models. Tool arguments and results are normalized at the boundary; components use neither `any` nor secondary JSON parsing. Automated tests cover native PTC correlation, failed results, and error fields. |
| Types and DAO boundary | GORM/SQL are confined to the DAO, with separate persistence and business models. Isolated-schema tests cover typed JSONB, false-value updates, rejection of invalid types in the DAO, transaction rollback, preset constraints, and lossless persistence of native event/OAuth extension fields. Architecture tests prevent DB access and late decoding from returning to the business layer. |
| Persistent Node / RPC | Real multi-turn sessions kept the same PID. MCP and images reused Node; E2B and PTY moved to Go. Automated tests passed for worker reuse, Content-Length handling in both SDKs, schema validation, request cancellation, crash replacement, large results, and nested callbacks. |
| Node recovery | After sending SIGKILL to the Node child process during real output, Go started a new PID automatically. Streaming output was preserved item by item; the active turn and pending review were interrupted, and a new turn recovered. An idle Node process was also restarted after being killed. Tests created and deleted only their own sessions. |
| Concurrency and stopping | While one PTC session waited for Plan review, another session completed a chat and recovered history. Stopping the review session kept its Node PID and did not affect other requests. Worker tests verified that a stuck worker is reclaimed independently. |
| Ordinary chat | A real model response was returned and persisted incrementally; no sandbox was created. |
| Go E2B adapter | Against real E2B: timeout-to-pause on creation, file writes and reads, Unicode command output, stdout/stderr, nonzero exit codes, cancellation, PTY resize/input/output/close, status checks that do not wake a paused sandbox, and file preservation after resume. Tests did not start Node. |
| Terminal independent of runtime | While Chrome and the WebSocket stayed connected, an idle Node process was first SIGSTOP'ed and then SIGKILL'ed. Terminal input/output continued, and the restarted process retained the shell environment. Unicode text, resize, and Ctrl-C passed. |
| E2B / web terminal | A real model wrote `/workspace/nimbus-proof.txt`; the WebSocket PTY read `NIMBUS_E2B_OK`. |
| Pause and resume | History remained readable after pausing without waking the sandbox. After resume, the E2B ID and file contents were unchanged. |
| Invalid template | E2B returned an explicit failure, and the page API showed an error. No placeholder sandbox or fake E2B ID was stored. |
| PTC | Native Harness PTC display mode called `run_code`; the SDK read a real E2B file from Wasm. The check inspected `tool/ptc-dispatch`, not just model text. |
| Crash recovery | The Go process was killed during output. After restart, persisted partial output remained unchanged, the old turn was interrupted, and the session was idle. “Continue” started a new turn with the output already shown. |
| Outbox | Five concurrent requests using the same `client_msg_id` created and returned the same outbox row. Two queued turns ran serially, and event sequence numbers remained contiguous. |
| Immutable database history | Direct UPDATE/DELETE operations on events were rejected by the database; deleting the owning session still cascaded normally. |
| Skills | A skill was saved through the page. A real model read its verification string from PostgreSQL. A new session could not read it after the skill was disabled. No sandbox was needed. |
| Skill validation | Invalid paths, binary files, 101 files, and individual files larger than 5 MiB were rejected. |
| Model / Plan / reasoning | Switched to `deepseek-flash`; real max reasoning + native Plan + Wasm PTC wrote `/workspace/plan-proof.txt` through E2B. |
| Full Plan review flow | Real Flash + DSH: PTC called native `exit_plan_mode`; Chrome refresh preserved the pending review card; approval closed Plan through native `plan/mode` and continued the response. Feedback produced a revised plan. Choosing “Decide later” ended the turn while keeping Plan active. Stop interrupted a pending review without approving it. |
| Plan waits and answer boundaries | With PTC `timeoutMs=10000`, a human could approve after a 12-second wait. Database integration tests cover cross-session answers, invalid options, idempotent duplicate answers, multi-tab conflicts, stale reviews after cancellation/restart, and Plan defaults updated only by native events. |
| Session isolation | A second session received a different E2B ID and could not read the verification file in the first sandbox. |
| Stop | Stopping during incremental output preserved partial text, marked the old turn `interrupted`, and returned the session to `idle`. |
| Automatic pause | The activity time of a dedicated test session was moved back 16 minutes, and the reaper successfully paused it through E2B. The test did not wait an additional full 15 minutes. |
| MCP / OAuth | A local real-HTTP protocol test server completed discovery, dynamic registration, PKCE, callback, and refresh after a 401. A real DeepSeek call successfully invoked the echo tool without starting a sandbox. |
| Notion MCP | Using the user's existing OAuth grant, the app discovered all 68 tools. Real Flash + Wasm PTC successfully called `notion-get-self` without starting a sandbox or reading/writing Notion pages. The test session was deleted after verification. |
| Large MCP responses | Persistent JSON-RPC retained the fix that waits for stdout writes to complete. Local real-HTTP MCP automated tests covered a paginated tool catalog (~300 KB) and a ~1.6 MB tool result, with every field verified and no real credentials required. |
| Credential / request boundaries | The API returns masked values, not OAuth tokens. Cross-Origin requests were rejected. The internal HTTP tool endpoint was removed, and RPC callbacks are bound to their request/session. |
| Browser | Checked Chrome desktop and a 390 px mobile viewport. Chat, settings, skill saving, and responsive layout worked with no page JavaScript errors. |
| Tool display and folding | Based on DSH Web tool summaries and call trees, HeroUI supports folding all tools in a turn or an individual tool. Chrome verified PTC child-call deduplication/nesting, command failures and exit codes, file-content copying, Enter/Space operation, preservation of the user's fold choices through SSE updates, interrupted calls, and the 390 px layout. |
| Preset lifecycle | PostgreSQL isolated-schema integration and race checks passed: selection in an empty session, locking on the first message, only one success for concurrent first sends with different presets, no bypass through PATCH/partial-message parameters/SQL, retry preserving the locked preset, and migration of old mixed sessions while retaining native history. |
| Model validation | The official update blog, model documentation, and real `/models` endpoint confirmed `deepseek-flash` as the API name for Flash. The product offers Flash only, and the backend rejects old model names. |
| PG notifications and Go iterators | Go baseline 1.25. DAO `iter.Seq2` consumes notifications, and business `iter.Seq` consumes merged wakeups. Integration tests cover visibility only after commit, no notification after rollback, six table types and delete cascades, schema isolation, reconnect after terminating a listener connection, and catch-up reads. |
| SSE idle queries | The session list and session stream were open while the outbox dispatcher ran. Across a 5-second heartbeat, `pg_stat_activity.query_start` stayed unchanged for all data connections in an isolated test schema, confirming there were no idle polling queries. |
| SSE replay and backpressure | A single transaction wrote more than 1,000 events and frames; merged notifications alone were enough to read every page. Tests cover Last-Event-ID reconnect, 25 ms notification merging, cross-session filtering, iterator cleanup, in-memory provisioning updates, and concurrent frame commits without cursor skips. |
| Session list updates | The browser continuously subscribed to `/api/sessions/stream` without polling `GET /api/sessions`. Title changes arrived live, and reconnecting returned the latest list snapshot. |
| SSE heartbeats | After the LISTEN/NOTIFY change, measured adjacent heartbeats were 4,999 ms and 5,000 ms for the list and chat streams. Data delivery was not delayed by the 5-second heartbeat interval. |
| Images | Chrome file selection, paste, thumbnails, full-size preview, and removal passed. Real Flash recognized `NIMBUS 4729` in the test image. The native `user/message` included the image attachment; it remained visible after refresh, was restored on the next turn, and was answered correctly without creating an E2B sandbox. |
| Image boundaries | Database integration tests cover cross-session image reads and references, spoofed formats, invalid/duplicate image IDs, count limits, image-only messages, and image cleanup when deleting a session. |
| Image recovery after restart | The first turn asked the model to reply only “Received.” After stopping the backend and deleting the test session's local turn cache, the restarted service could still read the image and retained the locked preset. In the second turn, Flash correctly read the previously untranscribed `ORBIT 5938`, confirming that the image was restored from PostgreSQL. |

## Lazy reads — 2026-11-06

Chrome against the production frontend build passed the lazy-history, tool, plan, Settings, and Library fixtures: one initial tail page; upward-only historical reads and stable anchors; nested tool/reasoning/document reads only on expansion; stale-request cancellation; completion catch-up; paged trajectory headers with exact-record reads; active-tab/selected-file loading; and no CSS-hidden mobile Terminal connection. Browser fixtures do not call a model or mutate saved conversations.

A read-only copy of the reported session was loaded into a disposable local PostgreSQL database: 12 turns, 495 native records, and 80,422 frames (34,920,095 bytes of source JSON). One local measurement returned the newest 10 turns in 13,592 bytes / 42.8 ms, with no native events or frames. The original large turn's execution metadata was 6,704 bytes / 112.4 ms; a 100-frame header page was 10,951 bytes / 5.4 ms. These are local measurements, not Render latency guarantees. Runtime write batching was not changed.

The actual Go binary rendered this local session copy in Chrome: one initial page request, one upward-history request, two metadata queries (process/direct-child headers), one exact tool-body query, zero unscoped raw-trace queries, and zero page errors. The expanded source code was visibly rendered. Dedicated PostgreSQL `go test -race ./...`, `go vet ./...`, frontend/service builds, and `npm test` passed. Incremental metadata SSE tests cover visible text accumulation, suppression of hidden argument/reasoning-only updates, settled-message replacement, and unchanged no-query heartbeats.

**Verification incident:** the initial shared-Neon test attempt revealed that its proxy ignored startup `search_path`. The test run unintentionally installed additive migration 012 (indexes/query functions) and triggered ownership handoffs at 07:09:22Z and 07:09:34Z; Render reacquired ownership at 07:09:39Z and health remained on application version `b226d51`. No application deployment was performed. Subsequent database verification moved to a dedicated local PostgreSQL 18 cluster. Both test schema helpers now verify `current_schema()` before any migration/ownership acquisition and refuse a connection whose isolation did not take effect. Do not use the shared Neon DSN for test runs.

## Rerun Checks

After starting the service, run these in order so crash-recovery tests do not affect other checks:

```sh
node scripts/e2e.mjs
node scripts/features-e2e.mjs
node scripts/invariants-e2e.mjs
node scripts/runtime-e2e.ts
node scripts/terminal-e2e.ts
node scripts/recovery-e2e.mjs
node scripts/browser-check.mjs
node scripts/session-updates-e2e.mjs
node scripts/plan-e2e.mjs
node scripts/tool-ui-check.mjs
node scripts/library-lazy-check.mjs
node scripts/lazy-history-check.mjs
node scripts/plan-lazy-check.mjs
node scripts/settings-lazy-check.mjs
```

Preset, model migration, image ownership, Plan review, skill preservation, lazy history/trace queries, and PG notification/SSE idle-query and reconnect integration tests use disposable schemas in a **dedicated local test database**. Test helpers verify effective schema isolation before migration or ownership acquisition. Do not point these checks at the shared application database:

```sh
NIMBUS_TEST_DATABASE_URL='postgres://nimbus@127.0.0.1:55432/nimbus?sslmode=disable' make test
go vet ./...
```

`npm test` verifies that the runtime rejects inconsistent session presets and old model names before loading the Harness, supports large MCP tool catalogs/results beyond the pipe buffer, and preserves tool-tree correlation, replay, exit codes, and pending state. `scripts/plan-e2e.mjs` uses local Chrome, responds only to test sessions it created, and deletes those sessions on exit. `scripts/tool-ui-check.mjs` uses simulated browser events; it does not call a real model or modify existing sessions.

`scripts/library-lazy-check.mjs` uses deterministic browser-only library fixtures. It checks that closed panels and version selectors do not fetch, bare links resolve only their own metadata with per-ID single-flight requests, previews load only selected content, closing/switching cancels stale reads, and sandboxed HTML remains interactive. It does not call a model or mutate saved library files.

The Go integration tests in `internal/sandbox` selectively enable real E2B with `NIMBUS_TEST_E2B_KEY` and create/delete their own sandboxes. `scripts/terminal-e2e.ts` interrupts Node only when all existing user sessions are idle; it uses local Chrome and cleans up its own session and sandbox.

`internal/runtimeprotocol` uses the real SDK to test request/response schemas, missing fields, and cleanup of disconnected callbacks. Go race tests in `internal/executor` cover 12 concurrent large messages, a reverse callback that requests Node again, output acknowledgment order, per-request and per-tool cancellation, active/idle crash recovery, and no restart after shutdown. `scripts/runtime-e2e.ts` uses real Flash/DSH/E2B to check persistent PIDs, Plan waits across multiple sessions, Stop, Node SIGKILL recovery, persisted output, Wasm tool calls, and PTY.

For MCP checks, start `node scripts/mcp-fixture.mjs` in another terminal before running `node scripts/mcp-e2e.mjs`. These checks call real models/sandboxes and incur usage. Recovery tests actively stop and restart the local service. Run the test scripts on a dedicated development instance.

## Explicit Limits

- This is a single-user application bound to the local loopback address. It does not include public deployment, login, multi-tenancy, or multi-instance scheduling.
- The persistent service reuses Node processes and workers, but creates a new DSH context each turn and restores the working directory from the database. After a crash, accepted tool calls are not replayed automatically, and side effects already started by external services cannot be rolled back.
- The Go binary embeds only the frontend. `nimbus-agent-runtime` is installed separately as an npm package, depends explicitly on the Harness, and is written in TypeScript. Go validates the Nimbus protocol and calls the runtime; it no longer accepts a Harness path. Node.js 24.11+ is required. The npm package is not yet published to a public registry; install the built tarball instead.
- Public DSH RC.5 persistent events do not include every token. Incremental frames are stored separately in PostgreSQL, and recovery includes an explicitly identified interrupted context. Server-side SSE is driven by PostgreSQL LISTEN/NOTIFY; idle periods and heartbeats do not query the database. Listener reconnects catch up by cursor. The client does not poll; heartbeats are sent every 5 seconds. The E2B reaper still checks cloud lifecycle every 30 seconds. See `ARCHITECTURE.md`.
- A single synchronous E2B command currently runs for at most 55 seconds and returns at most 1 MiB of output. Longer tasks can start background processes in the sandbox and poll their logs. Sandbox contents are not backed up to the Mac and cannot be recovered after deletion.
- OAuth passed protocol tests and a real read-only call using the user's existing Notion grant. No third-party login was automated. Whether a third-party service requires pre-registering an OAuth client or accepts a local HTTP callback depends on its policy.
- We did not load-test 20 real concurrent sandboxes or wait a full hour to test cloud pause. Capacity locking, waiting, near-expiry pause, and E2B `onTimeout=pause` are implemented.
- E2B uses a fixed version of a community Go SDK isolated under `internal/sandbox`; no official Go SDK is assumed. The SDK does not provide lifecycle operations by ID without waking the sandbox, so typed info/pause/delete HTTP calls are added in the same adapter. Node no longer depends on the E2B npm package or its dependency chain. Current `npm audit --omit=dev` reports zero issues; the frontend install audit also reports zero issues.

Screenshots and runtime logs are stored under the Git-ignored `.local/` directory. For normal delivery, remove the test MCP configuration and temporary skills, while retaining one inspectable E2B verification chat and its paused sandbox.
