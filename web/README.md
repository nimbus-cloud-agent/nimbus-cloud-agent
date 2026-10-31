# Nimbus web UI

React 19.3.1 + HeroUI v3.2.7 + Tailwind 4.3.3, built with Vite 8.3.5 and TypeScript 7.0.2. HeroUI is the sole component library; xterm.js supplies the terminal. No HeroUIProvider or Tailwind PostCSS configuration is used.

`npm ci && npm run build` creates `dist/`, embedded by the Go binary. For UI development use `npm run dev` with Nimbus on 127.0.0.1:8080. The Vite development proxy handles REST, SSE, and WebSocket; production has no separate frontend server.

`App.tsx` subscribes to `/api/sessions/stream` for the workspace list and the selected session's `/stream` endpoint for its events, outbox, and incremental output. EventSource reconnects automatically; both server streams emit heartbeats every 5 seconds. The browser does not periodically fetch the session list.

The model label is fixed to DeepSeek Flash. Plan/thinking remain editable, while the preset picker uses the server's `mode_locked` field and disables immediately during submission. `ImageGallery.tsx` renders draft previews and historical images; the composer supports upload, paste, drag/drop, and image-only messages. Image bytes are served separately from SSE.

Tool presentation follows the installed DSH Web UI's `ToolRow`, `ToolCallTree`, and turn-process disclosure structure, implemented with HeroUI Accordion and Button. Completed turn processes start collapsed; active processes start open, and explicit user choices survive stream updates. The final answer and plan-review controls remain outside the process fold. Each tool has a compact summary, status, and duration; expanding it reveals typed inputs, outputs, and nested PTC calls. Details mount only while expanded. `tool-events.ts` joins native records by call ID, deduplicates PTC start/end pairs, and preserves pending/interrupted calls. Command exit codes, stderr, file contents, and copy actions are displayed directly.

`node scripts/tool-ui-check.mjs` from the repository root tests folding, nested calls, keyboard interaction, copying, stream updates, and mobile layout against browser-only fixtures. It makes no model/tool calls and does not modify stored conversations. Set `NIMBUS_UI_URL=http://127.0.0.1:5173` to target Vite. `npm test` at the root includes native tool-event projection tests.

Build the root project after changing the production UI, then restart Nimbus and reload the page. `node scripts/session-updates-e2e.mjs` from the repository root checks these interactions in Chrome and uses real DeepSeek/E2B credentials. See [API contracts](../docs/API.md) and [verification](../docs/VERIFICATION.md).
