import {
  Cloud,
  Check,
  AlertCircle,
  LoaderCircle,
  BrainCircuit,
} from "lucide-react";
import { Button } from "@heroui/react";
import Rich from "./Rich";
import PlanReview from "./PlanReview";
import PlanReviewRetry from "./PlanReviewRetry";
import { Fold } from "./components";
import {
  api,
  type Event,
  type Outbox,
  type UserQuestion,
  type StreamFrame,
} from "./api";
import ImageGallery from "./ImageGallery";
import { ToolCall, ExecutionProcess } from "./ToolCalls";
import {
  buildToolTree,
  flattenTools,
  contentText as text,
} from "./tool-events";

export default function Conversation({
  sessionId,
  events,
  outbox,
  frames,
  questions,
  retryError,
}: {
  sessionId: string;
  events: Event[];
  outbox: Outbox[];
  frames: StreamFrame[];
  questions: UserQuestion[];
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
          if (p.chunk?.type === "text-delta") st.text += p.chunk.text ?? "";
          if (p.chunk?.type === "reasoning-delta")
            st.reasoning += p.chunk.text ?? p.chunk.reasoning ?? "";
          if (p.type === "end") st.ended = p.outcome?.seq;
        }
        const calls = rows.filter((e) => e.type === "tool/call");
        const results = rows.filter((e) => e.type === "tool/result");
        const assistants = rows.filter((e) => e.type === "assistant/message");
        const failedReview = [...rows]
          .reverse()
          .find(
            (e) =>
              e.type === "tool/ptc-dispatch" &&
              e.payload.data.name === "exit_plan_mode" &&
              e.payload.data.isError &&
              typeof e.tool?.args.plan === "string" &&
              text(e.payload.data.content).includes(
                "no user-questions channel is available",
              ),
          );
        const unresolved = [...streams.values()].filter(
          (st) =>
            !assistants.some(
              (e) =>
                e.seq === st.ended ||
                (e.payload.data.step === st.step &&
                  e.payload.data.turn === st.turn),
            ),
        );
        const tools = buildToolTree(
          rows,
          frames.filter((f) => f.turn_id === o.turn_id),
        );
        const allTools = flattenTools(tools);
        const bySeq = new Map(tools.map((tool) => [tool.seq, tool]));
        const lastToolSeq = Math.max(-1, ...allTools.map((t) => t.seq));
        const finalMessage = [...assistants]
          .reverse()
          .find(
            (e) => e.seq > lastToolSeq && text(e.payload.data.message?.content),
          );
        const pendingReviews = questions.filter(
          (q) => q.turn_id === o.turn_id && q.status === "pending",
        );
        const waiting = new Set(
          pendingReviews.flatMap((q) =>
            q.questions
              .map((item) => item.intent?.callId)
              .filter((id): id is string => !!id),
          ),
        );
        const turnEnd = rows.find((e) => e.type === "turn/end");
        const started = events.find(
          (e) =>
            e.type === "turn/start" &&
            e.payload.data.turn === turnEnd?.payload.data.turn,
        );
        const duration =
          started && turnEnd
            ? ((turnEnd.payload.time ?? 0) - (started.payload.time ?? 0)) / 1000
            : undefined;
        const processContent = (
          <>
            {rows
              .filter((e) => e.type === "assistant/message" || bySeq.has(e.seq))
              .map((e) => {
                const tool = bySeq.get(e.seq);
                if (tool)
                  return (
                    <ToolCall
                      key={tool.id}
                      node={tool}
                      settled={!!o.outcome}
                      waiting={waiting}
                    />
                  );
                const content = e.payload.data.message?.content;
                const reasoning = Array.isArray(content)
                  ? content
                      .filter((b) => b.type === "reasoning")
                      .map((b) => b.text ?? b.reasoning)
                      .join("")
                  : "";
                return (
                  <div key={e.seq}>
                    {reasoning && (
                      <Fold
                        className="reasoning-fold"
                        title={
                          <span className="reasoning-label">
                            <BrainCircuit size={15} />
                            思考过程
                          </span>
                        }
                      >
                        <Rich>{reasoning}</Rich>
                      </Fold>
                    )}
                    {e !== finalMessage && text(content) && (
                      <Rich>{text(content)}</Rich>
                    )}
                  </div>
                );
              })}
            {unresolved.map(
              (st, j) =>
                st.reasoning && (
                  <Fold
                    key={j}
                    className="reasoning-fold"
                    title={
                      <span className="reasoning-label">
                        <BrainCircuit size={15} />
                        思考过程
                      </span>
                    }
                  >
                    <Rich>{st.reasoning}</Rich>
                  </Fold>
                ),
            )}
          </>
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
              {tools.length > 0 ? (
                <ExecutionProcess
                  tools={allTools}
                  outcome={o.outcome}
                  duration={duration}
                  waiting={pendingReviews.length > 0}
                >
                  {processContent}
                </ExecutionProcess>
              ) : (
                processContent
              )}
              {finalMessage && (
                <Rich>{text(finalMessage.payload.data.message?.content)}</Rich>
              )}
              {unresolved.map(
                (st, j) => st.text && <Rich key={j}>{st.text}</Rich>,
              )}
              {questions
                .filter((q) => q.turn_id === o.turn_id)
                .map((q) => (
                  <PlanReview key={q.id} sessionId={sessionId} review={q} />
                ))}
              {failedReview && o.outcome && i === outbox.length - 1 && (
                <PlanReviewRetry
                  sessionId={sessionId}
                  seq={failedReview.seq}
                  plan={failedReview.tool?.args.plan ?? ""}
                />
              )}
              {!o.outcome && o.status !== "failed" && (
                <div className="activity">
                  <span className="pulse" />
                  {questions.some(
                    (q) => q.turn_id === o.turn_id && q.status === "pending",
                  )
                    ? "等待你确认计划"
                    : calls.length > results.length
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
