// Browser-only native event fixtures. No messages, tools, or sandbox operations
// are sent to the developer's application while testing disclosure behavior.
import assert from "node:assert/strict";
import { chromium } from "playwright";

const base = process.env.NIMBUS_UI_URL || "http://127.0.0.1:8080";
const sid = "tool-ui-fixture";
const libraryId = "e60ce60b-3e7a-4c41-8839-1db6f60cc2e9";
const session = {
  id: sid,
  title: "Tool call display verification",
  options: {
    mode: "ptc",
    model: "deepseek-flash",
    thinking: "off",
    plan: false,
  },
  mode_locked: true,
  agent_state: "running",
  last_active_at: new Date().toISOString(),
  sandbox: { state: "not_started" },
};
const event = (seq, type, data) => ({
  seq,
  type,
  payload: { seq, type, time: 1793440000000 + seq * 1000, data },
});
const events = [
  event(0, "turn/start", { turn: 1 }),
  event(1, "user/message", { content: [] }),
  event(2, "tool/call", {
    callId: "root-call",
    name: "run_code",
    arguments: JSON.stringify({
      description: "Inspect project files and build results",
      code: "const file = await tools.sandbox_read_file({path:'/workspace/README.md'});\nconsole.log(file);",
    }),
  }),
  event(3, "tool/ptc-dispatch-start", {
    subCallId: "read",
    rootCallId: "root-call",
    parentCallId: "root-call",
    name: "sandbox_read_file",
    arguments: { path: "/workspace/README.md" },
  }),
  event(4, "tool/ptc-dispatch", {
    subCallId: "read",
    rootCallId: "root-call",
    parentCallId: "root-call",
    name: "sandbox_read_file",
    arguments: { path: "/workspace/README.md" },
    content: [
      {
        type: "text",
        text: JSON.stringify({ content: "# Nimbus\nA cloud workspace." }),
      },
    ],
  }),
  event(5, "tool/ptc-dispatch-start", {
    subCallId: "exec",
    parentCallId: "root-call",
    name: "sandbox_exec",
    arguments: { command: "npm run check", cwd: "/workspace" },
  }),
  event(6, "tool/ptc-dispatch", {
    subCallId: "exec",
    parentCallId: "root-call",
    name: "sandbox_exec",
    arguments: { command: "npm run check", cwd: "/workspace" },
    content: [
      {
        type: "text",
        text: JSON.stringify({
          stdout: "Checking project…",
          stderr: "Missing script: check",
          exitCode: 1,
        }),
      },
    ],
  }),
  event(7, "tool/result", {
    message: {
      toolCallId: "root-call",
      content: [{ type: "text", text: "Inspection complete" }],
    },
  }),
  event(8, "assistant/message", {
    step: 1,
    turn: 1,
    message: {
      content: [
        {
          type: "text",
          text: `Read the project documentation. No inspection script is configured yet.\n\n[Preview saved page](nimbus://library/${libraryId})\n\n[Unsafe link](javascript:alert(1)) [Invalid Library link](nimbus://library/not-an-id)`,
        },
      ],
    },
  }),
  event(9, "turn/end", { turn: 1 }),
  event(10, "turn/start", { turn: 2 }),
  event(11, "user/message", { content: [] }),
  event(12, "tool/call", {
    callId: "live",
    name: "sandbox_exec",
    arguments: JSON.stringify({ command: "npm run build" }),
  }),
  event(20, "turn/start", { turn: 3 }),
  event(21, "user/message", { content: [] }),
  event(22, "tool/call", {
    callId: "stopped",
    name: "sandbox_exec",
    arguments: '{"command":"sleep 30"}',
  }),
  event(23, "turn/end", { turn: 3 }),
];
const outbox = [
  {
    id: "a",
    turn_id: "a",
    content: "Inspect project files",
    delivered_seq: 1,
    status: "delivered",
    outcome: "completed",
  },
  {
    id: "b",
    turn_id: "b",
    content: "Build the project",
    delivered_seq: 11,
    status: "delivered",
    outcome: "",
  },
  {
    id: "c",
    turn_id: "c",
    content: "Stop command",
    delivered_seq: 21,
    status: "delivered",
    outcome: "interrupted",
  },
];
const turns = outbox.map((row, i) => ({
  turn_id: row.turn_id,
  final_text:
    i === 0
      ? events.find((e) => e.seq === 8).payload.data.message.content[0].text
      : "",
  has_details: true,
  tool_count: i === 0 ? 3 : 1,
  failed_count: i === 0 ? 1 : 0,
}));
const frames = [];
const turnRows = (turn) => {
  const index = outbox.findIndex((row) => row.turn_id === turn);
  const bounds = [
    [0, 10],
    [10, 20],
    [20, Infinity],
  ][index];
  return events.filter((e) => e.seq >= bounds[0] && e.seq < bounds[1]);
};
const metadata = (turn, parent = "root") => {
  const rows = turnRows(turn);
  const calls = rows.filter((e) =>
    ["tool/call", "tool/ptc-dispatch-start"].includes(e.type),
  );
  const tools = calls.map((e) => {
    const d = e.payload.data;
    const id = d.subCallId ?? d.callId;
    const result = rows.find((r) =>
      r.type === "tool/result"
        ? r.payload.data.message?.toolCallId === id
        : r.type === "tool/ptc-dispatch" && r.payload.data.subCallId === id,
    );
    const args =
      typeof d.arguments === "string" ? JSON.parse(d.arguments) : d.arguments;
    return {
      id,
      name: d.name,
      seq: e.seq,
      parent: d.parentCallId ?? d.rootCallId,
      time: e.payload.time,
      ended: result?.payload.time,
      complete: !!result,
      failed: id === "exec",
      child_count: calls.filter(
        (c) =>
          (c.payload.data.parentCallId ?? c.payload.data.rootCallId) === id,
      ).length,
      args: Object.fromEntries(
        Object.entries(args ?? {}).filter(([key]) =>
          ["description", "path", "command", "query", "name"].includes(key),
        ),
      ),
    };
  });
  return {
    tools: tools.filter((tool) =>
      parent === "root" ? !tool.parent : tool.parent === parent,
    ),
    messages:
      parent === "root"
        ? rows
            .filter((e) => e.type === "assistant/message")
            .map((e) => ({
              seq: e.seq,
              text: e.payload.data.message.content[0].text,
              has_reasoning: false,
            }))
        : [],
    attempts: [],
    pending_tools:
      parent === "root"
        ? [
            ...new Map(
              frames
                .filter(
                  (f) =>
                    f.turn_id === turn &&
                    f.payload.chunk?.type === "tool-call-delta" &&
                    !tools.some((t) => t.id === f.payload.chunk.id),
                )
                .map((f) => [
                  f.payload.chunk.id,
                  {
                    id: f.payload.chunk.id,
                    name: f.payload.chunk.name ?? "run_code",
                  },
                ]),
            ).values(),
          ]
        : [],
    outbox: outbox.filter((o) => o.turn_id === turn),
    questions: [],
    cursor: { seq: rows.at(-1)?.seq ?? -1, frame: frames.at(-1)?.id ?? 0 },
  };
};
const browser = await chromium.launch({ channel: "chrome" });
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
const errors = [];
const requests = [];
page.on("pageerror", (e) => errors.push(e.message));
await page
  .context()
  .grantPermissions(["clipboard-read", "clipboard-write"], { origin: base });
await page.addInitScript(() => {
  window.__streams = [];
  const Original = window.EventSource;
  window.EventSource = class extends Original {
    constructor(...args) {
      super(...args);
      window.__streams.push(this);
    }
  };
});
await page.route("**/api/**", (route) => {
  const url = new URL(route.request().url());
  requests.push(url);
  const path = url.pathname;
  if (path === "/api/sessions/stream")
    return route.fulfill({
      contentType: "text/event-stream",
      body: `event: sessions\ndata: ${JSON.stringify([session])}\n\n`,
    });
  const turnMatch = path.match(/\/turns\/([^/]+)\/(events|stream)$/);
  if (turnMatch) {
    const turn = turnMatch[1];
    const call = url.searchParams.get("call_id");
    if (url.searchParams.get("projection") === "metadata") {
      const snapshot = metadata(
        turn,
        url.searchParams.get("parent_call_id") ?? "root",
      );
      return turnMatch[2] === "stream"
        ? route.fulfill({
            contentType: "text/event-stream",
            body: `event: metadata\ndata: ${JSON.stringify(snapshot)}\n\n`,
          })
        : route.fulfill({ json: snapshot });
    }
    const trace = (
      url.searchParams.has("chunk_types") ? [] : turnRows(turn)
    ).filter(
      (e) =>
        !call ||
        [
          e.payload.data.callId,
          e.payload.data.subCallId,
          e.payload.data.message?.toolCallId,
        ].includes(call),
    );
    const frameTypes = new Set(
      (url.searchParams.get("frame_types") ?? "").split(",").filter(Boolean),
    );
    const chunkTypes = new Set(
      (url.searchParams.get("chunk_types") ?? "").split(",").filter(Boolean),
    );
    const traceFrames = frames.filter(
      (f) =>
        f.turn_id === turn &&
        (!call || f.payload.callId === call || f.payload.chunk?.id === call) &&
        (!frameTypes.size || frameTypes.has(f.payload.type)) &&
        (!chunkTypes.size || chunkTypes.has(f.payload.chunk?.type)),
    );
    if (turnMatch[2] === "stream") {
      const native = trace
        .filter((e) => e.seq > Number(url.searchParams.get("after_seq") ?? -1))
        .map(
          (e) =>
            `event: session\nid: ${e.seq}:${Number(url.searchParams.get("after_frame") ?? 0)}\ndata: ${JSON.stringify(e)}\n\n`,
        )
        .join("");
      const liveFrames = traceFrames
        .filter((f) => f.id > Number(url.searchParams.get("after_frame") ?? 0))
        .map(
          (f) =>
            `event: frame\nid: ${trace.at(-1)?.seq ?? -1}:${f.id}\ndata: ${JSON.stringify(f)}\n\n`,
        )
        .join("");
      return route.fulfill({
        contentType: "text/event-stream",
        body: native + liveFrames || ": heartbeat\n\n",
      });
    }
    return route.fulfill({
      json: {
        events: trace,
        frames: traceFrames,
        questions: [],
        cursor: {
          seq: trace.at(-1)?.seq ?? -1,
          frame: traceFrames.at(-1)?.id ?? 0,
        },
      },
    });
  }
  if (path.endsWith("/stream"))
    return route.fulfill({
      contentType: "text/event-stream",
      body: ": heartbeat\n\n",
    });
  if (path.endsWith("/events"))
    return route.fulfill({
      json: {
        turns,
        outbox,
        questions: [],
        has_more: false,
        before_turn: null,
      },
    });
  if (path === `/api/library/${libraryId}`)
    return route.fulfill({
      json: {
        id: libraryId,
        name: "preview.html",
        mime_type: "text/html",
        size_bytes: 200,
        version: 1,
        updated_at: "2026-11-06T06:05:36Z",
        versions: [
          { version: 1, size_bytes: 200, created_at: "2026-11-06T06:05:36Z" },
        ],
      },
    });
  if (path === `/api/library/${libraryId}/content`)
    return route.fulfill({
      contentType: "text/html",
      headers: { "Content-Security-Policy": "sandbox allow-scripts" },
      body: '<style>#preview-content{opacity:0}</style><h1 id="preview-content">Interactive preview</h1><script>document.getElementById("preview-content").style.opacity="1";</script>',
    });
  return route.fulfill({
    status: 404,
    json: { error: "Unexpected test request" },
  });
});
const trigger = (owner) =>
  owner.locator(":scope > .accordion__item > .accordion__heading > button");
const rowTrigger = (owner) =>
  trigger(owner.locator(":scope > .tool-disclosure"));
try {
  await page.goto(base + "/#" + sid);
  const groups = page.locator(".execution-process");
  await groups.nth(2).waitFor();
  assert.equal(
    await trigger(groups.nth(0)).getAttribute("aria-expanded"),
    "false",
  );
  assert.equal(
    await trigger(groups.nth(1)).getAttribute("aria-expanded"),
    "true",
  );
  assert.equal(
    await page
      .getByText(
        "Read the project documentation. No inspection script is configured yet.",
        { exact: true },
      )
      .isVisible(),
    true,
  );
  assert.equal(
    requests.some((url) => url.pathname.includes("/turns/a/")),
    false,
    "closed completed process must not request metadata or trace",
  );
  await trigger(groups.nth(0)).click();
  const root = page.locator('[data-tool-id="root-call"]');
  await root.waitFor();
  assert.equal(
    requests.some(
      (url) =>
        url.pathname.endsWith("/turns/a/events") &&
        !url.searchParams.has("projection"),
    ),
    false,
    "process expansion must not fetch collapsed tool bodies",
  );
  await rowTrigger(root).click();
  await root.locator(".tool-children > .tool-call").nth(1).waitFor();
  assert.equal(await root.locator(".tool-children > .tool-call").count(), 2);
  assert.equal(
    requests.some((url) =>
      ["read", "exec"].includes(url.searchParams.get("call_id")),
    ),
    false,
    "opening a parent fetches child headers, never child results",
  );
  const read = page.locator('[data-tool-id="read"]');
  await rowTrigger(read).focus();
  await page.keyboard.press("Enter");
  await read
    .getByText("# Nimbus\nA cloud workspace.", { exact: true })
    .waitFor();
  assert.equal(
    await read
      .getByText("# Nimbus\nA cloud workspace.", { exact: true })
      .isVisible(),
    true,
  );
  await read
    .getByRole("button", { name: "Copy File contents", exact: true })
    .click();
  assert.equal(
    await page.evaluate(() => navigator.clipboard.readText()),
    "# Nimbus\nA cloud workspace.",
  );
  await rowTrigger(read).focus();
  await page.keyboard.press("Space");
  assert.equal(await rowTrigger(read).getAttribute("aria-expanded"), "false");
  const exec = page.locator('[data-tool-id="exec"]');
  assert.equal(await exec.getAttribute("data-state"), "error");
  await rowTrigger(exec).click();
  await exec.getByText("Exit code 1", { exact: true }).waitFor();
  assert.equal(
    await exec.getByText("Exit code 1", { exact: true }).isVisible(),
    true,
  );
  await root.scrollIntoViewIfNeeded();
  await page.screenshot({
    path: ".local/tool-ui-expanded.png",
    fullPage: true,
  });
  // Updates arrive through the same EventSource listeners as the real app.
  const live = page.locator('[data-tool-id="live"]');
  await rowTrigger(live).click();
  await page.waitForFunction(() =>
    window.__streams.some(
      (s) => new URL(s.url).searchParams.get("call_id") === "live",
    ),
  );
  await page.evaluate(() => {
    const stream = window.__streams.findLast(
      (s) => new URL(s.url).searchParams.get("call_id") === "live",
    );
    stream.dispatchEvent(
      new MessageEvent("frame", {
        data: JSON.stringify({
          id: 99,
          turn_id: "b",
          payload: { type: "tool-output", callId: "live", text: "LIVE_OUTPUT" },
        }),
      }),
    );
  });
  assert.equal(await rowTrigger(root).getAttribute("aria-expanded"), "true");
  assert.equal(
    await live.getByText("LIVE_OUTPUT", { exact: true }).isVisible(),
    true,
  );
  await rowTrigger(live).click();
  await trigger(groups.nth(1)).click();
  await page.evaluate(() => {
    const stream = window.__streams.findLast(
      (s) => new URL(s.url).searchParams.get("call_id") === "live",
    );
    stream.dispatchEvent(
      new MessageEvent("frame", {
        data: JSON.stringify({
          id: 100,
          turn_id: "b",
          payload: { type: "tool-output", callId: "live", text: "MORE_OUTPUT" },
        }),
      }),
    );
  });
  assert.equal(
    await trigger(groups.nth(1)).getAttribute("aria-expanded"),
    "false",
  );
  await trigger(groups.nth(2)).click();
  assert.equal(
    await page.locator('[data-tool-id="stopped"]').getAttribute("data-state"),
    "stopped",
  );
  await page.setViewportSize({ width: 390, height: 844 });
  await root.scrollIntoViewIfNeeded();
  assert.equal(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
    true,
  );
  await page.screenshot({ path: ".local/tool-ui-mobile.png", fullPage: true });
  await rowTrigger(root).click();
  assert.equal(await root.locator(".tool-detail").count(), 0);
  await trigger(groups.nth(1)).click();
  // Partial JSON must be readable before the model finishes the call, survive
  // further deltas and user folding, and be replaced by the native tool row.
  await page.setViewportSize({ width: 1440, height: 1000 });
  const argumentsText = JSON.stringify({
    code: 'console.log("<img src=x onerror=alert(1)>");\nconsole.log("中文");',
    description: "Inspect streamed arguments",
  });
  const split = argumentsText.indexOf("\\n") + 1;
  const prefix = argumentsText.slice(0, split);
  frames.push({
    id: 101,
    turn_id: "b",
    payload: {
      type: "chunk",
      attemptId: "pending",
      chunk: {
        type: "tool-call-delta",
        id: "incoming",
        name: "run_code",
        argumentsDelta: prefix,
      },
    },
  });
  await page.evaluate((snapshot) => {
    const stream = window.__streams.findLast(
      (s) =>
        new URL(s.url).pathname.endsWith("/turns/b/stream") &&
        new URL(s.url).searchParams.get("parent_call_id") === "root",
    );
    stream.dispatchEvent(
      new MessageEvent("metadata", { data: JSON.stringify(snapshot) }),
    );
  }, metadata("b"));
  const incoming = page.locator('[data-tool-id="incoming"]');
  await incoming.locator("pre").waitFor();
  await page.waitForFunction(
    (text) =>
      document.querySelector('[data-tool-id="incoming"] pre')?.textContent ===
      text,
    prefix,
  );
  assert.equal(await incoming.locator("pre").textContent(), prefix);
  assert.equal(await incoming.locator("pre img").count(), 0);
  await incoming
    .getByRole("button", {
      name: "Copy Arguments (streaming JSON)",
      exact: true,
    })
    .click();
  assert.equal(
    await page.evaluate(() => navigator.clipboard.readText()),
    prefix,
  );
  await rowTrigger(incoming).click();
  frames.push({
    id: 102,
    turn_id: "b",
    payload: {
      type: "chunk",
      attemptId: "pending",
      chunk: {
        type: "tool-call-delta",
        id: "incoming",
        argumentsDelta: argumentsText.slice(split),
      },
    },
  });
  await page.evaluate((argumentsDelta) => {
    const stream = window.__streams.findLast(
      (s) => new URL(s.url).searchParams.get("call_id") === "incoming",
    );
    stream.dispatchEvent(
      new MessageEvent("frame", {
        data: JSON.stringify({
          id: 102,
          turn_id: "b",
          payload: {
            type: "chunk",
            attemptId: "pending",
            chunk: {
              type: "tool-call-delta",
              id: "incoming",
              argumentsDelta,
            },
          },
        }),
      }),
    );
  }, argumentsText.slice(split));
  assert.equal(
    await rowTrigger(incoming).getAttribute("aria-expanded"),
    "false",
  );
  await rowTrigger(incoming).click();
  await page.waitForFunction(
    (text) =>
      document.querySelector('[data-tool-id="incoming"] pre')?.textContent ===
      text,
    argumentsText,
  );
  await page.setViewportSize({ width: 390, height: 844 });
  await incoming.scrollIntoViewIfNeeded();
  assert.equal(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
    true,
  );
  await page.screenshot({
    path: ".local/tool-arguments-streaming.png",
    fullPage: true,
  });
  events.push(
    event(13, "tool/call", {
      callId: "incoming",
      name: "run_code",
      arguments: argumentsText,
    }),
  );
  await page.evaluate((snapshot) => {
    const stream = window.__streams.findLast(
      (s) =>
        new URL(s.url).pathname.endsWith("/turns/b/stream") &&
        new URL(s.url).searchParams.get("parent_call_id") === "root",
    );
    stream.dispatchEvent(
      new MessageEvent("metadata", { data: JSON.stringify(snapshot) }),
    );
  }, metadata("b"));
  await page.waitForFunction(
    () => !document.querySelector('[data-tool-id="incoming"][data-streaming]'),
  );
  if ((await trigger(groups.nth(1)).getAttribute("aria-expanded")) === "false")
    await trigger(groups.nth(1)).click();
  assert.equal(await incoming.count(), 1);
  if ((await rowTrigger(incoming).getAttribute("aria-expanded")) === "false")
    await rowTrigger(incoming).click();
  await incoming.locator("pre").waitFor();
  assert.equal(
    await incoming.locator("pre").textContent(),
    JSON.parse(argumentsText).code,
  );
  assert.equal(
    await page
      .getByRole("link", { name: "Unsafe link", exact: true })
      .getAttribute("href"),
    "",
  );
  assert.equal(
    await page
      .getByRole("link", { name: "Invalid Library link", exact: true })
      .getAttribute("href"),
    "",
  );
  await page
    .getByRole("link", { name: "Preview saved page", exact: true })
    .click();
  const preview = page.frameLocator(".library-preview-frame");
  await preview.locator("#preview-content").waitFor();
  assert.equal(
    await preview
      .locator("#preview-content")
      .evaluate((el) => getComputedStyle(el).opacity),
    "1",
  );
  const previewFrame = await page
    .locator(".library-preview-frame")
    .elementHandle();
  const inner = await previewFrame.contentFrame();
  assert.equal(
    await inner.evaluate(() => {
      try {
        return !!parent.document;
      } catch {
        return false;
      }
    }),
    false,
  );
  await page
    .locator(".library-preview-dialog")
    .getByRole("button", { name: "Close", exact: true })
    .filter({ hasText: "Close" })
    .click();
  assert.deepEqual(errors, []);
  console.log(
    "PASS tool disclosures, stream growth/handoff, mobile layout, clickable Library links, rejected unsafe URLs and isolated interactive preview",
  );
} finally {
  await browser.close();
}
