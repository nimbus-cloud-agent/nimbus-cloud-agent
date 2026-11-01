# Documentation media

These screenshots and the recording were captured from the running Nimbus application at `http://127.0.0.1:8080` on 2026-11-01. They show real DeepSeek Flash / E2B interactions in dedicated demonstration sessions, with synthetic sales data. The interface is Chinese; screenshots are 1600 × 1100 pixels.

| File | What it shows |
| --- | --- |
| [workspace.png](workspace.png) | Completed sales analysis, generated files, and the same report run in the cloud terminal |
| [plan-review.png](plan-review.png) | A pending native Plan review with approval, revision, and defer controls |
| [tool-calls.png](tool-calls.png) | Expanded Wasm PTC execution, its nested sandbox command, and command output |
| [usage.gif](usage.gif) | Inline preview: 30 seconds, 1120 × 770, 6 fps, looping |
| [usage.mp4](usage.mp4) | Full-resolution recording: 30 seconds, 1600 × 1100, 25 fps, H.264, no audio |

The recording shows approval of the pending plan and its resulting file, then switches to the sales session to expand nested tool calls and run `analyze.py` in the terminal. Initial loading and part of the model wait are trimmed; playback speed is unchanged. The GIF and MP4 contain the same sequence. Raw recordings and temporary capture files are not committed.

Capture uses the application's session search to show only demonstration sessions. No settings, credentials, or existing user conversations are included. The UI and results have not been mocked or composited.

To reproduce a similar workspace, ask Nimbus to generate a sample sales CSV, analyze it with Python, and save a Markdown report under `/workspace`. For the Plan view, enable Plan in another session and ask for a short implementation plan with approval before execution. These interactions use your configured DeepSeek and E2B accounts.
