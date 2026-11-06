// Browser-only fixtures: no credentials, model calls, or stored data mutations.
import assert from "node:assert/strict";
import { chromium } from "playwright";

const base = process.env.NIMBUS_UI_URL || "http://127.0.0.1:8080";
const browser = await chromium.launch({ channel: "chrome", headless: true });
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
const requests = [];
const errors = [];
const skill = {
  id: "11111111-1111-4111-8111-111111111111",
  name: "lazy-files",
  description: "Fixture skill",
  enabled: true,
  files: [
    { path: "SKILL.md" },
    { path: "reference.md" },
    { path: "unopened.md" },
  ],
};
let saved;
page.on("pageerror", (error) => errors.push(error.message));
await page.route("**/api/**", async (route) => {
  const request = route.request(),
    url = new URL(request.url());
  requests.push({ path: url.pathname + url.search, method: request.method() });
  const json = (body) => route.fulfill({ json: body });
  if (url.pathname === "/api/sessions/stream") {
    return route.fulfill({
      contentType: "text/event-stream",
      body: "event: sessions\ndata: []\n\n",
    });
  }
  if (url.pathname === "/api/settings") {
    return json({
      deepseek_api_key: "masked",
      e2b_api_key: "masked",
      e2b_template: "base",
      idle_timeout_minutes: 15,
    });
  }
  if (url.pathname === "/api/mcp/servers") return json([]);
  if (url.pathname === "/api/skills") {
    assert.equal(url.searchParams.get("include_content"), "false");
    return json([skill]);
  }
  if (url.pathname === `/api/skills/${skill.id}/files`) {
    const path = url.searchParams.get("path");
    assert.ok(
      ["SKILL.md", "reference.md"].includes(path),
      `unexpected hidden file read ${path}`,
    );
    return json({
      path,
      content:
        path === "SKILL.md" ? "Original instructions" : "Reference contents",
    });
  }
  if (
    url.pathname === `/api/skills/${skill.id}` &&
    request.method() === "PATCH"
  ) {
    saved = request.postDataJSON();
    return json(skill);
  }
  throw Error(
    `Unexpected request: ${request.method()} ${url.pathname}${url.search}`,
  );
});
const count = (path) =>
  requests.filter((request) => request.path.startsWith(path)).length;
try {
  await page.goto(base);
  await page.getByRole("button", { name: "Settings", exact: true }).waitFor();
  await page.waitForTimeout(200);
  assert.equal(count("/api/settings"), 0);
  assert.equal(count("/api/skills"), 0);
  assert.equal(count("/api/mcp"), 0);
  assert.equal(count("/api/library"), 0);

  await page.getByRole("button", { name: "Settings", exact: true }).click();
  await page.locator(".settings-section").waitFor();
  assert.equal(count("/api/settings"), 1);
  assert.equal(count("/api/skills"), 0);
  assert.equal(count("/api/mcp"), 0);

  await page.getByRole("button", { name: "Skills", exact: true }).click();
  await page.getByText(skill.name, { exact: true }).waitFor();
  assert.equal(count("/api/skills?"), 1);
  assert.equal(count(`/api/skills/${skill.id}/files`), 0);
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  const contents = page.getByRole("textbox", { name: "Skill file contents" });
  await page.waitForFunction(
    () =>
      document.querySelector('textarea[aria-label="Skill file contents"]')
        ?.value === "Original instructions",
  );
  assert.equal(count(`/api/skills/${skill.id}/files`), 1);
  await contents.fill("Edited instructions");
  await page.getByRole("button", { name: "reference.md", exact: true }).click();
  await page.waitForFunction(
    () =>
      document.querySelector('textarea[aria-label="Skill file contents"]')
        ?.value === "Reference contents",
  );
  assert.equal(count(`/api/skills/${skill.id}/files`), 2);
  await page.getByRole("button", { name: "SKILL.md", exact: true }).click();
  assert.equal(await contents.inputValue(), "Edited instructions");
  assert.equal(
    count(`/api/skills/${skill.id}/files`),
    2,
    "cached file should not refetch",
  );
  await page.getByRole("button", { name: "Save skill", exact: true }).click();
  await page.waitForFunction(() => !document.querySelector(".skill-editor"));
  assert.equal(
    saved.files.find((file) => file.path === "SKILL.md").content,
    "Edited instructions",
  );
  assert.equal(
    Object.hasOwn(
      saved.files.find((file) => file.path === "unopened.md"),
      "content",
    ),
    false,
    "saving must preserve never-loaded contents without fetching or overwriting them",
  );
  assert.equal(count(`/api/skills/${skill.id}/files`), 2);

  await page.getByRole("button", { name: "MCP Servers", exact: true }).click();
  await page.getByRole("heading", { name: "Connect external tools" }).waitFor();
  assert.equal(count("/api/mcp/servers"), 1);
  assert.equal(count("/api/settings"), 1);
  await page.keyboard.press("Escape");
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  const before = requests.length;
  await page.waitForTimeout(200);
  assert.equal(requests.length, before);
  assert.deepEqual(errors, []);
  console.log(
    "PASS hidden settings tabs, lazy skill file contents, cached selection, and unloaded-file preservation",
  );
} finally {
  await browser.close();
}
