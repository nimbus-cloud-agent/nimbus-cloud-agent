import test from "node:test";
import assert from "node:assert/strict";
import { setTimeout as delay } from "node:timers/promises";
import { TurnPool } from "../src/pool.js";
import { turnInput } from "./helpers.js";
const url = new URL("./fixtures/pool-worker.js", import.meta.url);

test(
  "workers reuse isolates; cancellation, crashes and hung workers leave other sessions running",
  { timeout: 15000 },
  async () => {
    const pool = new TurnPool(2, url),
      threads: number[] = [];
    const run = (message: string, signal = new AbortController().signal) =>
      pool.run(
        turnInput({ message }),
        async (call) => {
          assert.equal(call.method, "event");
          if (call.method === "event" && call.data.event === "done")
            threads.push(call.data.data.seq);
          return { method: "event" };
        },
        signal,
      );
    try {
      await run("one");
      await run("two");
      assert.equal(threads[0], threads[1]);
      const controller = new AbortController();
      const failed = assert.rejects(
        run("10000", controller.signal),
        /cancelled/,
      );
      const survivor = run("100");
      controller.abort();
      await failed;
      assert.equal((await survivor).finished, true);
      await assert.rejects(run("crash"), /exited/);
      assert.equal((await run("after crash")).finished, true);
      const stuck = new AbortController();
      const hung = assert.rejects(run("hang", stuck.signal), /cancelled/);
      await delay(100);
      stuck.abort();
      assert.equal((await run("independent")).finished, true);
      await hung;
      assert.equal((await run("replacement")).finished, true);
    } finally {
      await pool.close();
    }
  },
);
test("queued cancellation never starts the queued turn", async () => {
  const pool = new TurnPool(1, url);
  try {
    const first = pool.run(
      turnInput({ message: "100" }),
      async () => ({ method: "event" }),
      new AbortController().signal,
    );
    const controller = new AbortController();
    const failed = assert.rejects(
      pool.run(
        turnInput({ message: "crash" }),
        async () => ({ method: "event" }),
        controller.signal,
      ),
      /cancelled/,
    );
    controller.abort();
    await failed;
    await first;
    assert.deepEqual(pool.stats(), { workers: 1, busy: 0, queued: 0 });
  } finally {
    await pool.close();
  }
});
test("worker forwards cancellation of a tool without replacing itself", async () => {
  const pool = new TurnPool(1, url);
  let cancelled = false;
  try {
    await pool.run(
      turnInput({ message: "cancel-tool" }),
      async (_call, signal) =>
        new Promise<never>((_resolve, reject) =>
          signal!.addEventListener(
            "abort",
            () => {
              cancelled = true;
              reject(Error("tool cancelled"));
            },
            { once: true },
          ),
        ),
      new AbortController().signal,
    );
    assert.equal(cancelled, true);
    assert.deepEqual(pool.stats(), { workers: 1, busy: 0, queued: 0 });
  } finally {
    await pool.close();
  }
});
