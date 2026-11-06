import type {
  AttachmentStore,
  ImageAttachmentRef,
  SaveImageAttachment,
} from "@deepseek-ai/dsh-attachment";
import type { AttachmentData } from "./generated/types.js";

// The harness stores attachments in the turn's work directory, which is
// replaced on the next turn and after every restart. An image an agent produces
// would therefore be gone before its history is replayed. Report every saved
// image to the control plane, which owns durable storage; images it already
// holds are never sent back.
export function persistSavedImages(
  attachments: Pick<AttachmentStore, "saveImage" | "readImage">,
  delivered: Iterable<string>,
  emit: (data: AttachmentData) => Promise<void>,
) {
  const known = new Set(delivered);
  const save = attachments.saveImage.bind(attachments);
  attachments.saveImage = async (input: SaveImageAttachment) => {
    const ref: ImageAttachmentRef = await save(input);
    if (!known.has(ref.attachmentId)) {
      const stored = await attachments.readImage(ref);
      await emit({
        ref,
        data: Buffer.from(stored.data).toString("base64"),
      });
      known.add(ref.attachmentId);
    }
    return ref;
  };
}
