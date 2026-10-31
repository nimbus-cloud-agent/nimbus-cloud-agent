import test from "node:test";
import assert from "node:assert/strict";
import { PassThrough } from "node:stream";
import { RpcPeer } from "../src/rpc/peer.js";
import { ControlClient, registerControl } from "../src/generated/sdk.js";
import { TestRuntime, turnInput } from "./helpers.js";

test(
  "real runtime validates the protocol and reuses workers after rejected turns",
  { timeout: 15000 },
  async () => {
    const runtime = new TestRuntime();
    try {
      await assert.rejects(
        runtime.client.check({ protocolVersion: 2 }),
        /Invalid CheckRequest/,
      );
      const info = await runtime.client.check({ protocolVersion: 3 });
      assert.equal(info.name, "nimbus-agent-runtime");
      const first = await runtime.client.status({});
      assert.equal(first.pid, runtime.child.pid);
      await assert.rejects(
        runtime.client.turn(
          turnInput({
            options: {
              mode: "standard",
              model: "deepseek-chat" as "deepseek-flash",
              thinking: "off",
              plan: false,
            },
          }),
        ),
        /Invalid TurnRequest/,
      );
      for (let i = 0; i < 3; i++) {
        await assert.rejects(
          runtime.client.turn(turnInput({ session_mode: "ptc" })),
          /preset mismatch/,
        );
        const status = await runtime.client.status({});
        assert.equal(status.pid, first.pid);
        assert.equal(status.workers, 1);
        assert.equal(status.busy, 0);
      }
    } finally {
      await runtime.close();
    }
  },
);

test("SDK cancellation is scoped to its reverse request and permits simultaneous calls", async () => {
  const ab = new PassThrough(),
    ba = new PassThrough();
  const a = new RpcPeer(ba, ab),
    b = new RpcPeer(ab, ba);
  const control = new ControlClient(a);
  let waiting!: () => void;
  const ready = new Promise<void>((resolve) => {
    waiting = resolve;
  });
  registerControl(b, {
    event: async () => ({ ok: true }),
    question: async () => ({ answer: { dismissed: true } }),
    tool: async (input, signal) => {
      if (input.call.name === "wait") {
        waiting();
        await new Promise<void>((resolve) =>
          signal.addEventListener("abort", () => resolve(), { once: true }),
        );
        signal.throwIfAborted();
      }
      return { value: input.call.args };
    },
  });
  a.listen();
  b.listen();
  try {
    const controller = new AbortController();
    const cancelled = control.tool(
      {
        operationId: "operation",
        call: { name: "wait", args: {}, call_id: "wait" },
      },
      controller.signal,
    );
    const failure = assert.rejects(cancelled, /abort/i);
    await ready;
    controller.abort();
    assert.deepEqual(
      await control.tool({
        operationId: "operation",
        call: { name: "echo", args: { alive: true }, call_id: "echo" },
      }),
      { value: { alive: true } },
    );
    await failure;
  } finally {
    a.close();
    b.close();
    ab.destroy();
    ba.destroy();
  }
});
