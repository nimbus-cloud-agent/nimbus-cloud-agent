import { createInterface } from "node:readline";
import { createRequire } from "node:module";
import { realpath } from "node:fs/promises";
import { pathToFileURL } from "node:url";
import { Context } from "@deepseek-ai/cordis";
let ctx;
try {
  const input = JSON.parse(
    await new Promise((resolve) =>
      createInterface({ input: process.stdin }).once("line", resolve),
    ),
  );
  const req = createRequire(await realpath(input.harness_path));
  const { default: Attachments } = await import(
    pathToFileURL(req.resolve("@deepseek-ai/dsh-attachment-local"))
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
    mediaType: input.media_type,
    name: input.name,
  });
  const saved = await ctx.attachments.readImage(ref);
  console.log(
    JSON.stringify({
      result: { ref, data: Buffer.from(saved.data).toString("base64") },
    }),
  );
} catch (error) {
  console.log(JSON.stringify({ error: error.message }));
} finally {
  await ctx?.fiber.dispose();
}
process.exit(0);
