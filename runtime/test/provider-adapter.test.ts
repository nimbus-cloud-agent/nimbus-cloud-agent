import test from "node:test";
import assert from "node:assert/strict";
import {
  MessageId,
  type AdapterRegistrationHandle,
  type GenerateOptions,
  type LlmAdapter,
  type RequestMessage,
} from "@deepseek-ai/dsh-llm";
import {
  DeepSeekAdapter,
  resolveAdapterOptions,
  type DeepSeekAdapterOptions,
} from "@deepseek-ai/dsh-llm-deepseek";
import { withProviderAdapter } from "../src/provider-adapter.js";
import { withoutMissingImages } from "../src/request-images.js";
import {
  AttachmentId,
  type ImageAttachmentRef,
} from "@deepseek-ai/dsh-attachment";
import type { ModelDiagnostic } from "../src/generated/types.js";

const SECRET = "sk-diagnostic-placeholder";
const request: GenerateOptions = {
  provider: "deepseek-official",
  model: "deepseek-flash",
  messages: [],
  maxTokens: 16,
};

function deepseek(overrides: Partial<{ stream: LlmAdapter["stream"] }> = {}) {
  const connection = resolveAdapterOptions({
    baseURL: "https://api.example.invalid/anthropic",
    thinking: "disabled",
  });
  const adapter = new DeepSeekAdapter({
    options: () => connection,
    prepareExtensions: async () => ({ fields: {}, accept: async () => {} }),
    resolveAuth: async () => ({ headers: { "x-api-key": SECRET } }),
    // Forwarded only as an attribution header; the brand has no runtime shape.
    resolveUserId: () =>
      "nimbus-test" as ReturnType<DeepSeekAdapterOptions["resolveUserId"]>,
  });
  if (overrides.stream) adapter.stream = overrides.stream;
  return adapter;
}

async function instrumented(
  adapter: LlmAdapter,
  imageHostPath: (ref: ImageAttachmentRef) => string | undefined = () => undefined,
) {
  const emitted: ModelDiagnostic[] = [];
  const registry = {
    registerAdapter(
      _providers: string[],
      registered: LlmAdapter,
    ): AdapterRegistrationHandle {
      adapter = registered;
      return Object.assign(() => {}, { replace() {} });
    },
  };
  await withProviderAdapter(
    { imageHostPath },
    registry,
    SECRET,
    async (diagnostic) => {
      emitted.push(diagnostic);
    },
    async () => {
      registry.registerAdapter(["deepseek-official"], adapter);
    },
  );
  return { emitted, adapter };
}

// The store is stubbed in these tests, so the opaque id never has to resolve.
function imageRef(name: string): ImageAttachmentRef {
  return {
    attachmentId: AttachmentId(`sha256:${name}`),
    mediaType: "image/png",
    bytes: 1,
    width: 1,
    height: 1,
    name,
  };
}

function imageMessage(name: string, text = "what is this"): RequestMessage {
  return {
    id: MessageId(`message-${name}`),
    role: "user",
    source: { kind: "user" },
    content: [
      { type: "image", attachment: imageRef(name) },
      { type: "text", text },
    ],
  };
}

async function drain(chunks: AsyncIterable<unknown>) {
  for await (const _ of chunks) continue;
}

test("a transport failure records the wrapped cause without the API key", async () => {
  const { emitted, adapter } = await instrumented(deepseek());
  const original = globalThis.fetch;
  globalThis.fetch = async () => {
    throw new TypeError(`fetch failed while using ${SECRET}`, {
      cause: Object.assign(new Error(`socket hang up for ${SECRET}`), {
        code: "ECONNRESET",
        syscall: "read",
        address: "10.0.0.7",
        port: 443,
      }),
    });
  };
  try {
    const prepared = await adapter.prepareCall(request.provider, request.model);
    await assert.rejects(drain(prepared.stream(request)));
  } finally {
    globalThis.fetch = original;
  }
  assert.equal(emitted.length, 1);
  const [diagnostic] = emitted;
  assert.equal(diagnostic.provider, "deepseek-official");
  assert.equal(diagnostic.model, "deepseek-flash");
  assert.equal(diagnostic.phase, "request");
  assert.equal(diagnostic.chunks_received, 0);
  assert.equal(diagnostic.attempt, 1);
  const chain = diagnostic.error_chain;
  assert.deepEqual(
    chain.map((entry) => entry.code ?? entry.name),
    ["TRANSPORT", "TypeError", "ECONNRESET"],
  );
  const cause = chain.at(-1);
  assert.equal(cause?.syscall, "read");
  assert.equal(cause?.address, "10.0.0.7");
  assert.equal(cause?.port, 443);
  assert.ok(!JSON.stringify(diagnostic).includes(SECRET), JSON.stringify(diagnostic));
});

test("HTTP status and provider request id come from the failure snapshot", async () => {
  const { emitted, adapter } = await instrumented(
    deepseek({
      stream: async function* () {
        throw Object.assign(new Error("rate limited"), {
          code: "RATE_LIMIT",
          failure: { status: 429, request_id: "req-42" },
        });
      },
    }),
  );
  await assert.rejects(drain(adapter.stream(request)));
  const [entry] = emitted[0].error_chain;
  assert.equal(entry.status, 429);
  assert.equal(entry.request_id, "req-42");
});

test("a failure after output is attributed to the stream phase", async () => {
  const { emitted, adapter } = await instrumented(
    deepseek({
      stream: async function* () {
        yield { type: "text-delta", index: 0, text: "partial" };
        throw new Error("connection closed mid-stream");
      },
    }),
  );
  await assert.rejects(drain(adapter.stream(request)));
  assert.equal(emitted.length, 1);
  assert.equal(emitted[0].phase, "stream");
  assert.equal(emitted[0].chunks_received, 1);
});

test("a preparation failure is reported before any request", async () => {
  const adapter = deepseek();
  adapter.prepareCall = async () => {
    throw Object.assign(new TypeError("getaddrinfo ENOTFOUND api.example.invalid"), {
      code: "ENOTFOUND",
      syscall: "getaddrinfo",
    });
  };
  const { emitted, adapter: registered } = await instrumented(adapter);
  await assert.rejects(registered.prepareCall(request.provider, request.model));
  assert.equal(emitted.length, 1);
  assert.equal(emitted[0].phase, "prepare");
  assert.equal(emitted[0].chunks_received, 0);
  assert.equal(emitted[0].error_chain[0].code, "ENOTFOUND");
});

test("a user cancellation is not reported as a provider failure", async () => {
  const controller = new AbortController();
  controller.abort();
  const { emitted, adapter } = await instrumented(
    deepseek({
      stream: async function* () {
        throw new Error("aborted by user");
      },
    }),
  );
  await assert.rejects(
    drain(adapter.stream({ ...request, signal: controller.signal })),
  );
  assert.deepEqual(emitted, []);
});

test("oversized error text is bounded and flagged", async () => {
  const { emitted, adapter } = await instrumented(
    deepseek({
      stream: async function* () {
        throw new Error("x".repeat(20000));
      },
    }),
  );
  await assert.rejects(drain(adapter.stream(request)));
  const [entry] = emitted[0].error_chain;
  assert.equal(emitted[0].truncated, true);
  assert.equal(entry.message?.length, 161);
});

test("a lost attachment is replaced by text instead of failing the request", async () => {
  const { emitted, adapter } = await instrumented(
    deepseek(),
    () => "/nonexistent/lost.png",
  );
  const original = globalThis.fetch;
  let body = "";
  globalThis.fetch = async (input, init) => {
    body = init?.body ? String(init.body) : await new Request(input).text();
    throw new TypeError("stop after serialization");
  };
  try {
    await assert.rejects(
      drain(
        adapter.stream({
          ...request,
          messages: [imageMessage("lost.png"), imageMessage("also-lost.png")],
        }),
      ),
    );
  } finally {
    globalThis.fetch = original;
  }
  assert.match(body, /image unavailable/);
  assert.doesNotMatch(body, /"type":"image"/);
  // The request reached the provider: the failure is the transport, not the store.
  assert.equal(emitted[0].phase, "request");
  assert.equal(emitted[0].error_chain.at(-1)?.message, "stop after serialization");
});

test("readable attachments and offloaded placeholders are sent unchanged", async () => {
  const { mkdtemp, writeFile } = await import("node:fs/promises");
  const { tmpdir } = await import("node:os");
  const { join } = await import("node:path");
  const dir = await mkdtemp(join(tmpdir(), "nimbus-image-"));
  const present = join(dir, "present.png");
  await writeFile(present, "png");
  const ref = imageRef("present.png");
  const offloaded = { ...imageRef("gone.png"), name: "gone.png" };
  const options = {
    ...request,
    messages: [
      {
        ...imageMessage("present.png"),
        content: [
          { type: "image" as const, attachment: ref },
          { type: "image" as const, attachment: offloaded, offloaded: true as const },
        ],
      },
    ],
  };
  const result = await withoutMissingImages(
    { imageHostPath: (candidate) => (candidate === ref ? present : join(dir, "missing.png")) },
    options,
  );
  assert.equal(result, options);
});
