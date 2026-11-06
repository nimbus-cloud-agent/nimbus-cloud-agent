import test from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  MessageId,
  type AdapterRegistrationHandle,
  type ContentBlock,
  type GenerateOptions,
  type ImageBlock,
  type LlmAdapter,
  type RequestMessage,
} from "@deepseek-ai/dsh-llm";
import {
  AttachmentId,
  type ImageAttachmentRef,
} from "@deepseek-ai/dsh-attachment";
import {
  DeepSeekAdapter,
  resolveAdapterOptions,
  type DeepSeekAdapterOptions,
} from "@deepseek-ai/dsh-llm-deepseek";
import { withoutMissingImages } from "../src/request-images.js";
import { withProviderAdapter } from "../src/provider-adapter.js";

const SECRET = "sk-prompt-cache";

function message(
  id: string,
  role: "user" | "assistant",
  content: ContentBlock[],
): RequestMessage {
  return role === "user"
    ? { id: MessageId(id), role, source: { kind: "user" }, content }
    : {
        id: MessageId(id),
        role,
        source: { kind: "model", provider: "deepseek-official", model: "deepseek-flash" },
        content,
      };
}

function text(text: string): ContentBlock {
  return { type: "text", text };
}

function image(name: string): ImageBlock {
  return {
    type: "image",
    attachment: {
      attachmentId: AttachmentId(`sha256:${name}`),
      mediaType: "image/png",
      bytes: 3,
      width: 1,
      height: 1,
      name,
    },
  };
}

function request(messages: RequestMessage[]): GenerateOptions {
  return {
    provider: "deepseek-official",
    model: "deepseek-flash",
    messages,
    maxTokens: 16,
  };
}

function deepseek(attachments?: {
  store: import("@deepseek-ai/dsh-attachment").AttachmentStore;
  path(ref: ImageAttachmentRef): string | undefined;
}): LlmAdapter {
  const connection = resolveAdapterOptions({
    baseURL: "https://api.example.invalid/anthropic",
    thinking: "disabled",
  });
  return new DeepSeekAdapter({
    options: () => connection,
    prepareExtensions: async () => ({ fields: {}, accept: async () => {} }),
    resolveAuth: async () => ({ headers: { "x-api-key": SECRET } }),
    // Forwarded only as an attribution header; the brand has no runtime shape.
    resolveUserId: () =>
      "nimbus-test" as ReturnType<DeepSeekAdapterOptions["resolveUserId"]>,
    ...(attachments === undefined
      ? {}
      : {
          resolveAttachments: () => attachments.store,
          resolveImageAccess: (_store, ref) => {
            const readonlyPath = attachments.path(ref);
            return readonlyPath === undefined ? undefined : { readonlyPath };
          },
        }),
  });
}

// A real 1x1 PNG so the attachment store accepts and normalizes it.
const PNG = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
  "base64",
);

async function instrumented(
  adapter: LlmAdapter,
  imageHostPath: (ref: ImageBlock["attachment"]) => string | undefined,
) {
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
    async () => {},
    async () => {
      registry.registerAdapter(["deepseek-official"], adapter);
    },
  );
  return adapter;
}

async function body(adapter: LlmAdapter, options: GenerateOptions) {
  const original = globalThis.fetch;
  let captured = "";
  globalThis.fetch = async (input, init) => {
    const request = input instanceof Request ? input : new Request(input, init);
    captured = await request.clone().text();
    throw new TypeError("stop after capture");
  };
  try {
    for await (const _ of adapter.stream(options)) continue;
  } catch {
    // The mocked transport always fails after the request is serialized.
  } finally {
    globalThis.fetch = original;
  }
  assert.ok(captured.length > 0, "the request was never serialized");
  return JSON.parse(captured) as {
    system?: unknown;
    tools?: unknown;
    messages: unknown[];
  };
}

// Prefix caching only pays off while the serialized prefix is byte-identical,
// so every transform below must leave untouched history exactly as it was.
test("a request whose images all exist is returned unchanged", async () => {
  const dir = await mkdtemp(join(tmpdir(), "nimbus-cache-"));
  const present = join(dir, "present.png");
  await writeFile(present, "png");
  const options = request([
    message("m1", "user", [image("present.png"), text("what is this")]),
  ]);
  const result = await withoutMissingImages(
    { imageHostPath: () => present },
    options,
  );
  assert.equal(result, options);
});

test("only the unreadable image block changes, never earlier history", async () => {
  const options = request([
    message("m1", "user", [text("first")]),
    message("m2", "assistant", [text("reply")]),
    message("m3", "user", [image("missing.png"), text("describe")]),
  ]);
  const result = await withoutMissingImages(
    { imageHostPath: () => "/nope/missing.png" },
    options,
  );
  assert.equal(
    JSON.stringify(result.messages.slice(0, 2)),
    JSON.stringify(options.messages.slice(0, 2)),
  );
  const [first, second] = result.messages[2].content;
  assert.equal(first.type, "text");
  assert.match(first.type === "text" ? first.text : "", /image unavailable/);
  assert.deepEqual(second, options.messages[2].content[1]);
});

test("a follow-up turn reproduces the previous request prefix byte for byte", async () => {
  const adapter = await instrumented(deepseek(), () => "/nope/missing.png");
  const history = [
    message("m1", "user", [image("missing.png"), text("what is this")]),
    message("m2", "assistant", [text("a screenshot")]),
  ];
  const first = await body(adapter, request([history[0]]));
  const second = await body(
    adapter,
    request([history[0], history[1], message("m3", "user", [text("next")])]),
  );
  assert.equal(second.system, first.system);
  assert.deepEqual(second.tools, first.tools);
  // The earlier messages are one byte-prefix of the later ones, so the provider
  // can reuse its cached prefix for the whole previous conversation.
  assert.ok(
    JSON.stringify(second.messages).startsWith(
      JSON.stringify(first.messages).slice(0, -1),
    ),
    "message prefix diverged between turns",
  );
});

test("an existing attachment is serialized identically with and without the transform", async () => {
  const { Context } = await import("@deepseek-ai/cordis");
  const { LocalAttachmentStore } = await import(
    "@deepseek-ai/dsh-attachment-local"
  );
  const dir = await mkdtemp(join(tmpdir(), "nimbus-store-"));
  const store = new LocalAttachmentStore(new Context(), { dshHome: dir });
  const ref = await store.saveImage({
    data: new Uint8Array(PNG),
    mediaType: "image/png",
    name: "present.png",
  });
  const path = (candidate: ImageAttachmentRef) => store.imageHostPath(candidate);
  const messages = [
    message("m1", "user", [
      { type: "image", attachment: ref },
      text("what is this"),
    ]),
  ];
  const plain = await body(deepseek({ store, path }), request(messages));
  const wrapped = await body(
    await instrumented(deepseek({ store, path }), path),
    request(messages),
  );
  assert.equal(
    JSON.stringify(wrapped),
    JSON.stringify(plain),
    "the transform changed a request whose attachment exists",
  );
});
