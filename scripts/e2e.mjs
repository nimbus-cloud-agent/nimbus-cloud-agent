import assert from "node:assert/strict";
import { writeFile } from "node:fs/promises";
const base = "http://127.0.0.1:8080";
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
const pause = (ms) => new Promise((r) => setTimeout(r, ms));
async function turn(s, message, opts = {}) {
  const o = await api(`/sessions/${s.id}/messages`, {
    message,
    client_msg_id: crypto.randomUUID(),
    ...opts,
  });
  const started = Date.now();
  while (Date.now() - started < 180000) {
    const d = await api(`/sessions/${s.id}/events`);
    const row = d.outbox.find((x) => x.id === o.id);
    if (row?.outcome) {
      console.log(
        "turn",
        row.outcome,
        row.error || "",
        d.events.length,
        "events",
        d.frames.length,
        "frames",
      );
      if (row.outcome !== "completed") {
        console.log(
          JSON.stringify(
            d.events.filter((x) =>
              ["turn/end", "tool/result"].includes(x.type),
            ),
            null,
            2,
          ),
        );
        throw Error(row.error || row.outcome);
      }
      return d;
    }
    await pause(700);
  }
  throw Error("turn timed out");
}
const s = await api("/sessions", {});
await api(`/sessions/${s.id}`, { title: "端到端验证 · E2B 工作区" }, "PATCH");
await writeFile(".local/e2e-session.json", JSON.stringify(s));
assert.equal(s.sandbox.state, "not_started");
await turn(s, "请只回复“连接成功”，不要使用工具。");
assert.equal((await api(`/sessions/${s.id}`)).sandbox.state, "not_started");
console.log("PASS pure chat does not create sandbox");
const d = await turn(
  s,
  "请使用工具在 /workspace/nimbus-proof.txt 写入字符串 NIMBUS_E2B_OK，然后执行 cat /workspace/nimbus-proof.txt 和 python3 --version，告诉我真实输出。",
);
const sb = (await api(`/sessions/${s.id}`)).sandbox;
assert.equal(sb.state, "running");
console.log("sandbox", sb.e2b_sandbox_id, sb.info.region);
async function terminal(command) {
  const ws = new WebSocket(
    base.replace("http:", "ws:") + `/api/sessions/${s.id}/terminal`,
  );
  let output = "";
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      ws.close();
      reject(Error("terminal timed out: " + output));
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
        if (output.includes("NIMBUS_E2B_OK")) {
          clearTimeout(timer);
          ws.close();
          resolve(output);
        }
      }
    };
    ws.onerror = reject;
  });
}
console.log(
  "terminal",
  String(await terminal("cat /workspace/nimbus-proof.txt")).slice(-400),
);
console.log("PASS Agent and PTY share files");
await api(`/sessions/${s.id}/sandbox/pause`, {});
assert.equal((await api(`/sessions/${s.id}`)).sandbox.state, "paused");
assert.ok((await api(`/sessions/${s.id}/events`)).events.length);
assert.equal((await api(`/sessions/${s.id}`)).sandbox.state, "paused");
console.log("PASS paused history does not resume sandbox");
await api(`/sessions/${s.id}/sandbox/start`, {});
assert.equal(
  (await api(`/sessions/${s.id}`)).sandbox.e2b_sandbox_id,
  sb.e2b_sandbox_id,
);
await terminal("cat /workspace/nimbus-proof.txt");
console.log("PASS resume preserves files and identity");
const ptcSession = await api("/sessions", {});
try {
  const ptc = await turn(
    ptcSession,
    "请使用 run_code 工具调用 tools.sandbox_exec 执行 printf NIMBUS_PTC_OK，打印输出。",
    { model: "deepseek-flash", thinking: "off", mode: "ptc", plan: false },
  );
  assert.ok(
    ptc.events.some(
      (e) =>
        e.type === "tool/ptc-dispatch" &&
        !e.payload.data.isError &&
        JSON.stringify(e.payload).includes("NIMBUS_PTC_OK"),
    ),
  );
  console.log("PASS Wasm PTC in a separate session");
} finally {
  await api(`/sessions/${ptcSession.id}`, undefined, "DELETE");
}
await api(`/sessions/${s.id}/sandbox/pause`, {});
console.log("ALL CORE E2E CHECKS PASSED");
