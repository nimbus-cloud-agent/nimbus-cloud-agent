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
const m = await api("/mcp/servers", {
  name: "本地 OAuth 验证",
  url: "http://127.0.0.1:18081/mcp",
  enabled: true,
});
await writeFile(".local/mcp-fixture-id", m.id);
const auth = await api(`/mcp/servers/${m.id}/oauth/start`, {});
assert.ok(auth.auth_url);
const authorize = await fetch(auth.auth_url, { redirect: "manual" });
const cb = authorize.headers.get("location");
assert.ok(cb);
const callback = await fetch(cb, { redirect: "manual" });
assert.equal(callback.status, 303);
const servers = await api("/mcp/servers");
assert.equal(servers.find((x) => x.id === m.id).oauth_state, "ok");
assert.ok(!JSON.stringify(servers).includes("access_token"));
console.log(
  "PASS OAuth discovery, registration, PKCE exchange, masked credentials",
);
const result = await api(`/mcp/servers/${m.id}/test`, {});
assert.equal(result.tools[0].name, "echo");
console.log("PASS authenticated MCP listTools");
await fetch("http://127.0.0.1:18081/expire");
await api(`/mcp/servers/${m.id}/test`, {});
const status = await fetch("http://127.0.0.1:18081/status").then((r) =>
  r.json(),
);
assert.ok(status.refreshes > 0);
console.log("PASS refresh token after 401");
const s = await api("/sessions", {});
await api(`/sessions/${s.id}`, { title: "OAuth + MCP 验证" }, "PATCH");
const o = await api(`/sessions/${s.id}/messages`, {
  message:
    "调用 MCP 的 echo 工具，text 参数为 hello-nimbus，返回真实结果，不要调用沙箱。",
  client_msg_id: crypto.randomUUID(),
});
let done;
for (let i = 0; i < 90; i++) {
  const d = await api(`/sessions/${s.id}/events`);
  if (d.outbox.find((x) => x.id === o.id)?.outcome) {
    done = d;
    break;
  }
  await new Promise((r) => setTimeout(r, 1000));
}
assert.ok(done);
assert.ok(
  done.events.some(
    (e) =>
      e.type === "tool/result" &&
      JSON.stringify(e.payload).includes("MCP_OK:hello-nimbus"),
  ),
);
assert.equal((await api(`/sessions/${s.id}`)).sandbox.state, "not_started");
console.log("PASS real Agent MCP call without sandbox");
await api(`/mcp/servers/${m.id}`, undefined, "DELETE");
console.log("MCP fixture config cleaned up");
