import { Worker } from "node:worker_threads";
import { RuntimeFault, errorMessage } from "./errors.js";
import type { TurnRequest, TurnResult } from "./generated/types.js";
import type {
  WorkerCall,
  WorkerMessage,
  WorkerRequest,
} from "./worker-protocol.js";

interface Slot {
  worker: Worker;
  job: Job | null;
}
interface Job {
  id: number;
  input: TurnRequest;
  call: WorkerCall;
  resolve: (v: TurnResult) => void;
  reject: (e: Error) => void;
  signal: AbortSignal;
  calls: Map<number, AbortController>;
  cancel: () => void;
  slot?: Slot;
  timer?: ReturnType<typeof setTimeout>;
}
// Reuse V8 isolates/module caches. CPU-bound QuickJS work cannot stall stdio or
// other sessions. DSH contexts and credentials remain private to each turn.
export class TurnPool {
  private limit: number;
  private workerURL: URL;
  private workers: Set<Slot>;
  private queue: Job[];
  private sequence: number;
  private closed: boolean;
  constructor(limit = 20, workerURL = new URL("./worker.js", import.meta.url)) {
    this.limit = limit;
    this.workerURL = workerURL;
    this.workers = new Set();
    this.queue = [];
    this.sequence = 0;
    this.closed = false;
  }
  run(
    input: TurnRequest,
    call: WorkerCall,
    signal: AbortSignal,
  ): Promise<TurnResult> {
    if (this.closed || signal.aborted)
      return Promise.reject(new RuntimeFault("cancelled", "Turn cancelled"));
    return new Promise((resolve, reject) => {
      const job: Job = {
        id: ++this.sequence,
        input,
        call,
        resolve,
        reject,
        signal,
        calls: new Map(),
        cancel: () => {},
      };
      job.cancel = () => {
        if (job.slot) {
          job.slot.worker.postMessage({ type: "cancel", job: job.id });
          job.timer = setTimeout(
            () =>
              this.retire(
                job.slot!,
                new RuntimeFault("cancelled", "Turn cancelled"),
              ),
            6000,
          );
        } else {
          this.queue = this.queue.filter((x) => x !== job);
          this.finish(job, new RuntimeFault("cancelled", "Turn cancelled"));
        }
      };
      signal.addEventListener("abort", job.cancel, { once: true });
      this.queue.push(job);
      this.drain();
    });
  }
  finish(job: Job, error: Error | null, result?: TurnResult) {
    clearTimeout(job.timer);
    job.signal.removeEventListener("abort", job.cancel);
    for (const controller of job.calls.values()) controller.abort();
    if (error) job.reject(error);
    else job.resolve(result!);
  }
  retire(slot: Slot, error: Error) {
    if (!this.workers.delete(slot)) return;
    if (slot.job) {
      this.finish(slot.job, error);
      slot.job = null;
    }
    void slot.worker.terminate();
    this.drain();
  }
  create() {
    const worker = new Worker(this.workerURL, { stdout: true, stderr: true });
    worker.stdout.resume();
    worker.stderr.resume();
    const slot: Slot = { worker, job: null };
    this.workers.add(slot);
    worker.on("error", () =>
      this.retire(
        slot,
        new RuntimeFault(
          "worker_lost",
          "Turn worker crashed; saved output is preserved",
        ),
      ),
    );
    worker.on("exit", () =>
      this.retire(
        slot,
        new RuntimeFault(
          "worker_lost",
          "Turn worker exited; saved output is preserved",
        ),
      ),
    );
    worker.on("message", async (m: WorkerMessage) => {
      const job = slot.job;
      if (!job || job.id !== m.job) return;
      if ("type" in m && m.type === "cancel-callback") {
        job.calls.get(m.id)?.abort();
        return;
      }
      if ("method" in m) {
        let reply: WorkerRequest;
        const controller = new AbortController();
        job.calls.set(m.id, controller);
        try {
          reply = {
            type: "reply",
            id: m.id,
            result: await job.call(m, controller.signal),
          };
        } catch (e) {
          reply = { type: "reply", id: m.id, error: errorMessage(e) };
        } finally {
          job.calls.delete(m.id);
        }
        if (slot.job === job) worker.postMessage(reply);
      } else if (!("type" in m)) {
        this.finish(job, m.error ? new Error(m.error) : null, m.result);
        slot.job = null;
        this.drain();
      }
    });
    return slot;
  }
  drain() {
    while (!this.closed && this.queue.length) {
      let slot = [...this.workers].find((s) => !s.job);
      if (!slot && this.workers.size < this.limit) slot = this.create();
      if (!slot) break;
      const job = this.queue.shift()!;
      slot.job = job;
      job.slot = slot;
      slot.worker.postMessage({ type: "run", job: job.id, input: job.input });
    }
  }
  stats() {
    return {
      workers: this.workers.size,
      busy: [...this.workers].filter((s) => s.job).length,
      queued: this.queue.length,
    };
  }
  async close() {
    this.closed = true;
    for (const job of this.queue.splice(0))
      this.finish(job, new Error("Runtime shutting down"));
    const workers = [...this.workers];
    for (const slot of workers)
      this.retire(slot, new Error("Runtime shutting down"));
    await Promise.all(workers.map((s) => s.worker.terminate()));
  }
}
