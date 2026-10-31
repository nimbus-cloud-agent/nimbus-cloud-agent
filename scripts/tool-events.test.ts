import test from "node:test";
import assert from "node:assert/strict";
import {
  buildToolTree,
  flattenTools,
  toolFailed,
} from "../web/src/tool-events.ts";
import { decodeEvent } from "../web/src/event-codec.ts";
import type { NativeData } from "../web/src/api.ts";
const event = (seq: number, type: string, data: NativeData) =>
  decodeEvent({
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
    {
      id: 1,
      turn_id: "turn",
      payload: { type: "tool-output", callId: "child", text: "live\n" },
    },
  ]);
  assert.equal(root.name, "run_code");
  assert.equal(root.children.length, 1);
  assert.equal(root.children[0].seq, 2);
  assert.equal(root.children[0].output, "live\n");
  assert.equal(root.children[0].ended, 3000);
  assert.equal(flattenTools([root]).length, 2);
  assert.equal(toolFailed(root.children[0]), false);
  assert.equal(root.result?.text, "ok");
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
    assert.equal(
      toolFailed(
        buildToolTree(
          [event(1, "tool/result", { ...result, callId: "call" })],
          [],
        )[0],
      ),
      true,
    );
  const [success] = buildToolTree(
    [
      event(1, "tool/result", {
        callId: "call",
        content: [{ type: "text", text: '{"exitCode":0}' }],
      }),
    ],
    [],
  );
  assert.equal(toolFailed(success), false);
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

test("event ingress normalizes encoded arguments and results before rendering", () => {
  const wire = {
    seq: 1,
    type: "tool/ptc-dispatch",
    payload: {
      time: 1000,
      data: {
        subCallId: "call",
        name: "sandbox_exec",
        arguments: '{"command":"pwd","providerArgument":{"keep":true}}',
        content: [
          {
            type: "text",
            text: '{"stdout":"/workspace","exitCode":0,"truncated":false}',
          },
        ],
      },
    },
  };
  const decoded = decodeEvent(wire);
  const [tool] = buildToolTree([decoded], []);
  assert.equal(tool.args.command, "pwd");
  assert.match(tool.args.formatted, /providerArgument/);
  assert.equal(tool.result?.command?.stdout, "/workspace");
  assert.equal(decoded.payload, wire.payload);
  const unexpected = decodeEvent({
    ...wire,
    payload: {
      ...wire.payload,
      data: {
        ...wire.payload.data,
        arguments: { command: 42, plan: { nested: true } },
      },
    },
  });
  assert.equal(unexpected.tool?.args.command, undefined);
  assert.equal(unexpected.tool?.args.plan, undefined);
});
