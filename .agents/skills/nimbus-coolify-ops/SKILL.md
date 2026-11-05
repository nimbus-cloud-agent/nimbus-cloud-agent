---
name: nimbus-coolify-ops
description: Inspect and maintain Nimbus's backup dev deployment with the Coolify CLI, review resource configuration and deployment/runtime logs, and deploy or restart only when explicitly requested. Render remains the primary dev environment.
---

# Nimbus Coolify Backup Operations

## Platform Selection

- As of 2026-11-03, Render + Neon remain the primary Nimbus dev environment. Coolify is the backup environment hosted on Alibaba Cloud. For a general “deploy dev” request, follow the Render workflow in [nimbus-dev-ops](../nimbus-dev-ops/SKILL.md).
- Use Coolify only when the user explicitly selects Coolify, the Alibaba Cloud backup, or `nimbus-aliyun`. Do not fail over automatically or make Coolify primary just because it is available.
- Use the installed `coolify` CLI. Pass `--context nimbus-coolify` to every command. Never display or pass tokens, and never use `--show-sensitive`.

## Coolify Resource IDs

These IDs identify resources. Status and configuration may change; read them again before each operation:

| Resource | Current identifier |
| --- | --- |
| Project | `nimbus`, UUID `xksx3cfpjfi4p90v2jsucnbf` |
| Environment | `dev`, UUID `xswngey34azvjmchoqykxdgc` |
| Application | `nimbus-aliyun`, UUID `3liiq80eey7yqwsk23u80sty` |
| Deployment server | `aliyun`, UUID `w2ndumy8tfc9h51f88ccebep` |
| Image | `ghcr.io/nimbus-cloud-agent/nimbus-cloud-agent:dev` |
| Application domain currently returned by Coolify | `https://3liiq80eey7yqwsk23u80sty.apps.aliyun.lambda1107.icu` |
| Database resource | `nimbus-postgres-aliyun`, UUID `lplpeyidmfslz6st8bicfcr7` |

The application currently uses a prebuilt Docker image, exposes port `10000`, and has a `GET /api/health` health check. Always use the live Coolify configuration for the domain, image, port, health check, and resource status; do not treat this snapshot as current state.

## Connect and Inspect Configuration

Verify the default Coolify context, then inspect the project, environment, and resources:

```sh
coolify --context nimbus-coolify context verify
coolify --context nimbus-coolify project list --format json
coolify --context nimbus-coolify project environments get xksx3cfpjfi4p90v2jsucnbf dev --format json \
  | jq '{uuid,name,applications:[.applications[]? | {uuid,name,status}],postgresqls:[.postgresqls[]? | {uuid,name,status}],services:[.services[]? | {uuid,name,status}]}'
coolify --context nimbus-coolify resource list --format json
```

Inspect application image, domain, port, health check, and other settings. Output only the fields you need:

```sh
coolify --context nimbus-coolify app get 3liiq80eey7yqwsk23u80sty --format json \
  | jq '{uuid,name,status,fqdn,docker_registry_image_name,docker_registry_image_tag,ports_exposes,health_check_enabled,health_check_path,health_check_method,health_check_scheme,health_check_port}'
```

To check whether an environment variable exists, output only its key and a non-sensitive presence marker. Do not output `value` or `real_value`:

```sh
coolify --context nimbus-coolify app env list 3liiq80eey7yqwsk23u80sty --format json \
  | jq 'map({key,is_buildtime,is_runtime,is_preview,is_shared})'
```

Inspect Postgres configuration or status only when the task involves that database:

```sh
coolify --context nimbus-coolify database get lplpeyidmfslz6st8bicfcr7 --format json \
  | jq '{uuid,name,status,image,is_public}'
```

Never add `--show-sensitive` to `app get`, resource listings, or database details. Do not put environment variable values, connection strings, passwords, webhooks, or authorization headers in reports, log excerpts, or repository files.

## Deploy and Verify

Coolify pulls images already published to GHCR; it does not build Nimbus images. Before deploying, confirm the user selected Coolify and that the corresponding GitHub Actions workflow pushed the expected version to GHCR. Do not deploy the `dev` tag before its image has been updated.

Deploy the existing `dev` image when requested:

```sh
coolify --context nimbus-coolify deploy uuid 3liiq80eey7yqwsk23u80sty --docker-tag dev
```

Do not use `--force` by default. After submitting a deployment request, read the deployment list and the specific deployment status. Do not retrigger or cancel an in-progress deployment just because no new log lines have appeared yet:

```sh
coolify --context nimbus-coolify app deployments list 3liiq80eey7yqwsk23u80sty --format json
coolify --context nimbus-coolify deploy get <deployment_uuid> --format json
```

Restart, stop, start, roll back, or cancel a deployment only when explicitly requested. After a failure, collect deployment status and logs before deciding whether another write operation is needed.

## Inspect Logs

Deployment logs and container runtime logs are different sources:

```sh
# Build, pull, and switch logs for a deployment
coolify --context nimbus-coolify app deployments logs 3liiq80eey7yqwsk23u80sty <deployment_uuid> --lines 200

# Application container runtime logs
coolify --context nimbus-coolify app logs 3liiq80eey7yqwsk23u80sty --lines 100 --show-timestamps
```

Report the deployment UUID, status, time range, whether logs were truncated, and the last completed stage. Logs may contain sensitive values; review and redact credentials before sharing. If a deployment is still `in_progress`, continue checking its status instead of inferring failure from a static log snapshot.
