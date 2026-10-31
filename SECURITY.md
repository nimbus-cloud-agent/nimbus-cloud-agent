# Security

Nimbus Cloud Agent is a single-user system with no login. Do not expose an
instance to an untrusted network.

The service enforces a loopback bind and same-origin browser requests. Generated
PTC code runs only in a bounded QuickJS WebAssembly instance. Host execution,
filesystem, environment and arbitrary network primitives are not exposed to it.
Registered tool calls reach isolated E2B sandboxes or explicitly configured MCP
services through a session-scoped capability.

API keys and OAuth tokens are server-side. Local PostgreSQL stores credentials
without application-level encryption; protect the machine and database backups.
The development database uses loopback-only trust authentication. Key files,
database files and runtime logs are excluded from Git.

See [verification notes](docs/VERIFICATION.md) for dependency audit results and
the limits of the current integration tests. A sandbox is the execution boundary;
commands within it are intentionally not filtered. MCP servers are trusted
integrations selected by the local user.

To report a vulnerability, open a private security advisory on GitHub or
contact the maintainers through the organisation page. Please do not open a
public issue for a vulnerability before it has been addressed.
