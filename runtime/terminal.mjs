import { Sandbox } from "e2b";
import { createInterface } from "node:readline";
const lines = createInterface({ input: process.stdin });
const iter = lines[Symbol.asyncIterator]();
const input = JSON.parse((await iter.next()).value);
let sb, pty;
try {
  sb = await Sandbox.connect(input.sandbox_id, {
    apiKey: input.api_key,
    timeoutMs: 60000,
    requestTimeoutMs: 60000,
  });
  pty = await sb.pty.create({
    cols: input.cols || 80,
    rows: input.rows || 24,
    cwd: "/workspace",
    user: "root",
    timeoutMs: 0,
    envs: { TERM: "xterm-256color" },
    onData: (data) =>
      process.stdout.write(
        JSON.stringify({ data: Buffer.from(data).toString("base64") }) + "\n",
      ),
  });
  process.stdout.write(JSON.stringify({ ready: true }) + "\n");
  for await (const line of iter) {
    const msg = JSON.parse(line);
    if (msg.type === "input")
      await sb.pty.sendInput(pty.pid, Buffer.from(msg.data, "base64"));
    if (msg.type === "resize")
      await sb.pty.resize(pty.pid, {
        cols: Math.max(2, Math.min(500, msg.cols)),
        rows: Math.max(2, Math.min(200, msg.rows)),
      });
  }
} catch (e) {
  process.stdout.write(
    JSON.stringify({
      error: String(e.message).replaceAll(input.api_key, "[REDACTED]"),
    }) + "\n",
  );
} finally {
  if (pty) await pty.kill().catch(() => {});
  process.exit(0);
}
