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
      const trace = await api(`/sessions/${s.id}/turns/${o.turn_id}/events`);
      console.log(
        "turn",
        row.outcome,
        row.error || "",
        trace.events.length,
        "events",
        trace.frames.length,
        "frames",
      );
      if (row.outcome !== "completed") {
        console.log(
          JSON.stringify(
            trace.events.filter((x) =>
              ["turn/end", "tool/result"].includes(x.type),
            ),
            null,
            2,
          ),
        );
        throw Error(row.error || row.outcome);
      }
      return { ...d, ...trace };
    }
    await pause(700);
  }
  throw Error("turn timed out");
}
const s = await api("/sessions", {});
await api(
  `/sessions/${s.id}`,
  { title: "E2E verification · E2B workspace" },
  "PATCH",
);
await writeFile(".local/e2e-session.json", JSON.stringify(s));
assert.equal(s.sandbox.state, "not_started");
await turn(s, 'Reply only with "Connected". Do not use tools.');
assert.equal((await api(`/sessions/${s.id}`)).sandbox.state, "not_started");
console.log("PASS pure chat does not create sandbox");
const d = await turn(
  s,
  "Use tools to write NIMBUS_E2B_OK to /workspace/nimbus-proof.txt, then run cat /workspace/nimbus-proof.txt and python3 --version. Report the actual output.",
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
assert.ok(
  (await api(`/sessions/${s.id}/turns/${d.outbox.at(-1).turn_id}/events`))
    .events.length,
);
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
    "Use the run_code tool to call tools.sandbox_exec and run printf NIMBUS_PTC_OK. Print the output.",
    { model: "deepseek-flash", thinking: "off", plan: false },
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
