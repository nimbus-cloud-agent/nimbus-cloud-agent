import { createInterface } from "node:readline";
import { readFile, realpath } from "node:fs/promises";
import { createRequire } from "node:module";
import { pathToFileURL } from "node:url";
import { Context } from "@deepseek-ai/cordis";
import { WasmRuntime } from "./wasm.mjs";
const lines = createInterface({ input: process.stdin });
const iterator = lines[Symbol.asyncIterator]();
const input = JSON.parse((await iterator.next()).value);
let ackID = 0;
const acks = new Map();
(async () => {
  for await (const line of iterator) {
    const m = JSON.parse(line);
    acks.get(m.ack)?.();
    acks.delete(m.ack);
  }
  process.exit(1);
})();
function emit(event, data) {
  const ack = ++ackID;
  return new Promise((resolve) => {
    acks.set(ack, resolve);
    process.stdout.write(JSON.stringify({ event, data, ack }) + "\n");
  });
}
// The model cannot access this process or its credentials. Only explicit tool functions are registered.
process.env.DEEPSEEK_API_KEY = input.api_key;
let ctx, handle;
process.on("SIGTERM", () => {
  if (handle) handle.agent.cancel({ kind: "user" });
  else process.exit(1);
});
try {
  if (
    !["standard", "ptc"].includes(input.session_mode) ||
    input.options.mode !== input.session_mode
  )
    throw Error(
      "Session preset mismatch; create a new session to change presets",
    );
  if (input.options.model !== "deepseek-flash")
    throw Error("Nimbus requires model deepseek-flash");
  const executable = await realpath(input.harness_path);
  const requireDSH = createRequire(executable);
  const version = JSON.parse(
    await readFile(requireDSH.resolve("@deepseek-ai/dsh/package.json"), "utf8"),
  ).version;
  if (version !== "0.2.1-rc.5")
    throw Error("Nimbus requires DeepSeek Harness exactly 0.2.1-rc.5");
  ctx = new Context();
  ctx.provide("workingDirectory", {
    get: () => "/workspace",
    ensure: async () => "/workspace",
  });
  async function mount(name, config) {
    const mod = await import(
      pathToFileURL(requireDSH.resolve("@deepseek-ai/" + name))
    );
    const fork = ctx.plugin(mod.default ?? mod, config);
    await fork.await();
    return mod;
  }
  await mount("cordis-plugin-timer");
  for (const name of [
    "dsh-llm",
    "dsh-session",
    "dsh-session-projection",
    "dsh-tools",
    "dsh-agent",
  ])
    await mount(name);
  if (input.attachments?.length) {
    await mount("dsh-attachment-local", {
      dshHome: input.work_dir + "/attachments",
      maxImageBytes: 5 * 1024 * 1024,
    });
    for (const image of input.attachments) {
      const ref = await ctx.attachments.saveImage({
        data: Buffer.from(image.data, "base64"),
        mediaType: image.ref.mediaType,
        name: image.ref.name,
      });
      if (ref.attachmentId !== image.ref.attachmentId)
        throw Error("Stored image failed attachment integrity verification");
    }
  }
  await mount("dsh-system-prompt", {
    includeHarnessIdentity: false,
    includeRuntimeContext: false,
    personaPrefix:
      "你是 Nimbus 云端 Agent。用用户的语言交流，准确汇报结果。你没有宿主机文件、命令、网络工具。所有命令和文件操作只通过 sandbox_* 工具在本 Session 独占的 E2B Linux 沙箱内执行；默认工作目录 /workspace。纯聊天和 Skill/MCP 操作无需创建沙箱。生成文件后报告沙箱内路径，工具出错必须如实说明。",
  });
  if (input.recovery)
    ctx.systemPrompt.section({
      name: "nimbus-interrupted-output",
      order: 90,
      text: input.recovery,
      interpolate: false,
    });
  await mount("dsh-deepseek-llm-api-extensions");
  await mount("dsh-llm-deepseek-api-key", {
    apiKeyEnv: "DEEPSEEK_API_KEY",
    streamIdleTimeoutMs: 120000,
  });
  await mount("dsh-session-persistence-jsonl", {
    root: input.work_dir,
    compression: "none",
  });
  await mount("dsh-skill");
  // Database-backed provider: all contents arrive from PG in memory; never copied into any filesystem.
  const summaries = (input.skills || []).map((s) => ({
    name: s.name,
    description: s.description,
    source: "postgres",
    provider: "nimbus-postgres",
    rank: 0,
    locator: s.id,
    invocation: { modelInvocable: true, userInvocable: true },
    resourceBase: {
      kind: "opaque",
      description: "Stored in PostgreSQL; use skill_read for associated files",
    },
  }));
  ctx.skills.registerProvider(() => ({
    name: "nimbus-postgres",
    list: async () => summaries,
    get: async (c) => {
      const s = input.skills.find((s) => s.id === c.locator);
      return {
        ...c,
        content:
          s.files.find((f) => f.path === "SKILL.md")?.content ??
          s.files[0].content,
      };
    },
  }));
  function register(name, description, properties, required, execute) {
    ctx.tools.register({
      name,
      description,
      parameters: {
        type: "object",
        properties,
        required,
        additionalProperties: false,
      },
      output: {
        schema: {},
        render: (_args, value) => [
          { type: "text", text: JSON.stringify(value) },
        ],
      },
      execute,
    });
  }
  const str = { type: "string" };
  const definitions = [
    [
      "sandbox_status",
      "Query this Session sandbox without starting it.",
      {},
      [],
    ],
    [
      "sandbox_start",
      "Create or resume this Session E2B sandbox on demand.",
      {},
      [],
    ],
    [
      "sandbox_exec",
      "Execute a Linux shell command in this Session E2B sandbox. Automatically creates/resumes it; never runs on the host. Output capped at 1 MiB.",
      { command: str, cwd: str, timeoutMs: { type: "number" } },
      ["command"],
    ],
    [
      "sandbox_read_file",
      "Read a UTF-8 file at any absolute path inside this Session E2B sandbox.",
      { path: str },
      ["path"],
    ],
    [
      "sandbox_write_file",
      "Write a UTF-8 file at any absolute path inside this Session E2B sandbox.",
      { path: str, content: str },
      ["path", "content"],
    ],
  ];
  async function remote(name, args, exec) {
    const res = await fetch(input.tools_url, {
      method: "POST",
      headers: {
        Authorization: input.token,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        name,
        args,
        call_id: exec?.callId ?? crypto.randomUUID(),
      }),
      signal: exec?.signal,
    });
    const data = await res.json();
    if (!res.ok) throw Error(data.error || "Tool failed");
    return data;
  }
  for (const [name, desc, props, req] of definitions)
    register(name, desc, props, req, (args, exec) => remote(name, args, exec));
  register(
    "skill_read",
    "Read an enabled skill from the PostgreSQL catalog. Files are not on the host or sandbox. Available: " +
      JSON.stringify(
        summaries.map((x) => ({ name: x.name, description: x.description })),
      ),
    { name: str, path: str },
    ["name"],
    async ({ name, path }) => {
      const s = input.skills.find((s) => s.name === name);
      if (!s) throw Error("Skill not enabled");
      return path
        ? (s.files.find((f) => f.path === path) ?? { error: "File not found" })
        : { name: s.name, files: s.files };
    },
  );
  for (const t of input.mcp || []) {
    if (t.error) continue;
    ctx.tools.register({
      name: t.name,
      description: t.description || t.name,
      parameters: t.inputSchema,
      output: {
        schema: {},
        render: (_a, v) =>
          v.content ?? [{ type: "text", text: JSON.stringify(v) }],
      },
      execute: (a, e) => remote(t.name, a, e),
    });
  }
  await mount("dsh-agent-loop", { agents: [], maxParallelToolCalls: 1 });
  // Native plan provider, without host fs/bash/sandbox or Node PTC providers.
  await mount("dsh-plan-mode", {
    section:
      "Plan mode is active. Investigate the task and produce a clear implementation plan. You may write plan documents into the E2B sandbox. No additional execution restrictions apply.",
  });
  if (input.session_mode === "ptc") {
    await ctx.plugin(WasmRuntime).await();
  }
  for (const name of ["fs", "subprocess", "sandbox", "bash", "jobs"]) {
    if (ctx.get(name) !== undefined)
      throw Error("Unsafe local provider loaded: " + name);
  }
  if (input.session_mode === "ptc" && ctx.ptcRuntime.isolation !== "wasm")
    throw Error("Non-Wasm PTC runtime rejected");

  // Hydrate using the public persistence handle. DSH owns its physical file format.
  if (input.header) {
    const h = await ctx.sessionPersistence.create(input.header);
    await h.append(input.records);
    await h.flush();
    await h.close();
  }
  let sent = input.records.at(-1)?.seq ?? -1,
    chain = Promise.resolve();
  let session;
  async function sync() {
    await ctx.sessionPersistence.flush();
    const h = await ctx.sessionPersistence.open(input.session_id, "read");
    try {
      const { events } = await h.read(sent + 1);
      for (const e of events) {
        await emit("record", e);
        sent = e.seq;
      }
    } finally {
      await h.close();
    }
  }
  ctx.on("session/event", () => {
    chain = chain.then(sync);
    chain.catch(() => {});
  });
  ctx.on("session/flush", async () => {
    await chain;
  });
  ctx.on("agent/assistant-stream", ({ frame }) => {
    chain = chain.then(() => emit("frame", frame));
    chain.catch(() => {});
  });
  const setup = (agentCtx) => {
    if (input.session_mode === "ptc") agentCtx.tools.presentAs("ptc");
  };
  const agentOptions = {
    provider: "deepseek-official",
    model: input.options.model,
    reasoningEffort: input.options.thinking,
    maxTokens: 8192,
  };
  handle = input.header
    ? await ctx.agents.resume({
        resumeSessionId: input.session_id,
        agentOptions,
        setup,
      })
    : await ctx.agents.create({
        sessionId: input.session_id,
        meta: { cwd: "/workspace" },
        agentOptions,
        setup,
      });
  session = handle.agent.session;
  await emit("header", session.header);
  await sync();
  ctx.planMode.set(handle.agent, !!input.options.plan);
  await emit("ready", {});
  handle.agent.followup({
    id: input.message_id,
    role: "user",
    content: [
      ...(input.message ? [{ type: "text", text: input.message }] : []),
      ...(input.images ?? []).map((image) => ({
        type: "image",
        attachment: image.attachment,
      })),
    ],
    source: { kind: "user" },
  });
  await handle.agent.whenIdle();
  await emit("flushing", {});
  await chain;
  await ctx.sessions.flush(session);
  await sync();
  const h = await ctx.sessionPersistence.open(input.session_id, "read");
  const { events } = await h.read();
  await h.close();
  const end = events.findLast((e) => e.type === "turn/end");
  await handle.dispose();
  handle = null;
  await chain;
  await sync();
  await emit("done", {
    seq: sent,
    outcome:
      end?.data?.reason?.kind === "error"
        ? "error"
        : end?.data?.reason?.kind === "aborted"
          ? "interrupted"
          : "completed",
    error: end?.data?.reason?.error
      ? JSON.stringify(end.data.reason.error)
      : "",
  });
  process.exit(0);
} catch (e) {
  await emit(
    "error",
    String(e.message).replaceAll(input.api_key, "[REDACTED]"),
  );
  process.exit(1);
}
