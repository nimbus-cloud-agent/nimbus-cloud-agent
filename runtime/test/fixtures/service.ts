#!/usr/bin/env node
// Real SDK fixture used by Go integration tests; the same generated contract is
// exercised in both directions, without implementing a second framing parser.
import { RpcPeer } from "../../src/rpc/peer.js";
import { ControlClient, registerRuntime } from "../../src/generated/sdk.js";
const peer = new RpcPeer(process.stdin, process.stdout),
  control = new ControlClient(peer);
registerRuntime(peer, {
  check: async () => ({
    name: "nimbus-agent-runtime",
    version: "fixture",
    protocolVersion: 2,
  }),
  status: async () => ({
    pid: process.pid,
    workers: 0,
    busy: 0,
    queued: 0,
    operations: 0,
  }),
  mcp: async () => ({ ok: true, oauth_state: "none", oauth_data: {} }),
  images: async (input) => ({ ref: { name: input.name }, data: input.data }),
  e2b: async (input, signal) => {
    if (input.args?.command === "wait") {
      await control.output(
        {
          operationId: input.operationId,
          data: { channel: "stdout", text: "waiting" },
        },
        signal,
      );
      await new Promise<void>((resolve) => {
        if (signal.aborted) resolve();
        else signal.addEventListener("abort", () => resolve(), { once: true });
      });
      signal.throwIfAborted();
    }
    if (input.stream)
      for (let i = 0; i < 3; i++)
        await control.output(
          {
            operationId: input.operationId,
            data: { channel: "stdout", text: String(i) },
          },
          signal,
        );
    if (input.op === "read_file")
      return {
        readFile: { content: input.args?.path ?? "", truncated: false },
      };
    return {
      command: { stdout: "", stderr: "", exitCode: 0, truncated: false },
    };
  },
  turn: async (input, signal) => {
    if (input.message === "cancel-tool") {
      const controller = new AbortController();
      const pending = control.tool(
        {
          operationId: input.operationId,
          call: { name: "wait", args: {}, call_id: "tool" },
        },
        controller.signal,
      );
      setTimeout(() => controller.abort(), 100);
      try {
        await pending;
        throw Error("callback should have been cancelled");
      } catch (error) {
        if (!controller.signal.aborted) throw error;
      }
    } else {
      await control.question(
        {
          operationId: input.operationId,
          data: { questions: [{ id: "review", question: "Review?" }] },
        },
        signal,
      );
    }
    await control.event(
      { operationId: input.operationId, event: "done", data: {} },
      signal,
    );
    return { finished: true };
  },
  terminal: async () => ({ ok: true }),
  terminalWrite: async () => ({ ok: true }),
});
peer.listen();
process.stdin.on("end", () => {
  peer.close();
  process.exit(0);
});
