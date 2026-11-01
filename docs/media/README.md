# Documentation media

These images were captured from the running Nimbus application at `http://127.0.0.1:8080` on 2026-11-01. They show real DeepSeek Flash / E2B interactions in dedicated demonstration sessions, with synthetic sales data. The interface is Chinese; screenshots are 1600 × 1100 pixels.

| File | What it shows |
| --- | --- |
| [workspace.png](workspace.png) | Completed sales analysis, generated files, and the same report run in the cloud terminal |
| [plan-review.png](plan-review.png) | A pending native Plan review with approval, revision, and defer controls |
| [tool-calls.png](tool-calls.png) | Expanded Wasm PTC execution, its nested sandbox command, and command output |

Capture uses the application's session search to show only demonstration sessions. No settings, credentials, or existing user conversations are included. The UI and results have not been mocked or composited.

To reproduce a similar workspace, ask Nimbus to generate a sample sales CSV, analyze it with Python, and save a Markdown report under `/workspace`. For the Plan view, enable Plan in another session and ask for a short implementation plan with approval before execution. These interactions use your configured DeepSeek and E2B accounts.
