import type { SessionHeader, SessionEvent } from "@deepseek-ai/dsh-session";
import type { AssistantStreamFrame } from "@deepseek-ai/dsh-agent";
import type { AskUserQuestionItem } from "@deepseek-ai/dsh-user-questions";
import type {
  ToolCall,
  TurnRequest,
  ExternalResult,
  ReviewAnswer,
} from "./generated/types.js";

// Keep the native SDK discriminants through the worker pool. Only the stdio
// adapter erases native payloads into the protocol's opaque extension fields.
export type TurnEvent =
  | { event: "header"; data: SessionHeader }
  | { event: "record"; data: SessionEvent }
  | { event: "frame"; data: AssistantStreamFrame }
  | { event: "ready" | "flushing"; data: Record<string, never> }
  | { event: "done"; data: { seq: number; outcome: string; error: string } };
export interface ReviewRequest {
  questions: AskUserQuestionItem[];
}
export interface TurnHost {
  signal: AbortSignal;
  emit(event: TurnEvent): Promise<void>;
  askQuestion(request: ReviewRequest): Promise<ReviewAnswer>;
  callTool(call: ToolCall, signal?: AbortSignal): Promise<ExternalValue>;
}
export type TurnInput = TurnRequest;
// MCP/plugin tools define their own result schemas. Nimbus forwards these to
// Harness without interpreting their shape; owned results have named DTOs.
export type ExternalValue = ExternalResult["value"];
