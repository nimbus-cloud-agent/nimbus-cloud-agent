import type { TurnEndReason } from "@deepseek-ai/dsh-session";

export interface TurnResult {
  outcome: "completed" | "interrupted" | "error";
  error: string;
}

/**
 * Harness drops every tool-call block from a step that reached its output-token
 * ceiling, so a `max-tokens` turn can end having executed nothing. Reporting it
 * as completed makes an empty turn look like finished work; surface it instead.
 */
export function turnResult(reason: TurnEndReason | undefined): TurnResult {
  switch (reason?.kind) {
    case "error":
      return { outcome: "error", error: JSON.stringify(reason.error) };
    case "max-tokens":
      return {
        outcome: "error",
        error:
          "The model reached its output token limit before the response finished, so the incomplete tool call was not executed. Retry the task or split it into smaller steps.",
      };
    case "aborted":
      return { outcome: "interrupted", error: "" };
    default:
      return { outcome: "completed", error: "" };
  }
}
