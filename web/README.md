# Nimbus web UI

React 19.3.1 + HeroUI v3.2.7 + Tailwind 4.3.3, built with Vite 8.3.5 and TypeScript 7.0.2. HeroUI is the sole component library; xterm.js supplies the terminal. No HeroUIProvider or Tailwind PostCSS configuration is used.

`npm ci && npm run build` creates `dist/`, embedded by the Go binary. For UI development use `npm run dev` with Nimbus on 127.0.0.1:8080. The Vite development proxy handles REST, SSE, and WebSocket; production has no separate frontend server.

`App.tsx` subscribes to `/api/sessions/stream` for the workspace list and the selected session's `/stream` endpoint for its events, outbox, and incremental output. EventSource reconnects automatically; both server streams emit heartbeats every 5 seconds. The browser does not periodically fetch the session list.

The model label is fixed to DeepSeek Flash. Plan/thinking remain editable, while the preset picker uses the server's `mode_locked` field and disables immediately during submission. `ImageGallery.tsx` renders draft previews and historical images; the composer supports upload, paste, drag/drop, and image-only messages. Image bytes are served separately from SSE.

Build the root project after changing the production UI, then restart Nimbus and reload the page. `node scripts/session-updates-e2e.mjs` from the repository root checks these interactions in Chrome and uses real DeepSeek/E2B credentials. See [API contracts](../docs/API.md) and [verification](../docs/VERIFICATION.md).
