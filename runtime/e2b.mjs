import { Sandbox } from "e2b";
import { createInterface } from "node:readline";
import { writeJSON } from "./protocol.mjs";
const input = JSON.parse(
  await new Promise((resolve) =>
    createInterface({ input: process.stdin }).once("line", resolve),
  ),
);
const opts = { apiKey: input.api_key, requestTimeoutMs: 60_000 };
const info = async (sb) => {
  const x = await sb.getInfo();
  return { ...x, sandboxId: sb.sandboxId, region: x.region ?? "us-west-1" };
};
try {
  let result, sb;
  if (input.op === "create") {
    sb = await Sandbox.create(input.template, {
      ...opts,
      timeoutMs: 3_600_000,
      lifecycle: { onTimeout: "pause", autoResume: false },
      metadata: { nimbus_session: input.args.session_id },
    });
    try {
      await sb.commands.run("mkdir -p /workspace && chmod 777 /workspace", {
        user: "root",
        timeoutMs: 15000,
      });
      result = await info(sb);
    } catch (e) {
      await sb.kill();
      throw e;
    }
  } else if (input.op === "kill") {
    await Sandbox.kill(input.sandbox_id, opts);
    result = { ok: true };
  } else if (input.op === "pause") {
    await Sandbox.pause(input.sandbox_id, opts);
    result = { ok: true };
  } else if (input.op === "info") {
    try {
      result = await Sandbox.getInfo(input.sandbox_id, opts);
    } catch (e) {
      if (e.name === "NotFoundError" || e.name === "SandboxNotFoundError")
        result = { state: "killed", sandboxId: input.sandbox_id };
      else throw e;
    }
  } else {
    sb = await Sandbox.connect(input.sandbox_id, {
      ...opts,
      timeoutMs: input.op === "connect" ? 3_600_000 : 60_000,
    });
    if (input.op === "connect") result = await info(sb);
    else if (input.op === "exec") {
      const a = input.args;
      if (typeof a.command !== "string" || !a.command.trim())
        throw Error("command is required");
      let stdout = "",
        stderr = "",
        size = 0,
        truncated = false;
      const take = (kind, chunk) => {
        const b = Buffer.from(chunk);
        const n = Math.min(b.length, 1048576 - size);
        if (n < b.length) truncated = true;
        size += n;
        if (input.stream && n > 0)
          process.stdout.write(
            JSON.stringify({
              output: {
                channel: kind === "out" ? "stdout" : "stderr",
                text: b.subarray(0, n).toString(),
              },
            }) + "\n",
          );
        if (kind === "out") stdout += b.subarray(0, n).toString();
        else stderr += b.subarray(0, n).toString();
      };
      let exitCode = 0;
      try {
        const r = await sb.commands.run(a.command, {
          cwd: a.cwd || "/workspace",
          user: "root",
          timeoutMs: Math.min(Math.max(a.timeoutMs || 55000, 1000), 55000),
          onStdout: (d) => take("out", d),
          onStderr: (d) => take("err", d),
        });
        exitCode = r.exitCode;
      } catch (e) {
        if (e.exitCode === undefined) throw e;
        exitCode = e.exitCode;
      }
      result = { stdout, stderr, exitCode, truncated };
    } else if (input.op === "read_file") {
      if (typeof input.args.path !== "string") throw Error("path is required");
      const content = await sb.files.read(input.args.path);
      result = {
        content: content.slice(0, 1048576),
        truncated: content.length > 1048576,
      };
    } else if (input.op === "write_file") {
      if (
        typeof input.args.path !== "string" ||
        typeof input.args.content !== "string"
      )
        throw Error("path and content are required");
      await sb.files.write(input.args.path, input.args.content, {
        user: "root",
      });
      result = { path: input.args.path, written: true };
    } else throw Error("Unknown E2B operation");
  }
  await writeJSON({ result });
} catch (e) {
  await writeJSON({
    error: String(e.message).replaceAll(input.api_key, "[REDACTED]"),
  });
}
process.exit(0);
