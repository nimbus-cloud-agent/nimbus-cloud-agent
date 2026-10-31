// HTTP/SSE anti-corruption boundary for extensible Harness events. Native tool
// arguments and text-encoded results are decoded once, before React state.
import type {
  Content,
  Event,
  NativeData,
  ToolArguments,
  ToolResultView,
  WireEvent,
} from "./api";

export function contentText(content?: Content): string {
  return typeof content === "string"
    ? content
    : (content ?? [])
        .filter((b) => b.type === "text")
        .map((b) => b.text ?? "")
        .join("\n");
}
function parsed(value: unknown): unknown {
  if (typeof value !== "string") return value;
  try {
    return JSON.parse(value) as unknown;
  } catch {
    return value;
  }
}
function object(value: unknown): Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}
function text(value: unknown): string | undefined {
  return typeof value === "string" ? value : undefined;
}
function argumentsView(value: unknown): ToolArguments {
  const raw = parsed(value) ?? {},
    v = object(raw);
  return {
    description: text(v.description),
    path: text(v.path),
    command: text(v.command),
    query: text(v.query),
    name: text(v.name),
    plan: text(v.plan),
    code: text(v.code),
    cwd: text(v.cwd),
    content: text(v.content),
    formatted: JSON.stringify(raw, null, 2),
  };
}
function resultView(d: NativeData): ToolResultView {
  const content = d.message?.content ?? d.content ?? d.result?.content;
  const raw =
    content != null
      ? contentText(content)
      : typeof d.error === "string"
        ? d.error
        : (d.error?.message ?? "");
  const value = parsed(raw),
    v = object(value);
  const code = typeof v.exitCode === "number" ? v.exitCode : undefined;
  const result: ToolResultView = {
    text: raw,
    formatted:
      value !== null && typeof value === "object"
        ? JSON.stringify(value, null, 2)
        : raw,
    failed: !!(
      d.isError ||
      d.message?.isError ||
      d.error ||
      v.isError ||
      (code !== undefined && code !== 0)
    ),
  };
  if ("stdout" in v || "stderr" in v)
    result.command = {
      stdout: text(v.stdout),
      stderr: text(v.stderr),
      exitCode: code,
      truncated: v.truncated === true,
    };
  if (typeof v.content === "string") result.fileContent = v.content;
  return result;
}
export function decodeEvent(wire: WireEvent): Event {
  if (!wire.type.startsWith("tool/")) return wire;
  return {
    ...wire,
    tool: {
      args: argumentsView(wire.payload.data.arguments),
      result:
        wire.type === "tool/result" || wire.type === "tool/ptc-dispatch"
          ? resultView(wire.payload.data)
          : undefined,
    },
  };
}
