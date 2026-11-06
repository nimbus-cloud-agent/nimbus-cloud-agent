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
import type {
  ImageAttachmentRef,
  ImageMediaType,
  SaveImageAttachment,
} from "@deepseek-ai/dsh-attachment";
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
// Library: global, PostgreSQL-backed, versioned store for finished
// user-facing files. The six tools route through remote() like the
// sandbox_* tools, so the Go backend executes them and the Wasm PTC
// runtime exposes them as SDK bindings automatically (dsh-tools builds
// the `tools` namespace from every visible registered schema).
const libraryStr = { type: "string" };
const libraryNum = { type: "number" };
export const libraryToolDefinitions: [
  string,
  string,
  Record<string, unknown>,
  string[],
][] = [
  [
    "library_list",
    "List the files currently in the Library, the global versioned store for finished user-facing files. Returns id, name, MIME type, size in bytes, current version, and update time for each file.",
    {},
    [],
  ],
  [
    "library_read",
    "Read a file from the Library by id or name. Text files are read by byte range (offset/length); one call returns at most 1 MiB of text, so page through larger files with offset. Images are shown to you directly, so you can describe what is in them; images over 5 MiB cannot be read this way, copy them to the sandbox with library_copy_to_sandbox instead. Other binary files cannot be read as text; copy them to the sandbox with library_copy_to_sandbox instead. Pass version to read an older version.",
    {
      id: libraryStr,
      name: libraryStr,
      version: libraryNum,
      offset: libraryNum,
      length: libraryNum,
    },
    [],
  ],
  [
    "library_write",
    "Save a finished user-facing file (HTML page, Markdown document, image, or other file) to the Library and get its shareable link (nimbus://library/<id>). Every write creates a new immutable version; older versions are kept. Maximum 100 MB per file. Use this only for finished deliverables the user should see — working files, scripts, and intermediate data stay in the session sandbox.",
    { name: libraryStr, content: libraryStr, mime_type: libraryStr },
    ["name", "content"],
  ],
  [
    "library_versions",
    "List all kept versions of a Library file by id or name, with size in bytes, SHA-256, and creation time for each version.",
    { id: libraryStr, name: libraryStr },
    [],
  ],
  [
    "library_copy_to_sandbox",
    "Copy a Library file into the session sandbox at the given path, in one call. The host streams the file in 1 MiB chunks; your code never handles chunking. Use this to process a large Library file with sandbox tools. Returns the sandbox path, byte count, and version copied.",
    {
      id: libraryStr,
      name: libraryStr,
      version: libraryNum,
      path: libraryStr,
    },
    ["path"],
  ],
  [
    "library_copy_from_sandbox",
    "Copy a file from the session sandbox into the Library in one call, creating a new version under the given name (defaults to the sandbox file's name). The host streams the file in 1 MiB chunks; your code never handles chunking. Use this to publish a finished file that was built in the sandbox. Maximum 100 MB per file.",
    { path: libraryStr, name: libraryStr, mime_type: libraryStr },
    ["path"],
  ],
];

export const librarySystemPrompt =
  "The Library is a global, PostgreSQL-backed, versioned file store for finished user-facing files — HTML pages, Markdown documents, and images. It is where finished work is published, not where work happens. When you finish a deliverable the user should see and keep, save it with library_write and share its link, nimbus://library/<id>; the user clicks the link to preview the file in a modal. Working files, scripts, and intermediate data stay in the session sandbox through the sandbox_* tools: the sandbox is the workbench, the Library is the shelf for finished deliverables. Do not store sandbox working files in the Library, and do not use the Library as scratch space. Every library_write creates a new immutable version of the file and older versions are kept; list them with library_versions and read an older one by passing version to library_read. List current files with library_list. Read text by byte range with library_read (offset/length); one text read returns at most 1 MiB, so page through larger files with offset. Images read with library_read are shown to you directly, so describe what is in them. Images over 5 MiB cannot be read this way — copy them to the sandbox with library_copy_to_sandbox instead. Other binary files cannot be read as text. Move files between the Library and the sandbox in one call with library_copy_to_sandbox and library_copy_from_sandbox — the host streams the file in 1 MiB chunks for you, so your code must never handle chunking, streaming, backpressure, or retries. Caps: 100 MB per file; 1 MiB per text read; 5 MiB per image read.";

/** Image payload of a Go library_read result: base64 data, replaced below. */
export interface LibraryReadImage {
  media_type?: string;
  data?: string;
  attachment?: ImageAttachmentRef;
}

/** Go library_read result. `image.data` is base64 and never stored. */
export interface LibraryReadResult {
  id?: string;
  name?: string;
  version?: number;
  mime_type?: string;
  size_bytes?: number;
  content?: string;
  truncated?: boolean;
  image?: LibraryReadImage;
  link?: string;
  note?: string;
  [key: string]: unknown;
}

/**
 * Turn a Go library_read result into the canonical tool value. Image bytes
 * must never reach the model or the session log as base64 text: the image is
 * saved into the attachment store and only its durable reference is kept,
 * so the render below can show it as an image block. When the image cannot
 * be admitted (unsupported type, over the 5 MiB attachment cap, corrupt
 * bytes) the base64 is still dropped and the result carries a note pointing
 * at the Library link instead.
 */
export async function resolveLibraryReadImage(
  result: LibraryReadResult,
  saveImage: (input: SaveImageAttachment) => Promise<ImageAttachmentRef>,
): Promise<LibraryReadResult> {
  const image = result?.image;
  if (!image || typeof image.data !== "string" || image.data.length === 0)
    return result;
  const { data: _base64, ...imageMeta } = image;
  try {
    const attachment = await saveImage({
      data: Buffer.from(image.data, "base64"),
      mediaType: (image.media_type ?? "") as ImageMediaType,
      ...(result.name ? { name: result.name } : {}),
    });
    return { ...result, image: { ...imageMeta, attachment } };
  } catch (error) {
    return {
      ...result,
      image: imageMeta,
      note: `Image preview unavailable (${errorMessage(error)}); open ${result.link ?? "the Library link"} to preview it.`,
    };
  }
}

/**
 * Render a resolved library_read value. An admitted image becomes an image
 * block the model can see (the DeepSeek adapter accepts image blocks in
 * tool results; inside run_code the same block is deferred as a user
 * message), beside the metadata as text. Anything else renders as JSON
 * text, exactly like the other remote tools.
 */
export function renderLibraryRead(
  _args: unknown,
  value: unknown,
): ContentBlock[] {
  const result = value as LibraryReadResult | null;
  const attachment = result?.image?.attachment;
  if (attachment && typeof attachment === "object") {
    return [
      {
        type: "image" as const,
        attachment: attachment as ImageAttachmentRef,
      },
      { type: "text" as const, text: JSON.stringify(value) },
    ];
  }
  return [{ type: "text" as const, text: JSON.stringify(value) }];
}

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
    // Always mounted: library_read saves Library images into the attachment
    // store so the model can see them as image blocks. Mounting only
    // resolves paths; storage directories are created lazily on save.
    await mount("dsh-attachment-local", {
      dshHome: input.work_dir + "/attachments",
      maxImageBytes: 5 * 1024 * 1024,
    });
    if (input.attachments?.length) {
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
        "You are Nimbus, a cloud agent. Communicate in English unless the user asks for another language, and report results accurately. You do not have access to host files, commands, or network tools. Run all commands and perform all file operations only through sandbox_* tools in the E2B Linux sandbox dedicated to this session. The default working directory is /workspace. Do not create a sandbox for ordinary chat or Skill/MCP tasks. When you create a file, report its sandbox path. Be transparent about tool errors.",
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
      name: "nimbus-library",
      order: 80,
      text: librarySystemPrompt,
      interpolate: false,
    });
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
      render: ToolDefinition["output"]["render"] = (_args, value) => [
        { type: "text", text: JSON.stringify(value) },
      ],
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
          render,
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
    // library_read needs a custom execute/render so image bytes are admitted
    // to the attachment store instead of reaching the model as base64 text.
    const libraryRead = libraryToolDefinitions.find(
      ([name]) => name === "library_read",
    );
    if (!libraryRead) throw Error("library_read definition missing");
    register(
      libraryRead[0],
      libraryRead[1],
      libraryRead[2],
      libraryRead[3],
      async (args, exec) => {
        const result = (await remote(
          "library_read",
          args,
          exec,
        )) as LibraryReadResult;
        return resolveLibraryReadImage(result, (input) =>
          ctx.attachments.saveImage(input),
        );
      },
      renderLibraryRead,
    );
    for (const [name, desc, props, req] of libraryToolDefinitions) {
      if (name === "library_read") continue;
      register(name, desc, props, req, (args, exec) =>
        remote(name, args, exec),
      );
    }
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
    await ctx.plugin(WasmRuntime).await();
    for (const name of ["fs", "subprocess", "sandbox", "bash", "jobs"]) {
      if (ctx.get(name) !== undefined)
        throw Error("Unsafe local provider loaded: " + name);
    }
    if (ctx.ptcRuntime.isolation !== "wasm")
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
      agentCtx.tools.presentAs("ptc");
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
