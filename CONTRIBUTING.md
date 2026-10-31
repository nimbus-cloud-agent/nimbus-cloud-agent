# Contributing

Issues and pull requests are welcome.

- Keep one Go control plane and one HTTP port. PostgreSQL is authoritative
  for history, image attachments, outbox delivery, settings, and credentials.
- Run generated commands and files only in the session's E2B sandbox.
  The trusted Harness supervisor runs locally, and generated PTC code runs
  inside QuickJS WebAssembly with only explicitly registered tool bindings.
  Do not introduce local filesystem, shell, process, or unrestricted network
  tools into the model-visible composition.
- Preserve native Harness events and image attachment references. Add a new
  numbered SQL migration instead of editing a migration already applied.
- Preserve the session preset lock and message idempotency. Changes to the
  browser should continue to use SSE for session updates, with 5-second heartbeats.
- Do not bundle third-party code without its licence. See
  `docs/packaging.md` for the DeepSeek Harness rule.
- By contributing, you agree your contribution is licensed under the
  project's MIT licence.

Please open an issue before a large change, so the design can be discussed
first.

Follow [README](README.md) for setup and [architecture](ARCHITECTURE.md) for
the runtime boundaries. Before submitting code, run `go test ./...`,
`go vet ./...`, `npm test`, and `npm run build`. For persistence changes, run
the PostgreSQL integration tests described in the README. Real API and crash
recovery checks belong in a disposable local workspace; see
[verification](docs/VERIFICATION.md). Do not commit keys, database dumps, or
runtime files from `.local/`.
