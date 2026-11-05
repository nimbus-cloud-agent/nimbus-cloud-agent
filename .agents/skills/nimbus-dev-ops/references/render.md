# Render CLI Operations

Official references: [CLI](https://render.com/docs/cli), [command reference](https://render.com/docs/cli-reference), [logs](https://render.com/docs/logging), and [Free instance sleep](https://render.com/docs/free).

## Authentication and Resource Selection

```sh
brew install render
render --version
# Reuse RENDER_API_KEY if available; otherwise ask the user to authorize once
render login
render workspace set tea-dbjjrd142hec73d81l60 -o json --confirm
render workspace current -o json
```

Use the terminal for routine operations. Browser authorization is needed only when logging in without existing credentials. Automation injects `RENDER_API_KEY` securely; never write its value into skills or scripts. Use `-o json` to avoid interactive menus. Configuration is stored in `~/.render/cli.yaml`; never output that file. To isolate a project, set `RENDER_CLI_CONFIG_DIR="$PWD/.local/render-cli"`, then authenticate and select the workspace for that directory.

## Read-Only Status and Configuration

```sh
render services -o json
render services instances srv-dbjl0pe7bikc73fplma0 -o json
render deploys list srv-dbjl0pe7bikc73fplma0 -o json
python3 .agents/skills/nimbus-dev-ops/scripts/render_extra.py env
```

Inspect the actual JSON structure first. Find the `name` and `id`, then verify the image, `serviceDetails.plan`, region, `healthCheckPath`, and suspended state. If the instance list is empty and the service was not manually suspended, use stop logs to determine whether it slept automatically. A historical deployment marked `live` does not mean an instance is running. A historical `image.ref` should match a Git SHA; do not rely only on the `:dev` tag or repository HEAD.

Repository configuration is defined in `render.yaml`, `Dockerfile`, and `docker-entrypoint.sh`. Runtime injects `NIMBUS_POSTGRES`, model/E2B keys, and Basic Auth; `PORT` maps the listen address. Use the supplemental `env` command to inspect variable presence, a few non-sensitive values, and the database host. Never output secrets.

**Official CLI 2.34.0 gaps:** It has no Web suspend/resume or environment-variable read/update commands, and `services update` has no `--env-var` option. Do not invent `render env`, `render services get`, or `render services suspend`. This skill's `render_extra.py` covers only these gaps through the Render API and requires `RENDER_API_KEY`; it does not read or display the CLI token. Continue using the official CLI for other operations.

## Logs

```sh
render logs -r srv-dbjl0pe7bikc73fplma0 --limit 100 -o json
render logs -r srv-dbjl0pe7bikc73fplma0 --level error,warning --limit 100 -o json
render logs -r srv-dbjl0pe7bikc73fplma0 --text 'sandbox' --limit 100 -o json
# Example times; replace them with the event's RFC3339 UTC interval
render logs -r srv-dbjl0pe7bikc73fplma0 --start 2026-11-02T06:50:30Z --end 2026-11-02T07:00:00Z --direction forward -o json
# Use only when you need to observe new events; terminate the process when done
render logs -r srv-dbjl0pe7bikc73fplma0 --tail -o json
```

Group logs by the latest deployment start time, `image.ref`, and instance; do not treat an old failure as a current error. In version 2.34.0, `logs -o json` returns a stream of JSON objects, not one array. To aggregate it, use `jq -s '.'` or decode each object separately; do not pass all stdout to a single `json.loads` call. Check ordering first and sort by timestamp if needed. The CLI limit of 100 is a bounded result, not the whole time window. Narrow or segment queries before reporting complete coverage. Do not copy sensitive business logs into public reports.

The Hobby baseline retains application logs for 7 days. Platform HTTP request logs require a higher plan; do not assume they are available. Nimbus's own `msg="http request"` JSON includes method, path, status, duration, and request ID.

Interpretation notes:

- A 401 on `/` usually means Basic Auth failed; it is still an inbound request. `/api/health` does not require authentication and appears at debug level.
- For `draining:true` or a brief 503, check ownership handoff first. A health-check 200 does not prove the application is ready for business traffic.
- For `database is already owned...`, check whether a local process or another deployment holds the lock and whether both versions support handoff. Do not kill connections or disable the lock casually.
- For E2B x509 / `tls_unknown_authority`, first check whether the image includes `ca-certificates`, then inspect DNS/TLS/timeout diagnostics. Do not bypass certificate verification.
- GORM logs duration, rows, and SQLSTATE but not SQL text or parameters. Distinguish normal cancellation/terminal closure at DEBUG from ERROR events.

## Deploy and Restart

Current deployment logic is in `.github/workflows/ci.yml`. The baseline value of `RENDER_DEPLOY_ENABLED` is false; recheck it each time. On this machine, the environment's `GITHUB_TOKEN` was previously restricted by organization policy; use `env -u GITHUB_TOKEN gh` to use the existing `gh` login credentials.

```sh
env -u GITHUB_TOKEN gh variable list --json name,value
env -u GITHUB_TOKEN gh workflow run ci.yml --ref dev -f deploy_render=true
env -u GITHUB_TOKEN gh run list --workflow ci.yml --branch dev --limit 5
env -u GITHUB_TOKEN gh run view RUN_ID --log-failed
```

Use `workflow_dispatch` to run checks, publish the SHA-tagged image, and deploy through the hook. Do not also trigger a second Render deployment. Deploy directly only when deploying an existing image, rolling back, or when the hook did not trigger (replace `COMMIT_SHA`):

```sh
render deploys create srv-dbjl0pe7bikc73fplma0 \
  --image ghcr.io/nimbus-cloud-agent/nimbus-cloud-agent:COMMIT_SHA \
  --wait -o json --confirm
render restart srv-dbjl0pe7bikc73fplma0 -o json --confirm
```

Do not pass `--commit` to image-backed services. After deployment, verify `image.ref`, `live` status, and a ready instance. If needed, make one health request to check `draining:false`. The health endpoint does not query the database; verify database connectivity with a business API read/write. Do not make health requests while observing sleep behavior.

Manual deployments do not require re-enabling continuous deployment. Change the GitHub variable only if the user asks to restore automatic deployments. If npm installation fails with ENOTFOUND, check whether the lockfile contains an internal registry URL; this has caused CI failures in this repository before.

## Update Variables, Suspend, and Resume

The following commands use this project's supplemental CLI. Run them only when the corresponding operation is authorized. Omitting `--confirm` displays the plan without applying it.

```sh
export NIMBUS_LOG_LEVEL=debug
python3 .agents/skills/nimbus-dev-ops/scripts/render_extra.py env-set NIMBUS_LOG_LEVEL --from-env NIMBUS_LOG_LEVEL --confirm
python3 .agents/skills/nimbus-dev-ops/scripts/render_extra.py suspend --confirm
python3 .agents/skills/nimbus-dev-ops/scripts/render_extra.py resume --confirm
```

`env-set` reads values from the process environment; do not pass keys as command-line arguments. It updates and reads back one key without replacing the entire variable list. When changing the database, load the local environment in a Node/Python process without printing secrets, then run `env-set NIMBUS_POSTGRES --from-env DATABASE_URL_UNPOOLED --confirm`. Saving a variable does not update the current container; deploy and verify afterward.

After a manual suspend, resume explicitly; ordinary requests do not clear a manual suspension. To stop the environment, suspend only Render Web and let Neon become idle on its own. Resuming Web wakes Neon on the next connection. After a successful operation, continue checking instances, deployments, and any necessary business behavior.

Free Web services do not provide a regular SSH/Shell. Do not try `render ssh` to inspect environment variables, and do not silently create a billable ephemeral shell.
