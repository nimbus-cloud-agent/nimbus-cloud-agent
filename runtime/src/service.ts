#!/usr/bin/env node
import { Console } from "node:console";
import { readFile } from "node:fs/promises";
import { RpcPeer } from "./rpc/peer.js";
import {
  ControlClient,
  registerRuntime,
  type RuntimeService,
} from "./generated/sdk.js";
import { TurnPool } from "./pool.js";
import { check } from "./check.js";
import { mcp } from "./mcp.js";
import { images } from "./images.js";

if (process.argv.includes("--version")) {
  const metadata = JSON.parse(
    await readFile(new URL("../package.json", import.meta.url), "utf8"),
  ) as { version: string };
  console.log(`nimbus-agent-runtime ${metadata.version}`);
} else {
  globalThis.console = new Console({
    stdout: process.stderr,
    stderr: process.stderr,
  });
  const peer = new RpcPeer(process.stdin, process.stdout),
    control = new ControlClient(peer),
    pool = new TurnPool();
  let operations = 0,
    closing = false;
  const execute = async <T>(fn: () => Promise<T>): Promise<T> => {
    if (closing) throw Error("Runtime shutting down");
    operations++;
    try {
      return await fn();
    } finally {
      operations--;
    }
  };
  const service: RuntimeService = {
    check,
    status: async () => ({ pid: process.pid, ...pool.stats(), operations }),
    turn: async (input, signal) => {
      // Keep domain fault identity for cancellation and worker-loss mapping.
      operations++;
      try {
        return await pool.run(
          input,
          async (call, callbackSignal) => {
            if (call.method === "event") {
              await control.event(
                {
                  ...call.data,
                  operationId: input.operationId,
                },
                callbackSignal,
              );
              return { method: "event" };
            }
            if (call.method === "question")
              return {
                method: "question",
                answer: (
                  await control.question(
                    { operationId: input.operationId, data: call.data },
                    callbackSignal,
                  )
                ).answer,
              };
            return {
              method: "tool",
              value: (
                await control.tool(
                  { operationId: input.operationId, call: call.data },
                  callbackSignal,
                )
              ).value,
            };
          },
          signal,
        );
      } finally {
        operations--;
      }
    },
    mcp: async (input, signal) => execute(() => mcp(input, { signal })),
    images: async (input) => execute(() => images(input)),
  };
  registerRuntime(peer, service);
  peer.listen();
  const shutdown = async () => {
    if (closing) return;
    closing = true;
    peer.close();
    setTimeout(() => process.exit(0), 2000);
    await pool.close();
    process.exit(0);
  };
  void peer.done.then(shutdown);
  process.stdin.on("end", shutdown);
  process.on("SIGTERM", shutdown);
  process.on("SIGINT", shutdown);
}
