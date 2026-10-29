# Architecture

This document describes the intended code layout of Nimbus Cloud Agent. It is a
map for future code, not a description of code that already exists: at scaffold
time, most packages contain only a placeholder. The rules below are the ones
future changes should follow.

## Product shape (non-negotiable)

- **Monolith, single binary, single instance.** The user downloads one file,
  runs it, and opens the port it listens on. There is no separate frontend
  server in deployment, even though the frontend and backend are built with
  different toolchains (React and Go).
- **Single user.** No login, no multi-user support, no permission checks.
  The user is the administrator.
- **One session, one VM.** A session is the context boundary: all of its
  history plus the VM it owns. A crashed run is finished (recorded as
  interrupted); a new message starts a new run in the same session.
- **Agent loop and execution environment are separated.** The DeepSeek
  Harness process and the in-VM relay both connect to the backend. They never
  connect directly to each other, and the agent can only act inside its VM.
- **One database.** PostgreSQL only. No MySQL, no Redis. Service registration
  also goes through PostgreSQL.

## Repository layout

```
nimbus-cloud-agent/
├── cmd/nimbus/          # Binary entry point only: flags, wiring, graceful shutdown.
├── internal/
│   ├── server/          # HTTP server, routing, SSE streaming to the browser.
│   ├── session/         # Session state machine, run lifecycle, history assembly.
│   ├── vm/              # QEMU/KVM lifecycle: create, start, suspend/resume, disk usage.
│   ├── executor/        # Backend side of the VM relay protocol (run_code execution).
│   ├── store/           # PostgreSQL access: sessions, events, skills, MCP config.
│   └── config/          # Startup flags, settings page values, DeepSeek key handling.
├── web/                 # React frontend (Vite). Built to web/dist, embedded by Go.
│   ├── src/pages/       # Chat page (three columns: sessions / chat / terminal).
│   ├── src/components/  # Session list, message stream, terminal, settings panels.
│   └── src/api/         # HTTP + SSE client code.
├── vmrelay/             # Program that runs INSIDE the guest VM and talks to the backend.
├── deploy/              # Release packaging, systemd unit example, install notes.
├── docs/                # Packaging, deployment, and operator docs.
├── third_party/         # Only populated by the opt-in bundle-harness step (see README).
└── .github/workflows/   # CI and the single-binary release pipeline.
```

## What each folder does

### `cmd/nimbus/`

The `main` package. It parses flags (`--addr`, `--postgres`, `--deepseek-api-key`,
`--harness-path`, `--vm-image`), opens the database, starts the HTTP server, and
shuts everything down cleanly. It contains no business logic, so the binary can
be tested and replaced without touching the rest of the code.

### `internal/server/`

The HTTP layer. Serves the embedded React build, the REST-style API used by the
settings and session-management pages, and the SSE stream that carries agent
output to the browser as it is produced. It also owns request validation and
the single-user assumption (no auth middleware is expected here).

### `internal/session/`

The session state machine and run lifecycle. A session has two independent
states: the agent-run state (idle / running / interrupted) and the VM state
(running / suspended / stopped). This package decides the transitions — for
example, a new message on a suspended session resumes the VM before any agent
run starts — and assembles a session's context from PostgreSQL when a new run
begins. It never talks to QEMU or to the model directly.

### `internal/vm/`

QEMU/KVM control, invoked directly from the host. Creates a guest from the
configured base image using copy-on-write overlays, starts / suspends / resumes
it, reports its status, and measures the overlay's actual disk usage (not the
guest's virtual disk size). It knows nothing about the agent loop.

### `internal/executor/`

The backend side of the relay protocol. When the agent loop (DeepSeek Harness,
PTC mode) needs to run code, the request goes through here, is checked, and is
forwarded to the in-VM relay (`vmrelay/`). Results come back the same way.
This is the single choke point that keeps the agent inside its VM.

### `internal/store/`

All PostgreSQL access. Tables (planned): sessions, append-only session events
(one record per message/event, never rewritten), VM records, executor
registrations, skills, and MCP server configurations. Skills are stored as text
content in the database (see limits below) and handed to the agent loop from
here; they are never copied into the VM as files.

### `internal/config/`

Startup flags and settings-page values, with one rule: secrets (the DeepSeek
API key, MCP OAuth tokens) are never logged and are only shown masked in the
UI. The only required startup parameter is the PostgreSQL connection string;
everything else can be set later in the page.

### `web/`

The React frontend. The chat page is three columns: session history on the
left, chat (history on top, send box below) in the middle, and the web terminal
on the right. On narrow screens the three columns become tabs (Session /
Chat / Terminal), one column at a time. The settings area covers the DeepSeek
API key, VM base image, model, plan mode, thinking level, skills, and MCP
servers. `web/dist/` is a build output and is not committed; the Go binary
embeds it at build time.

### `vmrelay/`

A small program that runs inside the guest VM. It registers itself with the
backend, receives `run_code` requests from `internal/executor`, executes them
locally in the guest, and returns the results. It has no model access and no
knowledge of sessions — it only knows how to run code in this VM and report
back.

### `deploy/`

Packaging and operator material: the release script, a systemd unit example,
and notes on running the binary behind a reverse proxy (optional). Nothing
here is required for the basic `./nimbus-cloud-agent --postgres ...` run.

### `docs/`

Operator-facing documentation that is not the README: `docs/packaging.md`
(how the single binary is built and what is — and is not — bundled into it),
`docs/deployment.md` (host requirements: Linux + KVM + PostgreSQL + DeepSeek
key), and later, runbooks for crash recovery and VM image management.

### `third_party/`

Reserved for the opt-in DeepSeek Harness bundle. It is empty in a normal
checkout. The bundle step must preserve the harness's original MIT licence
and record the exact version bundled. Nothing here is loaded at runtime
unless `--harness-path` points at it.

## Data flow: sending a message

1. Browser sends a message to `internal/server` (SSE stream opened).
2. `internal/session` loads the session's context from PostgreSQL and, if the
   VM is suspended, asks `internal/vm` to resume it first.
3. The backend starts a DeepSeek Harness process (PTC mode) on the same host.
4. The harness asks `internal/executor` to run code; the executor forwards to
   the in-VM relay; results return the same way.
5. Harness output is appended (as events) to PostgreSQL and streamed to the
   browser. The browser never waits for the whole run to finish.
6. If the backend crashes, the run is left marked interrupted in PostgreSQL.
   History is intact. The user's next message starts a new run in the same
   session — it does not resurrect the interrupted run.

## Limits that are already decided

- **Skills:** at most 100 files per skill, text files only (no binaries),
  at most 5 MB per file. Content lives in PostgreSQL.
- **MCP:** HTTP stream only. STDIO is not supported. OAuth-capable MCP
  servers (for example Notion) are supported through the in-page flow.
- **Models (this release):** DeepSeek only, via a user-supplied DeepSeek API key.
- **External dependencies:** PostgreSQL and QEMU/KVM only.
