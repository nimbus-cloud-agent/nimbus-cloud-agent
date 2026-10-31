import test from "node:test";
import assert from "node:assert/strict";
import {
  buildToolTree,
  flattenTools,
  toolFailed,
  resultText,
} from "../web/src/tool-events.ts";
const event = (seq, type, data) => ({
  seq,
  type,
  payload: { time: seq * 1000, data },
});

test("native PTC start/end events and root results form one nested call tree", () => {
  const events = [
    event(1, "tool/call", {
      callId: "root",
      name: "run_code",
      arguments: '{"code":"await tools.exec({})"}',
    }),
    event(2, "tool/ptc-dispatch-start", {
      subCallId: "child",
      parentCallId: "root",
      rootCallId: "root",
      name: "sandbox_exec",
      arguments: { command: "pwd" },
    }),
    event(3, "tool/ptc-dispatch", {
      subCallId: "child",
      parentCallId: "root",
      name: "sandbox_exec",
      arguments: { command: "pwd" },
      content: [{ type: "text", text: '{"stdout":"/workspace","exitCode":0}' }],
      isError: false,
    }),
    event(4, "tool/result", {
      message: { toolCallId: "root", content: [{ type: "text", text: "ok" }] },
    }),
  ];
  const [root] = buildToolTree(events, [
    { payload: { type: "tool-output", callId: "child", text: "live\n" } },
  ]);
  assert.equal(root.name, "run_code");
  assert.equal(root.children.length, 1);
  assert.equal(root.children[0].seq, 2);
  assert.equal(root.children[0].output, "live\n");
  assert.equal(root.children[0].ended, 3000);
  assert.equal(flattenTools([root]).length, 2);
  assert.equal(toolFailed(root.children[0]), false);
  assert.equal(resultText(root.result), "ok");
  // Replay order does not detach child calls or create duplicate result rows.
  assert.equal(
    buildToolTree([...events].reverse(), [])[0].children[0].id,
    "child",
  );
});

test("nonzero command exits and native tool errors are visible as failures", () => {
  for (const result of [
    {
      content: [
        { type: "text", text: '{"exitCode":2,"stderr":"missing file"}' },
      ],
    },
    { message: { isError: true, content: [{ type: "text", text: "failed" }] } },
    { error: { message: "failed" } },
  ])
    assert.equal(toolFailed({ result }), true);
  assert.equal(
    toolFailed({
      result: { content: [{ type: "text", text: '{"exitCode":0}' }] },
    }),
    false,
  );
});

test("pending calls and orphan PTC results remain inspectable without invented completion", () => {
  const tree = buildToolTree(
    [
      event(1, "tool/call", {
        callId: "waiting",
        name: "sandbox_exec",
        arguments: '{"command":"sleep 30"}',
      }),
      event(3, "tool/ptc-dispatch", {
        subCallId: "orphan",
        rootCallId: "missing",
        name: "skill_read",
        arguments: {},
        content: [],
      }),
    ],
    [],
  );
  assert.equal(tree.length, 2);
  assert.equal(tree[0].result, undefined);
  assert.equal(tree[1].name, "skill_read");
});
