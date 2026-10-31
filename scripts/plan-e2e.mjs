// Uses real Flash + DSH and a browser. All decisions are confined to sessions
// created by this test; existing user conversations are never answered or edited.
import assert from "node:assert/strict";
import { chromium } from "playwright";
import { writeFile } from "node:fs/promises";

const base = "http://127.0.0.1:8080";
const sessions = [];
const browser = await chromium.launch({ channel: "chrome" });
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
const errors = [];
page.on("pageerror", (e) => errors.push(e.message));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
async function api(path, body, method = body ? "POST" : "GET") {
  const r = await fetch(base + "/api" + path, {
    method,
    headers: { "Content-Type": "application/json" },
    body: body ? JSON.stringify(body) : undefined,
  });
  const d = r.status === 204 ? null : await r.json();
  assert.ok(r.ok, `${r.status}: ${d?.error}`);
  return d;
}
async function until(fn, description) {
  const deadline = Date.now() + 180000;
  while (Date.now() < deadline) {
    const result = await fn();
    if (result) return result;
    await sleep(500);
  }
  throw Error("Timed out: " + description);
}
async function start(mode, message) {
  const s = await api("/sessions", {});
  sessions.push(s.id);
  await api(
    `/sessions/${s.id}`,
    {
      title: "Plan 评审验证 · " + mode,
      options: { ...s.options, mode, plan: true },
    },
    "PATCH",
  );
  const o = await api(`/sessions/${s.id}/messages`, {
    message,
    client_msg_id: crypto.randomUUID(),
  });
  await page.goto(base + "/?plan-e2e=" + s.id + "#" + s.id);
  return { s, o };
}
async function pending(s, o, previous) {
  return until(async () => {
    const d = await api(`/sessions/${s.id}/events`);
    const q = d.questions.find(
      (q) =>
        q.turn_id === o.turn_id && q.status === "pending" && q.id !== previous,
    );
    if (q) return q;
    const turn = d.outbox.find((r) => r.id === o.id);
    assert.ok(
      !turn.outcome,
      "Turn ended without review: " +
        JSON.stringify({
          outcome: turn.outcome,
          error: turn.error,
          tools: d.events
            .filter((e) => e.type === "tool/result")
            .map((e) => e.payload.data),
        }),
    );
  }, "native plan review");
}
async function completed(s, o, outcome = "completed") {
  const d = await until(async () => {
    const d = await api(`/sessions/${s.id}/events`);
    return d.outbox.find((r) => r.id === o.id)?.outcome ? d : null;
  }, "turn completion");
  assert.equal(d.outbox.find((r) => r.id === o.id).outcome, outcome);
  return d;
}
const request =
  "这是计划评审验证。不要使用沙箱、网络或文件工具。立即调用 exit_plan_mode 提交 Markdown 计划：# 打招呼计划\\n\\n1. 获得批准后只回复 PLAN_APPROVED_OK。必须使用工具提交评审，不要只输出文本。如果用户要求修改，按意见重新调用 exit_plan_mode。";
try {
  const { s, o } = await start(
    "ptc",
    request +
      "通过 run_code 调用 tools.exit_plan_mode；run_code 的 timeoutMs 设为 10000。",
  );
  const q = await pending(s, o);
  const waiting = await api(`/sessions/${s.id}/events`);
  const codeCall = waiting.events.findLast(
    (e) => e.type === "tool/call" && e.payload.data.name === "run_code",
  );
  assert.equal(JSON.parse(codeCall.payload.data.arguments).timeoutMs, 10000);
  await page.getByRole("button", { name: "批准并执行", exact: true }).waitFor();
  await page.screenshot({
    path: ".local/plan-review-pending.png",
    fullPage: true,
  });
  await page.reload();
  await page.getByRole("button", { name: "批准并执行", exact: true }).waitFor();
  // Exceed the requested PTC timeout while waiting for a human.
  await sleep(12000);
  assert.equal(
    (await api(`/sessions/${s.id}/events`)).questions.find((r) => r.id === q.id)
      .status,
    "pending",
  );
  await page.getByRole("button", { name: "批准并执行", exact: true }).click();
  const approved = await completed(s, o);
  assert.equal((await api(`/sessions/${s.id}`)).options.plan, false);
  assert.ok(
    approved.events.some(
      (e) => e.type === "plan/mode" && e.payload.data.active === false,
    ),
  );
  assert.ok(
    approved.events.some(
      (e) =>
        e.type === "assistant/message" &&
        JSON.stringify(e.payload.data.message.content).includes(
          "PLAN_APPROVED_OK",
        ),
    ),
  );
  assert.equal((await api(`/sessions/${s.id}`)).sandbox.state, "not_started");
  console.log(
    "PASS PTC native review, refresh, human wait, browser approval, mode transition and continuation",
  );

  const next = await start("standard", request);
  const first = await pending(next.s, next.o);
  await page.getByRole("button", { name: "继续规划", exact: true }).click();
  await page
    .getByPlaceholder("说明需要调整的内容…")
    .fill("把批准后的回复改为 PLAN_REVISED_OK，然后重新提交评审。");
  await page.getByRole("button", { name: "提交修改意见", exact: true }).click();
  const revised = await pending(next.s, next.o, first.id);
  assert.ok(JSON.stringify(revised.questions).includes("PLAN_REVISED_OK"));
  assert.equal((await api(`/sessions/${next.s.id}`)).options.plan, true);
  await page.getByRole("button", { name: "暂不决定", exact: true }).click();
  await completed(next.s, next.o);
  assert.equal((await api(`/sessions/${next.s.id}`)).options.plan, true);
  assert.equal(
    (await api(`/sessions/${next.s.id}/events`)).questions.find(
      (r) => r.id === revised.id,
    ).status,
    "dismissed",
  );
  console.log(
    "PASS Standard native review, feedback, revised plan, dismissal and retained Plan mode",
  );

  const stopping = await start("ptc", request);
  const open = await pending(stopping.s, stopping.o);
  await api(`/sessions/${stopping.s.id}/stop`, {});
  const stopped = await completed(stopping.s, stopping.o, "interrupted");
  assert.equal(
    stopped.questions.find((r) => r.id === open.id).status,
    "interrupted",
  );
  assert.equal((await api(`/sessions/${stopping.s.id}`)).options.plan, true);
  console.log("PASS Stop interrupts pending review without approving");
  assert.deepEqual(errors, []);
  await writeFile(
    ".local/plan-e2e.json",
    JSON.stringify(
      { passed: true, testedAt: new Date().toISOString(), sessions },
      null,
      2,
    ),
  );
} catch (e) {
  await writeFile(
    ".local/plan-e2e.json",
    JSON.stringify({ passed: false, sessions, error: String(e) }, null, 2),
  );
  throw e;
} finally {
  await browser.close();
  for (const id of sessions) await api(`/sessions/${id}`, undefined, "DELETE");
}
