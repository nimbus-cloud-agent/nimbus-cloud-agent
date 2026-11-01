# Dev deployment (Railway, `dev` branch)

This is the **dev-only** deployment path. It is not the local deployment in
`docs/deployment.md`, and it does not change the default local behaviour:
without `--public-host`, Nimbus still binds loopback only, and without both
Basic Auth variables, there is no auth gate.

## What the `dev` branch adds

- `Dockerfile` + `docker-entrypoint.sh`: multi-stage build of the Go binary
  (frontend embedded) and the `nimbus-agent-runtime` tarball, both from this
  repository. No runtime package is published to npm for this path.
- `--public-host` (`NIMBUS_PUBLIC_HOST`): the single public hostname allowed
  to reach the server. Setting it opts the server into public binding; the
  Host guard then accepts that hostname plus loopback, and nothing else.
- `--basic-user` / `--basic-pass` (`NIMBUS_BASIC_USER` /
  `NIMBUS_BASIC_PASS`): when both are set, every route except `/api/health`
  requires HTTP Basic Auth. Health stays open so the host's health check
  works. A half-configured pair is treated as disabled.
- `.github/workflows/dev-image.yml`: on every push to `dev`, builds the
  image and publishes `ghcr.io/nimbus-cloud-agent/nimbus-cloud-agent:dev`
  and `:dev-<sha>`. Pull requests to `dev` only prove the image builds.

## Railway setup (one project, two services)

1. Create a Railway account (trial: one-time $5 for 30 days, no card; after
   that the Free plan is $0 with $1 of credit a month, which does not cover
   an always-on app plus database — measure actual usage before relying on
   it). Connect GitHub and select this repository.
2. Add a **PostgreSQL** service from the Railway template. For a dev
   environment this is the simplest database; it stays resident (it does not
   reliably sleep), so its cost is the floor of the Railway bill. Back it up
   before the trial ends: trial volumes are deleted 30 days after expiry.
   If the goal is strictly $0, use an external free Postgres (e.g. Neon)
   instead and point `NIMBUS_POSTGRES` at its direct (non-pooler) URL —
   Nimbus uses PostgreSQL LISTEN/NOTIFY, which needs a direct connection.
3. Add a **Web Service** from the same repository:
   - Branch: `dev`, Auto-deploy: on, Runtime: Docker (Railway builds the
     `Dockerfile` in this branch; the GHCR image from CI is an alternative
     deploy source, not required).
   - Health check path: `/api/health`.
   - Public domain: generate one, then copy its hostname exactly.
4. Set these environment variables on the web service (values go in the
   Railway dashboard, never in the repository):
   - `NIMBUS_POSTGRES`: the Postgres service's reference variable, e.g.
     `${{Postgres.DATABASE_URL}}` (add `?sslmode=require` for an external
     database; Railway's internal URL does not need it).
   - `NIMBUS_E2B_API_KEY`: required, held by Go, never passed to the runtime.
   - `NIMBUS_DEEPSEEK_API_KEY`: optional if set later in Settings.
   - `NIMBUS_PUBLIC_HOST`: the Railway public hostname from step 3, without
     scheme or port.
   - `NIMBUS_BASIC_USER`, `NIMBUS_BASIC_PASS`: the dev login.
   - Do **not** set `NIMBUS_ADDR`: the entrypoint maps Railway's `$PORT`
     to `0.0.0.0:$PORT` automatically.
5. Deploy, then verify in order: `/api/health` returns ok, the homepage
   asks for Basic Auth, a session can be created, and a turn completes
   against E2B. Merges to `dev` redeploy automatically; `main` is untouched.

## Costs and sleep, honestly

- Railway's Serverless sleep watches **outbound** traffic, not inbound:
  a service must send no outbound packets for 5 minutes before it sleeps.
  Nimbus's reaper queries PostgreSQL every 30 seconds while the server is
  awake, so the current code does not fall asleep on its own — do not budget
  on sleep savings until that loop is changed.
- The Postgres service is resident even when the app sleeps, so the database
  is the minimum monthly cost of this setup.
- Turn working files live under `/tmp/turns` in the container and are lost
  on redeploy. PostgreSQL is the source of truth, so session history and
  committed output survive; an in-flight turn is interrupted by a redeploy.
