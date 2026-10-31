// Real Go/E2B terminal regression: creates its own sandbox and interrupts only
// an idle runtime. No existing session is changed. Requires local Chrome.
import assert from "node:assert/strict";
import { chromium } from "playwright";
import type { Session, TerminalMessage } from "../web/src/api.ts";

const base = "http://127.0.0.1:8080";
let terminalFailure: Error | undefined;
interface Health {
  runtime: { state: string; pid: number; generation: number };
}
async function api<T>(
  path: string,
  body?: unknown,
  method = body ? "POST" : "GET",
): Promise<T> {
  const response = await fetch(base + "/api" + path, {
    method,
    headers: { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  assert.ok(response.ok, `HTTP ${response.status} ${path}`);
  return response.status === 204
    ? (undefined as T)
    : ((await response.json()) as T);
}
async function until<T>(
  fn: () => T | Promise<T>,
  description: string,
): Promise<NonNullable<T>> {
  const deadline = Date.now() + 30000;
  while (Date.now() < deadline) {
    if (terminalFailure) throw terminalFailure;
    const value = await fn();
    if (value) return value as NonNullable<T>;
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  throw Error("Timed out: " + description);
}
const originals = await api<Session[]>("/sessions");
assert.ok(
  originals.every((s) => s.agent_state === "idle"),
  "Node interruption requires idle user sessions",
);
const session = await api<Session>("/sessions", {});
const browser = await chromium.launch({ channel: "chrome" });
let ws: WebSocket | undefined;
let stoppedPID: number | undefined;
try {
  await api(`/sessions/${session.id}/sandbox/start`, {});
  ws = new WebSocket(
    base.replace("http:", "ws:") + `/api/sessions/${session.id}/terminal`,
  );
  let output = "",
    ready = false,
    closed = false;
  ws.onclose = () => {
    closed = true;
  };
  ws.onmessage = (event) => {
    const message = JSON.parse(String(event.data)) as TerminalMessage;
    if (message.data) output += Buffer.from(message.data, "base64").toString();
    if (message.ready) ready = true;
    if (message.error) terminalFailure = Error(message.error);
  };
  await until(() => ready, "PTY ready");
  const send = (command: string) =>
    ws!.send(
      JSON.stringify({
        type: "input",
        data: Buffer.from(command).toString("base64"),
      }),
    );
  ws.send(JSON.stringify({ type: "resize", cols: 112, rows: 34 }));
  send(
    "stty -echo; export NIMBUS_TERMINAL_SESSION=GO_PTY_PERSISTED; stty size\r",
  );
  await until(() => output.includes("34 112"), "PTY resize");
  send("printf '\\n中文终端_OK\\n'\r");
  await until(() => output.includes("中文终端_OK"), "UTF-8 output");

  const page = await browser.newPage({
    viewport: { width: 1440, height: 1000 },
  });
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto(base + "/#" + session.id);
  const input = page.locator(".xterm-helper-textarea");
  await input.waitFor({ state: "attached" });
  await page.locator(".xterm").click();
  await page.keyboard.type("stty -echo; printf '\\nBROWSER_GO_PTY_OK\\n'");
  await page.keyboard.press("Enter");
  await until(
    async () =>
      (await page.locator(".xterm-rows").innerText()).includes(
        "BROWSER_GO_PTY_OK",
      ),
    "browser PTY output",
  );

  const before = (await api<Health>("/health")).runtime;
  process.kill(before.pid, "SIGSTOP");
  stoppedPID = before.pid;
  send("printf '\\nNODE_STOPPED_PTY_OK\\n'\r");
  await until(
    () => output.includes("NODE_STOPPED_PTY_OK"),
    "PTY while Node is stopped",
  );
  assert.equal(closed, false);
  process.kill(before.pid, "SIGKILL");
  stoppedPID = undefined;
  const after = await until(async () => {
    const now = (await api<Health>("/health")).runtime;
    return now.state === "running" &&
      now.pid !== before.pid &&
      now.generation > before.generation
      ? now
      : null;
  }, "Node restart");
  send("printf '\\n%s\\n' \"$NIMBUS_TERMINAL_SESSION\"\r");
  await until(
    () => output.includes("\r\nGO_PTY_PERSISTED\r\n"),
    "same shell after Node restart",
  );
  assert.equal(closed, false);
  await page.locator(".xterm").click();
  await page.keyboard.type("printf '\\nBROWSER_AFTER_RESTART_OK\\n'");
  await page.keyboard.press("Enter");
  await until(
    async () =>
      (await page.locator(".xterm-rows").innerText()).includes(
        "BROWSER_AFTER_RESTART_OK",
      ),
    "browser after Node restart",
  );
  assert.deepEqual(errors, []);
  await page.screenshot({
    path: ".local/go-terminal-after-node-restart.png",
    fullPage: true,
  });
  console.log(
    "PASS Go PTY UTF-8/resize; browser and same shell remain live through Node SIGSTOP/SIGKILL/restart",
    before.pid,
    after.pid,
  );
  send("sleep 30\r");
  await new Promise((resolve) => setTimeout(resolve, 200));
  send("\x03");
  send("printf '\\nCTRL_C_OK\\n'\r");
  await until(() => output.includes("CTRL_C_OK"), "Ctrl-C");
  console.log("PASS interactive Ctrl-C and subsequent command");
} finally {
  if (stoppedPID) process.kill(stoppedPID, "SIGCONT");
  ws?.close();
  await browser.close();
  await api(`/sessions/${session.id}`, undefined, "DELETE");
}
