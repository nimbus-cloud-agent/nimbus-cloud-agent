import { useState } from "react";
import { Button } from "@heroui/react";
import { FileText } from "lucide-react";
import { api } from "./api";

// Recover a historical missing-channel failure through a new, explicit user
// message. Never rewrite the old tool result or treat it as an approval.
export default function PlanReviewRetry({
  sessionId,
  seq,
  plan,
}: {
  sessionId: string;
  seq: number;
  plan: string;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function resubmit() {
    setBusy(true);
    setError("");
    try {
      await api(`/sessions/${sessionId}/messages`, "POST", {
        client_msg_id: `plan-review-retry-${sessionId}-${seq}`,
        plan: true,
        message: `The plan review channel is available again. Resubmit the existing plan below exactly as written by calling exit_plan_mode, then wait for my approval. Do not repeat research, run commands, read files, or begin implementation.\n\n${plan}`,
      });
    } catch (e) {
      setError(String(e));
      setBusy(false);
    }
  }
  return (
    <div className="plan-review-retry">
      <p>
        This plan could not be submitted because the review channel was disconnected. The channel is available again, so you can resubmit the existing plan.
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
