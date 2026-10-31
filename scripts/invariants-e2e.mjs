import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
const root = "http://127.0.0.1:8080";
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
async function req(p, b, m = b ? "POST" : "GET") {
  const r = await fetch(root + "/api" + p, {
    method: m,
    headers: { "Content-Type": "application/json" },
    body: b ? JSON.stringify(b) : undefined,
  });
  return { status: r.status, data: r.status === 204 ? null : await r.json() };
}
async function api(...args) {
  const r = await req(...args);
  if (r.status >= 400) throw Error(r.data.error);
  return r.data;
}
const s = await api("/sessions", {});
const cid = crypto.randomUUID();
const duplicate = await Promise.all(
  Array.from({ length: 5 }, () =>
    api(`/sessions/${s.id}/messages`, {
      message: "只回答 OK，不使用工具。",
      client_msg_id: cid,
    }),
  ),
);
assert.equal(new Set(duplicate.map((o) => o.id)).size, 1);
console.log("PASS idempotent concurrent delivery");
await api(`/sessions/${s.id}/messages`, {
  message: "上一条让我回答什么？请简短回答，不使用工具。",
  client_msg_id: crypto.randomUUID(),
});
let d;
for (let i = 0; i < 120; i++) {
  d = await api(`/sessions/${s.id}/events`);
  if (d.outbox.length === 2 && d.outbox.every((x) => x.outcome)) break;
  await sleep(250);
}
assert.equal(d.outbox.length, 2);
assert.ok(d.outbox.every((x) => x.outcome === "completed"));
assert.equal(d.events.filter((e) => e.type === "user/message").length, 2);
assert.ok(d.events.every((e, i) => e.seq === i));
console.log("PASS queued turns preserve contiguous history");
assert.equal(
  (
    await req(`/sessions/${s.id}/messages`, {
      message: "test",
      client_msg_id: crypto.randomUUID(),
      model: "deepseek-flash",
      thinking: "medium",
      mode: "standard",
    })
  ).status,
  400,
);
assert.equal(
  (
    await req("/mcp/servers", {
      name: "bad",
      url: "npx -y server",
      enabled: true,
    })
  ).status,
  400,
);
for (const files of [
  Array.from({ length: 101 }, (_, i) => ({ path: i + ".md", content: "x" })),
  [{ path: "a.txt", content: "a\0b" }],
  [{ path: "a.txt", content: "x".repeat(5242881) }],
])
  assert.equal(
    (await req("/skills", { name: "invalid-test", enabled: true, files }))
      .status,
    400,
  );
console.log("PASS validation rejects invalid thinking, STDIO, skill limits");
const origin = await fetch(root + "/api/sessions", {
  method: "POST",
  headers: {
    Origin: "https://attacker.example",
    "Content-Type": "application/json",
  },
  body: "{}",
});
assert.equal(origin.status, 403);
const control = await fetch(root + "/internal/tools", {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: "{}",
});
assert.equal(control.status, 403);
console.log("PASS cross-origin and unauthenticated control requests rejected");
const pg = "/opt/homebrew/opt/postgresql@17/bin/psql";
const args = [
  "-h",
  "127.0.0.1",
  "-p",
  "55432",
  "-U",
  "nimbus",
  "-d",
  "nimbus",
  "-v",
  "ON_ERROR_STOP=1",
  "-c",
];
assert.throws(() =>
  execFileSync(
    pg,
    [
      ...args,
      `UPDATE session_events SET type='bad' WHERE session_id='${s.id}'`,
    ],
    { stdio: "pipe" },
  ),
);
assert.throws(() =>
  execFileSync(
    pg,
    [...args, `DELETE FROM session_events WHERE session_id='${s.id}'`],
    { stdio: "pipe" },
  ),
);
await api(`/sessions/${s.id}`, undefined, "DELETE");
assert.equal((await req(`/sessions/${s.id}`)).status, 404);
console.log("PASS database append-only guard and session cascade");
