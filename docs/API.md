# Session, image, plan review, and streaming API

All paths below are relative to `http://127.0.0.1:8080/api`. Nimbus is a single-user local application; requests are subject to loopback Host and same-origin checks. JSON errors use `{"error":"message"}`. API keys are configured server-side and must not be sent by the browser with messages.

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
  "plan": false,
  "mode": "standard"
}
```

`thinking` accepts `off`, `low`, `high`, or `max`. `mode` accepts `standard` or `ptc`. Flash is the only model accepted by Nimbus. Plan and thinking changes affect later turns; an accepted message retains its option snapshot.

An empty session has `mode_locked: false`. Its first accepted message atomically fixes the session preset and changes this field to `true`, including when that message is still queued or later fails. Changing the preset via PATCH, message overrides, or retry returns `409`; use a new session instead. An image upload alone does not lock the preset.

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

## EventSource streams

`GET /sessions/stream` immediately emits `event: sessions` with a full list snapshot. It emits another snapshot only when the list or a session's state changes. Every reconnect receives the current full snapshot; no cursor is required.

`GET /sessions/:id/stream?after_seq=-1` emits:

| Event | Data |
| --- | --- |
| `session` | Native persisted event; SSE `id` is the native sequence number |
| `state` | Current session view, including `mode_locked` and sandbox state |
| `outbox` | Current message queue/delivery snapshot and image references |
| `frame` | Persisted incremental assistant/tool output with its own durable ID |
| `questions` | Full durable plan-review snapshot, initially and on changes |

On reconnect, `Last-Event-ID` takes precedence over `after_seq`. Native events replay after that sequence. Frames replay separately and are deduplicated by ID in the browser. `GET /sessions/:id/events?after_seq=-1` supplies initial paginated history, outbox, frames, questions, and `has_more`; the UI then opens the session stream.

Both streams send the comment `: heartbeat` every **5 seconds**, independently of content delivery. Heartbeats have no event ID and are not messages. The server checks database changes every 250 ms; the browser keeps EventSource connections instead of issuing periodic session-list GET requests. Image bytes are fetched through their image URLs, never included as base64 in stream snapshots.
