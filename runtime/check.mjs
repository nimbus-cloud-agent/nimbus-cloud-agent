import { createInterface } from "node:readline";
import { createRequire } from "node:module";
import { realpath, readFile } from "node:fs/promises";
import { runWasm } from "./wasm.mjs";
try {
  const i = JSON.parse(
    await new Promise((r) =>
      createInterface({ input: process.stdin }).once("line", r),
    ),
  );
  const req = createRequire(await realpath(i.harness_path));
  const v = JSON.parse(
    await readFile(req.resolve("@deepseek-ai/dsh/package.json"), "utf8"),
  ).version;
  if (v !== "0.2.1-rc.5")
    throw Error("Install exactly @deepseek-ai/dsh@0.2.1-rc.5");
  const r = await runWasm({
    program: "return [typeof process,typeof require,typeof fetch]",
    timeoutMs: 1000,
    bindings: [],
  });
  if (r.error || r.value.some((v) => v !== "undefined"))
    throw Error("Wasm isolation check failed");
  console.log(JSON.stringify({ result: { version: v, wasm: true } }));
} catch (e) {
  console.log(JSON.stringify({ error: e.message }));
}
process.exit(0);
