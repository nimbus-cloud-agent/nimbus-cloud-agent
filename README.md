# Nimbus Cloud Agent

> One session, one cloud VM. An agent you can watch work — and a terminal you can open yourself.

Nimbus Cloud Agent is a self-hosted, open-source cloud agent system. You open a web page, start a session, and get a Linux virtual machine with an agent working inside it. The agent is powered by the DeepSeek model through the DeepSeek Harness agent loop, and it can only operate inside that session's VM — it cannot escape to the host.

**Status: early demo / scaffold.** The repository layout, build pipeline, and single-binary packaging described below are in place. The full agent loop, VM lifecycle, and streaming implementation are being built out in the open, milestone by milestone.

## What you get

- **ChatGPT-like chat page** — start a session, send a message, watch the agent reply as a stream, not only after the whole run finishes.
- **One session, one VM** — every session maps to its own Linux VM (Ubuntu-based, optimised for fast startup).
- **A web terminal** — look inside the VM yourself, alongside the agent.
- **History survives a crash** — conversation history lives in PostgreSQL. If the backend crashes mid-run, that run is recorded as interrupted; send a new message in the same session and a new run starts from the session's context. The system never silently resumes a dead run.
- **VM hibernation** — idle VMs can be suspended (snapshot) to save host resources and resumed when a new message arrives.
- **Admin configuration, in the page** — DeepSeek API key, VM base image, model selection, plan mode, and thinking level / reasoning effort.
- **Session management** — per-session VM status and copy-on-write disk usage.
- **Skills and MCP, in the page** — install skills (stored in PostgreSQL, served to the agent loop from the database, never placed inside the VM) and configure MCP servers over HTTP stream only (STDIO is not supported). MCP servers that need OAuth, such as Notion, are supported through an in-page authorisation flow.

## Who this is for

Nimbus Cloud Agent is for technical users who want to run their own agent cloud. It is a **single-user system**: there is no multi-user support, no login, and no permission model. The user is the administrator.

## Quick start (self-hosted)

You need only:

1. One Linux machine that can run QEMU/KVM.
2. A PostgreSQL database.
3. A DeepSeek API key (this release supports DeepSeek models only).

Download the release binary for your platform, then run it:

```sh
./nimbus-cloud-agent \
  --addr :8080 \
  --postgres "postgres://nimbus:YOUR_PASSWORD@localhost:5432/nimbus?sslmode=disable"
```

Then open `http://localhost:8080` in your browser. That's it — the web UI is served by the same binary; there is no separate frontend server to deploy.

### Startup flags

| Flag | Required | Description |
| --- | --- | --- |
| `--addr` | No (default `:8080`) | Address the HTTP server listens on. |
| `--postgres` | **Yes** | PostgreSQL connection string (DSN). This is the only required startup parameter. |
| `--deepseek-api-key` | No | DeepSeek API key. If omitted, set it later in the web UI's settings page. |
| `--harness-path` | No | Path to a DeepSeek Harness (`dsh`) executable, if you supply your own. See [DeepSeek Harness](#deepseek-harness) below. |
| `--vm-image` | No | VM base image. If omitted, set it later in the web UI's settings page. |

QEMU/KVM is invoked directly from the host (no extra service to configure), so as long as the host can run KVM guests, the VM side works.

## Architecture at a glance

```
Browser (React)
      │  HTTP + SSE
      ▼
Single Go binary (HTTP server)
      ├── Sessions, chat history, skills, MCP config ──► PostgreSQL
      ├── Agent loop (DeepSeek Harness, PTC mode) ─────► VM relay ──► QEMU/KVM guest
      └── Web terminal ───────────────────────────────► VM relay ──► QEMU/KVM guest
```

The agent loop and the execution environment are deliberately separated: the harness process and the in-VM relay both talk to the backend, never directly to each other. See [ARCHITECTURE.md](ARCHITECTURE.md) for the full folder-by-folder layout.

## Building from source

Prerequisites: Go, Node.js, PostgreSQL (for running), QEMU/KVM (for running VMs).

```sh
# Frontend (React) — builds into web/dist, which the Go binary embeds
make web

# Backend + embedded frontend, single binary
make build

# Run it
./bin/nimbus-cloud-agent --postgres "postgres://nimbus:YOUR_PASSWORD@localhost:5432/nimbus?sslmode=disable"
```

Releases are built by `.github/workflows/release.yml`: the React app is built first, its output is embedded into the Go binary, and a single Linux binary (plus checksum) is published per release. See [docs/packaging.md](docs/packaging.md).

## DeepSeek Harness

DeepSeek Harness is a separate open-source project (MIT licence) by DeepSeek AI. Nimbus Cloud Agent does **not** silently redistribute it:

- By default, the build does **not** bundle the harness. Supply it with `--harness-path`, or install it yourself and point Nimbus at it.
- An opt-in build step (`make bundle-harness`) may download the official `@deepseek-ai/dsh` package, keep its original `LICENSE` file intact under `third_party/deepseek-harness/`, and record its version in `third_party/deepseek-harness/NOTICE.md`. That step is only run when you explicitly ask for it, so no third-party code is redistributed by accident.
- The agent loop uses the harness in PTC mode only: a single `run_code` tool, with everything else executed through it inside the VM.

## External dependencies

Only two external components are required to run Nimbus Cloud Agent:

- **PostgreSQL** — the single database for sessions, events, skills, and MCP configuration.
- **QEMU/KVM** — the VM hypervisor on the host.

Everything else (HTTP server, agent orchestration, web UI) is inside the single binary.

## Roadmap

- [x] Repository scaffold, README, architecture, and release pipeline
- [ ] Session creation, chat UI (three-column: sessions / chat / terminal), and streaming replies
- [ ] PostgreSQL-backed append-only session history and crash-safe run records
- [ ] QEMU/KVM lifecycle: create, suspend/resume, and copy-on-write disk accounting
- [ ] DeepSeek Harness (PTC mode) integration via the backend-mediated relay
- [ ] Skills and MCP (HTTP stream + OAuth) configuration in the page
- [ ] Signed releases and install documentation

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Issues and pull requests are welcome. Because this project may be commercialised in the future, contributions are accepted under the project's MIT licence — please do not submit code you do not have the right to license that way.

## Security

See [SECURITY.md](SECURITY.md) for how to report a vulnerability. Note the single-user, no-login design: do not expose a Nimbus instance to an untrusted network.

## Licence

MIT. See [LICENSE](LICENSE).
