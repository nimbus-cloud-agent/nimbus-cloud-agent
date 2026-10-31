import { parentPort } from "node:worker_threads";
import { runTurn } from "./turn.js";
import { errorMessage } from "./errors.js";
import type {
  WorkerRequest,
  WorkerMessage,
  WorkerCall,
  WorkerReply,
} from "./worker-protocol.js";
const port = parentPort;
if (!port) throw Error("Turn worker requires a parent port");
let current: { id: number; controller: AbortController } | undefined,
  sequence = 0;
const pending = new Map<
  number,
  {
    resolve: (v: WorkerReply) => void;
    reject: (e: Error) => void;
    cleanup: () => void;
  }
>();
const request: WorkerCall = (call, signal) => {
  signal?.throwIfAborted();
  if (!current) throw Error("Worker is idle");
  const id = ++sequence,
    job = current.id;
  const abort = () =>
    port.postMessage({
      type: "cancel-callback",
      job,
      id,
    } satisfies WorkerMessage);
  return new Promise((resolve, reject) => {
    pending.set(id, {
      resolve,
      reject,
      cleanup: () => signal?.removeEventListener("abort", abort),
    });
    signal?.addEventListener("abort", abort, { once: true });
    port.postMessage({ job, id, ...call } satisfies WorkerMessage);
  });
};
port.on("message", async (m: WorkerRequest) => {
  if (m.type === "reply") {
    const p = pending.get(m.id);
    pending.delete(m.id);
    p?.cleanup();
    if (m.error !== undefined) p?.reject(Error(m.error));
    else p?.resolve(m.result);
  } else if (m.type === "cancel" && current?.id === m.job) {
    current.controller.abort();
  } else if (m.type === "run") {
    const controller = new AbortController();
    current = { id: m.job, controller };
    try {
      await runTurn(m.input, {
        signal: controller.signal,
        emit: async (data) => {
          const reply = await request({ method: "event", data });
          if (reply.method !== "event")
            throw Error("Invalid worker event reply");
        },
        askQuestion: async (data) => {
          const reply = await request({ method: "question", data });
          if (reply.method !== "question")
            throw Error("Invalid worker question reply");
          return reply.answer;
        },
        callTool: async (data, signal) => {
          const reply = await request({ method: "tool", data }, signal);
          if (reply.method !== "tool") throw Error("Invalid worker tool reply");
          return reply.value;
        },
      });
      port.postMessage({
        job: m.job,
        result: { finished: true },
      } satisfies WorkerMessage);
    } catch (error) {
      port.postMessage({
        job: m.job,
        error: m.input.api_key
          ? errorMessage(error).replaceAll(m.input.api_key, "[REDACTED]")
          : errorMessage(error),
      } satisfies WorkerMessage);
    } finally {
      for (const p of pending.values()) {
        p.cleanup();
        p.reject(Error("Turn finished"));
      }
      pending.clear();
      current = undefined;
    }
  }
});
