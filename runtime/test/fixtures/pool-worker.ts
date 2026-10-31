import { parentPort, threadId } from "node:worker_threads";
import type { WorkerRequest } from "../../src/worker-protocol.js";
const port = parentPort!;
let job: number, timer: ReturnType<typeof setTimeout>;
port.on("message", (m: WorkerRequest) => {
  if (m.type === "reply" && m.id === 1)
    port.postMessage({ job, result: { finished: true } });
  if (m.type === "cancel") {
    clearTimeout(timer);
    port.postMessage({ job: m.job, error: "cancelled" });
  }
  if (m.type !== "run") return;
  job = m.job;
  if (m.input.message === "cancel-tool") {
    port.postMessage({
      job,
      id: 1,
      method: "tool",
      data: { name: "wait", args: {}, call_id: "tool" },
    });
    setTimeout(
      () => port.postMessage({ type: "cancel-callback", job, id: 1 }),
      20,
    );
    return;
  }
  if (m.input.message === "crash") process.exit(1);
  if (m.input.message === "hang") while (true) {}
  port.postMessage({
    job,
    id: 2,
    method: "event",
    data: {
      event: "done",
      data: { seq: threadId, outcome: "completed", error: "" },
    },
  });
  timer = setTimeout(
    () => port.postMessage({ job, result: { finished: true } }),
    Number(m.input.message) || 0,
  );
});
