// Deterministic browser fixtures only: no model, sandbox, or persisted mutations.
import assert from "node:assert/strict";
import { chromium } from "playwright";

const base = process.env.NIMBUS_UI_URL || "http://127.0.0.1:8080";
const sid = "lazy-library-fixture";
const ids = {
  markdown: "11111111-1111-4111-8111-111111111111",
  other: "22222222-2222-4222-8222-222222222222",
  hidden: "33333333-3333-4333-8333-333333333333",
  html: "44444444-4444-4444-8444-444444444444",
};
const date = "2026-11-06T00:00:00Z";
const file = (id, name, mime_type, version = 1) => ({
  id,
  name,
  mime_type,
  version,
  size_bytes: 100,
  created_at: date,
  updated_at: date,
  versions: [],
});
const files = [
  file(ids.markdown, "fixture.md", "text/markdown", 3),
  file(ids.other, "other.md", "text/markdown"),
  file(ids.html, "interactive.html", "text/html"),
];
const session = {
  id: sid,
  title: "Lazy library fixture",
  options: { model: "deepseek-flash", thinking: "off", plan: false },
  agent_state: "idle",
  last_active_at: date,
  sandbox: { state: "not_started" },
};
const turn = (text) => ({
  turn_id: "library-turn",
  final_text: text,
  has_details: false,
  tool_count: 0,
  failed_count: 0,
  duration: 1,
});
const row = {
  id: "library-outbox",
  turn_id: "library-turn",
  client_msg_id: "library-message",
  content: "Show library links",
  status: "delivered",
  outcome: "completed",
  error: "",
  delivered_seq: 1,
};
const browser = await chromium.launch({ channel: "chrome" });
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
const requests = [];
const errors = [];
const gates = new Map();
const allGates = new Set();
const hold = (key) => {
  let release;
  const promise = new Promise((resolve) => {
    release = resolve;
  });
  const gate = { promise, release, entered: false };
  gates.set(key, gate);
  allGates.add(gate);
  return gate;
};
const pause = async (key) => {
  const gate = gates.get(key);
  if (!gate) return;
  gates.delete(key);
  gate.entered = true;
  await gate.promise;
};
const waitUntil = async (predicate) => {
  for (let i = 0; i < 100 && !predicate(); i++) await page.waitForTimeout(25);
  assert.ok(predicate(), "fixture condition did not settle");
};
const count = (predicate) => requests.filter(predicate).length;
const isMetadata = (url, id) =>
  url.pathname === `/api/library/${id}` &&
  url.searchParams.get("include_versions") === "false";
const historyCount = () =>
  count((url) => url.searchParams.get("include_versions") === "true");
const contentRequests = () =>
  requests.filter((url) => url.pathname.endsWith("/content"));
const dialog = page.locator(".library-preview-dialog");
const wasAborted = (path) =>
  page.evaluate(
    (path) =>
      window.__libraryFetches.some(
        (request) => request.path === path && request.aborted,
      ),
    path,
  );
const closePreview = async () => {
  await dialog
    .getByRole("button", { name: "Close", exact: true })
    .last()
    .click();
  await dialog.waitFor({ state: "detached" });
};
page.on("pageerror", (error) => errors.push(error.message));
await page.addInitScript(
  ({ session }) => {
    window.__streams = [];
    window.__libraryFetches = [];
    const originalFetch = window.fetch.bind(window);
    window.fetch = (input, init) => {
      const url = new URL(
        typeof input === "string"
          ? input
          : input instanceof URL
            ? input.href
            : input.url,
        location.href,
      );
      if (url.pathname.startsWith("/api/library")) {
        const request = { path: url.pathname + url.search, aborted: false };
        window.__libraryFetches.push(request);
        init?.signal?.addEventListener(
          "abort",
          () => {
            request.aborted = true;
          },
          { once: true },
        );
      }
      return originalFetch(input, init);
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
                  data: JSON.stringify([session]),
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
  { session },
);
await page.route("**/api/**", async (route) => {
  const url = new URL(route.request().url());
  requests.push(url);
  const fulfill = (response) => route.fulfill(response).catch(() => {});
  if (url.pathname === `/api/sessions/${sid}/events`)
    return fulfill({
      json: {
        outbox: [row],
        turns: [turn("Library fixture ready")],
        questions: [],
        has_more: false,
        before_turn: null,
      },
    });
  if (url.pathname === "/api/library") {
    await pause("list");
    return fulfill({ json: { files } });
  }
  const match = url.pathname.match(/^\/api\/library\/([^/]+)(\/content)?$/);
  if (match) {
    const id = match[1];
    const current = files.find((candidate) => candidate.id === id);
    assert.ok(
      current,
      `unexpected metadata/content request for hidden file ${id}`,
    );
    if (match[2]) {
      const version = url.searchParams.get("version");
      assert.ok(version, "preview must explicitly select its content version");
      await pause(`content:${id}:${version}`);
      if (id === ids.html)
        return fulfill({
          contentType: "text/html",
          headers: { "Content-Security-Policy": "sandbox allow-scripts" },
          body: "<button onclick=\"document.querySelector('p').textContent='HTML_INTERACTIVE_OK'\">Run example</button><p>HTML_READY</p>",
        });
      const body =
        id === ids.markdown
          ? version === "3"
            ? `CURRENT_MARKDOWN [Other file](nimbus://library/${ids.other})`
            : version === "2"
              ? "SECOND_MARKDOWN"
              : `OLD_MARKDOWN nimbus://library/${ids.hidden}`
          : `STALE_OTHER_MARKDOWN nimbus://library/${ids.hidden}`;
      return fulfill({ contentType: "text/markdown", body });
    }
    assert.ok(
      ["true", "false"].includes(url.searchParams.get("include_versions")),
      "metadata/history projection must be explicit",
    );
    const history = url.searchParams.get("include_versions") === "true";
    await pause(`${history ? "versions" : "metadata"}:${id}`);
    return fulfill({
      json: {
        ...current,
        versions: history
          ? Array.from({ length: current.version }, (_, index) => ({
              version: index + 1,
              size_bytes: 100,
              created_at: date,
              sha256: `sha${index + 1}`,
            }))
          : [],
      },
    });
  }
  return fulfill({
    status: 404,
    json: { error: `Unexpected fixture request ${url.pathname}` },
  });
});
const openMarkdown = async () => {
  await page
    .locator(`.conversation a[href="nimbus://library/${ids.markdown}"]`)
    .first()
    .click();
  await dialog.getByText("CURRENT_MARKDOWN", { exact: false }).waitFor();
};
try {
  await page.goto(`${base}/#${sid}`);
  await page.getByText("Library fixture ready", { exact: true }).waitFor();
  await page.waitForTimeout(100);
  assert.equal(
    count((url) => url.pathname.startsWith("/api/library")),
    0,
    "closed library surfaces must make no library requests",
  );

  // Two visible bare links share one per-id metadata request; named links do not resolve names.
  const names = hold(`metadata:${ids.markdown}`);
  await page.evaluate(
    ({ sid, summary }) => {
      const stream = window.__streams.findLast(
        (source) =>
          !source.closed &&
          new URL(source.url).pathname === `/api/sessions/${sid}/stream`,
      );
      if (!stream) throw Error("fixture session stream is missing");
      stream.dispatchEvent(
        new MessageEvent("turns", { data: JSON.stringify([summary]) }),
      );
    },
    {
      sid,
      summary: turn(
        `nimbus://library/${ids.markdown}\n\nnimbus://library/${ids.markdown}\n\n[HTML example](nimbus://library/${ids.html})`,
      ),
    },
  );
  await waitUntil(() => names.entered);
  assert.equal(
    count((url) => isMetadata(url, ids.markdown)),
    1,
  );
  assert.equal(
    count((url) => url.pathname === "/api/library"),
    0,
  );
  assert.equal(historyCount(), 0);
  assert.equal(contentRequests().length, 0);
  names.release();
  await page
    .locator(`.conversation a[href="nimbus://library/${ids.markdown}"]`)
    .first()
    .getByText("fixture.md", { exact: true })
    .waitFor();
  assert.equal(
    count((url) => url.pathname === `/api/library/${ids.html}`),
    0,
    "named library links need no name lookup",
  );

  await openMarkdown();
  assert.equal(
    historyCount(),
    0,
    "opening a preview must not query version history",
  );
  assert.deepEqual(
    contentRequests().map((url) => url.searchParams.get("version")),
    ["3"],
  );
  const versionGate = hold(`versions:${ids.markdown}`);
  await dialog.locator('[data-slot="select-trigger"]').click();
  await waitUntil(() => versionGate.entered);
  assert.equal(historyCount(), 1);
  await page.keyboard.press("Escape");
  await page.waitForFunction(
    (path) =>
      window.__libraryFetches.some(
        (request) => request.path === path && request.aborted,
      ),
    `/api/library/${ids.markdown}?include_versions=true`,
  );
  versionGate.release();
  await dialog.locator('[data-slot="select-trigger"]').click();
  await page.getByRole("option", { name: /^Version 1/ }).waitFor();
  assert.equal(
    historyCount(),
    2,
    "an aborted history request is retried on reopen",
  );
  await page.getByRole("option", { name: /^Version 2/ }).click();
  await dialog.getByText("SECOND_MARKDOWN", { exact: true }).waitFor();
  assert.deepEqual(
    contentRequests().map((url) => url.searchParams.get("version")),
    ["3", "2"],
  );
  assert.equal(
    await dialog
      .getByRole("link", { name: "Download", exact: true })
      .getAttribute("href"),
    `/api/library/${ids.markdown}/content?version=2`,
  );
  await dialog.locator('[data-slot="select-trigger"]').click();
  assert.equal(
    historyCount(),
    2,
    "loaded version summaries are reused while the preview stays open",
  );
  const oldContent = hold(`content:${ids.markdown}:1`);
  await page.getByRole("option", { name: /^Version 1/ }).click();
  await waitUntil(() => oldContent.entered);
  assert.deepEqual(
    contentRequests().map((url) => url.searchParams.get("version")),
    ["3", "2", "1"],
    "only selected version content is fetched",
  );
  await closePreview();
  assert.equal(
    await wasAborted(`/api/library/${ids.markdown}/content?version=1`),
    true,
  );
  oldContent.release();
  await page.waitForTimeout(50);
  assert.equal(
    count((url) => url.pathname.includes(ids.hidden)),
    0,
    "closed markdown must never parse stale links",
  );

  // Switching file IDs drops the old preview immediately; closing aborts new metadata.
  await openMarkdown();
  const switchedMetadata = hold(`metadata:${ids.other}`);
  await dialog.getByRole("link", { name: "Other file", exact: true }).click();
  await waitUntil(() => switchedMetadata.entered);
  assert.equal(
    await dialog.getByText("CURRENT_MARKDOWN", { exact: false }).count(),
    0,
  );
  await closePreview();
  assert.equal(
    await wasAborted(`/api/library/${ids.other}?include_versions=false`),
    true,
  );
  switchedMetadata.release();
  await page.waitForTimeout(50);
  assert.equal(
    contentRequests().some((url) => url.pathname.includes(ids.other)),
    false,
  );

  // List fetches belong to the mounted Library panel and are aborted on navigation.
  const listGate = hold("list");
  await page.locator(".library-nav").click();
  await waitUntil(() => listGate.entered);
  await page
    .locator(".session-list .session-item")
    .filter({ hasText: session.title })
    .click();
  assert.equal(await wasAborted("/api/library"), true);
  listGate.release();
  await page.locator(".library-nav").click();
  await page.locator(".library-item").filter({ hasText: "other.md" }).waitFor();
  const otherContent = hold(`content:${ids.other}:1`);
  await page.locator(".library-item").filter({ hasText: "other.md" }).click();
  await waitUntil(() => otherContent.entered);
  await closePreview();
  assert.equal(
    await wasAborted(`/api/library/${ids.other}/content?version=1`),
    true,
  );
  await page.locator(".library-item").filter({ hasText: "fixture.md" }).click();
  await dialog.getByText("CURRENT_MARKDOWN", { exact: false }).waitFor();
  otherContent.release();
  await page.waitForTimeout(50);
  assert.equal(
    await dialog.getByText("STALE_OTHER_MARKDOWN", { exact: false }).count(),
    0,
  );
  assert.equal(
    count((url) => url.pathname.includes(ids.hidden)),
    0,
    "stale content must not render or resolve its links after switch",
  );
  await closePreview();

  // Native HTML remains an interactive, opaque-origin sandbox, with versioned download.
  await page
    .locator(".library-item")
    .filter({ hasText: "interactive.html" })
    .click();
  const frame = dialog.locator("iframe");
  await frame.waitFor();
  await (await frame.elementHandle()).waitForElementState("stable");
  assert.equal(await frame.getAttribute("sandbox"), "allow-scripts");
  assert.equal(
    await dialog
      .getByRole("link", { name: "Download", exact: true })
      .getAttribute("href"),
    `/api/library/${ids.html}/content?version=1`,
  );
  await page
    .frameLocator(".library-preview-frame")
    .getByRole("button", { name: "Run example", exact: true })
    .click();
  await page
    .frameLocator(".library-preview-frame")
    .getByText("HTML_INTERACTIVE_OK", { exact: true })
    .waitFor();
  await closePreview();
  assert.equal(
    historyCount(),
    2,
    "history stays lazy in every subsequent preview",
  );
  assert.deepEqual(errors, []);
  console.log(
    "PASS library visibility, per-id single-flight names, selector history, selected content, abort isolation, safe markdown and interactive HTML",
  );
} finally {
  for (const gate of allGates) gate.release();
  await browser.close();
}
