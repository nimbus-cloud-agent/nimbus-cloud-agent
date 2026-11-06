import test from "node:test";
import assert from "node:assert/strict";
import { turnResult } from "../src/turn-result.js";

test("truncated steps are reported as failures, not completed turns", () => {
  assert.deepEqual(turnResult({ kind: "max-tokens" }), {
    outcome: "error",
    error:
      "The model reached its output token limit before the response finished, so the incomplete tool call was not executed. Retry the task or split it into smaller steps.",
  });
});

test("turn end reasons map to wire outcomes", () => {
  assert.equal(turnResult(undefined).outcome, "completed");
  assert.equal(turnResult({ kind: "completed" }).outcome, "completed");
  assert.equal(
    turnResult({ kind: "aborted", reason: { kind: "user" } }).outcome,
    "interrupted",
  );
  assert.deepEqual(
    turnResult({
      kind: "error",
      error: { message: "boom", code: "HTTP_ERROR" },
    }),
    { outcome: "error", error: '{"message":"boom","code":"HTTP_ERROR"}' },
  );
});
