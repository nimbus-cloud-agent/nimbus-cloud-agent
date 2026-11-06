import test from "node:test";
import assert from "node:assert/strict";
import { AttachmentId, type ImageAttachmentRef } from "@deepseek-ai/dsh-attachment";
import { persistSavedImages } from "../src/attachments.js";
import type { AttachmentData } from "../src/generated/types.js";

function imageRef(name: string): ImageAttachmentRef {
  return {
    attachmentId: AttachmentId(`sha256:${name}`),
    mediaType: "image/png",
    bytes: 3,
    width: 1,
    height: 1,
    name,
  };
}

function store(ref: ImageAttachmentRef, data: Uint8Array) {
  return {
    saveImage: async () => ref,
    readImage: async () => ({ ref, data }),
  };
}

test("an image the agent produced is reported to the control plane", async () => {
  const ref = imageRef("agent.png");
  const emitted: AttachmentData[] = [];
  const attachments = store(ref, new Uint8Array([1, 2, 3]));
  persistSavedImages(attachments, [], async (data) => {
    emitted.push(data);
  });
  assert.equal(await attachments.saveImage(), ref);
  assert.equal(emitted.length, 1);
  assert.deepEqual(emitted[0].ref, ref);
  assert.equal(
    Buffer.from(emitted[0].data, "base64").toString("hex"),
    "010203",
  );
  // Saving again within the same turn must not resend the bytes.
  await attachments.saveImage();
  assert.equal(emitted.length, 1);
});

test("images the control plane already holds are not sent back", async () => {
  const ref = imageRef("upload.png");
  const emitted: AttachmentData[] = [];
  const attachments = store(ref, new Uint8Array([9]));
  persistSavedImages(attachments, [ref.attachmentId], async (data) => {
    emitted.push(data);
  });
  await attachments.saveImage();
  assert.deepEqual(emitted, []);
});
