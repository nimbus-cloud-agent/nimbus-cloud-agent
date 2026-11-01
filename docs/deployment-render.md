# Render deployment (`dev` branch)

This deployment runs a [prebuilt container](https://render.com/docs/deploying-an-image) from GitHub Container Registry. GitHub Actions tests the `dev` branch, builds and publishes the image, then asks Render to deploy that exact image. Render does not build the application from source.

## Deployment flow

```text
push to dev → CI checks → build linux/amd64 image → publish to GHCR → Render deploy hook
```

The workflow publishes both `ghcr.io/nimbus-cloud-agent/nimbus-cloud-agent:dev` and an immutable image tagged with the commit SHA. It triggers Render with the SHA-tagged image after publishing.

## One-time setup

1. Push the workflow changes to `dev`. The first run publishes the image and warns that the Render deploy hook secret is not set yet.

2. Create a Render PostgreSQL database named **`nimbus-postgres`** in the **Singapore** region, matching the service in `render.yaml`. Use PostgreSQL 17, which is the version exercised by this repository's CI. The Blueprint connects through the database's [internal connection string](https://render.com/docs/postgresql-creating-connecting). Choose a paid database plan if you need the data to last: Render's [Free PostgreSQL databases](https://render.com/docs/free) expire after 30 days and are deleted after a further 14-day upgrade grace period. The Hobby workspace plan is separate from each service's compute plan.

3. Give Render permission to pull the GHCR image. In GitHub, create a [personal access token (classic)](https://docs.github.com/en/packages/learn-github-packages/about-permissions-for-github-packages) with the `read:packages` scope. In Render, add it under **Workspace Settings → Container Registry Credentials** with registry `ghcr.io`, your GitHub username, and the token. Keep the package private; the credential lets Render pull it without exposing it publicly.

4. In Render, create a **Blueprint** from this repository and select the `dev` branch. The Blueprint defines `nimbus-dev` as an image-backed web service using the GHCR image. The `nimbus-postgres` database must exist before the first sync so Render can populate `NIMBUS_POSTGRES` with its internal connection string.

   During the first Blueprint setup, provide these secrets in Render:

   - `NIMBUS_E2B_API_KEY` — required.
   - `NIMBUS_BASIC_USER` and `NIMBUS_BASIC_PASS` — set both to protect this single-user app. `/api/health` remains open for Render's health check.
   - `NIMBUS_DEEPSEEK_API_KEY` — optional; it can also be configured in the app's Settings.

   `NIMBUS_PUBLIC_HOST` references Render's `RENDER_EXTERNAL_HOSTNAME` automatically. The app uses that HTTPS hostname for MCP OAuth callbacks. The container entrypoint reads Render's `PORT` and binds to `0.0.0.0`; don't set `NIMBUS_ADDR`.

5. After the first service deploys, copy its [deploy hook](https://render.com/docs/deploy-hooks) from **Service Settings → Deploy Hook**. In the GitHub repository, add it as **Settings → Secrets and variables → Actions → New repository secret** named `RENDER_DEPLOY_HOOK_URL`.

After this, each successful CI run on a push to `dev` publishes a new image and triggers its deployment. Pull requests run CI but don't publish images or deploy.

## Logs and runtime configuration

The Go service writes structured JSON logs through Go's standard `log/slog` package to stdout. Render captures stdout and stderr; view the entries under the `nimbus-dev` service's **Logs** page or **Live Tail**. The application logs HTTP method, path, status, duration, and response size. Secrets, headers, and query strings are not included. [Hobby workspace logs](https://render.com/docs/logging) are retained for 7 days.

The service receives its runtime configuration from Render environment variables. The database URL and API keys are not baked into the image, and no config file or disk mount is needed. Conversation data lives in PostgreSQL; `/tmp/turns` is temporary working space.

The web service uses Render's Free compute plan in this Blueprint. [Free web services](https://render.com/docs/free) spin down after 15 minutes without inbound traffic and can take about a minute to wake. Upgrade the web service's compute plan in Render if you want it to stay running.
