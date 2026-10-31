import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";
import { RpcPeer } from "../src/rpc/peer.js";
import {
  RuntimeClient,
  registerControl,
  type ControlService,
} from "../src/generated/sdk.js";
import type { TurnRequest } from "../src/generated/types.js";

export function turnInput(overrides: Partial<TurnRequest> = {}): TurnRequest {
  return {
    operationId: "test",
    session_id: "session",
    turn_id: "turn",
    message_id: "message",
    message: "hello",
    session_mode: "standard",
    options: {
      mode: "standard",
      model: "deepseek-flash",
      thinking: "off",
      plan: false,
    },
    work_dir: "/tmp/nimbus-test",
    api_key: "",
    records: [],
    skills: [],
    mcp: [],
    attachments: [],
    images: [],
    ...overrides,
  };
}

export class TestRuntime {
  readonly child;
  readonly peer;
  readonly client;
  private readonly exited: Promise<void>;
  constructor(
    control?: ControlService,
    command = process.execPath,
    args = [fileURLToPath(new URL("../../dist/service.js", import.meta.url))],
  ) {
    this.child = spawn(command, args, { stdio: ["pipe", "pipe", "pipe"] });
    this.child.stderr.resume();
    this.peer = new RpcPeer(this.child.stdout, this.child.stdin);
    this.client = new RuntimeClient(this.peer);
    if (control) registerControl(this.peer, control);
    this.exited = new Promise((resolve) =>
      this.child.on("close", () => {
        this.peer.close();
        resolve();
      }),
    );
    this.peer.listen();
  }
  async close(): Promise<void> {
    this.child.stdin.end();
    const timeout = setTimeout(() => this.child.kill("SIGKILL"), 3000);
    try {
      await this.exited;
    } finally {
      clearTimeout(timeout);
    }
  }
}
