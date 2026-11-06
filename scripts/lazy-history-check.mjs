// Browser-only fixtures: no model, sandbox, or persisted conversation mutations.
import assert from "node:assert/strict";
import { chromium } from "playwright";

const base = process.env.NIMBUS_UI_URL || "http://127.0.0.1:8080";
const sid = "lazy-history-fixture";
const alternate = "lazy-history-alternate";
const session = (id) => ({
  id,
  title: id,
  options: { model: "deepseek-flash", thinking: "off", plan: false },
  agent_state: "idle",
  last_active_at: "2026-11-06T00:00:00Z",
  sandbox: { state: "not_started" },
});
const row = (n, outcome = "completed") => ({
  id: `o${n}`,
  turn_id: `t${n}`,
  client_msg_id: `m${n}`,
  content: `Question ${n}: ${"A readable conversation turn. ".repeat(12)}`,
  status: "delivered",
  outcome,
  error: "",
  delivered_seq: n * 10 + 1,
});
const summary = (n, text = `Final answer ${n}`) => ({
  turn_id: `t${n}`,
  final_text: text,
  has_details: true,
  tool_count: 1,
  failed_count: 0,
  duration: 2,
});
const pageData = (first, last, before) => ({
  outbox: Array.from({ length: last - first + 1 }, (_, i) => row(first + i)),
  turns: Array.from({ length: last - first + 1 }, (_, i) => summary(first + i)),
  questions: [],
  has_more: !!before,
  before_turn: before,
});
const event = (seq, type, data) => ({
  seq,
  type,
  payload: { time: 1793440000000 + seq * 1000, data },
});
const trace = (n, completed = true) => ({
  events: [
    event(n * 10, "turn/start", { turn: n }),
    event(n * 10 + 2, "tool/call", {
      callId: `call${n}`,
      name: "sandbox_exec",
      arguments: JSON.stringify({ command: `echo RAW_TRACE_${n}` }),
    }),
    ...(completed
      ? [
          event(n * 10 + 3, "tool/result", {
            message: {
              toolCallId: `call${n}`,
              content: [{ type: "text", text: `RESULT_${n}` }],
            },
          }),
          event(n * 10 + 4, "turn/end", { turn: n }),
        ]
      : []),
  ],
  frames: [],
  questions: [],
  cursor: { seq: n * 10 + (completed ? 4 : 2), frame: 0 },
});
const metadata = (n, completed = true) => ({
  tools: [
    {
      id: `call${n}`,
      name: "sandbox_exec",
      seq: n * 10 + 2,
      time: 1793440000000,
      complete: completed,
      failed: false,
      child_count: 0,
      args: { command: `echo RAW_TRACE_${n}` },
    },
  ],
  messages: [],
  attempts: [],
  pending_tools: [],
  questions: [],
  outbox: [row(n, completed ? "completed" : "")],
  cursor: { seq: n * 10 + (completed ? 4 : 2), frame: 0 },
});
const browser = await chromium.launch({ channel: "chrome" });
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
const errors = [];
const requests = [];
let releaseOlder;
let releaseStale;
let releaseTrace;
let failDetail = true;
let completedLive = false;
page.on("pageerror", (e) => errors.push(e.message));
await page.addInitScript(
  ({ sessions }) => {
    window.__streams = [];
    window.EventSource = class extends EventTarget {
      constructor(url) {
        super();
        this.url = new URL(url, location.href).href;
        this.closed = false;
        window.__streams.push(this);
        if (new URL(this.url).pathname === "/api/sessions/stream")
          setTimeout(
            () =>
              this.dispatchEvent(
                new MessageEvent("sessions", {
                  data: JSON.stringify(sessions),
                }),
              ),
            0,
          );
      }
      close() {
        this.closed = true;
      }
    };
    window.__sockets = [];
    window.WebSocket = class extends EventTarget {
      constructor(url) {
        super();
        this.url = url;
        this.readyState = 1;
        this.closed = false;
        window.__sockets.push(this);
      }
      send() {}
      close() {
        this.closed = true;
        this.readyState = 3;
      }
    };
  },
  { sessions: [session(sid), session(alternate)] },
);
await page.route("**/api/**", async (route) => {
  const url = new URL(route.request().url());
  requests.push(url.pathname + url.search);
  if (url.pathname === `/api/sessions/${sid}/events`) {
    const before = url.searchParams.get("before_turn");
    if (before === "t10") {
      await new Promise((resolve) => {
        releaseOlder = resolve;
      });
      return route.fulfill({ json: pageData(0, 9, "t0") });
    }
    if (before === "t0") {
      await new Promise((resolve) => {
        releaseStale = resolve;
      });
      return route.fulfill({ json: pageData(-5, -1, null) }).catch(() => {});
    }
    assert.equal(
      url.searchParams.has("after_seq"),
      false,
      "initial history must have no forward cursor",
    );
    assert.equal(url.searchParams.get("include_question_content"), "false");
    return route.fulfill({ json: pageData(10, 19, "t10") });
  }
  if (url.pathname === `/api/sessions/${alternate}/events`)
    return route.fulfill({
      json: {
        outbox: [{ ...row(100), content: "Alternate session only" }],
        turns: [summary(100)],
        questions: [],
        has_more: false,
        before_turn: null,
      },
    });
  const match = url.pathname.match(/\/turns\/t(\d+)\/events$/);
  if (match) {
    assert.equal(url.searchParams.get("include_questions"), "false");
    assert.equal(url.searchParams.get("include_outbox"), "false");
    const n = Number(match[1]);
    if (n === 10 && failDetail) {
      failDetail = false;
      return route.fulfill({
        status: 503,
        json: { error: "Fixture trace unavailable" },
      });
    }
    if (n === 11)
      await new Promise((resolve) => {
        releaseTrace = resolve;
      });
    if (url.searchParams.get("projection") === "metadata")
      return route
        .fulfill({ json: metadata(n, n !== 20 || completedLive) })
        .catch(() => {});
    if (url.searchParams.get("projection") === "headers") {
      assert.equal(url.searchParams.get("limit"), "100");
      if (url.searchParams.get("kind") === "frames")
        return route.fulfill({
          json: {
            events: [],
            frames: [{ id: 700, type: "tool-output", call_id: `call${n}` }],
            before: null,
            has_more: false,
            cursor: { seq: -1, frame: 700 },
          },
        });
      const before = url.searchParams.get("before");
      return route.fulfill({
        json: {
          events: before
            ? [
                { seq: n * 10, type: "turn/start" },
                { seq: n * 10 + 2, type: "tool/call" },
              ]
            : [
                { seq: n * 10 + 3, type: "tool/result" },
                { seq: n * 10 + 4, type: "turn/end" },
              ],
          frames: [],
          has_more: !before,
          before: before ? null : n * 10 + 3,
          cursor: { seq: before ? n * 10 + 2 : n * 10 + 4, frame: 0 },
        },
      });
    }
    assert.ok(
      url.searchParams.has("call_id") ||
        url.searchParams.has("seq") ||
        url.searchParams.has("attempt_id") ||
        url.searchParams.has("frame_id"),
      "raw detail must be explicitly scoped",
    );
    const snapshot = trace(n, n !== 20 || completedLive);
    const call = url.searchParams.get("call_id");
    if (url.searchParams.has("frame_id")) {
      snapshot.events = [];
      snapshot.frames = [
        {
          id: Number(url.searchParams.get("frame_id")),
          turn_id: `t${n}`,
          payload: {
            type: "tool-output",
            callId: `call${n}`,
            text: "FRAME_DETAIL_700",
          },
        },
      ];
      snapshot.cursor = {
        seq: -1,
        frame: Number(url.searchParams.get("frame_id")),
      };
    } else
      snapshot.events = snapshot.events.filter((e) =>
        url.searchParams.has("seq")
          ? e.seq === Number(url.searchParams.get("seq"))
          : e.payload.data.callId === call ||
            e.payload.data.message?.toolCallId === call,
      );
    return route.fulfill({ json: snapshot }).catch(() => {});
  }
  if (url.pathname === "/api/library")
    return route.fulfill({ json: { files: [] } });
  return route.fulfill({
    status: 404,
    json: { error: `Unexpected fixture request: ${url.pathname}` },
  });
});
const trigger = (owner) =>
  owner.locator(":scope > .accordion__item > .accordion__heading > button");
const processFor = (turn) =>
  page.locator(`[data-turn-id="${turn}"] .execution-process`);
const countHistory = () =>
  requests.filter((path) => path.startsWith(`/api/sessions/${sid}/events`))
    .length;
const countTrace = (turn) =>
  requests.filter(
    (path) =>
      path.includes(`/turns/${turn}/events`) &&
      path.includes("projection=metadata"),
  ).length;
const countRaw = (turn) =>
  requests.filter(
    (path) =>
      path.includes(`/turns/${turn}/events`) && !path.includes("projection="),
  ).length;
const emit = async (suffix, type, data, id = "") =>
  page.evaluate(
    ({ suffix, type, data, id }) => {
      const source = window.__streams.findLast(
        (s) =>
          new URL(s.url).pathname.endsWith(suffix) &&
          !s.closed &&
          (type !== "frame" ||
            new URL(s.url).searchParams.get("call_id") === data.payload.callId),
      );
      if (!source) throw Error(`No open fixture stream ${suffix}`);
      source.dispatchEvent(
        new MessageEvent(type, { data: JSON.stringify(data), lastEventId: id }),
      );
    },
    { suffix, type, data, id },
  );
const waitUntil = async (predicate) => {
  for (let i = 0; i < 100 && !predicate(); i++) await page.waitForTimeout(25);
  assert.ok(predicate());
};
try {
  await page.goto(`${base}/#${sid}`);
  await page.getByText("Final answer 19", { exact: true }).waitFor();
  await page.waitForTimeout(250);
  assert.equal(countHistory(), 1);
  assert.equal(
    requests.some((path) => path.includes("/turns/")),
    false,
    "completed processes must not fetch traces while idle",
  );
  assert.equal(
    await trigger(processFor("t10")).getAttribute("aria-expanded"),
    "false",
  );
  assert.ok(
    await page
      .locator(".conversation")
      .evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight < 5),
  );

  // A programmatic scroll is not an upward user gesture and must not paginate.
  await page.locator(".conversation").evaluate((el) => {
    el.scrollTop = 80;
  });
  await page.waitForTimeout(100);
  assert.equal(countHistory(), 1);
  await page.locator(".conversation").hover();
  await page.mouse.wheel(0, -1);
  await waitUntil(() => !!releaseOlder);
  await page.mouse.wheel(0, -1);
  assert.equal(countHistory(), 2, "pagination is single-flight");
  // New live turns must survive an in-flight older page.
  await emit(`/${sid}/stream`, "outbox", [row(20, "")]);
  await emit(`/${sid}/stream`, "turns", [summary(20, "")]);
  await waitUntil(() => countTrace("t20") === 1);
  assert.equal(
    countRaw("t20"),
    0,
    "active closed tool bodies must not fetch raw trace",
  );
  const anchorBefore = await page
    .locator('[data-turn-id="t10"]')
    .evaluate((el) => el.getBoundingClientRect().top);
  releaseOlder();
  await page.locator('[data-turn-id="t0"]').waitFor();
  const anchorAfter = await page
    .locator('[data-turn-id="t10"]')
    .evaluate((el) => el.getBoundingClientRect().top);
  assert.ok(
    Math.abs(anchorAfter - anchorBefore) < 3,
    `prepend moved reading anchor ${anchorAfter - anchorBefore}px`,
  );
  assert.equal(await page.locator('[data-turn-id="t20"]').count(), 1);
  assert.equal(countHistory(), 2, "prepend must not auto-drain more pages");

  // Completed detail errors/retry are independent from the visible final answer.
  await trigger(processFor("t10")).click();
  await page.getByText("Fixture trace unavailable", { exact: false }).waitFor();
  assert.equal(
    await page.getByText("Final answer 10", { exact: true }).count(),
    1,
  );
  await page
    .getByRole("button", { name: "Retry execution details", exact: true })
    .click();
  const tool = page.locator('[data-tool-id="call10"]');
  await tool.waitFor();
  assert.equal(
    countRaw("t10"),
    0,
    "opening a process loads metadata only, not collapsed tool bodies",
  );
  await trigger(tool.locator(":scope > .tool-disclosure")).click();
  await tool
    .locator(".tool-detail")
    .getByText("echo RAW_TRACE_10", { exact: true })
    .waitFor();
  assert.equal(countRaw("t10"), 1);
  assert.equal(countTrace("t10"), 2);
  await trigger(processFor("t10")).click();
  await trigger(processFor("t10")).click();
  assert.equal(
    countTrace("t10"),
    2,
    "completed detail cache must survive reopen",
  );

  // Closing an active process stops its stream. Frame-only cursor progress must
  // survive reopening, independently of the native event sequence.
  const liveTool = page.locator('[data-tool-id="call20"]');
  await trigger(liveTool.locator(":scope > .tool-disclosure")).click();
  await waitUntil(() => countRaw("t20") === 1);
  await page.waitForFunction(() =>
    window.__streams.some(
      (s) => new URL(s.url).searchParams.get("call_id") === "call20",
    ),
  );
  await emit(
    "/turns/t20/stream",
    "frame",
    {
      id: 9,
      turn_id: "t20",
      payload: {
        type: "tool-output",
        callId: "call20",
        text: "STREAMED_OUTPUT",
      },
    },
    "202:9",
  );
  await trigger(processFor("t20")).click();
  assert.equal(
    await page.evaluate(() =>
      window.__streams
        .filter((s) => s.url.includes("/turns/t20/stream"))
        .every((s) => s.closed),
    ),
    true,
  );
  await trigger(processFor("t20")).click();
  await page.waitForFunction(
    () =>
      window.__streams.filter(
        (s) => new URL(s.url).searchParams.get("call_id") === "call20",
      ).length === 2,
  );
  const reopened = new URL(
    await page.evaluate(
      () =>
        window.__streams.findLast(
          (s) => new URL(s.url).searchParams.get("call_id") === "call20",
        ).url,
    ),
  );
  assert.equal(reopened.searchParams.get("after_seq"), "202");
  assert.equal(reopened.searchParams.get("after_frame"), "9");
  assert.equal(countRaw("t20"), 1);

  // Main summary completion may beat detail SSE delivery: fetch a final snapshot.
  completedLive = true;
  await emit(`/${sid}/stream`, "outbox", [row(20)]);
  await emit(`/${sid}/stream`, "turns", [summary(20, "Live final answer")]);
  await waitUntil(() => countTrace("t20") === 2);
  await page.getByText("Live final answer", { exact: true }).waitFor();
  await waitUntil(() => countRaw("t20") === 2);
  await liveTool.getByText("RESULT_20", { exact: true }).waitFor();

  // An expanded trace and an older page both in flight must be ignored after a
  // session switch, and every old stream must be closed.
  await trigger(processFor("t11")).click();
  await waitUntil(() => !!releaseTrace);
  await page.locator(".conversation").evaluate((el) => {
    el.scrollTop = 30;
  });
  await page.locator(".conversation").hover();
  await page.mouse.wheel(0, -1);
  await waitUntil(() => !!releaseStale);
  await page
    .locator(".session-list .session-item")
    .filter({ hasText: alternate })
    .click();
  await page.getByText("Alternate session only", { exact: true }).waitFor();
  releaseTrace();
  releaseStale();
  await page.waitForTimeout(150);
  assert.equal(await page.locator('[data-turn-id="t11"]').count(), 0);
  assert.equal(await page.locator('[data-turn-id="t-1"]').count(), 0);
  assert.equal(
    await page.evaluate(
      (sid) =>
        window.__streams
          .filter((s) => s.url.includes(`/sessions/${sid}/`))
          .every((s) => s.closed),
      sid,
    ),
    true,
  );
  // CSS-hidden mobile panels must not keep their queries or terminal socket.
  await page.setViewportSize({ width: 390, height: 844 });
  const runningSession = {
    ...session(alternate),
    sandbox: { state: "running" },
  };
  await emit("/sessions/stream", "sessions", [session(sid), runningSession]);
  await page.evaluate(
    () =>
      new Promise((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(resolve)),
      ),
  );
  assert.equal(await page.evaluate(() => window.__sockets.length), 0);
  const alternateLoads = requests.filter((path) =>
    path.startsWith(`/api/sessions/${alternate}/events`),
  ).length;
  await page
    .locator(".mobile-tabs")
    .getByRole("button", { name: "Terminal", exact: true })
    .click();
  await page.waitForFunction(() => window.__sockets.length === 1);
  assert.equal(
    await page.evaluate(
      (id) =>
        window.__streams
          .filter(
            (s) => new URL(s.url).pathname === `/api/sessions/${id}/stream`,
          )
          .every((s) => s.closed),
      alternate,
    ),
    true,
  );
  await page
    .locator(".mobile-tabs")
    .getByRole("button", { name: "Chat", exact: true })
    .click();
  await page.getByText("Alternate session only", { exact: true }).waitFor();
  assert.equal(
    await page.evaluate(() =>
      window.__sockets.every((socket) => socket.closed),
    ),
    true,
  );
  assert.equal(
    requests.filter((path) =>
      path.startsWith(`/api/sessions/${alternate}/events`),
    ).length,
    alternateLoads,
    "returning to chat reuses its page cache",
  );
  await page
    .locator(".mobile-tabs")
    .getByRole("button", { name: "Sessions", exact: true })
    .click();
  await page.locator(".library-nav").click();
  await waitUntil(() => requests.some((path) => path === "/api/library"));
  assert.equal(
    await page.evaluate(
      (id) =>
        window.__streams
          .filter(
            (s) => new URL(s.url).pathname === `/api/sessions/${id}/stream`,
          )
          .every((s) => s.closed),
      alternate,
    ),
    true,
  );
  await page
    .locator(".mobile-tabs")
    .getByRole("button", { name: "Sessions", exact: true })
    .click();
  await page
    .locator(".session-list .session-item")
    .filter({ hasText: alternate })
    .click();
  await page.getByText("Alternate session only", { exact: true }).waitFor();
  assert.equal(
    requests.filter((path) =>
      path.startsWith(`/api/sessions/${alternate}/events`),
    ).length,
    alternateLoads,
  );
  // Activity is header-only at every level; frame headers are a separate,
  // initially closed lane and individual records query exact identifiers.
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.getByRole("button", { name: "Activity", exact: true }).click();
  assert.equal(
    requests.some((path) => path.includes("projection=headers")),
    false,
  );
  const trajectory = page.locator(".trajectory-turn");
  await trigger(trajectory).click();
  await trajectory
    .locator(".trajectory-events .trajectory-record")
    .first()
    .waitFor();
  assert.equal(countRaw("t100"), 0);
  assert.equal(
    requests.some(
      (path) =>
        path.includes("projection=headers") && path.includes("kind=frames"),
    ),
    false,
  );
  await trajectory
    .getByRole("button", { name: "Load older events", exact: true })
    .click();
  await trajectory
    .getByRole("button", { name: "#1000 turn/start", exact: true })
    .waitFor();
  assert.equal(
    countRaw("t100"),
    0,
    "header pagination must not fetch record bodies",
  );
  const eventRecord = trajectory
    .locator(".trajectory-events .trajectory-record")
    .filter({
      has: page.getByRole("button", { name: "#1003 tool/result", exact: true }),
    });
  await trigger(eventRecord).click();
  await eventRecord.locator("pre").waitFor();
  assert.ok(
    (await eventRecord.locator("pre").textContent()).includes("RESULT_100"),
  );
  assert.equal(countRaw("t100"), 1);
  const frameSection = trajectory.locator(".trajectory-frames");
  await trigger(frameSection).click();
  const frameRecord = frameSection.locator(".trajectory-record");
  await frameRecord.waitFor();
  assert.equal(countRaw("t100"), 1);
  await trigger(frameRecord).click();
  await frameRecord.locator("pre").waitFor();
  assert.ok(
    (await frameRecord.locator("pre").textContent()).includes(
      "FRAME_DETAIL_700",
    ),
  );
  assert.equal(countRaw("t100"), 2);
  assert.deepEqual(errors, []);
  console.log(
    "PASS lazy history, stable upward pagination, metadata-only processes, tool cache/retry, composite cursors, completion catch-up, session isolation, hidden panels and paged header-only trajectory inspection",
  );
} finally {
  await browser.close();
}
