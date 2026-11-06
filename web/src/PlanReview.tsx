import { useEffect, useRef, useState } from "react";
import { Button } from "@heroui/react";
import { Check, FileText, MessageSquare, Pause } from "lucide-react";
import { api, type UserQuestion } from "./api";
import Rich from "./Rich";

type Props = {
  sessionId: string;
  review: UserQuestion;
  onOpenLibraryFile?: (id: string) => void;
};

export default function PlanReview(props: Props) {
  return <Review key={`${props.sessionId}:${props.review.id}`} {...props} />;
}

function Review({ sessionId, review, onOpenLibraryFile }: Props) {
  const [feedback, setFeedback] = useState("");
  const [editing, setEditing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [sent, setSent] = useState(false);
  const [error, setError] = useState("");
  const pending = review.status === "pending";
  const [open, setOpen] = useState(pending);
  const [document, setDocument] = useState<UserQuestion>();
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState("");
  const [revision, setRevision] = useState(0);
  const request = useRef<AbortController | null>(null);
  useEffect(() => {
    setOpen(pending);
  }, [pending]);
  useEffect(() => {
    if (!open || document) return;
    const controller = new AbortController();
    request.current = controller;
    setLoading(true);
    setLoadError("");
    api<UserQuestion>(
      `/sessions/${sessionId}/questions/${review.id}`,
      "GET",
      undefined,
      controller.signal,
    )
      .then((data) => {
        if (!controller.signal.aborted) setDocument(data);
      })
      .catch((e) => {
        if (!controller.signal.aborted) setLoadError(String(e));
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => {
      controller.abort();
      if (request.current === controller) request.current = null;
    };
  }, [sessionId, review.id, open, document, revision]);
  const ready =
    !!document?.questions.length &&
    document.questions.length === review.questions.length &&
    document.questions.every(
      (q) =>
        review.questions.some((metadata) => metadata.id === q.id) &&
        (q.detail !== undefined || q.question !== undefined) &&
        q.options?.some((option) => option.label === q.intent?.approve),
    );
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
    if (!ready || !document || busy || sent) return;
    setBusy(true);
    setError("");
    try {
      await api(
        `/sessions/${sessionId}/questions/${review.id}/answer`,
        "POST",
        action === "dismiss"
          ? { dismissed: true }
          : {
              answers: document.questions.map((q) => ({
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
      <details
        open={open}
        className="plan-review-detail"
        onToggle={(event) => {
          const next = event.currentTarget.open;
          if (!next) request.current?.abort();
          setOpen(next);
        }}
      >
        <summary
          onClick={(event) => {
            event.preventDefault();
            if (open) request.current?.abort();
            setOpen(!open);
          }}
        >
          Plan
        </summary>
        {open && (
          <div className="plan-review-document">
            {document ? (
              document.questions.map((q) => (
                <Rich key={q.id} onOpenLibraryFile={onOpenLibraryFile}>
                  {q.detail ?? q.question ?? ""}
                </Rich>
              ))
            ) : loadError ? (
              <div role="alert">
                <p>{loadError}</p>
                <Button
                  size="sm"
                  variant="secondary"
                  onPress={() => setRevision((value) => value + 1)}
                >
                  Retry plan document
                </Button>
              </div>
            ) : (
              <p role="status">
                {loading ? "Loading plan…" : "Plan document is not loaded."}
              </p>
            )}
          </div>
        )}
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
          <p>
            Approving exits Plan mode and lets the agent continue with the plan.
          </p>
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
                isDisabled={busy || sent || !ready}
                onPress={() => answer("approve")}
              >
                <Check size={15} />
                Approve and continue
              </Button>
            )}
            <Button
              size="sm"
              variant="secondary"
              isDisabled={busy || sent || !ready}
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
              isDisabled={busy || sent || !ready}
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
