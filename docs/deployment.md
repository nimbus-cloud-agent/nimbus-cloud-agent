# Local deployment

Run Nimbus on macOS or Linux. The application consists of a Go control plane, a separately packaged TypeScript agent runtime, and PostgreSQL. E2B provides the execution sandboxes in the cloud; you do not need Docker, QEMU/KVM, or a local Linux VM.

[Back to README](../README.md) · [Packaging](packaging.md) · [Architecture](../ARCHITECTURE.md)

## Requirements

| Dependency | Requirement |
| --- | --- |
| Node.js + npm | Node **24.11 or later** |
| Go | **1.25 or later** when building from source |
| PostgreSQL | **17** is the tested version |
| Build tools | Git and Make; Homebrew for the macOS setup helper |
| DeepSeek | API key with access to `deepseek-flash`; also configurable in the UI |
| E2B | API key, required at startup; default template is `base` |
| Network | Access to DeepSeek and E2B; connected MCP services may need their own authorization |

The frontend ships inside the Go binary. `nimbus-agent-runtime` is a separate npm package and directly installs its pinned DeepSeek Harness dependencies. No Harness path needs to be configured.

## Option A: macOS from source

### 1. Install the prerequisites

With Homebrew installed:

```sh
brew install node@24 go postgresql@17
export PATH="$(brew --prefix node@24)/bin:$PATH"
node --version
go version
```

Ensure the versions meet the table above. The setup helper installs missing Node/Go executables but does not upgrade an older executable already on your PATH.

### 2. Clone and set up

```sh
git clone https://github.com/nimbus-cloud-agent/nimbus-cloud-agent.git
cd nimbus-cloud-agent
npm run setup:mac
```

The setup helper:

- Creates `.local/postgres` with UTF-8 encoding, the `nimbus` role, and the `nimbus` database.
- Starts PostgreSQL on **127.0.0.1:55432**, separate from the usual port 5432.
- Installs npm dependencies and generates the shared RPC contract.
- Builds the runtime, frontend, and `bin/nimbus-cloud-agent`.

It does not install a login service. The development database uses trust authentication on loopback and is intended for your local machine only.

### 3. Configure API keys and start

```sh
export NIMBUS_DEEPSEEK_API_KEY='your-deepseek-api-key'
export NIMBUS_E2B_API_KEY='your-e2b-api-key'
npm start
```

Open **http://127.0.0.1:8080**. Create a session and send a task. Pure chat does not create a sandbox; the first sandbox tool creates one on demand.

The source launcher can instead read these files from the repository root:

| File | Used when this variable is unset |
| --- | --- |
| `deepseekkey` | `NIMBUS_DEEPSEEK_API_KEY` |
| `e2bhobbykey` | `NIMBUS_E2B_API_KEY` |

Each file should contain only its API key. Both files are ignored by Git. `.env` files are **not loaded automatically**. The standalone Go binary reads configuration and environment variables, not these launcher-specific key files.

DeepSeek's key may also be stored through Settings. E2B's key is read by Go at startup; restart after changing it. Plan and thinking choices take effect on subsequent turns; a changed E2B template applies to new sandboxes.

### 4. Start and stop after setup

From the repository root, with credentials available:

```sh
npm start                 # foreground; stop with Ctrl-C
# Or use the background helper:
npm run start:background
npm stop
```

Background service logs: `.local/nimbus.log`. PostgreSQL logs: `.local/postgres.log`.

The background helper checks the default `127.0.0.1:8080` address. For a custom loopback port, use the foreground launcher or run the Go executable directly.

Stopping Nimbus does not remove conversations or stop PostgreSQL. Cloud sandboxes follow their own lifecycle: pause them in the UI when finished; the cloud timeout also pauses them. To stop the project-local database after stopping Nimbus:

```sh
"$(brew --prefix postgresql@17)/bin/pg_ctl" -D .local/postgres stop
```

The source launcher starts it again when using the default macOS database setup.

## Option B: Linux or an existing PostgreSQL server

Install the prerequisites and provision a PostgreSQL database owned by the application role. For example, run the following as a PostgreSQL administrator, replacing the password:

```sql
CREATE ROLE nimbus WITH LOGIN PASSWORD 'replace-with-a-long-password';
CREATE DATABASE nimbus OWNER nimbus;
```

Then build and launch the application:

```sh
git clone https://github.com/nimbus-cloud-agent/nimbus-cloud-agent.git
cd nimbus-cloud-agent
npm ci
make

export NIMBUS_POSTGRES='postgres://nimbus:replace-with-a-long-password@127.0.0.1:5432/nimbus?sslmode=disable'
export NIMBUS_DEEPSEEK_API_KEY='your-deepseek-api-key'
export NIMBUS_E2B_API_KEY='your-e2b-api-key'
npm start
```

URL-encode special characters in the DSN password. The example uses a local database; use your database's TLS settings for a remote connection. Nimbus applies its SQL migrations automatically at startup. The role needs permission to create tables, indexes, functions, and triggers in its schema.

When `NIMBUS_POSTGRES` is set, the source launcher uses that database and does not start the project-local macOS database. PostgreSQL must already be running. Nimbus permits only one active control-plane instance per schema.

## Option C: separate build artifacts

A standalone installation needs Node.js and PostgreSQL, but no Go compiler or source checkout. Install the Go executable for your platform and the matching runtime tarball. See [Packaging](packaging.md) for complete build and installation commands.

The current runtime is **0.3.0**, using **protocol v3**. Update it together with the Go executable. The runtime package has not been published to the public npm registry; use the tarball built by `make pack-runtime` or included in a release.

## Configuration

Precedence is **CLI flags → `NIMBUS_` environment variables → configuration file → defaults**. Go reads `config.yaml` in its working directory, or the file selected by `--config`.

| Environment variable | CLI flag | Default / purpose |
| --- | --- | --- |
| `NIMBUS_ADDR` | `--addr` | `127.0.0.1:8080`; loopback only |
| `NIMBUS_POSTGRES` | `--postgres` | Required database DSN; source launcher supplies the macOS default |
| `NIMBUS_RUNTIME_COMMAND` | `--runtime-command` | `nimbus-agent-runtime` on PATH; source launcher selects the workspace executable |
| `NIMBUS_E2B_API_KEY` | `--e2b-api-key` | Required; held by Go, never passed to runtime |
| `NIMBUS_DEEPSEEK_API_KEY` | `--deepseek-api-key` | Optional if configured through Settings |
| `NIMBUS_E2B_TEMPLATE` | `--e2b-template` | `base` |
| `NIMBUS_WORK_DIR` | `--work-dir` | `.local/turns`; per-turn Harness working copies |

Prefer environment variables over credential-bearing CLI flags. The Go server enforces loopback access and same-origin requests. This project currently has no public hosting, login, multi-user authentication, or multi-instance scheduler.

## Check that it is running

```sh
curl --fail http://127.0.0.1:8080/api/health
```

A healthy response includes `ok: true` and `runtime.state: "running"`. The runtime PID stays stable across turns and changes after automatic recovery. An E2B sandbox is not required for a healthy application.

For a quick functional check, ask Nimbus to create a file under `/workspace` and read it back. Its contents should also be visible in the terminal. The terminal connects through Go directly to E2B and can remain connected while Node restarts.

## Updates and backups

Back up the database, stop Nimbus, then update and rebuild a source checkout:

```sh
npm stop
git pull --ff-only
npm ci
make
npm run start:background
```

For separate artifacts, replace the Go executable and install the matching runtime tarball. Refresh the browser after updating. Startup migrations preserve existing conversations; a rebuild does not reset the database.

For the default macOS database, a backup can be made with:

```sh
"$(brew --prefix postgresql@17)/bin/pg_dump" \
  -h 127.0.0.1 -p 55432 -U nimbus -d nimbus \
  -Fc -f "$HOME/nimbus-backup.dump"
```

Keep backups outside the repository: they include conversation history, image attachments, settings, and integration credentials. Use `pg_restore` to restore into a database you provision separately, then point `NIMBUS_POSTGRES` at it.

| Location | Contents |
| --- | --- |
| PostgreSQL (`.local/postgres` for the macOS helper) | Authoritative conversations, attachments, settings, Skills, and OAuth state |
| `.local/turns` | Reconstructed per-turn working copies; not a backup of cloud files |
| E2B sandbox `/workspace` | Agent-generated files and command results; shared with the web terminal |

Deleting a session destroys its E2B sandbox and cascades its stored conversation data. Pausing preserves the sandbox for resumption. PostgreSQL backups do not include E2B files; copy any files you need before destroying a sandbox.

## Troubleshooting

| Symptom | What to check |
| --- | --- |
| Missing `nimbus-agent-runtime` | For a source checkout, run `npm ci` and `make`, then use `npm start`. For standalone use, install the tarball and check `command -v nimbus-agent-runtime`. |
| Protocol compatibility error | Install the Go executable and runtime 0.3.0 / protocol v3 together; rebuild after updating source. |
| Database connection refused | Check the DSN and that PostgreSQL is running. The macOS helper uses 55432, not 5432. |
| Another Nimbus instance owns the database | Stop the existing process before starting another against the same schema. |
| Port 8080 is already in use | Stop the existing service, or choose another loopback port and use the foreground launcher. |
| Sandbox creation fails | Check the E2B key, account quota, network access, and template. Errors appear in the session; chat can work before any sandbox exists. |
| MCP is authorized but unavailable | Use Settings → MCP → Test to check discovery. Enabled tools load at the start of a new turn. |
| Background start fails | Inspect `.local/nimbus.log`; try `npm start` to see startup errors directly. |

See [Verification](VERIFICATION.md) for reproducible tests and current operational limits.
