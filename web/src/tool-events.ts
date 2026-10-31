import type { Event, StreamFrame, ToolArguments, ToolResultView } from "./api";
export { contentText } from "./event-codec.ts";
export type ToolNode = {
  id: string;
  name: string;
  args: ToolArguments;
  seq: number;
  time?: number;
  ended?: number;
  parent?: string;
  result?: ToolResultView;
  output: string;
  children: ToolNode[];
};
export function toolFailed(node: ToolNode): boolean {
  return node.result?.failed ?? false;
}

// Native call IDs join results and stream frames. PTC dispatch start/end records
// describe one child call, not two separate rows. Attach after collecting all
// records so replay and late-arriving parents produce the same tree.
export function buildToolTree(
  events: Event[],
  frames: StreamFrame[],
): ToolNode[] {
  const nodes = new Map<string, ToolNode>();
  const ensure = (id: string, e: Event): ToolNode => {
    let node = nodes.get(id);
    if (!node) {
      node = {
        id,
        name: "tool",
        args: { formatted: "{}" },
        seq: e.seq,
        time: e.payload.time,
        children: [],
        output: "",
      };
      nodes.set(id, node);
    }
    if (e.seq < node.seq) {
      node.seq = e.seq;
      node.time = e.payload.time;
    }
    return node;
  };
  for (const e of events) {
    const d = e.payload.data;
    if (
      e.type === "tool/call" ||
      e.type === "tool/ptc-dispatch-start" ||
      e.type === "tool/ptc-dispatch"
    ) {
      const id = d.subCallId ?? d.callId ?? d.id;
      if (!id) continue;
      const n = ensure(id, e);
      n.name = d.name ?? d.toolName ?? n.name;
      n.args = e.tool?.args ?? { formatted: "{}" };
      n.parent = d.parentCallId ?? d.rootCallId;
      if (e.type === "tool/ptc-dispatch") {
        n.result = e.tool?.result;
        n.ended = e.payload.time;
      }
    } else if (e.type === "tool/result") {
      const id =
        d.callId ?? d.message?.toolCallId ?? d.message?.source?.callId ?? d.id;
      if (!id) continue;
      const n = ensure(id, e);
      n.result = e.tool?.result;
      n.ended = e.payload.time;
    }
  }
  for (const f of frames) {
    if (f.payload.type === "tool-output") {
      const n = nodes.get(f.payload.callId ?? "");
      if (n) n.output += f.payload.text ?? "";
    }
  }
  const roots: ToolNode[] = [];
  for (const n of [...nodes.values()].sort((a, b) => a.seq - b.seq)) {
    const parent = n.parent ? nodes.get(n.parent) : undefined;
    if (parent && parent !== n && parent.seq < n.seq) parent.children.push(n);
    else roots.push(n);
  }
  return roots;
}
export function flattenTools(nodes: ToolNode[]): ToolNode[] {
  return nodes.flatMap((n) => [n, ...flattenTools(n.children)]);
}
