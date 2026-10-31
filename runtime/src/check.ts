import { createRequire } from "node:module";
import { readFile } from "node:fs/promises";
import { runWasm } from "./wasm.js";
import type { CheckRequest, RuntimeInfo } from "./generated/types.js";
export async function check(input: CheckRequest): Promise<RuntimeInfo> {
  if (input.protocolVersion !== 2)
    throw Error("Unsupported Nimbus protocol version");
  const require = createRequire(import.meta.url);
  const harness = JSON.parse(
    await readFile(require.resolve("@deepseek-ai/dsh/package.json"), "utf8"),
  ) as { version: string };
  if (harness.version !== "0.2.1-rc.5")
    throw Error("nimbus-agent-runtime requires @deepseek-ai/dsh@0.2.1-rc.5");
  const result = await runWasm({
    program: "return [typeof process,typeof require,typeof fetch]",
    cwd: "/workspace",
    timeoutMs: 1000,
    bindings: [],
  });
  if (
    result.error ||
    !Array.isArray(result.value) ||
    result.value.some((value) => value !== "undefined")
  )
    throw Error("Wasm isolation check failed");
  const metadata = JSON.parse(
    await readFile(new URL("../package.json", import.meta.url), "utf8"),
  ) as { version: string };
  return {
    name: "nimbus-agent-runtime",
    version: metadata.version,
    protocolVersion: 2,
  };
}
