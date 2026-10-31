import test from "node:test";
import assert from "node:assert/strict";
import { runWasm, createHumanWaitClock } from "./wasm.mjs";
test("TypeScript executes inside Wasm and only explicit bindings can cause effects", async () => {
  let calls = 0;
  const r = await runWasm({
    program:
      "const n: number = await tools.double({n:21}); console.log(typeof process, typeof require, typeof fetch); return n;",
    timeoutMs: 3000,
    bindings: [
      {
        global: "tools",
        functions: {
          double: async ({ n }) => {
            calls++;
            return n * 2;
          },
        },
      },
    ],
  });
  assert.equal(r.value, 42);
  assert.equal(calls, 1);
  assert.deepEqual(r.logs, ["undefined undefined undefined"]);
});
test("infinite loops are interrupted", async () => {
  const r = await runWasm({
    program: "while(true){}",
    timeoutMs: 30,
    bindings: [],
  });
  assert.equal(r.error.kind, "timeout");
});
test("Node import cannot escape WebAssembly", async () => {
  const r = await runWasm({
    program: 'return await import("node:fs");',
    timeoutMs: 1000,
    bindings: [],
  });
  assert.ok(r.error);
});
test("host binding failures are observable to the program", async () => {
  const r = await runWasm({
    program: "try { await tools.fail({}); } catch(e) { return e.message; }",
    timeoutMs: 1000,
    bindings: [
      {
        global: "tools",
        functions: {
          fail: async () => {
            throw Error("remote failure");
          },
        },
      },
    ],
  });
  assert.equal(r.value, "remote failure");
});
test("pending promises cannot bypass the deadline", async () => {
  const r = await runWasm({
    program: "await new Promise(()=>{});",
    timeoutMs: 30,
    bindings: [],
  });
  assert.equal(r.error.kind, "timeout");
});
test("memory allocation beyond the instance cap fails", async () => {
  const r = await runWasm({
    program: "return new Uint8Array(300*1024*1024).length;",
    timeoutMs: 1000,
    bindings: [],
  });
  assert.ok(r.error);
});
test("PTC SDK rejection preserves ToolCallError identity", async () => {
  const r = await runWasm({
    program:
      "try { await tools.fail({}); } catch(e) { return [e instanceof ToolCallError,e.toolName]; }",
    timeoutMs: 1000,
    bindings: [
      {
        global: "tools",
        errorClass: { name: "ToolCallError", memberNameProperty: "toolName" },
        functions: {
          fail: async () => {
            throw Error("remote failure");
          },
        },
      },
    ],
  });
  assert.deepEqual(r.value, [true, "fail"]);
});

test("human review excludes idle waiting without removing the execution deadline", async () => {
  const clock = createHumanWaitClock();
  const bindings = [
    {
      global: "tools",
      functions: {
        review: async () => {
          const resume = clock.pause();
          try {
            await new Promise((r) => setTimeout(r, 150));
            return "approved";
          } finally {
            resume();
          }
        },
      },
    },
  ];
  const accepted = await runWasm(
    { program: "return await tools.review({});", bindings, timeoutMs: 60 },
    clock,
  );
  assert.equal(accepted.value, "approved");
  const loop = await runWasm(
    {
      program: "await tools.review({}); while(true){}",
      bindings,
      timeoutMs: 60,
    },
    clock,
  );
  assert.equal(loop.error.kind, "timeout");
});

test("human review cannot shield concurrent CPU loops or cancellation", async () => {
  const clock = createHumanWaitClock();
  const resume = clock.pause();
  try {
    const loop = await runWasm(
      { program: "while(true){}", timeoutMs: 50 },
      clock,
    );
    assert.equal(loop.error.kind, "timeout");
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 80);
    try {
      const aborted = await runWasm(
        {
          program: "await new Promise(()=>{});",
          timeoutMs: 50,
          signal: controller.signal,
        },
        clock,
      );
      assert.equal(aborted.error.kind, "abort");
    } finally {
      clearTimeout(timer);
    }
  } finally {
    resume();
  }
});
