import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import {
  auth,
  type OAuthClientProvider,
} from "@modelcontextprotocol/sdk/client/auth.js";
import type { MCPRequest, MCPResult, MCPTool } from "./generated/types.js";
import {
  OAuthClientInformationSchema,
  OAuthTokensSchema,
  type OAuthClientInformationMixed,
  type OAuthTokens,
} from "@modelcontextprotocol/sdk/shared/auth.js";
import {
  CallToolResultSchema,
  type CallToolResult,
} from "@modelcontextprotocol/sdk/types.js";
import { errorMessage } from "./errors.js";
interface OAuthData {
  state?: string;
  client?: OAuthClientInformationMixed;
  tokens?: OAuthTokens;
  expires_at?: number;
  verifier?: string;
}
export async function mcp(
  input: MCPRequest,
  { signal }: { signal: AbortSignal },
): Promise<MCPResult> {
  const fetchWithSignal: typeof fetch = (url, init = {}) =>
    fetch(url, {
      ...init,
      signal:
        signal && init.signal
          ? AbortSignal.any([signal, init.signal])
          : (signal ?? init.signal),
    });
  const source = input.oauth_data;
  const data: OAuthData = {
    state: source?.state,
    expires_at: source?.expires_at,
    verifier: source?.verifier,
    client:
      source?.client == null
        ? undefined
        : OAuthClientInformationSchema.passthrough().parse(source.client),
    tokens:
      source?.tokens == null
        ? undefined
        : OAuthTokensSchema.passthrough().parse(source.tokens),
  };
  let authURL: string | undefined;
  let client: Client | undefined;
  const provider: OAuthClientProvider = {
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
    state: () => data.state ?? "",
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
    codeVerifier: () => {
      if (!data.verifier) throw Error("Missing OAuth verifier");
      return data.verifier;
    },
    invalidateCredentials: (scope) => {
      if (scope === "all" || scope === "tokens") delete data.tokens;
      if (scope === "all" || scope === "client") delete data.client;
    },
  };
  let tools: MCPTool[] | undefined,
    callResult: CallToolResult | undefined,
    error: string | undefined,
    ok = false;
  try {
    if (input.op === "authorize" || input.op === "callback") {
      await auth(provider, {
        serverUrl: input.url,
        authorizationCode: input.args?.code,
        fetchFn: fetchWithSignal,
      });
      ok = !!data.tokens;
    } else {
      client = new Client({ name: "nimbus", version: "0.2.0" });
      await client.connect(
        new StreamableHTTPClientTransport(new URL(input.url), {
          authProvider: provider,
          fetch: fetchWithSignal,
        }),
      );
      if (input.op === "list") {
        tools = [];
        let cursor: string | undefined;
        do {
          const page = await client.listTools(cursor ? { cursor } : {}, {
            signal,
          });
          tools.push(
            ...page.tools.map((t) => ({
              name: t.name,
              description: t.description,
              inputSchema: t.inputSchema,
            })),
          );
          cursor = page.nextCursor;
        } while (cursor);
        ok = true;
      } else if (input.op === "call") {
        if (!input.args?.name) throw Error("MCP tool name is required");
        const args = input.args.arguments;
        if (args != null && (typeof args !== "object" || Array.isArray(args)))
          throw Error("MCP tool arguments must be an object");
        callResult = CallToolResultSchema.parse(
          await client.callTool(
            {
              name: input.args.name,
              arguments: (args ?? undefined) as
                | Record<string, unknown>
                | undefined,
            },
            CallToolResultSchema,
            { signal },
          ),
        );
        ok = true;
      }
    }
  } catch (e) {
    error = errorMessage(e);
  } finally {
    if (client) await client.close().catch(() => {});
  }
  return {
    ok,
    tools,
    result: callResult,
    error,
    auth_url: authURL,
    oauth_data: data,
    oauth_state: authURL
      ? "pending"
      : data.tokens
        ? "ok"
        : error
          ? "expired"
          : "none",
  };
}
