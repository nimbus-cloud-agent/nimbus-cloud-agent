import {
  Sandbox,
  CommandExitError,
  type SandboxInfo as ProviderInfo,
} from "e2b";
import type { E2BRequest, E2BResult, SandboxInfo } from "./generated/types.js";
import type { OutputHost, CommandOutput } from "./ports.js";
import { errorMessage } from "./errors.js";
function sandboxInfo(x: ProviderInfo): SandboxInfo {
  return {
    sandboxId: x.sandboxId,
    templateId: x.templateId,
    name: x.name ?? "",
    state: x.state,
    metadata: x.metadata,
    startedAt: x.startedAt.toISOString(),
    endAt: x.endAt.toISOString(),
    region: (x as ProviderInfo & { region?: string }).region ?? "us-west-1",
    cpuCount: x.cpuCount,
    memoryMB: x.memoryMB,
    envdVersion: x.envdVersion,
  };
}
export async function e2b(
  input: E2BRequest,
  { emit, signal }: OutputHost<CommandOutput>,
): Promise<E2BResult> {
  let output: Promise<void> = Promise.resolve();
  const opts = { apiKey: input.api_key, requestTimeoutMs: 60_000 };
  const info = async (sb: Sandbox) => sandboxInfo(await sb.getInfo());
  try {
    let result: E2BResult;
    let sb: Sandbox;
    if (input.op === "create") {
      sb = await Sandbox.create(input.template ?? "base", {
        ...opts,
        timeoutMs: 3_600_000,
        lifecycle: { onTimeout: "pause", autoResume: false },
        metadata: { nimbus_session: input.args?.session_id ?? "" },
      });
      try {
        await sb.commands.run("mkdir -p /workspace && chmod 777 /workspace", {
          user: "root",
          timeoutMs: 15000,
        });
        signal?.throwIfAborted();
        result = { info: await info(sb) };
      } catch (e) {
        await sb.kill();
        throw e;
      }
    } else if (input.op === "kill") {
      await Sandbox.kill(input.sandbox_id!, opts);
      result = { ok: true };
    } else if (input.op === "pause") {
      await Sandbox.pause(input.sandbox_id!, opts);
      result = { ok: true };
    } else if (input.op === "info") {
      try {
        result = {
          info: sandboxInfo(await Sandbox.getInfo(input.sandbox_id!, opts)),
        };
      } catch (e) {
        if (
          (e instanceof Error && e.name === "NotFoundError") ||
          (e instanceof Error && e.name === "SandboxNotFoundError")
        )
          result = {
            info: {
              sandboxId: input.sandbox_id!,
              state: "killed",
              templateId: "",
              name: "",
              region: "",
              startedAt: "",
              endAt: "",
              envdVersion: "",
              metadata: {},
              cpuCount: 0,
              memoryMB: 0,
            },
          };
        else throw e;
      }
    } else {
      sb = await Sandbox.connect(input.sandbox_id!, {
        ...opts,
        timeoutMs: input.op === "connect" ? 3_600_000 : 60_000,
      });
      if (input.op === "connect") result = { info: await info(sb) };
      else if (input.op === "exec") {
        const a = input.args ?? {};
        if (typeof a.command !== "string" || !a.command.trim())
          throw Error("command is required");
        let stdout = "",
          stderr = "",
          size = 0,
          truncated = false;
        const take = (kind: "out" | "err", chunk: string) => {
          const b = Buffer.from(chunk);
          const n = Math.min(b.length, 1048576 - size);
          if (n < b.length) truncated = true;
          size += n;
          if (input.stream && n > 0)
            output = output.then(() =>
              emit({
                channel: kind === "out" ? "stdout" : "stderr",
                text: b.subarray(0, n).toString(),
              }),
            );
          output.catch(() => {});
          if (kind === "out") stdout += b.subarray(0, n).toString();
          else stderr += b.subarray(0, n).toString();
          return output;
        };
        let exitCode = 0;
        try {
          const r = await sb.commands.run(a.command, {
            signal,
            cwd: a.cwd || "/workspace",
            user: "root",
            timeoutMs: Math.min(Math.max(a.timeoutMs || 55000, 1000), 55000),
            onStdout: async (d) => {
              await take("out", d);
            },
            onStderr: async (d) => {
              await take("err", d);
            },
          });
          exitCode = r.exitCode;
        } catch (e) {
          if (!(e instanceof CommandExitError)) throw e;
          exitCode = e.exitCode;
        }
        result = { command: { stdout, stderr, exitCode, truncated } };
      } else if (input.op === "read_file") {
        if (typeof input.args?.path !== "string")
          throw Error("path is required");
        const content = await sb.files.read(input.args.path, { signal });
        result = {
          readFile: {
            content: content.slice(0, 1048576),
            truncated: content.length > 1048576,
          },
        };
      } else if (input.op === "write_file") {
        if (
          typeof input.args?.path !== "string" ||
          typeof input.args?.content !== "string"
        )
          throw Error("path and content are required");
        await sb.files.write(input.args.path, input.args.content, {
          user: "root",
          signal,
        });
        result = { writeFile: { path: input.args.path, written: true } };
      } else throw Error("Unknown E2B operation");
    }
    await output;
    return result;
  } catch (e) {
    throw new Error(
      input.api_key
        ? errorMessage(e).replaceAll(input.api_key, "[REDACTED]")
        : errorMessage(e),
    );
  }
}
