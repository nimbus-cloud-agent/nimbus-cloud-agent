# Contributing

Issues and pull requests are welcome.

- Keep the monolith shape: one binary, one HTTP port, PostgreSQL only.
- Keep the agent inside its VM: the harness and the in-VM relay must only
  ever talk through the backend, never directly to each other.
- Do not bundle third-party code without its licence. See
  `docs/packaging.md` for the DeepSeek Harness rule.
- By contributing, you agree your contribution is licensed under the
  project's MIT licence.

Please open an issue before a large change, so the design can be discussed
first.
