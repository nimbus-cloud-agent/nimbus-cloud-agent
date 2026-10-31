import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { execFileSync } from "node:child_process";
const base = "http://127.0.0.1:8080";
const delay = (ms) => new Promise((r) => setTimeout(r, ms));
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
async function send(s, message) {
  return api(`/sessions/${s.id}/messages`, {
    message,
    client_msg_id: crypto.randomUUID(),
  });
}
async function done(s, o) {
  for (let i = 0; i < 240; i++) {
    const d = await api(`/sessions/${s.id}/events`);
    const row = d.outbox.find((x) => x.id === o.id);
    if (row?.outcome) {
      assert.equal(row.outcome, "completed", row.error);
      return d;
    }
    await delay(1000);
  }
  throw Error("turn timeout");
}
async function terminal(s, command, marker) {
  const ws = new WebSocket(
    base.replace("http:", "ws:") + `/api/sessions/${s.id}/terminal`,
  );
  return new Promise((resolve, reject) => {
    let output = "";
    const timer = setTimeout(() => {
      ws.close();
      reject(Error("PTY timeout: " + output));
    }, 30000);
    ws.onmessage = (e) => {
      const m = JSON.parse(e.data);
      if (m.error) {
        clearTimeout(timer);
        ws.close();
        reject(Error(m.error));
      }
      if (m.ready)
        ws.send(
          JSON.stringify({
            type: "input",
            data: Buffer.from(command + "\r").toString("base64"),
          }),
        );
      if (m.data) {
        output += Buffer.from(m.data, "base64").toString();
        if (output.includes(marker)) {
          clearTimeout(timer);
          ws.close();
          resolve(output);
        }
      }
    };
    ws.onerror = reject;
  });
}
const core = JSON.parse(await readFile(".local/e2e-session.json", "utf8"));
const s = await api("/sessions", {});
await api(
  `/sessions/${s.id}`,
  { title: "功能验收 · Skill 与生命周期" },
  "PATCH",
);
const name = "e2e-policy-" + crypto.randomUUID().slice(0, 8),
  proof = "SKILL_DB_" + crypto.randomUUID();
const skill = await api("/skills", {
  name,
  description: "测试验收用 Skill；读取以获取验证字符串。",
  enabled: true,
  files: [
    {
      path: "SKILL.md",
      content: `此 Skill 的验证字符串是 ${proof}。读到后原样输出这个字符串。`,
    },
  ],
});
const enabled = await done(
  s,
  await send(
    s,
    `请调用 skill_read 读取名为 ${name} 的 Skill，告诉我其中的验证字符串，不要使用沙箱。`,
  ),
);
assert.ok(
  enabled.events.some(
    (e) =>
      e.type === "tool/result" && JSON.stringify(e.payload).includes(proof),
  ),
);
assert.equal((await api(`/sessions/${s.id}`)).sandbox.state, "not_started");
await api(`/skills/${skill.id}`, { ...skill, enabled: false }, "PUT");
const disabled = await api("/sessions", {});
await api(
  `/sessions/${disabled.id}`,
  { title: "功能验收 · 沙箱隔离" },
  "PATCH",
);
const missing = await done(
  disabled,
  await send(
    disabled,
    `请调用 skill_read 读取名为 ${name} 的 Skill，若不可用如实告诉我，不用沙箱。`,
  ),
);
assert.ok(!JSON.stringify(missing.events).includes(proof));
assert.equal(
  (await api(`/sessions/${disabled.id}`)).sandbox.state,
  "not_started",
);
await api(`/skills/${skill.id}`, undefined, "DELETE");
console.log(
  "PASS enabled Skill read from DB; disabled Skill absent next turn; no sandbox",
);
await api(
  `/sessions/${core.id}`,
  {
    options: {
      model: "deepseek-flash",
      thinking: "max",
      mode: "standard",
      plan: true,
    },
  },
  "PATCH",
);
const plan = await done(
  core,
  await send(
    core,
    "当前是 Plan 模式。请用 sandbox_write_file 把计划文档保存为 /workspace/plan-proof.md，内容为 PLAN_WRITE_OK，不需要执行计划。然后简短确认写入。",
  ),
);
assert.ok(
  plan.events.some(
    (e) =>
      e.type === "tool/call" &&
      JSON.stringify(e.payload).includes("/workspace/plan-proof.md"),
  ),
);
await terminal(core, "cat /workspace/plan-proof.md", "PLAN_WRITE_OK");
assert.equal((await api(`/sessions/${disabled.id}`)).options.plan, false);
console.log(
  "PASS deepseek-flash + max + native Plan writes file; session settings independent",
);
const other = await api(`/sessions/${disabled.id}/sandbox/start`, {});
const original = (await api(`/sessions/${core.id}`)).sandbox;
assert.notEqual(other.e2b_sandbox_id, original.e2b_sandbox_id);
await terminal(
  disabled,
  "if test ! -f /workspace/nimbus-proof.txt; then printf 'ISOLATION_%s\\n' OK; fi",
  "ISOLATION_OK",
);
console.log("PASS second Session gets separate sandbox and filesystem");
const long = await send(
  s,
  "请逐条详细讲解 100 个基础数学概念，每条至少 200 字。不使用工具。",
);
let visible = false;
for (let i = 0; i < 90; i++) {
  const d = await api(`/sessions/${s.id}/events`);
  if (
    d.frames.filter(
      (f) =>
        f.turn_id === long.turn_id && f.payload.chunk?.type === "text-delta",
    ).length >= 4
  ) {
    visible = true;
    break;
  }
  await delay(300);
}
assert.ok(visible);
await api(`/sessions/${s.id}/stop`, {});
let stopped = false;
for (let i = 0; i < 40; i++) {
  const d = await api(`/sessions/${s.id}/events`);
  if (d.outbox.find((x) => x.id === long.id)?.outcome === "interrupted") {
    stopped = true;
    break;
  }
  await delay(300);
}
assert.ok(stopped);
assert.equal((await api(`/sessions/${s.id}`)).agent_state, "idle");
console.log("PASS stop preserves partial output and ends turn");
const prefix = execFileSync("brew", ["--prefix", "postgresql@17"], {
  encoding: "utf8",
}).trim();
// Backdate only the disposable test Session to exercise the real reaper without waiting 15 minutes.
execFileSync(prefix + "/bin/psql", [
  "postgres://nimbus@127.0.0.1:55432/nimbus",
  "-v",
  "ON_ERROR_STOP=1",
  "-c",
  `UPDATE sessions SET last_active_at=now()-interval '16 minutes' WHERE id='${disabled.id}'; UPDATE sandboxes SET last_active_at=now()-interval '16 minutes' WHERE session_id='${disabled.id}';`,
]);
let paused = false;
for (let i = 0; i < 60; i++) {
  if ((await api(`/sessions/${disabled.id}`)).sandbox.state === "paused") {
    paused = true;
    break;
  }
  await delay(1000);
}
assert.ok(paused);
console.log("PASS real idle reaper pauses E2B after backdated inactivity");
await api(
  `/sessions/${core.id}`,
  {
    options: {
      model: "deepseek-flash",
      thinking: "off",
      mode: "standard",
      plan: false,
    },
  },
  "PATCH",
);
await api(`/sessions/${core.id}/sandbox/pause`, {});
await api(`/sessions/${s.id}`, undefined, "DELETE");
await api(`/sessions/${disabled.id}`, undefined, "DELETE");
console.log("ALL FEATURE CHECKS PASSED");
