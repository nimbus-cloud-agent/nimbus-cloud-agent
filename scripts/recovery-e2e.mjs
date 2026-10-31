import assert from "node:assert/strict";
import { readFile, open, writeFile } from "node:fs/promises";
import { spawn } from "node:child_process";
const base = "http://127.0.0.1:8080";
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
async function api(path, body, method = body ? "POST" : "GET") {
  const r = await fetch(base + "/api" + path, {
    method,
    headers: { "Content-Type": "application/json" },
    body: body ? JSON.stringify(body) : undefined,
  });
  const d = r.status === 204 ? null : await r.json();
  if (!r.ok) throw Error(`${r.status}: ${d.error}`);
  return d;
}
const s = await api("/sessions", {});
await writeFile(".local/recovery-session.json", JSON.stringify(s));
await api(`/sessions/${s.id}`, { title: "崩溃恢复验证" }, "PATCH");
const o = await api(`/sessions/${s.id}/messages`, {
  message:
    "请写一篇很长的中文文章，逐段介绍 100 个 Python 编程技巧，每个技巧不少于 100 字。不要使用工具。",
  client_msg_id: crypto.randomUUID(),
});
let saved;
for (let i = 0; i < 90; i++) {
  const d = await api(`/sessions/${s.id}/events`);
  if (
    d.frames.filter((f) => f.payload.chunk?.type === "text-delta").length >= 8
  ) {
    saved = d;
    break;
  }
  await sleep(300);
}
assert.ok(saved, "must see persisted incremental text before killing backend");
const visible = saved.frames
  .map((f) =>
    f.payload.chunk?.type === "text-delta" ? f.payload.chunk.text : "",
  )
  .join("");
assert.ok(visible.length > 10);
console.log("persisted visible prefix", visible.length, "characters");
process.kill(Number(await readFile(".local/nimbus.pid", "utf8")), "SIGKILL");
await sleep(800);
const log = await open(".local/nimbus.log", "a");
const p = spawn("node", ["scripts/start.mjs"], {
  detached: true,
  stdio: ["ignore", log.fd, log.fd],
});
p.unref();
await log.close();
let healthy = false;
for (let i = 0; i < 60; i++) {
  try {
    await api("/health");
    healthy = true;
    break;
  } catch {}
  await sleep(300);
}
assert.ok(healthy);
const restored = await api(`/sessions/${s.id}/events`);
assert.equal(restored.outbox.find((x) => x.id === o.id).outcome, "interrupted");
assert.equal((await api(`/sessions/${s.id}`)).agent_state, "idle");
assert.ok(restored.frames.length >= saved.frames.length);
for (const f of saved.frames)
  assert.deepEqual(
    restored.frames.find((x) => x.id === f.id).payload,
    f.payload,
  );
console.log(
  "PASS hard crash preserves all displayed frames and closes old turn",
);
const next = await api(`/sessions/${s.id}/messages`, {
  message:
    "继续，但不需要再写长文。只告诉我上一轮在做什么，并引用上轮已输出内容中的一句话。",
  client_msg_id: crypto.randomUUID(),
});
let after;
for (let i = 0; i < 90; i++) {
  const d = await api(`/sessions/${s.id}/events`);
  if (d.outbox.find((x) => x.id === next.id)?.outcome) {
    after = d;
    break;
  }
  await sleep(700);
}
assert.ok(after);
assert.equal(after.outbox.find((x) => x.id === next.id).outcome, "completed");
assert.notEqual(next.turn_id, o.turn_id);
assert.ok(
  after.events.some(
    (e) =>
      e.type === "system/message" &&
      JSON.stringify(e.payload).includes(
        "Already displayed before backend crash",
      ),
  ),
);
console.log("PASS continue opens a new turn with interrupted context");
