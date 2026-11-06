// Browser fixtures only: no model calls or persisted review mutations.
import assert from "node:assert/strict";
import { chromium } from "playwright";

const base = process.env.NIMBUS_UI_URL || "http://127.0.0.1:8080";
const names = [
  "plan-closed",
  "plan-pending",
  "plan-retry",
  "plan-retry-cancel",
  "plan-alternate",
];
const session = (id) => ({
  id,
  title: id,
  options: { model: "deepseek-flash", thinking: "off", plan: true },
  agent_state: id === "plan-pending" ? "running" : "idle",
  last_active_at: "2026-11-06T00:00:00Z",
  sandbox: { state: "not_started" },
});
const row = (sid, n = 1) => ({
  id: `${sid}-out-${n}`,
  turn_id: `${sid}-turn-${n}`,
  client_msg_id: `${sid}-msg-${n}`,
  content: `User request ${sid} ${n}`,
  status: "delivered",
  outcome: sid === "plan-pending" ? "" : "completed",
  error: "",
  delivered_seq: 1,
});
const question = (sid, id, status = "pending") => ({
  id,
  turn_id: row(sid).turn_id,
  status,
  questions: [
    {
      id: "plan",
      intent: { kind: "plan-review", approve: "Approve", callId: id },
    },
  ],
  ...(status === "answered"
    ? {
        answer: {
          answers: [
            {
              id: "plan",
              selected: [
                id === "approved-review" ? "Approve" : "Keep planning",
              ],
            },
          ],
        },
      }
    : {}),
});
const closed = [
  question("plan-closed", "approved-review", "answered"),
  question("plan-closed", "changes-review", "answered"),
];
const pending = ["approve-review", "revise-review", "dismiss-review"].map(
  (id) => question("plan-pending", id),
);
const full = (metadata) => ({
  ...metadata,
  questions: metadata.questions.map((q) => ({
    ...q,
    question: "Review this plan",
    detail: `PRIVATE_PLAN_DOCUMENT_${metadata.id}`,
    options: [{ label: "Approve" }, { label: "Keep planning" }],
  })),
});
const questions = [...closed, ...pending];
const history = (sid) => ({
  outbox: [row(sid)],
  turns: [
    {
      turn_id: row(sid).turn_id,
      final_text: sid === "plan-pending" ? "" : `Visible final ${sid}`,
      has_details: false,
      tool_count: 0,
      failed_count: 0,
      ...(sid.startsWith("plan-retry")
        ? { plan_review_retry: { seq: 17 } }
        : {}),
    },
  ],
  questions:
    sid === "plan-closed" ? closed : sid === "plan-pending" ? pending : [],
  has_more: false,
  before_turn: null,
});
const browser = await chromium.launch({ channel: "chrome" });
const page = await browser.newPage({ viewport: { width: 1440, height: 1100 } });
const requests = [];
const answers = [];
const messages = [];
const errors = [];
const releases = new Map();
const reviewLoads = new Map();
let releaseRetry;
page.on("pageerror", (error) => errors.push(error.message));
await page.addInitScript(
  ({ sessions }) => {
    window.__streams = [];
    window.__abortedRequests = [];
    const original = window.fetch;
    window.fetch = (input, init) => {
      init?.signal?.addEventListener(
        "abort",
        () => window.__abortedRequests.push(String(input)),
        { once: true },
      );
      return original(input, init);
    };
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
  },
  { sessions: names.map(session) },
);
await page.route("**/api/**", async (route) => {
  const request = route.request();
  const url = new URL(request.url());
  requests.push(url.pathname + url.search);
  const answer = url.pathname.match(
    /^\/api\/sessions\/([^/]+)\/questions\/([^/]+)\/answer$/,
  );
  if (answer) {
    answers.push({ id: answer[2], body: request.postDataJSON() });
    return route.fulfill({ json: { ok: true } });
  }
  const review = url.pathname.match(
    /^\/api\/sessions\/([^/]+)\/questions\/([^/]+)$/,
  );
  if (review) {
    const id = review[2];
    const count = (reviewLoads.get(id) || 0) + 1;
    reviewLoads.set(id, count);
    if (id === "changes-review" && count === 1)
      return route.fulfill({
        status: 503,
        json: { error: "Review document temporarily unavailable" },
      });
    if (review[1] === "plan-pending" || (id === "changes-review" && count >= 2))
      await new Promise((resolve) => releases.set(id, resolve));
    return route
      .fulfill({ json: full(questions.find((q) => q.id === id)) })
      .catch(() => {});
  }
  const trace = url.pathname.match(
    /^\/api\/sessions\/([^/]+)\/turns\/([^/]+)\/events$/,
  );
  if (trace) {
    const sid = trace[1];
    assert.equal(url.searchParams.get("include_question_content"), "false");
    assert.equal(
      url.searchParams.get("include_questions"),
      "false",
      "per-turn requests reuse main-lane review metadata",
    );
    assert.equal(
      url.searchParams.get("include_outbox"),
      "false",
      "per-turn requests must not duplicate hidden user prompts",
    );
    if (url.searchParams.get("projection") === "metadata")
      return route.fulfill({
        json: {
          tools: [],
          messages: [],
          attempts: [],
          pending_tools: [],
          questions: [],
          outbox: [],
          cursor: { seq: 1, frame: 0 },
        },
      });
    assert.ok(
      sid.startsWith("plan-retry"),
      "only retry actions may request native plan details",
    );
    assert.equal(
      url.searchParams.get("seq"),
      "17",
      "retry must request exactly its native event sequence",
    );
    if (sid === "plan-retry-cancel")
      await new Promise((resolve) => {
        releaseRetry = resolve;
      });
    return route
      .fulfill({
        json: {
          events: [
            {
              seq: 17,
              type: "tool/ptc-dispatch",
              payload: {
                time: 1793440000000,
                data: {
                  name: "exit_plan_mode",
                  subCallId: "failed-review",
                  arguments: JSON.stringify({
                    plan: "# EXACT_RETRY_PLAN\nDo not alter this text.",
                  }),
                  isError: true,
                  content: "no user-questions channel is available",
                },
              },
            },
          ],
          frames: [],
          questions: [],
          cursor: { seq: 17, frame: 0 },
        },
      })
      .catch(() => {});
  }
  const summary = url.pathname.match(/^\/api\/sessions\/([^/]+)\/events$/);
  if (summary) {
    assert.equal(
      url.searchParams.get("include_question_content"),
      "false",
      "normal history must request question metadata only",
    );
    const data = history(summary[1]);
    assert.equal(JSON.stringify(data).includes("PRIVATE_PLAN_DOCUMENT"), false);
    return route.fulfill({ json: data });
  }
  if (/\/messages$/.test(url.pathname)) {
    messages.push({ path: url.pathname, body: request.postDataJSON() });
    return route.fulfill({ status: 202, json: { ok: true } });
  }
  return route.fulfill({
    status: 404,
    json: { error: `Unexpected fixture request: ${url.pathname}` },
  });
});
const waitUntil = async (predicate) => {
  for (let i = 0; i < 100 && !predicate(); i++) await page.waitForTimeout(25);
  assert.ok(predicate());
};
const choose = async (sid) => {
  await page
    .locator(".session-list .session-item")
    .filter({ hasText: sid })
    .filter({
      hasNotText: sid === "plan-retry" ? "plan-retry-cancel" : "__none__",
    })
    .click();
  await page.getByText(`User request ${sid} 1`, { exact: true }).waitFor();
};
const nativeRequests = (sid) =>
  requests.filter(
    (path) =>
      path.includes(`/sessions/${sid}/turns/`) &&
      !path.includes("projection=metadata"),
  );
const release = (id) => {
  const done = releases.get(id);
  assert.ok(done);
  releases.delete(id);
  done();
};
try {
  await page.goto(`${base}/#plan-closed`);
  await page.getByText("Visible final plan-closed", { exact: true }).waitFor();
  const reviews = page.locator(".plan-review");
  await page.waitForTimeout(150);
  assert.equal(
    reviewLoads.size,
    0,
    "completed closed reviews must not fetch full documents",
  );
  assert.equal(
    await page.locator(".plan-review-document").count(),
    0,
    "closed disclosures must not mount plan documents",
  );
  assert.equal(
    await page.getByText("Plan approved", { exact: true }).count(),
    1,
  );
  assert.equal(
    await page.getByText("Changes requested", { exact: true }).count(),
    1,
  );
  await reviews.nth(0).locator("summary").click();
  await page
    .getByText("PRIVATE_PLAN_DOCUMENT_approved-review", { exact: true })
    .waitFor();
  assert.equal(reviewLoads.get("approved-review"), 1);
  assert.ok(
    requests.includes("/api/sessions/plan-closed/questions/approved-review"),
    "expansion fetches the exact review resource",
  );
  await reviews.nth(0).locator("summary").click();
  assert.equal(await page.locator(".plan-review-document").count(), 0);
  await reviews.nth(0).locator("summary").click();
  await page
    .getByText("PRIVATE_PLAN_DOCUMENT_approved-review", { exact: true })
    .waitFor();
  assert.equal(
    reviewLoads.get("approved-review"),
    1,
    "completed document cache survives reopen",
  );
  await reviews.nth(1).locator("summary").click();
  await page
    .getByText("Review document temporarily unavailable", { exact: false })
    .waitFor();
  await page.getByRole("button", { name: "Retry plan document" }).click();
  await waitUntil(() => releases.has("changes-review"));
  await reviews.nth(1).locator("summary").click();
  await page.waitForFunction(() =>
    window.__abortedRequests.some((path) =>
      path.endsWith("/questions/changes-review"),
    ),
  );
  release("changes-review");
  await reviews.nth(1).locator("summary").click();
  await waitUntil(
    () =>
      reviewLoads.get("changes-review") === 3 && releases.has("changes-review"),
  );
  await choose("plan-alternate");
  release("changes-review");
  await page.waitForTimeout(100);
  assert.equal(
    await page.locator(".plan-review").count(),
    0,
    "stale review response cannot cross sessions",
  );

  await choose("plan-pending");
  await waitUntil(() => pending.every((q) => releases.has(q.id)));
  assert.equal(
    await page.locator(".plan-review details[open]").count(),
    3,
    "pending reviews default open",
  );
  assert.equal(
    answers.length,
    0,
    "opening a pending review must not approve it",
  );
  for (const button of await page
    .getByRole("button", { name: "Approve and continue", exact: true })
    .all())
    assert.equal(
      await button.isDisabled(),
      true,
      "approval requires the loaded document",
    );
  for (const q of pending) release(q.id);
  await page
    .getByText("PRIVATE_PLAN_DOCUMENT_approve-review", { exact: true })
    .waitFor();
  const approve = page
    .locator(".plan-review")
    .filter({ hasText: "PRIVATE_PLAN_DOCUMENT_approve-review" });
  const revise = page
    .locator(".plan-review")
    .filter({ hasText: "PRIVATE_PLAN_DOCUMENT_revise-review" });
  const dismiss = page
    .locator(".plan-review")
    .filter({ hasText: "PRIVATE_PLAN_DOCUMENT_dismiss-review" });
  await approve
    .getByRole("button", { name: "Approve and continue", exact: true })
    .click();
  await revise
    .getByRole("button", { name: "Request changes", exact: true })
    .click();
  await revise.getByRole("textbox").fill("Keep the safety checks.");
  await revise
    .getByRole("button", { name: "Submit feedback", exact: true })
    .click();
  await dismiss
    .getByRole("button", { name: "Decide later", exact: true })
    .click();
  await waitUntil(() => answers.length === 3);
  assert.deepEqual(answers.find((a) => a.id === "approve-review").body, {
    answers: [{ id: "plan", selected: ["Approve"] }],
  });
  assert.deepEqual(answers.find((a) => a.id === "revise-review").body, {
    answers: [
      {
        id: "plan",
        selected: ["Keep planning"],
        custom: "Keep the safety checks.",
      },
    ],
  });
  assert.deepEqual(answers.find((a) => a.id === "dismiss-review").body, {
    dismissed: true,
  });

  await choose("plan-retry");
  await page
    .getByRole("button", { name: "Resubmit for review", exact: true })
    .waitFor();
  assert.equal(
    nativeRequests("plan-retry").length,
    0,
    "retry plan is not fetched while its action is idle",
  );
  await page
    .getByRole("button", { name: "Resubmit for review", exact: true })
    .click();
  await waitUntil(() => messages.length === 1);
  assert.equal(nativeRequests("plan-retry").length, 1);
  assert.equal(messages[0].body.plan, true);
  assert.equal(
    messages[0].body.client_msg_id,
    "plan-review-retry-plan-retry-17",
  );
  assert.ok(
    messages[0].body.message.endsWith(
      "\n\n# EXACT_RETRY_PLAN\nDo not alter this text.",
    ),
    "retry preserves the exact native plan",
  );
  await choose("plan-retry-cancel");
  assert.equal(nativeRequests("plan-retry-cancel").length, 0);
  await page
    .getByRole("button", { name: "Resubmit for review", exact: true })
    .click();
  await waitUntil(() => !!releaseRetry);
  await choose("plan-alternate");
  await page.waitForFunction(() =>
    window.__abortedRequests.some((path) =>
      path.includes("/sessions/plan-retry-cancel/turns/"),
    ),
  );
  releaseRetry();
  await page.waitForTimeout(100);
  assert.equal(
    messages.length,
    1,
    "a cancelled retry must not send a message in the old session",
  );
  const mainStreams = await page.evaluate(() =>
    window.__streams
      .filter((stream) =>
        /^\/api\/sessions\/[^/]+\/stream$/.test(new URL(stream.url).pathname),
      )
      .map((stream) => stream.url),
  );
  assert.ok(mainStreams.length > 0);
  for (const stream of mainStreams)
    assert.equal(
      new URL(stream).searchParams.get("include_question_content"),
      "false",
    );
  assert.deepEqual(errors, []);
  console.log(
    "PASS lazy completed plan documents, metadata headers, pending review actions, exact-sequence retry, cancellation and session isolation",
  );
} finally {
  for (const release of releases.values()) release();
  releaseRetry?.();
  await browser.close();
}
