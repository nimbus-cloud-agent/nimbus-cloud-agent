# Nimbus Cloud Agent

A local workspace for a DeepSeek agent with an isolated, on-demand E2B cloud sandbox. Runs on **macOS or Linux**; no QEMU, KVM, Docker, or Linux execution host is required.

The Go service serves the React UI at **http://127.0.0.1:8080**. PostgreSQL owns conversations, image attachments, incremental output, settings, Skills, and MCP credentials. DeepSeek Harness runs the agent loop; commands and generated files stay in E2B.

## Start an existing installation

After completing the macOS setup below, start the application from the repository directory:

```sh
npm start                 # foreground; Ctrl-C stops Nimbus
# or
npm run start:background  # logs: .local/nimbus.log
npm stop                  # stop the background service
open http://127.0.0.1:8080
```

The launcher reads `deepseekkey` and `e2bhobbykey`, or the environment variables `NIMBUS_DEEPSEEK_API_KEY` and `NIMBUS_E2B_API_KEY`. Both key files, `.env*`, database files, and logs are ignored by Git. Credentials never appear in process arguments or browser responses.

PostgreSQL listens only on `127.0.0.1:55432`; its data is in `.local/postgres/`. The local development database uses trust authentication on loopback. Do not expose this database or the single-user application to an untrusted network.

## Fresh macOS installation

Prerequisites: Homebrew, Node.js **24.11+**, Go **1.25+**, DeepSeek and E2B API keys.

```sh
npm run setup:mac
# Save your keys in deepseekkey and e2bhobbykey, or export NIMBUS_* variables.
npm start
```

`setup:mac` installs PostgreSQL 17 when needed, initializes a project-local database, installs the explicitly declared Harness and runtime dependencies, builds the frontend, and embeds it in the Go binary. It does not install a login service or any virtual machine.

## Other environments

Use an existing PostgreSQL instance, install the Node runtime dependencies and the exact Harness release, then build:

```sh
npm ci
npm ci --prefix web
npm run build
export NIMBUS_POSTGRES='postgres://user:password@127.0.0.1:5432/nimbus?sslmode=disable'
export NIMBUS_E2B_API_KEY='your-e2b-key'
export NIMBUS_DEEPSEEK_API_KEY='your-deepseek-key'
./bin/nimbus-cloud-agent serve --harness-path node_modules/.bin/dsh
```

The Go binary embeds the UI and Nimbus's trusted runtime scripts. If the `runtime/` source directory is absent, it extracts its embedded scripts there. **Node.js and the installed npm dependencies remain runtime requirements**, including the separately installed `@deepseek-ai/dsh@0.2.1-rc.5`; the Harness is not bundled into the executable. Keep `node_modules/` in the application directory (or provide a compatible runtime directory containing those dependencies).

Configuration precedence: flags > `NIMBUS_` environment variables > `config.yaml` (or `--config`) > defaults.

| Flag | Default | Purpose |
| --- | --- | --- |
| `--addr` | `127.0.0.1:8080` | Loopback-only HTTP server |
| `--postgres` | required | PostgreSQL DSN |
| `--harness-path` | `node_modules/.bin/dsh` | Exact Harness installation |
| `--e2b-api-key` | required | E2B credentials; prefer environment variables |
| `--deepseek-api-key` | unset | May also be set in the UI |
| `--e2b-template` | `base` | Template for new sandboxes |
| `--work-dir` | `.local/turns` | Per-turn Harness working copies |
| `--runtime-dir` | `runtime` | Trusted Node supervisor scripts |

Changing a key or template in the UI takes effect on subsequent turns/new sandboxes. The model is `deepseek-flash`. Plan and thinking settings persist per session and take effect on subsequent turns; Standard / Wasm PTC becomes fixed when the first message is accepted.

## Implemented behavior

- Three-column Chinese workspace; mobile Session / Chat / Terminal tabs; Markdown replies and expandable tool records.
- Durable outbox with idempotency, serial processing per session, queueing during a turn, retry of failed delivery, stop, rename, and delete.
- Incremental assistant **and command output committed before SSE publication**. Reconnect replays from PostgreSQL and deduplicates records.
- Crash recovery marks unfinished turns interrupted. A new message, including “continue”, starts a new turn. Original Harness records are hydrated through its public persistence API; unfinished displayed text is supplied as explicit recovery context.
- One lazily created E2B sandbox per session; plain chat and MCP-only work do not start it. Files live in E2B, not the Mac.
- E2B create/connect/pause/kill, 15-minute idle pause, one-hour `onTimeout: pause`, proactive pause near the deadline, and cloud state reconciliation. Default Hobby template reports its actual CPU and RAM from E2B.
- Real xterm.js PTY with input, resize, Ctrl-C, reconnect, and the same filesystem as the agent.
- `deepseek-flash` with native Harness Plan behavior and `off / low / high / max` thinking. Standard / Wasm PTC is a session preset: selectable before the first message, permanently fixed when that message is accepted, including queued or failed turns. Start a new session to change presets. Plan and thinking remain adjustable for subsequent turns.
- Native `exit_plan_mode` opens a persistent plan review in the conversation. Approve to leave Plan and continue, send revision feedback to keep planning, or defer to end the turn without approval. Browser refresh retains pending reviews; Stop or a backend restart interrupts them. Human review time does not consume the PTC execution deadline.
- Upload, paste, or drag PNG, JPEG, WebP and GIF images into the composer; preview/remove before sending and view full images in history. Up to 4 images per message, 5 MiB per image and 10 MiB total. GIF input uses the first frame. Native DSH image attachments are retained in PostgreSQL and restored on later turns; image-only messages are supported. Viewing or analyzing an image does not require an E2B sandbox.
- TypeScript PTC runs in QuickJS WebAssembly; 256 MiB maximum linear memory and 120-second default / 600-second maximum execution deadline. No host filesystem, environment, network, or process primitives are exposed to generated programs.
- Session lists and the active conversation use EventSource streams, with changes pushed as they happen and independent 5-second heartbeats. There is no browser session-list polling.
- Database-backed Skills: create/edit/delete/enable, multi-file text editor/upload, 100-file and 5 MiB limits with all-or-nothing validation.
- Streamable HTTP MCP: enable/edit/delete, connectivity test, OAuth discovery, dynamic registration, PKCE, callback state validation, server-side token storage and refresh. Actual third-party OAuth authorization still requires the account owner's browser login.

Flash supports both text and image input through the same model. The model name and vision formats were checked against the [official model documentation](https://api-docs.deepseek.com/zh-cn/quick_start/pricing/), [October 17 update](https://api-docs.deepseek.com/zh-cn/news/news261017/), and [vision guide](https://api-docs.deepseek.com/zh-cn/guides/vision/) on 2026-10-31. Nimbus intentionally offers only Flash; the upstream API also lists V4 Pro.

## Verification

```sh
go test ./...
go vet ./...
npm test
npm run build
node scripts/browser-check.mjs  # requires installed Chrome
node scripts/tool-ui-check.mjs  # tool disclosures, nested PTC, keyboard, streaming, mobile; browser fixtures only
```

The following integration checks use real API credentials and may consume DeepSeek tokens / E2B usage. **Run them sequentially against a disposable local test workspace**; recovery testing intentionally kills and restarts Nimbus.

```sh
node scripts/e2e.mjs             # real model, files, PTY, pause/resume, Wasm
node scripts/features-e2e.mjs    # Skills, Plan/max, isolation, stop, idle pause
node scripts/invariants-e2e.mjs  # idempotency, queue, append-only DB, validation
node scripts/session-updates-e2e.mjs # Flash vision, preset locking, SSE, 5s heartbeat, UI
node scripts/plan-e2e.mjs        # real Flash/DSH plan approval, feedback, defer, refresh, stop
node scripts/recovery-e2e.mjs    # hard crash, partial output, new turn
# Separate terminal:
node scripts/mcp-fixture.mjs
# Then:
node scripts/mcp-e2e.mjs         # local OAuth protocol + real model MCP call
```

The database integration tests use a disposable schema, exercise real migrations and API handlers, and do not call DeepSeek or E2B:

```sh
NIMBUS_TEST_DATABASE_URL='postgres://nimbus@127.0.0.1:55432/nimbus?sslmode=disable' go test -race ./internal/server
```

Without that variable, the PostgreSQL integration tests are skipped. See [ARCHITECTURE.md](ARCHITECTURE.md) for implementation boundaries, [docs/API.md](docs/API.md) for messages/images and stream contracts, and [docs/VERIFICATION.md](docs/VERIFICATION.md) for completed verification and operational limits.

## MCP connection checks

“已授权” in Settings → MCP means OAuth credentials are saved. Use “测试” to verify that tool discovery succeeds and see the available tool count. Enabled integrations are loaded at the start of each turn, including in existing sessions. If discovery fails, the model receives the server name and error rather than silently losing the integration. Credentials stay in the control plane; there is no need to paste a Notion token into chat or place it in the sandbox.

## Updating an installation

Stop Nimbus before updating dependencies or runtime scripts. Back up PostgreSQL, then run `npm ci`, `npm ci --prefix web`, and `npm run build`. Restart with `npm run start:background` and reload the browser to load the new UI. Startup applies the versioned SQL migrations automatically; rebuilding does not clear conversations.

Existing sessions move to Flash for future requests. Previously mixed Standard/PTC sessions keep the last delivered preset; incompatible queued messages become failed and need a new session. Empty sessions remain selectable. Historical Harness events and delivered model snapshots are preserved.

## Storage and cleanup

Back up PostgreSQL to preserve history, normalized image attachments, and configuration. Removing `.local/postgres` removes this installation's database. Sandbox files require E2B persistence and are not backed up to the Mac.

Turn files are untrusted as a source of history: each turn creates a fresh directory from PostgreSQL and never reopens previous turn directories. Completed directories can be archived or removed while their session is idle; future turns reconstruct history and attachments from the database. Deleting a session stops its turn, kills its E2B sandbox, removes its supervisor work directory, and cascades its database records, including images. Removing a draft image only detaches it from that unsent message; its uploaded copy remains scoped to the session until session deletion.

MIT — see [LICENSE](LICENSE).
