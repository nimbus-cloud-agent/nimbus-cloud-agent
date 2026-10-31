// Uses real Flash/DSH/E2B. Kills only the supervised Node child, and only when
// no pre-existing user session is running. All test sessions are removed.
import assert from "node:assert/strict";
import { setTimeout as delay } from "node:timers/promises";
interface RuntimeState {
  state: string;
  pid: number;
  generation: number;
}
interface Session {
  id: string;
  agent_state: string;
  options: Record<string, unknown>;
}
interface Outbox {
  id: string;
  turn_id: string;
  outcome?: string;
  error?: string;
}
interface Frame {
  id: number;
  turn_id: string;
  payload?: { chunk?: { text?: string } };
}
interface Question {
  id: string;
  turn_id: string;
  status: string;
}
interface Snapshot {
  outbox: Outbox[];
  events: { type: string; payload: unknown }[];
  frames: Frame[];
  questions: Question[];
}
const base = "http://127.0.0.1:8080",
  ids: string[] = [];
async function api<T = unknown>(
  path: string,
  body?: unknown,
  method = body ? "POST" : "GET",
): Promise<T> {
  const r = await fetch(base + "/api" + path, {
    method,
    headers: { "Content-Type": "application/json" },
    body: body ? JSON.stringify(body) : undefined,
  });
  const value = r.status === 204 ? null : await r.json();
  assert.ok(r.ok, `${r.status}: ${value?.error}`);
  return value as T;
}
async function until<T>(
  fn: () => Promise<T | false | null | undefined>,
  label: string,
  timeout = 180000,
): Promise<T> {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    const value = await fn();
    if (value) return value;
    await delay(150);
  }
  throw Error("Timed out: " + label);
}
const health = async () =>
  (await api<{ runtime: RuntimeState }>("/health")).runtime;
async function create(mode = "standard", plan = false) {
  const s = await api<Session>("/sessions", {});
  ids.push(s.id);
  await api(
    `/sessions/${s.id}`,
    {
      title: "常驻运行时验证 · " + mode,
      options: { ...s.options, mode, plan },
    },
    "PATCH",
  );
  return s;
}
const send = (s: Session, message: string) =>
  api<Outbox>(`/sessions/${s.id}/messages`, {
    message,
    client_msg_id: crypto.randomUUID(),
  });
const events = (s: Session) => api<Snapshot>(`/sessions/${s.id}/events`);
async function settled(s: Session, o: Outbox, outcome = "completed") {
  return until(async () => {
    const d = await events(s),
      row = d.outbox.find((x) => x.id === o.id);
    if (!row?.outcome) return;
    assert.equal(row.outcome, outcome, row.error);
    return d;
  }, "turn completion");
}
async function pending(s: Session, o: Outbox) {
  return until(async () => {
    const d = await events(s),
      row = d.outbox.find((x) => x.id === o.id);
    assert.ok(!row?.outcome, "Plan ended before review: " + row?.error);
    return d.questions.find(
      (q) => q.turn_id === o.turn_id && q.status === "pending",
    );
  }, "plan review");
}
async function killRuntime() {
  const sessions = await api<Session[]>("/sessions");
  assert.ok(
    sessions.every((s) => ids.includes(s.id) || s.agent_state === "idle"),
    "A user session is active; refusing fault injection",
  );
  const old = await health();
  process.kill(old.pid, "SIGKILL");
  return until(
    async () => {
      const now = await health();
      return now.state === "running" &&
        now.generation > old.generation &&
        now.pid !== old.pid
        ? now
        : null;
    },
    "Node restart",
    15000,
  );
}
async function terminal(s: Session) {
  const ws = new WebSocket(
    base.replace("http:", "ws:") + `/api/sessions/${s.id}/terminal`,
  );
  let output = "";
  return new Promise<void>((resolve, reject) => {
    const timer = setTimeout(() => {
      ws.close();
      reject(Error("PTY timed out"));
    }, 30000);
    ws.onopen = () =>
      ws.send(JSON.stringify({ type: "resize", cols: 100, rows: 30 }));
    ws.onerror = () => {
      clearTimeout(timer);
      reject(Error("PTY connection failed"));
    };
    ws.onmessage = (e) => {
      const m = JSON.parse(e.data as string);
      if (m.error) {
        clearTimeout(timer);
        ws.close();
        reject(Error(m.error));
      }
      if (m.ready)
        ws.send(
          JSON.stringify({
            type: "input",
            data: Buffer.from(
              "cat /workspace/rpc-proof.txt; printf '\\nPTY_RPC_OK\\n'\r",
            ).toString("base64"),
          }),
        );
      if (m.data) output += Buffer.from(m.data, "base64").toString();
      if (output.includes("RPC_E2B_OK") && output.includes("PTY_RPC_OK")) {
        clearTimeout(timer);
        ws.close();
        resolve();
      }
    };
  });
}
const plan =
  "这是运行时集成测试。不要用沙箱、文件或 MCP。立即通过 run_code 调用 tools.exit_plan_mode 提交计划，内容是等待批准后回复 PLAN_OK；必须使用工具提交。run_code 的 timeoutMs 设为 10000。";
try {
  const first = await health();
  assert.ok(first.pid);
  const a = await create(),
    b = await create("ptc", true);
  await settled(
    a,
    await send(
      a,
      "请记住标记 NIMBUS_RPC_MEMORY，只回复 MEMORY_SAVED，不使用工具。",
    ),
  );
  const reviewTurn = await send(b, plan);
  await pending(b, reviewTurn);
  const second = await settled(
    a,
    await send(a, "上轮让你记住的标记是什么？只回复标记，不使用工具。"),
  );
  assert.ok(
    second.events.some(
      (e) =>
        e.type === "assistant/message" &&
        JSON.stringify(e.payload).includes("NIMBUS_RPC_MEMORY"),
    ),
  );
  assert.equal((await health()).pid, first.pid);
  assert.equal(
    (await events(b)).questions.find((q) => q.turn_id === reviewTurn.turn_id)
      ?.status,
    "pending",
  );
  console.log(
    "PASS sequential turns reuse Node; native history resumes; another session completes while PTC waits for review",
  );
  await api(`/sessions/${b.id}/stop`, {});
  await settled(b, reviewTurn, "interrupted");
  assert.equal((await health()).pid, first.pid);
  console.log(
    "PASS cancellation interrupts only its turn, preserving the shared process",
  );
  const crashReview = await send(b, plan);
  const question = await pending(b, crashReview);
  const generating = await send(
    a,
    "不要用工具，请逐行写出从 1 到 2000 的所有数字，持续输出，不要省略或概括。",
  );
  const before = await until(async () => {
    const d = await events(a);
    return d.frames.some(
      (f) => f.turn_id === generating.turn_id && f.payload?.chunk?.text,
    )
      ? d
      : null;
  }, "partial output");
  const partial = before.frames.filter((f) => f.turn_id === generating.turn_id);
  assert.ok(!before.outbox.find((o) => o.id === generating.id)?.outcome);
  const restarted = await killRuntime();
  const recovered = await settled(a, generating, "interrupted");
  await settled(b, crashReview, "interrupted");
  for (const frame of partial)
    assert.deepEqual(
      recovered.frames.find((f) => f.id === frame.id),
      frame,
    );
  assert.equal(
    (await events(b)).questions.find((q) => q.id === question.id)?.status,
    "interrupted",
  );
  await settled(
    a,
    await send(
      a,
      "取消计数任务。这是恢复检查，只回复 RECOVERY_OK，不要使用工具。",
    ),
  );
  assert.equal((await health()).pid, restarted.pid);
  console.log(
    "PASS active Node SIGKILL: automatic restart, interrupted turns/review, durable partial output, new turn recovery",
  );
  const idle = await killRuntime();
  await settled(a, await send(a, "只回复 IDLE_RESTART_OK，不使用工具。"));
  assert.equal((await health()).pid, idle.pid);
  console.log("PASS idle Node SIGKILL restarts without a new request");
  await api(
    `/sessions/${b.id}`,
    { options: { ...b.options, mode: "ptc", plan: false } },
    "PATCH",
  );
  const tools = await settled(
    b,
    await send(
      b,
      "之前的计划测试已取消。请通过 run_code 调用 tools.sandbox_exec，在 /workspace/rpc-proof.txt 写入 RPC_E2B_OK，再 cat 此文件，报告真实输出。",
    ),
  );
  assert.ok(
    tools.events.some(
      (e) =>
        e.type === "tool/result" &&
        JSON.stringify(e.payload).includes("RPC_E2B_OK"),
    ),
  );
  await terminal(b);
  assert.equal((await health()).pid, idle.pid);
  console.log(
    "PASS PTC → JSON-RPC tool callback → E2B streaming; resident PTY input/resize/output; PID unchanged",
  );
} finally {
  for (const id of ids)
    await api(`/sessions/${id}`, undefined, "DELETE").catch((e) =>
      console.error("cleanup:", e.message),
    );
}
