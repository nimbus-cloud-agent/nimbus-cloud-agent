# Implementation status

The scaffold has been replaced by a working Go + React application with PostgreSQL, DeepSeek Harness, E2B, WebAssembly PTC, PTY, Skills, and HTTP MCP/OAuth.

The current implementation also includes:

- DeepSeek Flash text and image input, upload/paste/drop, previews, and durable image history.
- A session-level Standard/PTC preset, fixed at first message acceptance and enforced through SQL, API, retry, and runtime resume.
- Session-list and conversation EventSource streams with independent 5-second heartbeats.
- Refined desktop/mobile layouts, resizable terminal width, adjustable terminal fonts, and a working Plan control.

The application builds and runs directly on macOS. Real model, E2B, browser, image recovery, and PostgreSQL migration checks have been completed; detailed evidence is recorded below.

Use [README](../README.md) for startup, [ARCHITECTURE](../ARCHITECTURE.md) for runtime boundaries and verified proposal differences, [API](API.md) for session/image/stream contracts, and [VERIFICATION](VERIFICATION.md) for end-to-end coverage. The old QEMU/KVM implementation plan is superseded.
