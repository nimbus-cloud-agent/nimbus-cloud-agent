import { useCallback, useLayoutEffect } from "react";
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
  loadPlanReview,
  type TurnSummary,
  type Outbox,
  type UserQuestion,
} from "./api";
import {
  useTurnTrace,
  useTurnMetadata,
  useTurnDisclosure,
  TraceStatus,
} from "./TurnTrace";
import ImageGallery from "./ImageGallery";
import { ToolCall, ExecutionProcess, PendingToolCall } from "./ToolCalls";

type ConversationProps = {
  sessionId: string;
  outbox: Outbox[];
  turns: TurnSummary[];
  questions: UserQuestion[];
  retryError: (s: string) => void;
  onOpenLibraryFile?: (id: string) => void;
  onOutput: () => void;
};

export default function Conversation(props: ConversationProps) {
  return (
    <>
      {props.outbox.map((o, i) => (
        <ConversationTurn
          key={o.turn_id}
          {...props}
          o={o}
          summary={props.turns.find((t) => t.turn_id === o.turn_id)}
          last={i === props.outbox.length - 1}
        />
      ))}
    </>
  );
}

function Reasoning({
  sessionId,
  turnId,
  seq,
  attemptId,
  running,
  onOpenLibraryFile,
}: {
  sessionId: string;
  turnId: string;
  seq?: number;
  attemptId?: string;
  running: boolean;
  onOpenLibraryFile?: (id: string) => void;
}) {
  const [open, setOpen] = useTurnDisclosure(
    `reasoning:${turnId}:${seq ?? attemptId}`,
  );
  const query =
    seq != null
      ? `seq=${seq}`
      : `attempt_id=${encodeURIComponent(attemptId!)}&chunk_types=reasoning-delta`;
  const detail = useTurnTrace(sessionId, turnId, open, running, query);
  const content = detail.trace?.events[0]?.payload.data.message?.content;
  const reasoning =
    seq != null
      ? Array.isArray(content)
        ? content
            .filter((block) => block.type === "reasoning")
            .map((block) => block.text ?? block.reasoning ?? "")
            .join("")
        : ""
      : (detail.trace?.frames ?? [])
          .map(
            (frame) =>
              frame.payload.chunk?.text ?? frame.payload.chunk?.reasoning ?? "",
          )
          .join("");
  return (
    <Fold
      className="reasoning-fold"
      open={open}
      onOpenChange={setOpen}
      title={
        <span className="reasoning-label">
          <BrainCircuit size={15} />
          Reasoning
        </span>
      }
    >
      <TraceStatus {...detail} />
      <Rich onOpenLibraryFile={onOpenLibraryFile}>{reasoning}</Rich>
    </Fold>
  );
}

function ConversationTurn({
  sessionId,
  o,
  summary,
  questions,
  retryError,
  onOpenLibraryFile,
  last,
  onOutput,
}: ConversationProps & { o: Outbox; summary?: TurnSummary; last: boolean }) {
  const running = !o.outcome && o.status !== "failed";
  const [open, setOpen] = useTurnDisclosure(`process:${o.turn_id}`, running);
  const detail = useTurnMetadata(sessionId, o.turn_id, open, running);
  const metadata = detail.metadata;
  useLayoutEffect(() => {
    onOutput();
  }, [metadata, onOutput]);
  const retrySeq = summary?.plan_review_retry?.seq;
  const loadRetryPlan = useCallback(
    (signal: AbortSignal) =>
      loadPlanReview(sessionId, o.turn_id, retrySeq!, signal),
    [sessionId, o.turn_id, retrySeq],
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
  const tools = metadata?.tools ?? [];
  const messages = metadata?.messages ?? [];
  const lastToolSeq = Math.max(-1, ...tools.map((tool) => tool.seq));
  const finalMessage = [...messages]
    .reverse()
    .find((message) => message.seq > lastToolSeq && !!message.text);
  const finalText = summary?.final_text || finalMessage?.text || "";
  const processRows = [
    ...tools.map((tool) => ({ seq: tool.seq, tool })),
    ...messages.map((message) => ({ seq: message.seq, message })),
  ].sort((a, b) => a.seq - b.seq);
  return (
    <section className="turn" data-turn-id={o.turn_id}>
      <div className="user-message">
        <span className="message-author">You</span>
        <ImageGallery images={o.images ?? []} />
        {o.content && <p>{o.content}</p>}
        <div className="delivery">
          {["queued", "starting"].includes(o.status) && (
            <>
              <LoaderCircle size={12} className="spin" />
              {o.status === "queued" ? "Queued" : "Starting agent"}
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
                Retry
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
          <span>Cloud agent</span>
        </div>
        {(summary?.has_details || running || !!metadata) && (
          <ExecutionProcess
            count={summary?.tool_count ?? tools.length}
            failures={
              summary?.failed_count ??
              tools.filter((tool) => tool.failed).length
            }
            outcome={o.outcome || (o.status === "failed" ? "error" : "")}
            duration={summary?.duration}
            waiting={pendingReviews.length > 0}
            open={open}
            onOpenChange={setOpen}
          >
            <TraceStatus {...detail} />
            {processRows.map((row) =>
              "tool" in row ? (
                <ToolCall
                  key={`tool-${row.tool.id}`}
                  header={row.tool}
                  settled={!running}
                  waiting={waiting}
                  sessionId={sessionId}
                  turnId={o.turn_id}
                />
              ) : (
                <div key={`message-${row.message.seq}`}>
                  {row.message.has_reasoning && (
                    <Reasoning
                      sessionId={sessionId}
                      turnId={o.turn_id}
                      seq={row.message.seq}
                      running={false}
                      onOpenLibraryFile={onOpenLibraryFile}
                    />
                  )}
                  {row.message !== finalMessage && row.message.text && (
                    <Rich onOpenLibraryFile={onOpenLibraryFile}>
                      {row.message.text}
                    </Rich>
                  )}
                </div>
              ),
            )}
            {metadata?.attempts.map(
              (attempt) =>
                attempt.has_reasoning && (
                  <Reasoning
                    key={attempt.id}
                    sessionId={sessionId}
                    turnId={o.turn_id}
                    attemptId={attempt.id}
                    running={running && attempt.ended_seq == null}
                    onOpenLibraryFile={onOpenLibraryFile}
                  />
                ),
            )}
            {running &&
              metadata?.pending_tools.map((call) => (
                <PendingToolCall
                  key={call.id}
                  sessionId={sessionId}
                  turnId={o.turn_id}
                  id={call.id}
                  name={call.name}
                  running={running}
                />
              ))}
          </ExecutionProcess>
        )}
        {finalText && (
          <Rich onOpenLibraryFile={onOpenLibraryFile}>{finalText}</Rich>
        )}
        {!summary?.final_text &&
          metadata?.attempts.map(
            (attempt) =>
              attempt.text && (
                <Rich key={attempt.id} onOpenLibraryFile={onOpenLibraryFile}>
                  {attempt.text}
                </Rich>
              ),
          )}
        {questions
          .filter((q) => q.turn_id === o.turn_id)
          .map((q) => (
            <PlanReview
              key={q.id}
              sessionId={sessionId}
              review={q}
              onOpenLibraryFile={onOpenLibraryFile}
            />
          ))}
        {o.outcome && last && summary?.plan_review_retry && (
          <PlanReviewRetry
            sessionId={sessionId}
            turnId={o.turn_id}
            seq={summary.plan_review_retry.seq}
            loadPlan={loadRetryPlan}
          />
        )}
        {running && (
          <div className="activity">
            <span className="pulse" />
            {pendingReviews.length
              ? "Waiting for your plan approval"
              : metadata?.pending_tools.length
                ? "Streaming tool arguments"
                : tools.some((tool) => !tool.complete)
                  ? "Running tools"
                  : "Thinking and responding"}
            <span>Output saved in real time</span>
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
                completed: "Turn completed",
                interrupted:
                  "Turn interrupted · Send a new message to continue",
                error: "Turn failed",
              }[o.outcome]
            }
            {o.error && o.status === "delivered" && <span>{o.error}</span>}
          </div>
        )}
      </div>
    </section>
  );
}
