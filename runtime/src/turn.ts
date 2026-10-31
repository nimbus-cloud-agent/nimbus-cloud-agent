import { readFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { pathToFileURL } from "node:url";
import { Context } from "@deepseek-ai/cordis";
import { WasmRuntime, createHumanWaitClock } from "./wasm.js";
import type { TurnRequest } from "./generated/types.js";
import type { TurnHost } from "./ports.js";
import { errorMessage } from "./errors.js";
import type {} from "@deepseek-ai/dsh-agent";
import type {} from "@deepseek-ai/dsh-llm";
import type {} from "@deepseek-ai/dsh-session-persistence-jsonl";
import type {} from "@deepseek-ai/dsh-session";
import type {} from "@deepseek-ai/dsh-system-prompt";
import type {} from "@deepseek-ai/dsh-tools";
import type {} from "@deepseek-ai/dsh-skill";
import type {} from "@deepseek-ai/dsh-attachment-local";
import type {} from "@deepseek-ai/dsh-plan-mode";
import type {} from "@deepseek-ai/dsh-user-questions";
import type { AgentHandle } from "@deepseek-ai/dsh-agent";
import {
  SessionId,
  SessionLogOffset,
  type Session,
  type SessionHeader,
  type SessionEvent,
} from "@deepseek-ai/dsh-session";
import {
  MessageId,
  ReasoningEffortId,
  type ContentBlock,
} from "@deepseek-ai/dsh-llm";
import type {} from "@deepseek-ai/dsh-session-persistence";
import type { ImageAttachmentRef } from "@deepseek-ai/dsh-attachment";
import type { SkillCandidate } from "@deepseek-ai/dsh-skill";
import type { ToolDefinition, ToolRunContext } from "@deepseek-ai/dsh-tools";
import { UserQuestionError } from "@deepseek-ai/dsh-user-questions";
type HarnessInput = Omit<
  TurnRequest,
  "header" | "records" | "attachments" | "images"
> & {
  header?: SessionHeader;
  records: SessionEvent[];
  attachments: { ref: ImageAttachmentRef; data: string }[];
  images: { attachment: ImageAttachmentRef }[];
};
export async function runTurn(
  wireInput: TurnRequest,
  { emit, askQuestion, callTool, signal }: TurnHost,
) {
  // Native history/attachment payloads are opaque to Nimbus. Harness validates
  // them through its public persistence and attachment services on hydration.
  const input = wireInput as HarnessInput;
  const ctx = new Context();
  let handle: AgentHandle | undefined;
  const cancel = () => handle?.agent.cancel({ kind: "user" });
  signal.addEventListener("abort", cancel, { once: true });
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
    const requireDSH = createRequire(import.meta.url);
    const metadata: { version: string } = JSON.parse(
      await readFile(
        requireDSH.resolve("@deepseek-ai/dsh/package.json"),
        "utf8",
      ),
    );
    if (metadata.version !== "0.2.1-rc.5")
      throw Error("Nimbus requires DeepSeek Harness exactly 0.2.1-rc.5");
    signal.throwIfAborted();
    const { createLaunchEnvironmentSnapshot } = await import(
      pathToFileURL(requireDSH.resolve("@deepseek-ai/dsh-launch-environment"))
        .href
    );
    ctx.provide(
      "launchEnvironment",
      createLaunchEnvironmentSnapshot([
        { source: "process", values: { DEEPSEEK_API_KEY: input.api_key } },
      ]),
    );
    ctx.provide("workingDirectory", {
      get: () => "/workspace",
      ensure: async () => "/workspace",
    });
    async function mount(name: string, config?: Record<string, unknown>) {
      const mod = await import(
        pathToFileURL(requireDSH.resolve("@deepseek-ai/" + name)).href
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
    const mcpStatus = new Map<
      string,
      { server: string; tools: number; error?: string }
    >();
    for (const t of input.mcp ?? []) {
      const name = t.server ?? "MCP";
      const status = mcpStatus.get(name) ?? { server: name, tools: 0 };
      if (t.error) status.error = t.error;
      else status.tools++;
      mcpStatus.set(name, status);
    }
    ctx.systemPrompt.section({
      name: "nimbus-mcp-status",
      order: 85,
      text:
        "External integrations are provided by registered MCP tools; OAuth credentials stay on the control plane and are intentionally absent from the E2B sandbox. Use available MCP tools to access connected services. Do not infer missing authorization from sandbox environment variables or an unauthenticated curl request. Never ask the user to paste integration tokens into chat; use Settings → MCP for authorization. If discovery failed, report the specific server and error and direct the user to its connection test instead of claiming no integration is configured. Current enabled MCP discovery results (tools become available at the start of each new turn):\n" +
        JSON.stringify([...mcpStatus.values()]),
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
    const summaries: SkillCandidate[] = (input.skills || []).map((s) => ({
      name: s.name,
      description: s.description,
      source: "postgres",
      provider: "nimbus-postgres",
      rank: 0,
      locator: s.id,
      invocation: { modelInvocable: true, userInvocable: true },
      resourceBase: {
        kind: "opaque",
        description:
          "Stored in PostgreSQL; use skill_read for associated files",
      },
    }));
    ctx.skills.registerProvider(() => ({
      name: "nimbus-postgres",
      list: async () => summaries,
      get: async (c) => {
        const s = input.skills.find((s) => s.id === c.locator);
        if (!s) return undefined;
        return {
          ...c,
          content:
            s.files.find((f) => f.path === "SKILL.md")?.content ??
            s.files[0].content,
        };
      },
    }));
    function register(
      name: string,
      description: string,
      properties: Record<string, unknown>,
      required: string[],
      execute: ToolDefinition["execute"],
    ) {
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
    const definitions: [string, string, Record<string, unknown>, string[]][] = [
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
    async function remote(name: string, args: unknown, exec: ToolRunContext) {
      exec?.signal?.throwIfAborted();
      return callTool(
        {
          name,
          args,
          call_id: exec?.callId ?? crypto.randomUUID(),
        },
        exec?.signal,
      );
    }

    for (const [name, desc, props, req] of definitions)
      register(name, desc, props, req, (args, exec) =>
        remote(name, args, exec),
      );
    register(
      "skill_read",
      "Read an enabled skill from the PostgreSQL catalog. Files are not on the host or sandbox. Available: " +
        JSON.stringify(
          summaries.map((x) => ({ name: x.name, description: x.description })),
        ),
      { name: str, path: str },
      ["name"],
      async (args) => {
        const { name, path } = args as { name: string; path?: string };
        const s = input.skills.find((s) => s.name === name);
        if (!s) throw Error("Skill not enabled");
        return path
          ? (s.files.find((f) => f.path === path) ?? {
              error: "File not found",
            })
          : { name: s.name, files: s.files };
      },
    );
    for (const t of input.mcp || []) {
      if (t.error || !t.name) continue;
      ctx.tools.register({
        name: t.name,
        description: t.description || t.name,
        parameters: t.inputSchema as Record<string, unknown>,
        output: {
          schema: {},
          render: (_a, v) =>
            (v as { content?: ContentBlock[] })?.content ?? [
              { type: "text", text: JSON.stringify(v) },
            ],
        },
        execute: (a, e) => remote(t.name!, a, e),
      });
    }
    await mount("dsh-agent-loop", { agents: [], maxParallelToolCalls: 1 });
    await mount("dsh-user-questions");
    const humanWait = createHumanWaitClock();
    ctx.provide("nimbusHumanWait", humanWait);
    ctx.on(
      "user-questions/request",
      async (request) => {
        await chain;
        if (request.signal?.aborted)
          throw new UserQuestionError("Review interrupted", "ASK_ABORTED");
        const resume = humanWait.pause();
        try {
          const answer = await askQuestion({
            questions: request.questions,
          });
          if (request.signal?.aborted)
            throw new UserQuestionError("Review interrupted", "ASK_ABORTED");
          if (answer?.dismissed)
            throw new UserQuestionError(
              "User deferred plan review",
              "ASK_CANCELLED",
            );
          return { answers: answer.answers ?? [] };
        } finally {
          resume();
        }
      },
      { global: true },
    );
    // Native plan provider, without host fs/bash/sandbox or Node PTC providers.
    await mount("dsh-plan-mode", {
      section:
        "Plan mode is active. Investigate the task and produce a clear implementation plan. You may write plan documents into the E2B sandbox. Submit the complete Markdown plan with exit_plan_mode for human review. Wait for approval before implementing the plan; if the user asks for revisions, revise and submit again. No additional tool execution restrictions apply.",
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
    let sent: number = input.records.at(-1)?.seq ?? -1;
    let chain: Promise<void> = Promise.resolve();
    let session: Session;
    async function sync() {
      await ctx.sessionPersistence.flush();
      const h = await ctx.sessionPersistence.open(
        SessionId(input.session_id),
        "read",
      );
      try {
        const { events } = await h.read(SessionLogOffset(sent + 1));
        for (const e of events) {
          await emit({ event: "record", data: e });
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
      chain = chain.then(() => emit({ event: "frame", data: frame }));
      chain.catch(() => {});
    });
    const setup = (agentCtx: Context) => {
      if (input.session_mode === "ptc") agentCtx.tools.presentAs("ptc");
    };
    const agentOptions = {
      provider: "deepseek-official",
      model: input.options.model,
      reasoningEffort: ReasoningEffortId(input.options.thinking),
      maxTokens: 8192,
    };
    handle = input.header
      ? await ctx.agents.resume({
          resumeSessionId: SessionId(input.session_id),
          agentOptions,
          setup,
        })
      : await ctx.agents.create({
          sessionId: SessionId(input.session_id),
          meta: { cwd: "/workspace" },
          agentOptions,
          setup,
        });
    session = handle.agent.session;
    await emit({ event: "header", data: session.header });
    await sync();
    ctx.planMode.set(handle.agent, !!input.options.plan);
    await emit({ event: "ready", data: {} });
    signal.throwIfAborted();
    handle.agent.followup({
      id: MessageId(input.message_id),
      role: "user",
      content: [
        ...(input.message
          ? [{ type: "text" as const, text: input.message }]
          : []),
        ...(input.images ?? []).map((image) => ({
          type: "image" as const,
          attachment: image.attachment,
        })),
      ],
      source: { kind: "user" },
    });
    await handle.agent.whenIdle();
    await emit({ event: "flushing", data: {} });
    await chain;
    await ctx.sessions.flush(session);
    await sync();
    const h = await ctx.sessionPersistence.open(
      SessionId(input.session_id),
      "read",
    );
    const { events } = await h.read();
    await h.close();
    const end = events.findLast((e) => e.type === "turn/end");
    await handle.dispose();
    handle = undefined;
    await chain;
    await sync();
    await emit({
      event: "done",
      data: {
        seq: sent,
        outcome:
          end?.data?.reason?.kind === "error"
            ? "error"
            : end?.data?.reason?.kind === "aborted"
              ? "interrupted"
              : "completed",
        error:
          end?.data?.reason?.kind === "error"
            ? JSON.stringify(end.data.reason.error)
            : "",
      },
    });
    return { finished: true };
  } finally {
    signal.removeEventListener("abort", cancel);
    try {
      if (handle) {
        handle.agent.cancel({ kind: "user" });
        await handle.dispose();
      }
    } finally {
      await ctx?.fiber.dispose();
    }
  }
}
