// Browser-only native event fixtures. No messages, tools, or sandbox operations
// are sent to the developer's application while testing disclosure behavior.
import assert from "node:assert/strict";
import { chromium } from "playwright";

const base = process.env.NIMBUS_UI_URL || "http://127.0.0.1:8080";
const sid = "tool-ui-fixture";
const session = {
  id: sid,
  title: "工具调用展示验证",
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
    callId: "root",
    name: "run_code",
    arguments: JSON.stringify({
      description: "检查项目文件与构建结果",
      code: "const file = await tools.sandbox_read_file({path:'/workspace/README.md'});\nconsole.log(file);",
    }),
  }),
  event(3, "tool/ptc-dispatch-start", {
    subCallId: "read",
    rootCallId: "root",
    parentCallId: "root",
    name: "sandbox_read_file",
    arguments: { path: "/workspace/README.md" },
  }),
  event(4, "tool/ptc-dispatch", {
    subCallId: "read",
    rootCallId: "root",
    parentCallId: "root",
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
    parentCallId: "root",
    name: "sandbox_exec",
    arguments: { command: "npm run check", cwd: "/workspace" },
  }),
  event(6, "tool/ptc-dispatch", {
    subCallId: "exec",
    parentCallId: "root",
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
      toolCallId: "root",
      content: [{ type: "text", text: "检查完成" }],
    },
  }),
  event(8, "assistant/message", {
    step: 1,
    turn: 1,
    message: {
      content: [{ type: "text", text: "已读取项目文档。检查脚本尚未配置。" }],
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
    content: "检查项目文件",
    delivered_seq: 1,
    status: "delivered",
    outcome: "completed",
  },
  {
    id: "b",
    turn_id: "b",
    content: "构建项目",
    delivered_seq: 11,
    status: "delivered",
    outcome: "",
  },
  {
    id: "c",
    turn_id: "c",
    content: "停止命令",
    delivered_seq: 21,
    status: "delivered",
    outcome: "interrupted",
  },
];
const browser = await chromium.launch({ channel: "chrome" });
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
const errors = [];
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
  const path = new URL(route.request().url()).pathname;
  if (path === "/api/sessions/stream")
    return route.fulfill({
      contentType: "text/event-stream",
      body: `event: sessions\ndata: ${JSON.stringify([session])}\n\n`,
    });
  if (path.endsWith("/stream"))
    return route.fulfill({
      contentType: "text/event-stream",
      body: ": heartbeat\n\n",
    });
  if (path.endsWith("/events"))
    return route.fulfill({
      json: { events, outbox, frames: [], questions: [], has_more: false },
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
      .getByText("已读取项目文档。检查脚本尚未配置。", { exact: true })
      .isVisible(),
    true,
  );
  await trigger(groups.nth(0)).click();
  const root = page.locator('[data-tool-id="root"]');
  await rowTrigger(root).click();
  assert.equal(await root.locator(".tool-children > .tool-call").count(), 2);
  const read = page.locator('[data-tool-id="read"]');
  await rowTrigger(read).focus();
  await page.keyboard.press("Enter");
  assert.equal(
    await read
      .getByText("# Nimbus\nA cloud workspace.", { exact: true })
      .isVisible(),
    true,
  );
  await read.getByRole("button", { name: "复制文件内容", exact: true }).click();
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
  assert.equal(
    await exec.getByText("退出码 1", { exact: true }).isVisible(),
    true,
  );
  await root.scrollIntoViewIfNeeded();
  await page.screenshot({
    path: ".local/tool-ui-expanded.png",
    fullPage: true,
  });
  // Updates arrive through the same EventSource listeners as the real app.
  await page.evaluate(() => {
    const stream = window.__streams.find((s) =>
      new URL(s.url).pathname.endsWith("/tool-ui-fixture/stream"),
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
  const live = page.locator('[data-tool-id="live"]');
  await rowTrigger(live).click();
  assert.equal(
    await live.getByText("LIVE_OUTPUT", { exact: true }).isVisible(),
    true,
  );
  await rowTrigger(live).click();
  await trigger(groups.nth(1)).click();
  await page.evaluate(() => {
    const stream = window.__streams.find((s) =>
      new URL(s.url).pathname.endsWith("/tool-ui-fixture/stream"),
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
  assert.deepEqual(errors, []);
  console.log(
    "PASS grouped/individual folds, nested PTC, native failure states, copy, keyboard, SSE updates, stopped calls, mobile layout",
  );
} finally {
  await browser.close();
}
