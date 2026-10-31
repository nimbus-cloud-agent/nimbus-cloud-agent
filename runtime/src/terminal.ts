import { Sandbox, type CommandHandle } from "e2b";

import type { TerminalRequest, TerminalMessage } from "./generated/types.js";
import type { OutputHost, TerminalOutput } from "./ports.js";
export class Terminal {
  private queue: TerminalMessage[];
  private wake: (() => void) | null;
  constructor() {
    this.queue = [];
    this.wake = null;
  }
  write(message: TerminalMessage) {
    if (this.queue.length >= 256) throw Error("Terminal input queue is full");
    this.queue.push(message);
    this.wake?.();
  }
  async run(
    input: TerminalRequest,
    { signal, emit }: OutputHost<TerminalOutput>,
  ) {
    let pty: CommandHandle | undefined,
      output: Promise<void> = Promise.resolve();
    const abort = () => this.wake?.();
    signal.addEventListener("abort", abort);
    try {
      const sb = await Sandbox.connect(input.sandbox_id, {
        apiKey: input.api_key,
        timeoutMs: 60000,
        requestTimeoutMs: 60000,
      });
      signal.throwIfAborted();
      pty = await sb.pty.create({
        cols: input.cols || 80,
        rows: input.rows || 24,
        cwd: "/workspace",
        user: "root",
        timeoutMs: 0,
        envs: { TERM: "xterm-256color" },
        onData: async (data) => {
          output = output.then(() =>
            emit({ data: Buffer.from(data).toString("base64") }),
          );
          output.catch(() => {});
          await output;
        },
      });
      output = output.then(() => emit({ ready: true }));
      output.catch(() => {});
      while (!signal.aborted) {
        if (!this.queue.length)
          await new Promise<void>((resolve) => {
            this.wake = resolve;
          });
        this.wake = null;
        if (signal.aborted) break;
        const msg = this.queue.shift()!;
        if (msg.type === "input")
          await sb.pty.sendInput(
            pty.pid,
            Buffer.from(msg.data ?? "", "base64"),
          );
        if (msg.type === "resize")
          await sb.pty.resize(pty.pid, {
            cols: Math.max(2, Math.min(500, msg.cols ?? 80)),
            rows: Math.max(2, Math.min(200, msg.rows ?? 24)),
          });
        await output;
      }
    } finally {
      signal.removeEventListener("abort", abort);
      if (pty) await pty.kill().catch(() => {});
      await output.catch(() => {});
    }
  }
}
