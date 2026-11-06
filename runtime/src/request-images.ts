import { access } from "node:fs/promises";
import type {
  ContentBlock,
  GenerateOptions,
  ImageBlock,
  RequestMessage,
} from "@deepseek-ai/dsh-llm";
import type { ImageAttachmentRef } from "@deepseek-ai/dsh-attachment";

// Attachment bytes live in the container's working directory, but session
// history outlives the container. After a restart or deploy the harness fails
// the whole model request with ATTACHMENT_NOT_FOUND while re-encoding history,
// so every later turn in that session dies before producing a token. Replace
// only the unreadable images with a text placeholder and keep the turn usable.
const UNAVAILABLE =
  "[image unavailable: its stored bytes were removed before this turn]";

export type ImageAccess = {
  imageHostPath(ref: ImageAttachmentRef): string | undefined;
};

export async function withoutMissingImages(
  attachments: ImageAccess,
  options: GenerateOptions,
): Promise<GenerateOptions> {
  if (!options.messages.some((message) => message.content.some(isImage)))
    return options;
  const messages: RequestMessage[] = [];
  let changed = false;
  for (const message of options.messages) {
    if (!message.content.some(isImage)) {
      messages.push(message);
      continue;
    }
    let missing = false;
    const content: ContentBlock[] = [];
    for (const block of message.content) {
      if (!isImage(block) || block.offloaded || (await stored(attachments, block))) {
        content.push(block);
      } else {
        content.push({
          type: "text",
          text: `${UNAVAILABLE} (${block.attachment.name})`,
        });
        missing = true;
      }
    }
    changed ||= missing;
    messages.push(missing ? { ...message, content } : message);
  }
  return changed ? { ...options, messages } : options;
}

function isImage(block: ContentBlock): block is ImageBlock {
  return block.type === "image";
}

async function stored(attachments: ImageAccess, block: ImageBlock) {
  const path = attachments.imageHostPath(block.attachment);
  // A non host-file-backed store cannot be checked without reading the bytes.
  if (path === undefined) return true;
  try {
    await access(path);
    return true;
  } catch (error) {
    if (
      error instanceof Error &&
      "code" in error &&
      (error.code === "ENOENT" || error.code === "ENOTDIR")
    )
      return false;
    throw error;
  }
}
