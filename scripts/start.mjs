import { readFile, mkdir, writeFile } from "node:fs/promises";
import { spawn, spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
process.chdir(fileURLToPath(new URL("..", import.meta.url)));
await mkdir(".local", { recursive: true });
const pgPrefix =
  process.platform === "darwin"
    ? spawnSync("brew", ["--prefix", "postgresql@17"], {
        encoding: "utf8",
      }).stdout?.trim()
    : "";
const pg = (pgPrefix || "/opt/homebrew/opt/postgresql@17") + "/bin/";
if (process.platform === "darwin" && !process.env.NIMBUS_POSTGRES) {
  const check = spawnSync(
    pg + "pg_isready",
    ["-h", "127.0.0.1", "-p", "55432"],
    { stdio: "ignore" },
  );
  if (check.status !== 0) {
    const start = spawnSync(
      pg + "pg_ctl",
      [
        "-D",
        ".local/postgres",
        "-l",
        ".local/postgres.log",
        "-o",
        "-h 127.0.0.1 -p 55432",
        "start",
      ],
      { stdio: "inherit" },
    );
    if (start.status) process.exit(start.status);
  }
}
const env = {
  ...process.env,
  NIMBUS_POSTGRES:
    process.env.NIMBUS_POSTGRES ||
    "postgres://nimbus@127.0.0.1:55432/nimbus?sslmode=disable",
  NIMBUS_RUNTIME_COMMAND:
    process.env.NIMBUS_RUNTIME_COMMAND ||
    fileURLToPath(
      new URL("../node_modules/.bin/nimbus-agent-runtime", import.meta.url),
    ),
};
for (const [key, file] of [
  ["NIMBUS_DEEPSEEK_API_KEY", "deepseekkey"],
  ["NIMBUS_E2B_API_KEY", "e2bhobbykey"],
])
  if (!env[key]) {
    try {
      env[key] = (await readFile(file, "utf8")).trim();
    } catch (error) {
      if (error.code !== "ENOENT") throw error;
      if (key === "NIMBUS_E2B_API_KEY")
        throw Error(
          "请设置 NIMBUS_E2B_API_KEY 或把 E2B key 写入 e2bhobbykey 文件。",
        );
    }
  }
const child = spawn("./bin/nimbus-cloud-agent", [], { env, stdio: "inherit" });
await writeFile(".local/nimbus.pid", String(child.pid));
for (const sig of ["SIGINT", "SIGTERM"]) process.on(sig, () => child.kill(sig));
child.on("exit", (code) => process.exit(code ?? 0));
