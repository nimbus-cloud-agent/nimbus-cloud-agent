import {
  Cloud,
  TerminalSquare,
  Check,
  AlertCircle,
  LoaderCircle,
  Braces,
  BrainCircuit,
  FilePenLine,
  FileText,
  BookOpen,
  Plug,
} from "lucide-react";
import { Button } from "@heroui/react";
import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { Fold } from "./components";
import { api, type Event, type Outbox } from "./api";
import ImageGallery from "./ImageGallery";
const text = (content: any) =>
  Array.isArray(content)
    ? content
        .filter((x) => x.type === "text")
        .map((x) => x.text)
        .join("")
    : typeof content === "string"
      ? content
      : "";

function toolInfo(data: any) {
  const name = data.name ?? data.toolName ?? data.call?.name ?? "tool";
  let args: any = {};
  try {
    args =
      typeof data.arguments === "string"
        ? JSON.parse(data.arguments)
        : (data.arguments ?? {});
  } catch {}
  const labels: Record<string, [string, typeof Cloud]> = {
    sandbox_start: ["启动云端沙箱", Cloud],
    sandbox_status: ["查看沙箱状态", Cloud],
    sandbox_exec: ["运行命令", TerminalSquare],
    sandbox_write_file: ["写入文件", FilePenLine],
    sandbox_read_file: ["读取文件", FileText],
    skill_read: ["读取技能", BookOpen],
    run_code: ["运行代码", Braces],
  };
  const [label, Icon] = labels[name] ?? [
    name.startsWith("mcp__") ? "调用外部工具" : name,
    Plug,
  ];
  return {
    label,
    Icon,
    detail: args.path ?? args.command ?? args.description ?? args.name ?? "",
    name,
  };
}
export function Rich({ children }: { children: string }) {
  return (
    <div className="markdown">
      <Markdown remarkPlugins={[remarkGfm]}>{children}</Markdown>
    </div>
  );
}
export default function Conversation({
  sessionId,
  events,
  outbox,
  frames,
  retryError,
}: {
  sessionId: string;
  events: Event[];
  outbox: Outbox[];
  frames: any[];
  retryError: (s: string) => void;
}) {
  return (
    <>
      {outbox.map((o, i) => {
        const next =
          outbox.slice(i + 1).find((n) => n.delivered_seq != null)
            ?.delivered_seq ?? Infinity;
        const rows = events.filter(
          (e) => e.seq >= (o.delivered_seq ?? Infinity) && e.seq < next,
        );
        const streams = new Map<
          string,
          {
            text: string;
            reasoning: string;
            step?: number;
            turn?: number;
            ended?: number;
          }
        >();
        for (const f of frames.filter((f) => f.turn_id === o.turn_id)) {
          const p = f.payload;
          if (!p.attemptId) continue;
          let st = streams.get(p.attemptId);
          if (!st) {
            st = { text: "", reasoning: "" };
            streams.set(p.attemptId, st);
          }
          if (p.type === "start") {
            st.step = p.step;
            st.turn = p.turn;
          }
          if (p.chunk?.type === "text-delta") st.text += p.chunk.text;
          if (p.chunk?.type === "reasoning-delta")
            st.reasoning += p.chunk.text ?? p.chunk.reasoning ?? "";
          if (p.type === "end") st.ended = p.outcome?.seq;
        }
        const calls = rows.filter((e) => e.type === "tool/call");
        const results = rows.filter((e) => e.type === "tool/result");
        const assistants = rows.filter((e) => e.type === "assistant/message");
        const unresolved = [...streams.values()].filter(
          (st) =>
            !assistants.some(
              (e) =>
                e.seq === st.ended ||
                (e.payload.data.step === st.step &&
                  e.payload.data.turn === st.turn),
            ),
        );
        return (
          <section className="turn" key={o.id}>
            <div className="user-message">
              <span className="message-author">你</span>
              <ImageGallery images={o.images ?? []} />
              {o.content && <p>{o.content}</p>}
              <div className="delivery">
                {["queued", "starting"].includes(o.status) && (
                  <>
                    <LoaderCircle size={12} className="spin" />
                    {o.status === "queued" ? "排队中" : "正在启动 Agent"}
                  </>
                )}
                {o.status === "failed" && (
                  <>
                    <AlertCircle size={13} />
                    <span>{o.error}</span>
                    <Button
                      size="sm"
                      variant="ghost"
                      onPress={() =>
                        api(
                          `/sessions/${sessionId}/messages/${o.id}/retry`,
                          "POST",
                        ).catch((e) => retryError(e.message))
                      }
                    >
                      重发
                    </Button>
                  </>
                )}
              </div>
            </div>
            <div className="agent-message">
              <div className="agent-label">
                <span className="agent-avatar">
                  <Cloud size={18} />
                </span>
                <strong>Nimbus</strong>
                <span>云端 Agent</span>
              </div>
              {rows
                .filter((e) =>
                  [
                    "assistant/message",
                    "tool/call",
                    "tool/ptc-dispatch",
                  ].includes(e.type),
                )
                .map((e) => {
                  const d = e.payload.data;
                  if (e.type === "assistant/message") {
                    const m = d.message;
                    const reasoning = m.content
                      ?.filter((b: any) => b.type === "reasoning")
                      .map((b: any) => b.text ?? b.reasoning)
                      .join("");
                    return (
                      <div key={e.seq}>
                        {reasoning && (
                          <Fold
                            className="reasoning-fold"
                            title={
                              <span className="reasoning-label">
                                <BrainCircuit size={17} />
                                思考过程
                              </span>
                            }
                          >
                            <Rich>{reasoning}</Rich>
                          </Fold>
                        )}
                        {text(m.content) && <Rich>{text(m.content)}</Rich>}
                      </div>
                    );
                  }
                  if (e.type === "tool/ptc-dispatch")
                    return (
                      <Fold
                        className="reasoning-fold"
                        key={e.seq}
                        title={
                          <span className="tool-title">
                            <Braces size={17} />
                            查看代码中的工具调用
                          </span>
                        }
                      >
                        <pre>{JSON.stringify(d, null, 2)}</pre>
                      </Fold>
                    );
                  const result = results.find(
                    (r) =>
                      (r.payload.data.callId ??
                        r.payload.data.message?.toolCallId ??
                        r.payload.data.id) === (d.callId ?? d.id),
                  );
                  const { label, Icon, detail } = toolInfo(d);
                  const failed =
                    result?.payload.data.isError ||
                    result?.payload.data.message?.isError;
                  return (
                    <div className="tool-card" key={e.seq}>
                      <Fold
                        title={
                          <span className="tool-title">
                            <span className="tool-icon">
                              <Icon size={18} />
                            </span>
                            <span className="tool-copy">
                              <strong>{label}</strong>
                              {detail && (
                                <code title={String(detail)}>
                                  {String(detail)}
                                </code>
                              )}
                            </span>
                            {failed ? (
                              <span className="tool-status failed">
                                <AlertCircle size={15} />
                                失败
                              </span>
                            ) : result ? (
                              <span className="tool-status">
                                <Check size={15} />
                                完成
                              </span>
                            ) : (
                              <LoaderCircle size={13} className="spin" />
                            )}
                          </span>
                        }
                      >
                        <pre>{JSON.stringify(d, null, 2)}</pre>
                        {!result &&
                          frames.some(
                            (f) =>
                              f.turn_id === o.turn_id &&
                              f.payload.type === "tool-output" &&
                              f.payload.callId === d.callId,
                          ) && (
                            <pre>
                              {frames
                                .filter(
                                  (f) =>
                                    f.turn_id === o.turn_id &&
                                    f.payload.type === "tool-output" &&
                                    f.payload.callId === d.callId,
                                )
                                .map((f) => f.payload.text)
                                .join("")}
                            </pre>
                          )}
                        {result && (
                          <pre>
                            {text(
                              result.payload.data.message?.content ??
                                result.payload.data.content ??
                                result.payload.data.result?.content,
                            ) || JSON.stringify(result.payload.data, null, 2)}
                          </pre>
                        )}
                      </Fold>
                    </div>
                  );
                })}
              {unresolved.map((st, j) => (
                <div key={j}>
                  {st.reasoning && (
                    <Fold
                      className="reasoning-fold"
                      title={
                        <span className="reasoning-label">
                          <BrainCircuit size={17} />
                          思考过程
                        </span>
                      }
                    >
                      <Rich>{st.reasoning}</Rich>
                    </Fold>
                  )}
                  {st.text && <Rich>{st.text}</Rich>}
                </div>
              ))}
              {!o.outcome && o.status !== "failed" && (
                <div className="activity">
                  <span className="pulse" />
                  {calls.length > results.length
                    ? "正在执行工具"
                    : "正在思考与输出"}
                  <span>输出实时保存</span>
                </div>
              )}
              {o.outcome && (
                <div className={"turn-outcome " + o.outcome}>
                  {o.outcome === "completed" ? (
                    <Check size={13} />
                  ) : (
                    <AlertCircle size={13} />
                  )}
                  {
                    {
                      completed: "本轮已完成",
                      interrupted: "本轮已中断 · 发送新消息即可继续",
                      error: "本轮出错",
                    }[o.outcome]
                  }
                  {o.error && o.status === "delivered" && (
                    <span>{o.error}</span>
                  )}
                </div>
              )}
            </div>
          </section>
        );
      })}
    </>
  );
}
