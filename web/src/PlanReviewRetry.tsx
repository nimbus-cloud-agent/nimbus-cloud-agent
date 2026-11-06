import { useEffect, useRef, useState } from "react";
import { Button } from "@heroui/react";
import { FileText } from "lucide-react";
import { api } from "./api";

// Recover a historical missing-channel failure through a new, explicit user
// message. Never rewrite the old tool result or treat it as an approval.
type Props = {
  sessionId: string;
  turnId: string;
  seq: number;
  loadPlan: (signal: AbortSignal) => Promise<string>;
};

export default function PlanReviewRetry(props: Props) {
  return (
    <Retry key={`${props.sessionId}:${props.turnId}:${props.seq}`} {...props} />
  );
}

function Retry({ sessionId, seq, loadPlan }: Props) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => request.current?.abort(), []);
  async function resubmit() {
    if (request.current) return;
    const controller = new AbortController();
    request.current = controller;
    setBusy(true);
    setError("");
    try {
      const plan = await loadPlan(controller.signal);
      if (controller.signal.aborted) return;
      if (!plan.trim()) throw Error("The original plan is unavailable.");
      await api(
        `/sessions/${sessionId}/messages`,
        "POST",
        {
          client_msg_id: `plan-review-retry-${sessionId}-${seq}`,
          plan: true,
          message: `The plan review channel is available again. Resubmit the existing plan below exactly as written by calling exit_plan_mode, then wait for my approval. Do not repeat research, run commands, read files, or begin implementation.\n\n${plan}`,
        },
        controller.signal,
      );
    } catch (e) {
      if (!controller.signal.aborted) {
        setError(String(e));
        setBusy(false);
      }
    } finally {
      if (request.current === controller) request.current = null;
    }
  }
  return (
    <div className="plan-review-retry">
      <p>
        This plan could not be submitted because the review channel was
        disconnected. The channel is available again, so you can resubmit the
        existing plan.
      </p>
      <Button
        size="sm"
        variant="secondary"
        isDisabled={busy}
        onPress={resubmit}
      >
        <FileText size={15} />
        {busy ? "Resubmitting…" : "Resubmit for review"}
      </Button>
      {error && <p role="alert">{error}</p>}
    </div>
  );
}
