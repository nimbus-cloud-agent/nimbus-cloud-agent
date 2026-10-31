import type {
  ToolCall,
  TurnRequest,
  TurnResult,
  ReviewAnswer,
} from "./generated/types.js";
import type { TurnEvent, ReviewRequest, ExternalValue } from "./ports.js";

export type WorkerInvocation =
  | { method: "event"; data: TurnEvent }
  | { method: "question"; data: ReviewRequest }
  | { method: "tool"; data: ToolCall };
export type WorkerReply =
  | { method: "event" }
  | { method: "question"; answer: ReviewAnswer }
  | { method: "tool"; value: ExternalValue };
export type WorkerCall = (
  call: WorkerInvocation,
  signal?: AbortSignal,
) => Promise<WorkerReply>;
export type WorkerRequest =
  | { type: "run"; job: number; input: TurnRequest }
  | { type: "cancel"; job: number }
  | { type: "reply"; id: number; result: WorkerReply; error?: never }
  | { type: "reply"; id: number; error: string; result?: never };
export type WorkerResponse =
  | { job: number; result: TurnResult; error?: never }
  | { job: number; error: string; result?: never };
export type WorkerMessage =
  | WorkerResponse
  | (WorkerInvocation & { job: number; id: number })
  | { type: "cancel-callback"; job: number; id: number };
