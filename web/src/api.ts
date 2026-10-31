export async function api<T = any>(
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
  return r.status === 204 ? (undefined as T) : await r.json();
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
export type Event = { seq: number; type: string; payload: any };
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
