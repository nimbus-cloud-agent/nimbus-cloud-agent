import test from "node:test";
import assert from "node:assert/strict";
import { renderToolsSdk } from "@deepseek-ai/dsh-tools";
import type { ImageAttachmentRef } from "@deepseek-ai/dsh-attachment";
import {
  librarySystemPrompt,
  libraryToolDefinitions,
  renderLibraryRead,
  resolveLibraryReadImage,
  type LibraryReadResult,
} from "../src/turn.js";

const LIBRARY_NAMES = [
  "library_list",
  "library_read",
  "library_write",
  "library_versions",
  "library_copy_to_sandbox",
  "library_copy_from_sandbox",
];

function definition(name: string) {
  const found = libraryToolDefinitions.find(([n]) => n === name);
  assert.ok(found, `${name} must be defined`);
  return found;
}

// 1x1 transparent PNG.
const PNG_BASE64 =
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==";

const fakeRef = {
  attachmentId: "test-attachment",
  mediaType: "image/png",
  bytes: 70,
  width: 1,
  height: 1,
} as unknown as ImageAttachmentRef;

test("library tools are defined with the contracted names and schemas", () => {
  assert.deepEqual(
    libraryToolDefinitions.map(([name]) => name).sort(),
    [...LIBRARY_NAMES].sort(),
  );
  const read = definition("library_read");
  assert.deepEqual(Object.keys(read[2]).sort(), [
    "id",
    "length",
    "name",
    "offset",
    "version",
  ]);
  assert.deepEqual(read[3], []);
  assert.match(read[1], /1 MiB/);
  const write = definition("library_write");
  assert.deepEqual(write[3], ["name", "content"]);
  assert.match(write[1], /100 MB/);
  assert.match(write[1], /nimbus:\/\/library\/<id>/);
  assert.deepEqual(definition("library_list")[3], []);
  assert.deepEqual(definition("library_versions")[3], []);
  assert.deepEqual(definition("library_copy_to_sandbox")[3], ["path"]);
  assert.deepEqual(definition("library_copy_from_sandbox")[3], ["path"]);
  assert.match(definition("library_copy_to_sandbox")[1], /1 MiB chunks/);
  assert.match(definition("library_copy_from_sandbox")[1], /100 MB/);
});

test("library system prompt tells the model when to use the Library", () => {
  for (const phrase of [
    "nimbus://library/<id>",
    "PostgreSQL-backed",
    "version",
    "sandbox is the workbench",
    "shelf for finished deliverables",
    "library_copy_to_sandbox",
    "library_copy_from_sandbox",
    "never handle chunking",
    "100 MB per file",
    "1 MiB per text read",
    "shown to you directly",
  ])
    assert.ok(
      librarySystemPrompt.includes(phrase),
      `prompt must mention: ${phrase}`,
    );
});

test("renderLibraryRead renders text results as a single JSON text block", () => {
  const value: LibraryReadResult = {
    id: "abc",
    name: "notes.md",
    version: 2,
    content: "# hi\n",
    truncated: false,
    link: "nimbus://library/abc",
  };
  const blocks = renderLibraryRead({}, value);
  assert.equal(blocks.length, 1);
  assert.equal(blocks[0]?.type, "text");
  assert.deepEqual(
    JSON.parse((blocks[0] as { text: string }).text),
    value,
  );
});

test("renderLibraryRead maps an admitted image to an image block, never base64", () => {
  const value: LibraryReadResult = {
    id: "abc",
    name: "cat.png",
    version: 1,
    mime_type: "image/png",
    size_bytes: 70,
    image: { media_type: "image/png", attachment: fakeRef },
    link: "nimbus://library/abc",
  };
  const blocks = renderLibraryRead({}, value);
  assert.equal(blocks.length, 2);
  assert.equal(blocks[0]?.type, "image");
  assert.deepEqual(
    (blocks[0] as { attachment: ImageAttachmentRef }).attachment,
    fakeRef,
  );
  assert.equal(blocks[1]?.type, "text");
  assert.ok(!JSON.stringify(blocks).includes(PNG_BASE64));
});

test("renderLibraryRead renders a metadata-only image result as text", () => {
  const value: LibraryReadResult = {
    id: "abc",
    name: "big.png",
    mime_type: "image/png",
    image: { media_type: "image/png" },
    link: "nimbus://library/abc",
    note: "Image preview unavailable (too large); open the link to preview it.",
  };
  const blocks = renderLibraryRead({}, value);
  assert.equal(blocks.length, 1);
  assert.equal(blocks[0]?.type, "text");
  assert.match((blocks[0] as { text: string }).text, /preview unavailable/);
});

test("resolveLibraryReadImage admits the image and strips its base64", async () => {
  const seen: { bytes: Buffer; mediaType: string }[] = [];
  const result: LibraryReadResult = {
    id: "abc",
    name: "cat.png",
    mime_type: "image/png",
    image: { media_type: "image/png", data: PNG_BASE64 },
    link: "nimbus://library/abc",
  };
  const resolved = await resolveLibraryReadImage(result, async (input) => {
    seen.push({
      bytes: Buffer.from(input.data),
      mediaType: input.mediaType,
    });
    return fakeRef;
  });
  assert.equal(seen.length, 1);
  assert.equal(seen[0]?.mediaType, "image/png");
  assert.equal(
    seen[0]?.bytes.toString("base64"),
    PNG_BASE64,
  );
  assert.deepEqual(resolved.image, {
    media_type: "image/png",
    attachment: fakeRef,
  });
  assert.ok(!("data" in (resolved.image ?? {})));
  assert.ok(!JSON.stringify(resolved).includes(PNG_BASE64));
});

test("resolveLibraryReadImage falls back to metadata plus a note when admission fails", async () => {
  const result: LibraryReadResult = {
    id: "abc",
    name: "drawing.svg",
    mime_type: "image/svg+xml",
    image: { media_type: "image/svg+xml", data: PNG_BASE64 },
    link: "nimbus://library/abc",
  };
  const resolved = await resolveLibraryReadImage(result, async () => {
    throw Error("unsupported media type");
  });
  assert.deepEqual(resolved.image, { media_type: "image/svg+xml" });
  assert.ok(!("data" in (resolved.image ?? {})));
  assert.match(resolved.note ?? "", /Image preview unavailable/);
  assert.match(resolved.note ?? "", /nimbus:\/\/library\/abc/);
});

test("resolveLibraryReadImage passes text results through untouched", async () => {
  const result: LibraryReadResult = {
    id: "abc",
    name: "notes.md",
    content: "# hi\n",
  };
  const resolved = await resolveLibraryReadImage(result, async () => {
    throw Error("saveImage must not be called for text");
  });
  assert.equal(resolved, result);
});

test("library tool schemas project to Wasm SDK bindings", () => {
  // Same projection the PTC runtime uses: renderToolsSdk turns registered
  // tool schemas into the `declare const tools` SDK the model programs
  // against inside run_code. If a library tool is missing here, Wasm code
  // cannot call it.
  const sdk = renderToolsSdk(
    libraryToolDefinitions.map(([name, description, properties, required]) => ({
      name,
      description,
      parameters: {
        type: "object",
        properties,
        required,
        additionalProperties: false,
      },
      output: {},
    })),
  );
  assert.match(sdk, /declare const tools/);
  for (const name of LIBRARY_NAMES)
    assert.ok(
      sdk.includes(`${name}(`) || sdk.includes(`${name}:`),
      `SDK must declare ${name}`,
    );
});
