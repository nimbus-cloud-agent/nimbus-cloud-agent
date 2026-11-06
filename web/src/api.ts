import { decodeEvent } from "./event-codec";
export async function api<T = unknown>(
  path: string,
  method = "GET",
  body?: unknown,
  signal?: AbortSignal,
): Promise<T> {
  const r = await fetch("/api" + path, {
    method,
    headers: body ? { "Content-Type": "application/json" } : {},
    body: body ? JSON.stringify(body) : undefined,
    signal,
  });
  if (!r.ok) {
    let error = "Request failed";
    try {
      error = (await r.json()).error;
    } catch {
      error = r.statusText;
    }
    throw Error(error);
  }
  if (r.status === 204) return undefined as T;
  const data: unknown = await r.json();
  return data as T;
}
export type Options = {
  model: string;
  thinking: string;
  plan: boolean;
};
export type Session = {
  id: string;
  title: string;
  agent_state: string;
  options: Options;
  last_active_at: string;
  sandbox: {
    state: string;
    e2b_sandbox_id?: string;
    info?: { region?: string; cpuCount?: number; memoryMB?: number };
    error?: string;
  };
};
export type ContentBlock = { type: string; text?: string; reasoning?: string };
export type Content = string | ContentBlock[];
// Foreign extension fields remain available for the trace viewer; rendering uses
// the known projection and the normalized tool fields computed at ingress.
export type NativeData = {
  id?: string;
  callId?: string;
  subCallId?: string;
  parentCallId?: string;
  rootCallId?: string;
  name?: string;
  toolName?: string;
  arguments?: unknown;
  message?: {
    content?: Content;
    toolCallId?: string;
    isError?: boolean;
    source?: { callId?: string };
  };
  content?: Content;
  result?: { content?: Content };
  isError?: boolean;
  error?: string | { message?: string };
  step?: number;
  turn?: number;
};
export type WireEvent = {
  seq: number;
  type: string;
  payload: { time?: number; data: NativeData };
};
export type ToolArguments = {
  description?: string;
  path?: string;
  command?: string;
  query?: string;
  name?: string;
  plan?: string;
  code?: string;
  cwd?: string;
  content?: string;
  formatted: string;
};
export type ToolResultView = {
  text: string;
  formatted: string;
  failed: boolean;
  command?: {
    stdout?: string;
    stderr?: string;
    exitCode?: number;
    truncated: boolean;
  };
  fileContent?: string;
};
export type Event = WireEvent & {
  tool?: { args: ToolArguments; result?: ToolResultView };
};
export type StreamFrame = {
  id: number;
  turn_id: string;
  payload: {
    type: string;
    attemptId?: string;
    callId?: string;
    channel?: string;
    text?: string;
    step?: number;
    turn?: number;
    chunk?: {
      type: string;
      text?: string;
      reasoning?: string;
      id?: string;
      name?: string;
      index?: number;
      argumentsDelta?: string;
    };
    outcome?: { seq?: number };
  };
};
export type TokenUsage = {
  input_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
  output_tokens: number;
  total_tokens: number;
  calls: number;
};
export type TurnSummary = {
  turn_id: string;
  final_text: string;
  has_details: boolean;
  tool_count: number;
  failed_count: number;
  duration?: number;
  usage?: TokenUsage;
  plan_review_retry?: { seq: number };
};
export type History = {
  outbox: Outbox[];
  turns: TurnSummary[];
  questions: UserQuestion[];
  has_more: boolean;
  before_turn: string | null;
};
export type TurnTrace = {
  events: Event[];
  frames: StreamFrame[];
  questions: UserQuestion[];
  cursor: { seq: number; frame: number };
};
export type ToolHeader = {
  id: string;
  name: string;
  seq: number;
  parent?: string;
  time?: number;
  ended?: number;
  complete: boolean;
  failed: boolean;
  child_count: number;
  args: Pick<
    ToolArguments,
    "description" | "path" | "command" | "query" | "name"
  >;
};
export type TurnMetadata = {
  tools: ToolHeader[];
  messages: {
    seq: number;
    time?: number;
    step?: number;
    turn?: number;
    text: string;
    has_reasoning: boolean;
  }[];
  attempts: {
    id: string;
    step?: number;
    turn?: number;
    ended_seq?: number;
    text: string;
    has_reasoning: boolean;
  }[];
  pending_tools: { id: string; name: string }[];
  questions: UserQuestion[];
  outbox: Outbox[];
  cursor: { seq: number; frame: number };
};
export type EventHeader = { seq: number; type: string; time?: number };
export type FrameHeader = {
  id: number;
  type: string;
  chunk_type?: string;
  attempt_id?: string;
  call_id?: string;
};
export type TurnHeaders = {
  events: EventHeader[];
  frames: FrameHeader[];
  has_more: boolean;
  before: number | null;
  cursor: { seq: number; frame: number };
};
export type SettingsData = {
  deepseek_api_key: string;
  e2b_api_key: string;
  e2b_template: string;
  idle_timeout_minutes: number;
};
export type MCPServer = {
  id: string;
  name: string;
  url: string;
  enabled: boolean;
  oauth_state: string;
};
export type MCPDraft = Omit<MCPServer, "id" | "oauth_state"> & {
  id?: string;
  oauth_state?: string;
};
export type MCPResult = {
  ok: boolean;
  auth_url?: string;
  tools?: { name: string; description?: string }[];
  error?: string;
};
export type TerminalMessage = {
  data?: string;
  ready?: boolean;
  error?: string;
};

// All JSON parsing belongs to transport ingress, never component render paths.
export function decodeJSON<T>(text: string): T {
  return JSON.parse(text) as T;
}
export function history(
  id: string,
  before?: string,
  signal?: AbortSignal,
): Promise<History> {
  return api<History>(
    `/sessions/${id}/events?include_question_content=false${before ? `&before_turn=${encodeURIComponent(before)}` : ""}`,
    "GET",
    undefined,
    signal,
  );
}
export async function turnTrace(
  id: string,
  turn: string,
  signal?: AbortSignal,
  query = "",
): Promise<TurnTrace> {
  const wire = await api<Omit<TurnTrace, "events"> & { events: WireEvent[] }>(
    `/sessions/${id}/turns/${turn}/events${query ? `?${query}` : ""}`,
    "GET",
    undefined,
    signal,
  );
  return { ...wire, events: wire.events.map(decodeEvent) };
}
export async function loadPlanReview(
  id: string,
  turn: string,
  seq: number,
  signal: AbortSignal,
): Promise<string> {
  const snapshot = await turnTrace(
    id,
    turn,
    signal,
    `seq=${seq}&include_question_content=false&include_questions=false&include_outbox=false`,
  );
  const plan = snapshot.events.find((event) => event.seq === seq)?.tool?.args
    .plan;
  if (!plan) throw Error("The plan review content is unavailable.");
  return plan;
}
export type MessageImage = {
  id: string;
  name: string;
  url: string;
  attachment: {
    bytes: number;
    width: number;
    height: number;
    mediaType: string;
  };
};
export type Outbox = {
  id: string;
  turn_id: string;
  client_msg_id: string;
  content: string;
  images?: MessageImage[];
  status: string;
  error: string;
  outcome: string;
  delivered_seq?: number;
};
export type UserQuestion = {
  id: string;
  turn_id: string;
  status: "pending" | "answered" | "dismissed" | "interrupted";
  questions: {
    id: string;
    question?: string;
    detail?: string;
    header?: string;
    options?: { label: string; description?: string }[];
    intent?: { kind: string; approve: string; callId?: string };
  }[];
  answer?: { answers?: { id: string; selected: string[]; custom?: string }[] };
};
export type LibraryFile = {
  id: string;
  name: string;
  mime_type: string;
  size_bytes: number;
  version: number;
  updated_at: string;
};
export type LibraryVersion = {
  version: number;
  size_bytes: number;
  sha256: string;
  created_at: string;
};
export type LibraryDetail = {
  id: string;
  name: string;
  mime_type: string;
  size_bytes: number;
  version: number;
  created_at: string;
  updated_at: string;
  versions: LibraryVersion[];
};
export async function listLibrary(
  signal?: AbortSignal,
): Promise<LibraryFile[]> {
  const d = await api<{ files: LibraryFile[] }>(
    "/library",
    "GET",
    undefined,
    signal,
  );
  return d.files ?? [];
}
export async function getLibraryFile(
  id: string,
  options?: { includeVersions?: boolean; signal?: AbortSignal },
): Promise<LibraryDetail> {
  return api<LibraryDetail>(
    `/library/${id}?include_versions=${options?.includeVersions ?? false}`,
    "GET",
    undefined,
    options?.signal,
  );
}
export function libraryContentUrl(id: string, version?: number): string {
  return `/api/library/${id}/content${version != null ? `?version=${version}` : ""}`;
}
export const sandboxLabels: Record<string, string> = {
  not_started: "Not started",
  provisioning: "Starting / resuming",
  running: "Running",
  paused: "Paused",
  killed: "Deleted",
  error: "Error",
};
