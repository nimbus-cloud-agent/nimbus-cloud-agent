# Neon CLI Operations

Official references: [CLI](https://neon.com/docs/cli), [connection pooling](https://neon.com/docs/connect/connection-pooling), and [scale to zero](https://neon.com/docs/introduction/scale-to-zero). This environment uses PostgreSQL only.

## Authentication and Resource Selection

```sh
neon --version
# Install the CLI if missing; if installation fails, use npx neon@latest instead
npm i -g neon@latest
neon profile list -o json
```

Reuse an existing profile or `NEON_API_KEY`. `account: "-"` and `file: "missing"` do not count as valid authentication. If credentials are missing or expired, ask the user to run `neon login`, complete browser authorization, and retry. Do not create another Claimable project to bypass authentication.

If the repository is already linked, do not rerun init. If it is not linked, link the existing project to avoid accidentally creating a project or branch:

```sh
neon link -y --org-id org-dry-feather-16104029 --project-id silent-field-76474041 --branch production
neon config status --current-branch
```

`.neon` is an ignored local resource-selection file. This project's `production` branch serves the dev app; do not create another database just because the Git branch is named `dev`. The commands below specify project and branch explicitly so another task cannot change the active context.

## Live Configuration and Sleep State (No SQL)

```sh
neon status --project-id silent-field-76474041 --branch production
neon config plan --project-id silent-field-76474041 --branch production
neon api /projects/silent-field-76474041/endpoints -o json
```

For endpoints, inspect `id`, `branch_id`, `current_state`, region, autoscaling limits, and `suspend_timeout_seconds`. Report only the fields needed. `active` means running and `idle` means idle/asleep. `0` uses the plan default (5 minutes on the Free baseline); it does not disable sleep. Only `-1` disables sleep.

`neon.ts` is the declaration, `status` reports live state, and `plan` shows pending differences. Do not run `neon deploy` for read-only inspection.

## Pull Connection and Application Configuration

```sh
neon env pull --project-id silent-field-76474041 --branch production
neon config plan --project-id silent-field-76474041 --branch production
# Run only when application configuration was requested and the diff has been reviewed
neon deploy --project-id silent-field-76474041 --branch production
```

The CLI writes `DATABASE_URL`, `DATABASE_URL_UNPOOLED`, and `NEON_BRANCH` to an existing `.env`, or `.env.local` if none exists. Never output full connection strings. Confirm the file is Git-ignored and has restricted permissions. An already-exported `NIMBUS_POSTGRES` takes precedence over the newly pulled values.

Application migrations, session locks, and LISTEN require a direct connection. This is not a limitation of Go's Neon support. Keep pgx/GORM, use `DATABASE_URL_UNPOOLED`, preserve TLS parameters, and do not introduce a JavaScript serverless driver.

After changing a branch, connection, or password, pulling environment variables does not update Render. Follow the Render reference to load the local environment in-process and pass it to `render_extra.py env-set NIMBUS_POSTGRES --from-env DATABASE_URL_UNPOOLED --confirm`, then deploy. Write a test record through the application API and verify the same record in Neon; clean it up afterward. Do not put a DSN in an image or CI build argument.

Use `--update-existing` only when the user asks to overwrite existing configuration and the diff has been reviewed. The Free plan can keep its default sleep setting. If init is required, pass `--agent codex`: the baseline CLI once failed while auto-installing a detected Zed skill. Neon 8.11.4 does not support `neon skills status`; do not retry it in a loop.

## Connections and Migrations (Wake Compute)

Use the installed `psql`. The Neon CLI uses a direct connection by default; do not add `--pooled`:

```sh
neon psql production --project-id silent-field-76474041 --database-name neondb
neon psql production --project-id silent-field-76474041 --database-name neondb -- -X -c 'SELECT version();'
neon psql production --project-id silent-field-76474041 --database-name neondb -- -X -c 'SELECT version FROM schema_migrations ORDER BY version;'
```

Startup automatically runs `internal/store/migrations/*.sql`. There were 8 versions in `schema_migrations` at the baseline date; compare with the actual repository files instead of assuming there will always be 8. Put schema changes in versioned migrations. Run temporary DDL through `psql` only when specifically requested; never replace migrations with manually created tables.

Check only whether database settings are configured; do not output their values:

```sql
SELECT key, CASE WHEN value = '' THEN 'empty' ELSE 'configured' END AS state
FROM settings ORDER BY key;
```

Use `psql`'s `\conninfo` to verify client-side TLS. The Neon proxy may terminate TLS, so `pg_stat_ssl.ssl=false` alone does not mean public traffic was unencrypted.

## Logs and SQL Diagnostics

Check Render slog for Go/GORM and connection errors. Neon telemetry comes from the database platform and does not include application `request_id` logs.

```sh
neon logs query --help
neon logs query --project-id silent-field-76474041 --branch production --source pg_endpoint --since 1h --limit 100 -o json
```

Capability limits: CLI 8.11.4 accepts `pg_endpoint`, but a project check on 2026-11-02 returned no logs. The installed Neon guide says PostgreSQL log availability is not guaranteed. **An empty list does not prove that the database has no errors.** Do not substitute Function or Storage logs; neither service is deployed here. Recheck CLI help and official documentation when capabilities change.

If `minimum-severity` filtering is unsupported, first use `neon logs field-values severity_text` with the same project and branch to retrieve valid values, then choose a severity. Distinguish no records from permission failures and disabled telemetry. If the response includes `next_cursor` or `is_truncated`, paginate within the same time window and report truncation.

Choose read-only diagnostics by symptom. These commands query the database and reset idle timers, so do not run them while observing sleep behavior:

```sh
neon inspect db long-running-queries --project-id silent-field-76474041 --branch production --database-name neondb
neon inspect db locks --project-id silent-field-76474041 --branch production --database-name neondb
neon inspect db table-sizes --project-id silent-field-76474041 --branch production --database-name neondb
```

Outlier/call diagnostics require `pg_stat_statements`; cache diagnostics may require a Neon extension. Do not install extensions automatically for read-only troubleshooting. Statistics may reset after a restart or sleep. SQL diagnostics may contain business data; redact them before reporting.

## Stop and Resume

Rely on automatic sleep for normal shutdowns; do not delete the project, branch, or compute. First stop the Render Web service and local clients such as `npm start` and `psql`, wait for Neon to become idle, then observe it through endpoint metadata. Resuming the Web service wakes Neon on the next connection. Do not run repeated `SELECT 1` queries to test sleep.

For an explicit deletion, reset, or manual shutdown request, verify the target and supported operation with the relevant CLI help or `neon api ... --describe` before making the change. This skill does not provide a one-command database wipe.
