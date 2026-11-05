---
name: nimbus-dev-ops
description: Operate Nimbus's dev environment with Render, Neon, and GitHub CLI. Inspect configuration, logs, instances, and sleep state, and perform authorized deployments, restarts, and start/stop operations. Use for Nimbus dev troubleshooting and maintenance.
---

# Nimbus Dev Operations

Run commands from the project root. Use the official `render` and `neon` CLIs, and use `gh` for GitHub Actions. For environment variable access and Web service suspend/resume operations not covered by the Render CLI, use this skill's supplemental terminal script. Do not rely on browser automation for platform operations.

## Environment IDs

Resource IDs were verified on 2026-11-02. Recheck runtime status, images, plans, and deployment toggles before each operation.

| Resource | Identifier |
| --- | --- |
| GitHub repository / branch | `nimbus-cloud-agent/nimbus-cloud-agent` / `dev` |
| Render workspace | `tea-dbjjrd142hec73d81l60` |
| Render Web | `nimbus-dev` / `srv-dbjl0pe7bikc73fplma0`, Singapore, Free baseline |
| Application | `https://nimbus-dev-8zy2.onrender.com` |
| Neon organization / project | `org-dry-feather-16104029` / `silent-field-76474041`, project name `nimbus` |
| Neon branch | **`production`** / `br-morning-firefly-aolcg06f`, used by the **dev** app; this is not the Git branch name |
| Neon compute | `ep-royal-sound-aobqyzhu`, `aws-ap-southeast-1`, Free baseline / PostgreSQL 18 |
| Database | `neondb`; the former Render `nimbus-postgres` database was deleted and must not be reconnected or recreated |

## Read by Task

- For Render configuration, logs, instances, deployments, restarts, and suspend/resume, read [references/render.md](references/render.md).
- For Neon configuration, connections, migrations, log capabilities, and database diagnostics, read [references/neon.md](references/neon.md).
- For sleep and cross-platform troubleshooting, read the shared constraints below before the relevant platform reference.
- Repository configuration and implementation live in `docs/deployment-render.md`, `render.yaml`, `neon.ts`, and `.github/workflows/ci.yml`. Repository declarations do not replace the live configuration.

## Start an Inspection

```sh
render --version
neon --version
render workspace current -o json
neon config status --current-branch
env -u GITHUB_TOKEN gh variable list --json name,value
```

Baseline CLI versions are Render 2.34.0 and Neon 8.11.4. If commands change between versions, read `--help` first. If resource discovery or authentication fails, follow the reference instructions; do not create another project.

## Shared Constraints

- This skill does not grant additional production write access. Use read-only commands to inspect status, configuration, and logs. Perform deployments, restarts, and start/stop operations directly when explicitly requested and within scope. The current request must explicitly cover deletion, reset, or plan changes; past permission to delete test data does not authorize future deletions.
- Never store or echo API keys, full DSNs, passwords, deploy hooks, or authorization headers. Reuse existing credentials; ask the user to log in only if credentials are missing. `.env`, `.env.local`, and `.neon` are ignored; credential files are readable only by the current user.
- The architecture is Go/Gin + pgx/GORM. Render runs a prebuilt GHCR image, and Neon hosts PostgreSQL only. Do not deploy Functions, Auth, or Data API services for operations work.
- Startup runs embedded migrations and uses session advisory locks and `LISTEN/NOTIFY`. It requires a **direct** `DATABASE_URL_UNPOOLED` connection with TLS parameters preserved; do not use the transaction pooler URL ending in `-pooler`.
- Local connection priority is `--postgres`, `NIMBUS_POSTGRES`, `DATABASE_URL_UNPOOLED`, then `DATABASE_URL`. `npm start` loads `.env.local` and `.env`, with exported variables taking precedence. Render's `NIMBUS_POSTGRES` stores the Neon direct connection string; pulling local environment variables does not update Render.
- Some UI settings are stored in the database `settings` table. For example, a model key may override its environment-variable fallback. Distinguish repository declarations, Render environment variables, and database settings when checking effective configuration. Never output secret setting values.
- The application writes slog JSON to stdout/stderr, with debug logging in dev. Correlate `request_id`, `session_id`, `turn_id`, and `sandbox_id`. The app's `X-Request-ID` differs from Render's `Rndr-Id`. Do not output request bodies, credentials, or SQL parameters during troubleshooting.

## Sleep Checks

Render Free Web services sleep after roughly 15 minutes without inbound HTTP/WebSocket activity. Neon Free compute sleeps about 5 minutes after the last database activity. Nimbus checks runtime handoffs every second and reaps sandboxes every 30 seconds; it also keeps a LISTEN connection open. After closing the page, terminal, and other clients, the Web service usually sleeps before Neon, for roughly 20 minutes total. Platform scheduling may add delay.

Use `render services instances` and `neon api .../endpoints` to observe state. Do not call the public health endpoint, run SQL, or add an uptime ping, as these can reset idle timers or wake services. A `live` deployment does not mean an instance is running. Neon `suspend_timeout_seconds=0` uses the plan default; it does **not** disable sleep. Only `-1` disables it.

SSE heartbeats do not query the database. Browser auto-reconnects, 401/404 responses, and WebSocket input/resize events can still count as inbound activity. HTTP completion logs may not appear until an SSE/WebSocket connection closes, so missing completion logs alone do not prove that no client is connected. Do not remove ownership safety checks to force database sleep.

Report the actual image/deployment ID, instance and database status, log time range, and whether output was truncated. Separate observations, inferences, and unverified details. After a failed change, read status and logs first and preserve any working version. Do not repeatedly restart or blindly resubmit write requests.
