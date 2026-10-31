import { spawn } from "node:child_process";
import { readFile, open, mkdir } from "node:fs/promises";
const action = process.argv[2] ?? "start";
if (action === "stop") {
  try {
    const pid = Number(await readFile(".local/nimbus.pid", "utf8"));
    process.kill(pid, "SIGTERM");
    for (let i = 0; i < 150; i++) {
      try {
        process.kill(pid, 0);
      } catch {
        console.log("Nimbus stopped");
        process.exit(0);
      }
      await new Promise((r) => setTimeout(r, 200));
    }
    throw Error(
      "Nimbus is still finishing its active operations; check .local/nimbus.log",
    );
  } catch (error) {
    if (error.code === "ENOENT" || error.code === "ESRCH") {
      console.log("Nimbus is not running");
      process.exit(0);
    }
    throw error;
  }
}
try {
  const r = await fetch("http://127.0.0.1:8080/api/health");
  if (r.ok) {
    console.log("Nimbus already running: http://127.0.0.1:8080");
    process.exit(0);
  }
} catch {}
await mkdir(".local", { recursive: true });
const log = await open(".local/nimbus.log", "a");
const child = spawn("node", ["scripts/start.mjs"], {
  detached: true,
  stdio: ["ignore", log.fd, log.fd],
});
child.unref();
await log.close();
for (let i = 0; i < 60; i++) {
  try {
    const r = await fetch("http://127.0.0.1:8080/api/health");
    if (r.ok) {
      console.log("Nimbus ready: http://127.0.0.1:8080");
      process.exit(0);
    }
  } catch {}
  await new Promise((r) => setTimeout(r, 300));
}
console.error("Startup failed. See .local/nimbus.log");
process.exit(1);
