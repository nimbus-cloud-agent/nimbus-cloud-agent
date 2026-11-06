import { useState } from "react";
import { Button } from "@heroui/react";
import { Check, FileText, MessageSquare, Pause } from "lucide-react";
import { api, type UserQuestion } from "./api";
import Rich from "./Rich";

export default function PlanReview({
  sessionId,
  review,
  onOpenLibraryFile,
}: {
  sessionId: string;
  review: UserQuestion;
  onOpenLibraryFile?: (id: string) => void;
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
    ? "Review and approve this plan"
    : approved
      ? "Plan approved"
      : review.status === "answered"
        ? "Changes requested"
        : review.status === "dismissed"
          ? "Not approved · Plan mode is still active"
          : "Review interrupted · Send a message to submit a new plan";
  return (
    <section
      className={"plan-review" + (pending ? " is-pending" : "")}
      aria-label="Plan review"
    >
      <header className="plan-review-header">
        <FileText size={18} />
        <strong>{status}</strong>
      </header>
      <details open={pending} className="plan-review-detail">
        <summary>Plan</summary>
        <div className="plan-review-document">
          {review.questions.map((q) => (
            <Rich key={q.id} onOpenLibraryFile={onOpenLibraryFile}>{q.detail ?? q.question}</Rich>
          ))}
        </div>
      </details>
      {review.answer?.answers?.map(
        (a) =>
          a.custom && (
            <p key={a.id} className="plan-review-feedback">
              Feedback: {a.custom}
            </p>
          ),
      )}
      {pending && (
        <div className="plan-review-controls">
          <p>Approving exits Plan mode and lets the agent continue with the plan.</p>
          {editing && (
            <label className="plan-review-edit">
              Feedback (optional)
              <textarea
                autoFocus
                value={feedback}
                onChange={(e) => setFeedback(e.target.value)}
                maxLength={6000}
                placeholder="Describe what you would like changed…"
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
                Approve and continue
              </Button>
            )}
            <Button
              size="sm"
              variant="secondary"
              isDisabled={busy || sent}
              onPress={() => (editing ? answer("revise") : setEditing(true))}
            >
              <MessageSquare size={15} />
              {editing ? "Submit feedback" : "Request changes"}
            </Button>
            {editing && (
              <Button
                size="sm"
                variant="ghost"
                isDisabled={busy || sent}
                onPress={() => setEditing(false)}
              >
                Cancel
              </Button>
            )}
            <Button
              size="sm"
              variant="ghost"
              isDisabled={busy || sent}
              onPress={() => answer("dismiss")}
            >
              <Pause size={14} />
              Decide later
            </Button>
          </div>
          {sent && <p role="status">Submitted. Waiting for the agent…</p>}
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
