import { useState } from "react";
import { Button } from "@heroui/react";
import { Check, FileText, MessageSquare, Pause } from "lucide-react";
import { api, type UserQuestion } from "./api";
import Rich from "./Rich";

export default function PlanReview({
  sessionId,
  review,
}: {
  sessionId: string;
  review: UserQuestion;
}) {
  const [feedback, setFeedback] = useState("");
  const [editing, setEditing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [sent, setSent] = useState(false);
  const [error, setError] = useState("");
  const pending = review.status === "pending";
  const approved =
    review.status === "answered" &&
    review.questions.every((q) => {
      const a = review.answer?.answers?.find((a) => a.id === q.id);
      return (
        a?.selected.length === 1 &&
        a.selected[0] === q.intent?.approve &&
        a.custom === undefined
      );
    });
  async function answer(action: "approve" | "revise" | "dismiss") {
    setBusy(true);
    setError("");
    try {
      await api(
        `/sessions/${sessionId}/questions/${review.id}/answer`,
        "POST",
        action === "dismiss"
          ? { dismissed: true }
          : {
              answers: review.questions.map((q) => ({
                id: q.id,
                selected: [
                  action === "approve" ? q.intent!.approve : "Keep planning",
                ],
                ...(action === "revise" && feedback.trim()
                  ? { custom: feedback.trim() }
                  : {}),
              })),
            },
      );
      setSent(true);
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  const status = pending
    ? "请确认这份计划"
    : approved
      ? "已批准计划"
      : review.status === "answered"
        ? "已退回修改"
        : review.status === "dismissed"
          ? "暂未批准 · 仍在 Plan 模式"
          : "评审已中断 · 发送消息可重新提交计划";
  return (
    <section
      className={"plan-review" + (pending ? " is-pending" : "")}
      aria-label="计划评审"
    >
      <header className="plan-review-header">
        <FileText size={18} />
        <strong>{status}</strong>
      </header>
      <details open={pending} className="plan-review-detail">
        <summary>计划内容</summary>
        <div className="plan-review-document">
          {review.questions.map((q) => (
            <Rich key={q.id}>{q.detail ?? q.question}</Rich>
          ))}
        </div>
      </details>
      {review.answer?.answers?.map(
        (a) =>
          a.custom && (
            <p key={a.id} className="plan-review-feedback">
              修改意见：{a.custom}
            </p>
          ),
      )}
      {pending && (
        <div className="plan-review-controls">
          <p>批准后退出 Plan 模式，Agent 将按计划继续执行。</p>
          {editing && (
            <label className="plan-review-edit">
              修改意见（可选）
              <textarea
                autoFocus
                value={feedback}
                onChange={(e) => setFeedback(e.target.value)}
                maxLength={6000}
                placeholder="说明需要调整的内容…"
                disabled={busy || sent}
              />
            </label>
          )}
          <div className="plan-review-actions">
            {!editing && (
              <Button
                size="sm"
                isDisabled={busy || sent}
                onPress={() => answer("approve")}
              >
                <Check size={15} />
                批准并执行
              </Button>
            )}
            <Button
              size="sm"
              variant="secondary"
              isDisabled={busy || sent}
              onPress={() => (editing ? answer("revise") : setEditing(true))}
            >
              <MessageSquare size={15} />
              {editing ? "提交修改意见" : "继续规划"}
            </Button>
            {editing && (
              <Button
                size="sm"
                variant="ghost"
                isDisabled={busy || sent}
                onPress={() => setEditing(false)}
              >
                取消修改
              </Button>
            )}
            <Button
              size="sm"
              variant="ghost"
              isDisabled={busy || sent}
              onPress={() => answer("dismiss")}
            >
              <Pause size={14} />
              暂不决定
            </Button>
          </div>
          {sent && <p role="status">已提交，等待 Agent 继续…</p>}
          {error && (
            <p className="plan-review-error" role="alert">
              {error}
            </p>
          )}
        </div>
      )}
    </section>
  );
}
