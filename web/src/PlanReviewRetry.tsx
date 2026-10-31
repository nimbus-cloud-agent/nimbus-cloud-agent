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
        message: `计划评审通道已修复。请把下面已有的计划原样通过 exit_plan_mode 重新提交评审，等待我确认。无需重复调研、运行命令或读取文件，也不要开始实施。\n\n${plan}`,
      });
    } catch (e) {
      setError(String(e));
      setBusy(false);
    }
  }
  return (
    <div className="plan-review-retry">
      <p>
        这份计划因评审通道未连接而提交失败。通道现已修复，可以重新提交已有计划。
      </p>
      <Button
        size="sm"
        variant="secondary"
        isDisabled={busy}
        onPress={resubmit}
      >
        <FileText size={15} />
        {busy ? "正在重新提交…" : "重新提交计划评审"}
      </Button>
      {error && <p role="alert">{error}</p>}
    </div>
  );
}
