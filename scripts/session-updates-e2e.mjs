import assert from "node:assert/strict";
import { chromium } from "playwright";

const base = "http://127.0.0.1:8080";
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
async function request(path, body, method = body ? "POST" : "GET") {
  const r = await fetch(base + "/api" + path, {
    method,
    headers: { "Content-Type": "application/json" },
    body: body ? JSON.stringify(body) : undefined,
  });
  return { status: r.status, data: r.status === 204 ? null : await r.json() };
}
async function api(...args) {
  const r = await request(...args);
  if (r.status >= 400) throw Error(JSON.stringify(r));
  return r.data;
}
async function until(fn, timeout = 15000) {
  const start = Date.now();
  while (Date.now() - start < timeout) {
    const result = await fn();
    if (result) return result;
    await sleep(150);
  }
  throw Error("condition timed out");
}
async function stream(path) {
  const controller = new AbortController(),
    frames = [];
  const response = await fetch(base + "/api" + path, {
    signal: controller.signal,
  });
  assert.equal(response.status, 200);
  assert.match(response.headers.get("content-type"), /text\/event-stream/);
  const reader = response.body.getReader(),
    decoder = new TextDecoder();
  const done = (async () => {
    let pending = "";
    try {
      while (true) {
        const { value, done } = await reader.read();
        if (done) break;
        pending += decoder.decode(value, { stream: true });
        let end;
        while ((end = pending.indexOf("\n\n")) >= 0) {
          const frame = pending.slice(0, end);
          pending = pending.slice(end + 2);
          const data = frame
            .split("\n")
            .find((line) => line.startsWith("data: "));
          frames.push({
            at: Date.now(),
            raw: frame,
            data: data ? JSON.parse(data.slice(6)) : undefined,
          });
        }
      }
    } catch (error) {
      if (error.name !== "AbortError") throw error;
    }
  })();
  return {
    frames,
    close: async () => {
      controller.abort();
      await done;
    },
  };
}
async function completed(sid, count, dismissPlan = false) {
  return until(async () => {
    const d = await api(`/sessions/${sid}/events`);
    if (dismissPlan) {
      const pending = d.questions.find((q) => q.status === "pending");
      // This scenario only saves a plan file; defer any native review instead
      // of approving implementation or waiting forever for a human answer.
      if (pending)
        await api(`/sessions/${sid}/questions/${pending.id}/answer`, {
          dismissed: true,
        });
    }
    if (d.outbox.length < count || !d.outbox[count - 1].outcome) return;
    assert.equal(
      d.outbox[count - 1].outcome,
      "completed",
      d.outbox[count - 1].error,
    );
    return d;
  }, 180000);
}
const ids = [],
  streams = [];
const browser = await chromium.launch({ channel: "chrome", headless: true });
try {
  const s = await api("/sessions", {});
  ids.push(s.id);
  const path = "/sessions/" + s.id;
  const list = await stream("/sessions/stream"),
    detail = await stream(path + "/stream");
  streams.push(list, detail);
  await until(() =>
    list.frames.some((f) => f.data?.some((x) => x.id === s.id)),
  );
  const page = await browser.newPage({
      viewport: { width: 1440, height: 1000 },
    }),
    requests = [],
    errors = [];
  page.on("request", (r) =>
    requests.push({ method: r.method(), url: new URL(r.url()).pathname }),
  );
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto(base + "/#" + s.id);
  await page.getByText("DeepSeek Flash", { exact: true }).waitFor();
  const picker = page.locator(".model-bar .select").last().locator("button");
  assert.equal(await picker.isDisabled(), false);
  await picker.click();
  await page.getByRole("option", { name: "Wasm PTC" }).click();
  await until(async () => (await api(path)).options.mode === "ptc");
  await picker.click();
  await page.getByRole("option", { name: "标准工具" }).click();
  await until(async () => (await api(path)).options.mode === "standard");
  await page.locator(".model-bar .switch__control").click();
  await until(async () => (await api(path)).options.plan);
  await page.locator(".model-bar .switch__control").click();
  await until(async () => !(await api(path)).options.plan);
  console.log("PASS blank preset selection and Plan switch");

  const fixture = await browser.newPage({
    viewport: { width: 500, height: 240 },
    deviceScaleFactor: 1,
  });
  await fixture.setContent(
    '<body style="margin:0;background:white;display:grid;place-items:center;height:240px;font:50px Arial;color:black"><div>NIMBUS 4729</div></body>',
  );
  const png = await fixture.screenshot();
  await fixture.close();
  await page
    .getByLabel("选择图片")
    .setInputFiles({ name: "fixture.png", mimeType: "image/png", buffer: png });
  await page.locator(".image-drafts img").waitFor();
  await page.getByRole("button", { name: "预览图片：fixture.png" }).click();
  await page.locator(".image-preview").waitFor();
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "移除图片：fixture.png" }).click();
  assert.equal(await page.locator(".image-drafts img").count(), 0);
  // Exercise real browser clipboard-file handling, not a direct API substitute.
  await page.getByRole("textbox", { name: "发送消息" }).evaluate((el, b64) => {
    const bytes = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
    const transfer = new DataTransfer();
    transfer.items.add(new File([bytes], "pasted.png", { type: "image/png" }));
    el.dispatchEvent(
      new ClipboardEvent("paste", { bubbles: true, clipboardData: transfer }),
    );
  }, png.toString("base64"));
  await page.locator(".image-drafts img").waitFor();
  console.log("PASS image upload, preview, removal and paste");

  await until(() =>
    streams.every(
      (s) => s.frames.filter((f) => f.raw === ": heartbeat").length >= 2,
    ),
  );
  for (const st of streams) {
    const beats = st.frames.filter((f) => f.raw === ": heartbeat");
    const interval = beats[1].at - beats[0].at;
    assert.ok(interval > 4500 && interval < 5500, `heartbeat ${interval}ms`);
    console.log("PASS SSE heartbeat interval", interval, "ms");
  }
  assert.equal(
    requests.filter((r) => r.method === "GET" && r.url === "/api/sessions")
      .length,
    0,
  );
  console.log("PASS no browser session-list polling");
  await api(path, { title: "SSE 即时更新验证" }, "PATCH");
  await page.getByRole("heading", { name: "SSE 即时更新验证" }).waitFor();
  await until(() =>
    list.frames.some((f) =>
      f.data?.some((x) => x.title === "SSE 即时更新验证"),
    ),
  );
  const reconnect = await stream("/sessions/stream");
  streams.push(reconnect);
  await until(() =>
    reconnect.frames.some((f) =>
      f.data?.some((x) => x.id === s.id && x.title === "SSE 即时更新验证"),
    ),
  );
  console.log("PASS list pushes changes and reconnect sends current snapshot");

  await page
    .getByRole("textbox", { name: "发送消息" })
    .fill("读取图片里的字母和数字，只回答该字符串，不使用工具。");
  await page.getByRole("button", { name: "发送", exact: true }).click();
  let d = await completed(s.id, 1);
  assert.match(
    JSON.stringify(d.events.filter((e) => e.type === "assistant/message")),
    /NIMBUS\s*4729/,
  );
  assert.ok(
    d.events.some(
      (e) =>
        e.type === "user/message" &&
        e.payload.data.content.some(
          (b) => b.type === "image" && b.attachment.attachmentId,
        ),
    ),
  );
  assert.equal((await api(path)).sandbox.state, "not_started");
  await until(() => picker.isDisabled());
  assert.equal(
    (
      await request(path + "/messages", {
        message: "wrong preset",
        mode: "ptc",
        client_msg_id: crypto.randomUUID(),
      })
    ).status,
    409,
  );
  console.log(
    "PASS Flash reads native image content; preset locked; no E2B allocation",
  );
  await page.reload();
  await page.locator(".user-message img").waitFor();
  assert.equal(await picker.isDisabled(), true);
  await page
    .getByRole("textbox", { name: "发送消息" })
    .fill("刚才图片里的文字是什么？只回答该字符串。");
  await page.getByRole("button", { name: "发送", exact: true }).click();
  d = await completed(s.id, 2);
  assert.match(
    JSON.stringify(
      d.events.filter((e) => e.type === "assistant/message").at(-1),
    ),
    /NIMBUS\s*4729/,
  );
  await page.screenshot({
    path: ".local/session-updates-desktop.png",
    fullPage: true,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  assert.equal(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth,
    ),
    false,
  );
  await page.screenshot({
    path: ".local/session-updates-mobile.png",
    fullPage: true,
  });
  assert.deepEqual(errors, []);
  console.log("PASS image history, resumed model context, desktop/mobile UI");

  const ptc = await api("/sessions", {});
  ids.push(ptc.id);
  await api(`/sessions/${ptc.id}/messages`, {
    client_msg_id: crypto.randomUUID(),
    message:
      "当前为 Plan 模式。请使用 run_code 调用 tools.sandbox_write_file，在 /workspace/plan-proof.txt 写入 FLASH_PTC_PLAN_OK；仅保存这个计划文档，不要执行其他任务。",
    mode: "ptc",
    thinking: "max",
    plan: true,
  });
  const result = await completed(ptc.id, 1, true);
  assert.ok(
    result.events.some(
      (e) =>
        e.type === "tool/ptc-dispatch" &&
        JSON.stringify(e.payload).includes("sandbox_write_file") &&
        !e.payload.data.isError,
    ),
  );
  assert.ok(
    result.events.some(
      (e) =>
        e.type.includes("plan") && JSON.stringify(e.payload).includes("true"),
    ),
  );
  assert.equal((await api(`/sessions/${ptc.id}`)).options.mode, "ptc");
  console.log("PASS Flash + max + native Plan + Wasm PTC + real E2B write");
  console.log("ALL SESSION UPDATE E2E CHECKS PASSED");
} finally {
  await browser.close();
  for (const st of streams) await st.close();
  for (const id of ids) await api("/sessions/" + id, undefined, "DELETE");
}
