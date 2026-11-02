# Render + Neon deployment (`dev` branch)

For day-to-day CLI operations, use the project skill [nimbus-dev-ops](../.agents/skills/nimbus-dev-ops/SKILL.md). It covers configuration, logs, deploys, start/stop, and idle checks for both providers.

This deployment runs a [prebuilt container](https://render.com/docs/deploying-an-image) from GitHub Container Registry. GitHub Actions tests the `dev` branch, builds and publishes the image, then asks Render to deploy that exact image. Render does not build the application from source.

## Deployment flow

```text
push to dev → CI checks → build linux/amd64 image → publish to GHCR → Render deploy hook
```

The workflow publishes both `ghcr.io/nimbus-cloud-agent/nimbus-cloud-agent:dev` and an immutable image tagged with the commit SHA. It triggers Render with the SHA-tagged image after publishing.

## One-time setup

1. Push the workflow changes to `dev`. The first run publishes the image and warns that the Render deploy hook secret is not set yet.

2. Link this directory to the existing Neon project and deploy its PostgreSQL configuration:

   ```sh
   npm i -g neon@latest
   neon login
   neon init -y --org-id org-dry-feather-16104029 --project-id silent-field-76474041 --branch production --services postgres --agent codex
   neon deploy
   ```

   `neon.ts` manages the Neon configuration; `.neon` pins the local project/branch and stays ignored. The CLI writes `DATABASE_URL` and `DATABASE_URL_UNPOOLED` into `.env` if it exists, otherwise `.env.local`. Both files stay ignored. The existing Go pgx/GORM driver connects over TLS. Nimbus uses migrations, session advisory locks and `LISTEN/NOTIFY`, so use the **direct** `DATABASE_URL_UNPOOLED`, never the `-pooler` endpoint ([Neon pooling limitations](https://neon.com/docs/connect/connection-pooling)). Embedded SQL migrations initialize a fresh database on the first application start; no manual DDL is required. This setup uses a fresh test database and does not copy data from Render.

3. Give Render permission to pull the GHCR image. In GitHub, create a [personal access token (classic)](https://docs.github.com/en/packages/learn-github-packages/about-permissions-for-github-packages) with the `read:packages` scope. In Render, add it under **Workspace Settings → Container Registry Credentials** with registry `ghcr.io`, your GitHub username, and the token. Keep the package private; the credential lets Render pull it without exposing it publicly.

4. In Render, create a **Blueprint** from this repository and select the `dev` branch. The Blueprint defines `nimbus-dev` as an image-backed web service using the GHCR image. PostgreSQL is hosted on Neon; this Blueprint does not provision a Render database.

   During the first Blueprint setup, provide these secrets in Render:

   - `NIMBUS_POSTGRES` — copy the secret value of Neon's `DATABASE_URL_UNPOOLED`. Keep the TLS options from the CLI.
   - `NIMBUS_E2B_API_KEY` — required.
   - `NIMBUS_BASIC_USER` and `NIMBUS_BASIC_PASS` — set both to protect this single-user app. `/api/health` remains open for Render's health check.
   - `NIMBUS_DEEPSEEK_API_KEY` — optional; it can also be configured in the app's Settings.

   `NIMBUS_PUBLIC_HOST` references Render's `RENDER_EXTERNAL_HOSTNAME` automatically. The app uses that HTTPS hostname for MCP OAuth callbacks. The container entrypoint reads Render's `PORT` and binds to `0.0.0.0`; don't set `NIMBUS_ADDR`.

5. After the first service deploys, copy its [deploy hook](https://render.com/docs/deploy-hooks) from **Service Settings → Deploy Hook**. In the GitHub repository, add it as **Settings → Secrets and variables → Actions → New repository secret** named `RENDER_DEPLOY_HOOK_URL`.

After this, each successful CI run on a push to `dev` publishes a new image and triggers its deployment. Pull requests run CI but don't publish images or deploy.

## Logs and runtime configuration

The Go service writes structured JSON logs through Go's standard `log/slog` package to stdout. Render captures stdout and stderr; view the entries under the `nimbus-dev` service's **Logs** page or **Live Tail**. Dev defaults to `NIMBUS_LOG_LEVEL=debug`, including health probes, task/tool identities, sandbox operations, and E2B DNS, connection, and TLS diagnostics. Request logs include an `X-Request-ID`, method, path, status, duration, and response size; 4xx and 5xx responses are logged at WARN and ERROR. E2B/tool failures are logged even when returned to the agent rather than an HTTP caller. Headers, query strings, request/response bodies, command output, and file contents are excluded, and configured secrets are redacted. [Hobby workspace logs](https://render.com/docs/logging) are retained for 7 days.

The final container installs `ca-certificates` so Go can verify E2B's public HTTPS certificates. A `tls_unknown_authority` error indicates a trust-store problem; the request's host, TLS diagnostics and error appear in Render logs.

`npm start` loads `.env.local` and `.env` without overriding exported shell variables. Database selection is `--postgres` / `NIMBUS_POSTGRES`, then `DATABASE_URL_UNPOOLED`, then `DATABASE_URL`. An explicit Nimbus configuration wins over the standard aliases; remove an old local `NIMBUS_POSTGRES` when switching to Neon.

The service receives its runtime configuration from Render environment variables. The database URL and API keys are not baked into the image, and no config file or disk mount is needed. Conversation data lives in Neon PostgreSQL; `/tmp/turns` is temporary working space.

The web service uses Render's Free compute plan in this Blueprint. [Free web services](https://render.com/docs/free) spin down after 15 minutes without inbound traffic and can take about a minute to wake. Upgrade the web service's compute plan in Render if you want it to stay running.

## Database ownership during deployment

Render starts the next instance before retiring the current one. Nimbus remains a single active control plane per database schema; `NIMBUS_DEPLOY_HANDOFF=true` makes these overlapping deployments cooperate. The next instance waits on the same PostgreSQL advisory lock. The current instance detects that waiter, cancels and persists interrupted work, drains HTTP requests, and then releases ownership. Only then can the next instance apply migrations and recover interrupted tasks.

The retiring instance keeps its health endpoint alive with `draining: true` until Render sends SIGTERM, preventing a restart loop. Business requests briefly return 503 with `Retry-After: 2` during the handoff; this is not a guarantee of uninterrupted requests. If the successor releases ownership before Render retires the previous instance, the previous instance can reacquire it and resume serving. A standby probe never asks a healthy successor to give ownership back. The owner also stops serving if its lock connection is lost.

Both versions must understand the handoff protocol. When upgrading from an older binary that reports `database is already owned by another Nimbus process`, stop that legacy instance once before starting the first handoff-enabled image. Subsequent image deploys and environment redeploys use the automatic handoff. The standalone local default continues to reject a second process.

## Pausing this environment

Set the GitHub repository variable `RENDER_DEPLOY_ENABLED=false` to keep pushes running CI tests without publishing or deploying the dev image. Suspend the web service in Render when an explicit stop is needed. Neon compute suspends automatically when idle; suspension preserves the database and does not delete conversations.

To resume, resume the web service; Neon wakes automatically when Nimbus connects. For a one-time release while automatic deployment stays disabled, run the CI workflow manually on `dev` with `deploy_render=true` (or `gh workflow run ci.yml --ref dev -f deploy_render=true`). Set `RENDER_DEPLOY_ENABLED=true` (or remove the variable) only when continuous deployment is desired.

## Idle behavior

Render Free Web sleeps after 15 minutes without inbound HTTP traffic or WebSocket messages. Neon compute then sleeps after its configured idle timeout (5 minutes on Free); the next database connection wakes it. The database can therefore remain awake for roughly 20 minutes after the last user request. Actual timings depend on in-flight work and provider scheduling.

While Nimbus is running, deployment handoff checks query PostgreSQL every second, sandbox cleanup checks run every 30 seconds, and change notifications hold a direct LISTEN connection. These checks stop when Render spins down the process. Do not add external uptime pings to the public service, and do not disable ownership safety to force a running instance's database to sleep. SSE heartbeats carry no database reads. Incoming terminal input, resize messages, page loads and reconnects are real inbound activity.

To verify idle behavior, close app/client connections and use the Render and Neon management APIs to inspect compute state. Repeatedly calling the application's health URL or running a SQL query can wake/reset the resource being observed. Storage persists while Neon compute is suspended; scale-to-zero is not a promise of zero storage or network charges. See [Render Free Web](https://render.com/docs/free) and [Neon scale-to-zero](https://neon.com/docs/introduction/scale-to-zero).

## Log safety

All Go business and framework diagnostics use slog. GORM emits timings, row counts and SQLSTATE without SQL text, bound parameters or PostgreSQL DETAIL. JSON-RPC emits protocol errors without message bodies. Runtime exits include exit status, generation, uptime and a bounded stderr exception classification; arbitrary running-child stderr is never dumped into logs.

The shared handler strips URL user information, query strings and fragments, recursively sanitizes structured fields, and redacts configured credentials and sensitive field names. Credentials updated through Settings join the same redactor. Panic and terminal failures retain the request identity and a sanitized reason; normal terminal closure is DEBUG. Turn completion is logged after the final persistence transaction commits; finalization failure emits ERROR instead.
