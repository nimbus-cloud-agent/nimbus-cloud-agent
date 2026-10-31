import { decodeEvent } from "./event-codec";
export async function api<T = unknown>(
  path: string,
  method = "GET",
  body?: unknown,
): Promise<T> {
  const r = await fetch("/api" + path, {
    method,
    headers: body ? { "Content-Type": "application/json" } : {},
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!r.ok) {
    let error = "请求失败";
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
  mode: string;
};
export type Session = {
  id: string;
  title: string;
  agent_state: string;
  options: Options;
  mode_locked: boolean;
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
    chunk?: { type: string; text?: string; reasoning?: string };
    outcome?: { seq?: number };
  };
};
export type History = {
  events: Event[];
  frames: StreamFrame[];
  outbox: Outbox[];
  questions: UserQuestion[];
  has_more: boolean;
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
export async function history(id: string, after: number): Promise<History> {
  const wire = await api<Omit<History, "events"> & { events: WireEvent[] }>(
    `/sessions/${id}/events?after_seq=${after}`,
  );
  return { ...wire, events: wire.events.map(decodeEvent) };
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
    question: string;
    detail?: string;
    header?: string;
    options?: { label: string; description?: string }[];
    intent?: { kind: string; approve: string; callId?: string };
  }[];
  answer?: { answers?: { id: string; selected: string[]; custom?: string }[] };
};
export const sandboxLabels: Record<string, string> = {
  not_started: "未启动",
  provisioning: "创建 / 唤醒中",
  running: "运行中",
  paused: "已暂停",
  killed: "已销毁",
  error: "异常",
};
