# Packaging

## What a release contains

One file: `nimbus-cloud-agent` (Linux amd64), plus a SHA-256 checksum file.
The React frontend is built first (`make web`) and embedded into the Go
binary, so the user never deploys a frontend separately.

## What is not bundled by default

DeepSeek Harness is **not** bundled into the release binary. It is a
separate MIT-licensed project. Bundling it silently would risk
redistributing third-party code without its licence notice, so:

- The default build ships no harness code.
- The user supplies a harness with `--harness-path`, or installs one
  themselves.
- The opt-in `make bundle-harness` step downloads the official
  `@deepseek-ai/dsh` package, preserves its original `LICENSE` under
  `third_party/deepseek-harness/`, and records the exact version in
  `third_party/deepseek-harness/NOTICE.md`. Only that step may populate
  `third_party/`.

## Runtime requirements

- Linux host with QEMU/KVM available (`qemu-system-x86_64`, KVM device).
- PostgreSQL, reachable via the `--postgres` connection string.
- A DeepSeek API key, supplied at startup or in the web UI.
