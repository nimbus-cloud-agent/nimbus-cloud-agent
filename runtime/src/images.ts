import { createRequire } from "node:module";
import { pathToFileURL } from "node:url";
import { Context } from "@deepseek-ai/cordis";
import type { ImageMediaType } from "@deepseek-ai/dsh-attachment";
import type { ImageRequest } from "./generated/types.js";
import type {} from "@deepseek-ai/dsh-attachment-local";
export async function images(input: ImageRequest) {
  let ctx;
  try {
    const req = createRequire(import.meta.url);
    const { default: Attachments } = await import(
      pathToFileURL(req.resolve("@deepseek-ai/dsh-attachment-local")).href
    );
    ctx = new Context();
    await ctx
      .plugin(Attachments, {
        dshHome: input.work_dir,
        maxImageBytes: 5 * 1024 * 1024,
      })
      .await();
    const ref = await ctx.attachments.saveImage({
      data: Buffer.from(input.data, "base64"),
      mediaType: input.media_type as ImageMediaType,
      name: input.name,
    });
    const saved = await ctx.attachments.readImage(ref);
    return { ref, data: Buffer.from(saved.data).toString("base64") };
  } finally {
    await ctx?.fiber.dispose();
  }
}
