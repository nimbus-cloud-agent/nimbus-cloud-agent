// Local protocol fixture; never contacts external services or contains real credentials.
import http from "node:http";
import { createHash, randomUUID } from "node:crypto";
const origin = "http://127.0.0.1:18081";
const codes = new Map();
let token = "";
let refreshes = 0;
const send = (res, status, data, headers = {}) => {
  res.writeHead(status, { "Content-Type": "application/json", ...headers });
  res.end(JSON.stringify(data));
};
const server = http.createServer(async (req, res) => {
  const u = new URL(req.url, origin);
  let raw = "";
  for await (const b of req) raw += b;
  let body;
  try {
    body = JSON.parse(raw || "{}");
  } catch {
    body = Object.fromEntries(new URLSearchParams(raw));
  }
  if (
    u.pathname === "/.well-known/oauth-protected-resource" ||
    u.pathname === "/.well-known/oauth-protected-resource/mcp"
  )
    return send(res, 200, {
      resource: origin + "/mcp",
      authorization_servers: [origin],
      scopes_supported: ["tools"],
    });
  if (u.pathname === "/.well-known/oauth-authorization-server")
    return send(res, 200, {
      issuer: origin,
      authorization_endpoint: origin + "/authorize",
      token_endpoint: origin + "/token",
      registration_endpoint: origin + "/register",
      response_types_supported: ["code"],
      grant_types_supported: ["authorization_code", "refresh_token"],
      code_challenge_methods_supported: ["S256"],
      token_endpoint_auth_methods_supported: ["none"],
    });
  if (u.pathname === "/register")
    return send(res, 201, { client_id: "nimbus-fixture", ...body });
  if (u.pathname === "/authorize") {
    const code = randomUUID();
    codes.set(code, {
      challenge: u.searchParams.get("code_challenge"),
      redirect: u.searchParams.get("redirect_uri"),
    });
    const cb = new URL(u.searchParams.get("redirect_uri"));
    cb.searchParams.set("code", code);
    cb.searchParams.set("state", u.searchParams.get("state"));
    res.writeHead(302, { Location: cb.href });
    return res.end();
  }
  if (u.pathname === "/token") {
    if (body.grant_type === "authorization_code") {
      const c = codes.get(body.code);
      if (
        !c ||
        createHash("sha256")
          .update(body.code_verifier || "")
          .digest("base64url") !== c.challenge
      )
        return send(res, 400, { error: "invalid_grant" });
      codes.delete(body.code);
    } else if (body.grant_type === "refresh_token") {
      if (body.refresh_token !== "fixture-refresh")
        return send(res, 400, { error: "invalid_grant" });
      refreshes++;
    } else return send(res, 400, { error: "unsupported_grant_type" });
    token = randomUUID();
    return send(res, 200, {
      access_token: token,
      refresh_token: "fixture-refresh",
      token_type: "Bearer",
      expires_in: 3600,
      scope: "tools",
    });
  }
  if (u.pathname === "/expire") {
    token = "expired";
    return send(res, 200, { ok: true });
  }
  if (u.pathname === "/status") return send(res, 200, { refreshes });
  if (u.pathname !== "/mcp") return send(res, 404, {});
  if (req.headers.authorization !== "Bearer " + token || !token)
    return send(
      res,
      401,
      { error: "unauthorized" },
      {
        "WWW-Authenticate": `Bearer resource_metadata="${origin}/.well-known/oauth-protected-resource"`,
      },
    );
  if (req.method === "DELETE") {
    res.writeHead(200);
    return res.end();
  }
  if (req.method !== "POST") {
    res.writeHead(405);
    return res.end();
  }
  if (body.method?.startsWith("notifications/")) {
    res.writeHead(202);
    return res.end();
  }
  let result;
  if (body.method === "initialize")
    result = {
      protocolVersion: "2025-03-26",
      capabilities: { tools: {} },
      serverInfo: { name: "nimbus-test", version: "1" },
    };
  else if (body.method === "tools/list")
    result = {
      tools: [
        {
          name: "echo",
          description: "Return an exact echo for integration verification",
          inputSchema: {
            type: "object",
            properties: { text: { type: "string" } },
            required: ["text"],
          },
        },
      ],
    };
  else if (body.method === "tools/call")
    result = {
      content: [{ type: "text", text: "MCP_OK:" + body.params.arguments.text }],
    };
  else if (body.method === "ping") result = {};
  else
    return send(res, 200, {
      jsonrpc: "2.0",
      id: body.id,
      error: { code: -32601, message: "method not found" },
    });
  send(res, 200, { jsonrpc: "2.0", id: body.id, result });
});
server.listen(18081, "127.0.0.1", () =>
  console.log("MCP OAuth fixture on " + origin),
);
