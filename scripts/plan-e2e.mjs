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
async function start(message) {
  const s = await api("/sessions", {});
  sessions.push(s.id);
  await api(
    `/sessions/${s.id}`,
    {
      title: "Plan review verification",
      options: { ...s.options, plan: true },
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
  "This is a plan review test. Do not use the sandbox, network, or file tools. Call exit_plan_mode now to submit this Markdown plan: # Greeting plan\\n\\n1. After approval, reply only with PLAN_APPROVED_OK. You must submit the review using the tool; do not just output text. If I request changes, update the plan and call exit_plan_mode again.";
try {
  const { s, o } = await start(
    request +
      "Call tools.exit_plan_mode through run_code with timeoutMs set to 10000.",
  );
  const q = await pending(s, o);
  const waiting = await api(`/sessions/${s.id}/events`);
  const codeCall = waiting.events.findLast(
    (e) => e.type === "tool/call" && e.payload.data.name === "run_code",
  );
  assert.equal(JSON.parse(codeCall.payload.data.arguments).timeoutMs, 10000);
  await page.getByRole("button", { name: "Approve and continue", exact: true }).waitFor();
  await page.screenshot({
    path: ".local/plan-review-pending.png",
    fullPage: true,
  });
  await page.reload();
  await page.getByRole("button", { name: "Approve and continue", exact: true }).waitFor();
  // Exceed the requested PTC timeout while waiting for a human.
  await sleep(12000);
  assert.equal(
    (await api(`/sessions/${s.id}/events`)).questions.find((r) => r.id === q.id)
      .status,
    "pending",
  );
  await page.getByRole("button", { name: "Approve and continue", exact: true }).click();
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

  const next = await start(request);
  const first = await pending(next.s, next.o);
  await page.getByRole("button", { name: "Request changes", exact: true }).click();
  await page
    .getByPlaceholder("Describe what you would like changed…")
    .fill("Change the approved response to PLAN_REVISED_OK, then resubmit the plan for review.");
  await page.getByRole("button", { name: "Submit feedback", exact: true }).click();
  const revised = await pending(next.s, next.o, first.id);
  assert.ok(JSON.stringify(revised.questions).includes("PLAN_REVISED_OK"));
  assert.equal((await api(`/sessions/${next.s.id}`)).options.plan, true);
  await page.getByRole("button", { name: "Decide later", exact: true }).click();
  await completed(next.s, next.o);
  assert.equal((await api(`/sessions/${next.s.id}`)).options.plan, true);
  assert.equal(
    (await api(`/sessions/${next.s.id}/events`)).questions.find(
      (r) => r.id === revised.id,
    ).status,
    "dismissed",
  );
  console.log(
    "PASS PTC native review, feedback, revised plan, dismissal and retained Plan mode",
  );

  const stopping = await start(request);
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
