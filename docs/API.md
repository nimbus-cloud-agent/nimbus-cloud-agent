# Session, image, plan review, and streaming API

All paths below are relative to `http://127.0.0.1:8080/api`. Nimbus is a single-user local application; requests are subject to loopback Host and same-origin checks. JSON errors use `{"error":"message"}`. API keys are configured server-side and must not be sent by the browser with messages.

## Runtime health

`GET /health` returns the control-plane status and supervised Node process:

```json
{"ok":true,"runtime":{"state":"running","pid":12345,"generation":1}}
```

The Node PID remains stable across turns, MCP calls and image uploads. E2B operations and terminal connections are owned by Go and do not require Node. `state` is `running`, `restarting` or `stopped`; a restart increments `generation` within the current Go process lifetime. `ok` indicates the Go HTTP service is alive, so inspect `runtime.state` separately. Internal tool dispatch uses the separately installed `nimbus-agent-runtime` package through SDK-based JSON-RPC 2.0 on stdio (Content-Length framing, protocol v3); the old `/internal/tools` HTTP route is removed.

## Sessions and options

| Method and path | Behavior |
| --- | --- |
| `POST /sessions` | Create an empty session; returns `201` |
| `GET /sessions` | Read a list snapshot; available to API clients, not polled by the UI |
| `GET /sessions/:id` | Read options, `mode_locked`, agent state, and sandbox state |
| `PATCH /sessions/:id` | Rename with `title`, or replace the full `options` object |
| `DELETE /sessions/:id` | Stop the turn, destroy its E2B sandbox, remove local working copies, and delete session data |
| `POST /sessions/:id/stop` | Cancel the active turn while retaining its history |

New session options are:

```json
{
  "model": "deepseek-flash",
  "thinking": "off",
  "plan": false
}
```

`thinking` accepts `off`, `low`, `high`, or `max`. Flash is the only model accepted by Nimbus. All tools run through Wasm PTC. Plan and thinking changes affect later turns; an accepted message retains its option snapshot.

## Images

Upload an image before referencing it in a message:

```http
POST /sessions/:id/images
Content-Type: application/json

{
  "name": "screenshot.png",
  "media_type": "image/png",
  "data": "<base64 bytes, without a data: URL prefix>"
}
```

The server verifies the actual media type, fully decodes and normalizes the image with DSH, and returns `201` with `id`, `name`, `url`, and native `attachment` metadata. The `url` is a same-origin `/api/sessions/:id/images/:image` path that returns the stored raster bytes. A different session cannot read or reference the image.

PNG, JPEG, WebP, and GIF are supported. Limits are 5 MiB per upload, 4 images per message, and 10 MiB of stored image data per message. The browser also checks the aggregate upload size. DSH applies its dimension/pixel admission limits and normalizes metadata, orientation, and large images; GIF uses the first frame. Invalid content, MIME mismatch, duplicate IDs, or invalid message image references return `400`.

PostgreSQL owns the normalized bytes and attachment reference. Later turns reconstruct the supervisor's private attachment cache from these bytes, so deleting a completed local cache or restarting Nimbus does not lose images. Removing an image preview detaches it from the draft; session deletion removes all uploaded copies.

## Settings and skill content

The frontend reads `/settings`, `/skills`, or `/mcp/servers` only while the corresponding Settings tab is visible. It does not prefetch the other tabs.

`GET /skills?include_content=false` returns skill metadata and file paths, without file contents. Omitting the flag retains the full-content query. `GET /skills/:id/files?path=<encoded path>` reads one exact file and returns `{path,content}`; a missing skill/file returns `404`. The editor requests only the selected file and cancels obsolete requests.

`PATCH /skills/:id` accepts optional `name`, `description`, `enabled`, and `files`. When supplied, `files` is a complete path manifest: `{path,content?}` entries retain existing bytes if `content` is omitted, replace bytes if supplied (including `""`), and require content for new paths. Paths omitted from the manifest are removed. Omitting `files` preserves all files. Updates validate and commit atomically, so toggling a skill or saving one edited file neither fetches nor overwrites unopened file contents. `POST` / `PUT` retain their full replacement semantics.

## Library

The Library is a global (not per-session) object store in PostgreSQL. Files live in `library_files`, each write creates a new row in `file_versions` (version numbers start at 1), and version bytes are split into 1 MiB `file_chunks` rows. A file is addressed by its UUID or by its flat, unique name; a write with an existing name creates a new version of that file. The link returned for a file is `nimbus://library/<uuid>` and resolves to the current version. Caps are 1 MiB chunks, 100 MB per file, 1 MiB per text read, and 5 MiB per image read.

| Method and path | Behavior |
| --- | --- |
| `GET /library` | List all files as `{files:[{id,name,mime_type,size_bytes,version,updated_at}]}`, most recently updated first |
| `GET /library/:id[?include_versions=false]` | File metadata/current version; `versions: []`, without querying history |
| `GET /library/:id?include_versions=true` | Explicit version history as `versions:[{version,size_bytes,sha256,created_at}]` |
| `GET /library/:id/content?version=N` | Stream one version's raw bytes; `version` defaults to the current version |

The browser queries the Library list only while that panel is visible, resolves bare-link names by file ID, and requests version history only when the version selector opens. Preview content is limited to the selected file/version; closing or switching aborts obsolete requests.

The content endpoint sets `Content-Type` to the stored MIME type, `Content-Disposition: inline`, and `X-Content-Type-Options: nosniff`. Stored HTML additionally gets `Content-Security-Policy: sandbox allow-scripts`: scripts can render interactive pages, but the sandbox does not grant same-origin access to the app's DOM, cookies, or storage. An invalid id, a missing file, or a missing version returns `404`. There is no delete or upload endpoint; the agent tools below are the only writers.

The agent tools are dispatched by the Go control plane:

| Tool | Arguments | Result |
| --- | --- | --- |
| `library_list` | none | `{files:[{id,name,mime_type,size_bytes,version,updated_at}]}` |
| `library_read` | `id` or `name`, optional `version` (default current), `offset`, `length` | `{id,name,version,mime_type,size_bytes,content?,truncated?,image?,link}` |
| `library_write` | `name`, `content` (UTF-8 text), optional `mime_type` | `{id,name,version,size_bytes,mime_type,link,sha256}` |
| `library_versions` | `id` or `name` | `{id,name,versions:[{version,size_bytes,sha256,created_at}]}` |
| `library_copy_to_sandbox` | `id` or `name`, optional `version`, `path` | `{path,bytes,version}` |
| `library_copy_from_sandbox` | `path`, optional `name` (default: basename of `path`), `mime_type` | `{id,name,version,size_bytes,link}` |

Names are flat: empty names, names over 200 characters, and names containing `/`, `\`, or `..` are rejected. The MIME type is inferred from the extension (`.html`, `.md`/`.markdown`, `.txt`, `.png`, `.jpg`/`.jpeg`, `.gif`, `.webp`, `.svg`, `.json`); an explicit `mime_type` overrides it, otherwise the type is `application/octet-stream`. Text reads honour `offset`/`length` up to 1 MiB per call and set `truncated` when more remains; image reads ignore the range and return the whole image as `{media_type, data}` base64, up to 5 MiB — larger images are refused with an error telling the agent to use `library_copy_to_sandbox` instead. Any other type cannot be read as text or an image and the error tells the agent to use `library_copy_to_sandbox` instead. `library_write` rejects content that is not valid UTF-8 or exceeds 100 MB. The copy tools stream in 1 MiB chunks between PostgreSQL and the session sandbox without holding the whole file in memory, and `library_copy_from_sandbox` enforces the 100 MB cap while reading.

## Messages and retries

```http
POST /sessions/:id/messages
Content-Type: application/json

{
  "client_msg_id": "<client-generated UUID>",
  "message": "Explain this screenshot.",
  "image_ids": ["<image ID returned by upload>"],
  "thinking": "high",
  "plan": false
}
```

At least text or an image is required. Text may be empty for an image-only message and is limited to 100 KB. `image_ids` is optional and preserves the supplied order. Options omitted from this request inherit the current session values; explicit `plan: false` overrides `true`. `model` and `mode` overrides are also accepted subject to model validation and the preset lock.

The response is `202` with the durable outbox row. Reusing `client_msg_id` within a session returns the original row, including its original text, images, and options. A new row is not created, even if the retry body has changed.

Outbox status progresses through `queued`, `starting`, and `delivered`; failure before delivery uses `failed`. Native `user/message` persistence determines delivery. A delivered turn separately records `completed`, `interrupted`, or `error` in `outcome`. Only undelivered failures can be retried through `POST /sessions/:id/messages/:outbox/retry`.

## Plan review

When native `exit_plan_mode` requests review, the initial events response and `questions` SSE event contain records with `id`, `session_id`, `turn_id`, `questions`, `status`, `created_at`, and optional `answer` / `resolved_at`. Each question preserves the native `id`, Markdown `detail`, options, and review intent. Status is `pending`, `answered`, `dismissed`, or `interrupted`.

Explicitly approve a pending plan:

```http
POST /sessions/:id/questions/:question/answer
Content-Type: application/json

{"answers":[{"id":"plan-review","selected":["Approve"]}]}
```

To revise, select `Keep planning` and optionally include `custom` with feedback. To defer, send only `{"dismissed":true}`. DSH approval requires exactly `Approve` and no custom feedback. Approval releases the native tool call; DSH turns Plan off at its next accepted step and continues. Revision preserves Plan and returns feedback to the model. Deferral concludes the turn without approval.

An invalid or incomplete answer returns `400`, a question belonging to another session returns `404`, and a conflicting decision or interrupted review returns `409`. Repeating the same accepted answer returns `200`. Browser disconnects do not resolve questions. Stop or backend restart interrupts pending reviews; sending a new message can request resubmission. Native history is not rewritten.

## Conversation history and execution details

`GET /sessions/:id/events` returns only the latest **10 turns**, oldest first within the page. `limit` accepts 1–50 turns. To load older turns, send `before_turn=<turn UUID>` from the preceding response; it is an exclusive session-scoped keyset cursor ordered by `(created_at, id)`.

```json
{
  "outbox": [],
  "turns": [],
  "questions": [],
  "has_more": false,
  "before_turn": null
}
```

`outbox`, `turns`, and `questions` cover only this page. Each turn summary contains `turn_id`, `final_text`, `has_details`, `tool_count`, `failed_count`, and optional `duration` in seconds. `final_text` is the final assistant reply after the last tool call; raw reasoning, tool arguments, results, and token frames are not returned. Historical missing-channel plan failures can include `plan_review_retry: {"seq": 123}`; the plan body is fetched by exact native sequence only when the user resubmits. History pagination uses turn cursors, not native `after_seq` / `types`.

`GET /sessions/:id/turns/:turn/events` explicitly loads one turn's execution trace:

```json
{
  "events": [],
  "frames": [],
  "questions": [],
  "cursor": {"seq": -1, "frame": 0}
}
```

Native events and frames preserve their original payloads. The read snapshot and its cursors are consistent; use those cursors for the turn-detail stream below. Turns are validated within their owning session. Undelivered turns have no native events.

The trace endpoints also accept explicit data-query conditions:

| Query | Selection |
| --- | --- |
| `projection=metadata` | Typed tool/message/attempt metadata, without tool bodies, reasoning, or raw frame replay |
| `projection=headers&kind=events` or `kind=frames` | Payload-free trace identities/types, newest 100 by default (max 500); exclusive `before` cursor and `limit` for older pages |
| `parent_call_id=root` or a call ID | Root tools or direct children of the given call |
| `argument_keys=description,path,command,query,name&max_field_chars=256` | Requested bounded scalar argument fields for labels; the client chooses label precedence |
| `call_id=<call ID>` | Exact native tool call/result/PTC records and matching call output/argument frames |
| `seq=<native sequence>` | One native record, no frames |
| `frame_id=<frame ID>` | One exact frame, no native records; still restricted to the selected turn |
| `attempt_id=<attempt ID>` | Matching assistant frames, no unrelated native records |
| `chunk_types=reasoning-delta` | Comma-separated frame chunk types |
| `frame_types=tool-output` | Comma-separated frame categories, while retaining matching native records; assembled tool cards omit historical argument deltas |
| `include_questions=false&include_outbox=false` | Omit unused review/outbox envelopes and their data queries; normal nested detail readers use this |

The metadata projection returns `tools`, `messages`, `pending_tools`, `attempts`, `questions`, `outbox`, and `cursor`. Tool headers include IDs, parentage, status, times, child count, and requested scalar `args` only. Messages include visible text and a `has_reasoning` flag. Unresolved attempts contain compact visible text and reasoning-presence metadata; pending tools contain only IDs/names. Full native records are never replaced with truncated or fabricated native payloads.

Header pages return `{events,frames,has_more,before,cursor}`. Opening a trajectory turn reads event headers only; frame headers have a separate disclosure, and each individual payload is fetched only on expansion by `seq` or `frame_id`. Older header pages require an explicit user action. Header SSE (`projection=headers&kind=...`) emits `event_header` / `frame_header` with composite cursors, draining forward batches without skipping unseen headers. It never replays the stored payloads.

`include_question_content=false` on history or trace queries returns review status, answers, timestamps, and question IDs/intents without hidden document content. The browser uses this projection and fetches `GET /sessions/:id/questions/:question` only when that review document is opened. This query is session-scoped. Pending/completed default expansion is frontend policy, not a backend status-dependent projection.

The browser fetches one tail page on entry and another only when the user scrolls upward near the top, preserving the reading position. Opening execution details loads metadata; opening a tool loads that call's inputs/results and its direct child headers; opening reasoning loads its native record or filtered attempt frames. Closing a parent stops/unmounts nested requests and streams. Cached details catch up on reopening, including a turn that completed while closed. Final replies remain visible without any detail request. Trajectory loads raw traces only through explicit inspection. Hidden Library/mobile chat panels do not continue conversation-detail queries, and mobile-hidden Terminal does not create a WebSocket.

## EventSource streams

`GET /sessions/stream` immediately emits `event: sessions` with a full list snapshot. It emits another snapshot only when the list or a session's state changes. Every reconnect receives the current full snapshot; no cursor is required.

`GET /sessions/:id/stream?since_turn=<turn UUID>` emits lightweight selected-session updates. `since_turn` is the oldest turn from the initial page, inclusive; newer turns remain in scope. Without it the stream covers the latest 10 turns. The browser merges upserts into its loaded history, retaining older pages.

| Event | Data |
| --- | --- |
| `state` | Current session view, including `mode_locked` and sandbox state |
| `outbox` | In-scope message queue/delivery snapshot and image references |
| `turns` | In-scope final-reply and execution summaries |
| `questions` | In-scope durable plan-review snapshot |

This stream never returns native events or frames. Frame-only notifications do not recompute turn summaries.

`GET /sessions/:id/turns/:turn/stream?after_seq=<seq>&after_frame=<frame>` accepts the same projection and filters as the HTTP trace query. Full projection emits `session`, `frame`, `questions`, and the one-turn `outbox`; metadata projection emits typed `metadata` snapshots. Both native `session` and `frame` events carry composite SSE IDs `<seq>:<frame>`; `Last-Event-ID` takes precedence over query cursors on reconnect. Records replay strictly after their respective cursors and are scoped to this turn and the explicit filters. The browser streams only currently displayed running detail resources; closing a disclosure stops its lane.

All streams send the comment `: heartbeat` every **5 seconds**, independently of content delivery. Heartbeats have no event ID and are not messages. The server uses one shared PostgreSQL LISTEN connection and fans changes out by session; there is no periodic SSE data query. Notification bursts are coalesced for 25 ms. Subscriptions register before reading snapshots so concurrent writes are not lost. Listener reconnects trigger catch-up, while each browser reconnect receives current lightweight snapshots or cursor-based trace replay. Heartbeat ticks do not query the database. Image bytes are fetched through their image URLs, never included as base64 in stream snapshots.

## Web terminal

`GET /api/sessions/:id/terminal` upgrades to WebSocket after validating that the session owns a running sandbox. Go creates and owns the E2B PTY directly; runtime RPC and SSE are not involved. The existing wire format is unchanged:

- Browser → Go: `{type:"input", data:"<base64 UTF-8 bytes>"}` or `{type:"resize", cols, rows}`.
- Go → browser: `{ready:true}`, `{data:"<base64 terminal bytes>"}`, or `{error:"message"}`.

Each connection gets a fresh root shell in `/workspace`. Resize is bounded to 2–500 columns and 2–200 rows. Node crash/restart does not close the WebSocket or PTY. Browser disconnect and graceful Go shutdown close the remote PTY; reconnect starts a new shell without replay. Input activity writes are limited to once per five seconds per connection. Terminal output is transient and is not persisted in PostgreSQL.
