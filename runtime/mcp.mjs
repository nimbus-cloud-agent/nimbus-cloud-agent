import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import { auth } from "@modelcontextprotocol/sdk/client/auth.js";
import { createInterface } from "node:readline";
import { writeJSON } from "./protocol.mjs";
const input = JSON.parse(
  await new Promise((r) =>
    createInterface({ input: process.stdin }).once("line", r),
  ),
);
const data = input.oauth_data || {};
let authURL;
let client;
const provider = {
  get redirectUrl() {
    return input.callback_url;
  },
  get clientMetadata() {
    return {
      client_name: "Nimbus Cloud Agent",
      redirect_uris: [input.callback_url],
      grant_types: ["authorization_code", "refresh_token"],
      response_types: ["code"],
      token_endpoint_auth_method: "none",
    };
  },
  state: () => data.state,
  clientInformation: () => data.client,
  saveClientInformation: (v) => {
    data.client = v;
  },
  tokens: () => data.tokens,
  saveTokens: (v) => {
    data.tokens = v;
    delete data.state;
    delete data.expires_at;
  },
  redirectToAuthorization: (u) => {
    authURL = u.toString();
  },
  saveCodeVerifier: (v) => {
    data.verifier = v;
  },
  codeVerifier: () => data.verifier,
  invalidateCredentials: (scope) => {
    if (scope === "all" || scope === "tokens") delete data.tokens;
    if (scope === "all" || scope === "client") delete data.client;
  },
};
let result = {};
try {
  if (input.op === "authorize" || input.op === "callback") {
    await auth(provider, {
      serverUrl: input.url,
      authorizationCode: input.args?.code,
    });
    result = { auth_url: authURL, ok: !!data.tokens };
  } else {
    client = new Client({ name: "nimbus", version: "0.1.0" });
    await client.connect(
      new StreamableHTTPClientTransport(new URL(input.url), {
        authProvider: provider,
      }),
    );
    if (input.op === "list") {
      let tools = [],
        cursor;
      do {
        const page = await client.listTools(cursor ? { cursor } : {});
        tools.push(...page.tools);
        cursor = page.nextCursor;
      } while (cursor);
      result = { tools, ok: true };
    } else if (input.op === "call")
      result = { result: await client.callTool(input.args) };
  }
} catch (e) {
  result = { error: String(e.message), auth_url: authURL };
} finally {
  if (client) await client.close().catch(() => {});
}
await writeJSON({
  result: {
    ...result,
    oauth_data: data,
    oauth_state: authURL
      ? "pending"
      : data.tokens
        ? "ok"
        : result.error
          ? "expired"
          : "none",
  },
});
process.exit(0);
