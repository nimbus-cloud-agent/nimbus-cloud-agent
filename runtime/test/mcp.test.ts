import test from "node:test";
import assert from "node:assert/strict";
import http from "node:http";
import { TestRuntime } from "./helpers.js";

test(
  "MCP preserves large paginated tool lists and large call results over stdout pipes",
  { timeout: 20000 },
  async () => {
    // Larger than the macOS pipe buffer; do not depend on a live OAuth account.
    const description = "Large MCP schema 描述 ".repeat(6000);
    const content = "Large tool response 正文 ".repeat(60000);
    const cursors: string[] = [];
    const server = http.createServer(async (req, res) => {
      if (req.method !== "POST") {
        res.writeHead(405);
        res.end();
        return;
      }
      let raw = "";
      for await (const chunk of req) raw += chunk;
      const message = JSON.parse(raw);
      if (message.method.startsWith("notifications/")) {
        res.writeHead(202);
        res.end();
        return;
      }
      let result;
      if (message.method === "initialize")
        result = {
          protocolVersion: "2025-03-26",
          capabilities: { tools: {} },
          serverInfo: { name: "large-fixture", version: "1" },
        };
      else if (message.method === "tools/list") {
        const cursor = message.params?.cursor;
        cursors.push(cursor ?? "first");
        result = {
          tools: [
            {
              name: cursor ? "last-tool" : "first-tool",
              description,
              inputSchema: { type: "object", properties: {} },
            },
          ],
          ...(cursor ? {} : { nextCursor: "page-2" }),
        };
      } else if (message.method === "tools/call")
        result = { content: [{ type: "text", text: content }], isError: false };
      else result = {};
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ jsonrpc: "2.0", id: message.id, result }));
    });
    await new Promise<void>((resolve) =>
      server.listen(0, "127.0.0.1", resolve),
    );
    const runtime = new TestRuntime();
    async function invoke(op: "list" | "call") {
      return runtime.client.mcp({
        op,
        url: `http://127.0.0.1:${(server.address() as import("node:net").AddressInfo).port}/mcp`,
        oauth_data: {},
        callback_url: "http://127.0.0.1/oauth/callback",
        args: { name: "first-tool", arguments: {} },
      });
    }
    try {
      const listed = await invoke("list");
      assert.equal(listed.ok, true);
      assert.deepEqual(cursors, ["first", "page-2"]);
      assert.deepEqual(
        listed.tools!.map((t) => t.name),
        ["first-tool", "last-tool"],
      );
      assert.ok(listed.tools!.every((t) => t.description === description));
      const called = await invoke("call");
      const result =
        called.result as import("@modelcontextprotocol/sdk/types.js").CallToolResult;
      assert.equal(result.content[0].type, "text");
      assert.equal(result.content[0].text, content);
      assert.equal(result.isError, false);
    } finally {
      await runtime.close();
      await new Promise<void>((resolve, reject) =>
        server.close((error) => (error ? reject(error) : resolve())),
      );
    }
  },
);
