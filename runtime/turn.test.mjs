import { spawn } from "node:child_process";
import { createInterface } from "node:readline";
import test from "node:test";
import assert from "node:assert/strict";

for (const [name, input, expected] of [
  [
    "missing session preset",
    { options: { mode: "standard", model: "deepseek-flash" } },
    /preset mismatch/,
  ],
  [
    "changed session preset",
    {
      session_mode: "ptc",
      options: { mode: "standard", model: "deepseek-flash" },
    },
    /preset mismatch/,
  ],
  [
    "retired model",
    {
      session_mode: "standard",
      options: { mode: "standard", model: "deepseek-chat" },
    },
    /requires model deepseek-flash/,
  ],
]) {
  test(
    `turn rejects ${name} before loading Harness`,
    { timeout: 5000 },
    async () => {
      const child = spawn(
        process.execPath,
        [new URL("./turn.mjs", import.meta.url).pathname],
        { stdio: ["pipe", "pipe", "pipe"] },
      );
      let error;
      const done = new Promise((resolve) => child.on("exit", resolve));
      try {
        child.stdin.write(JSON.stringify(input) + "\n");
        for await (const line of createInterface({ input: child.stdout })) {
          const frame = JSON.parse(line);
          if (frame.event === "error") error = frame.data;
          child.stdin.write(JSON.stringify({ ack: frame.ack }) + "\n");
        }
        await done;
        assert.match(error, expected);
      } finally {
        child.kill();
      }
    },
  );
}
