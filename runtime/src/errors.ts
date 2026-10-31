// Domain faults; transport libraries are confined to rpc/peer.ts.
export class RuntimeFault extends Error {
  constructor(
    public readonly kind: "cancelled" | "worker_lost" | "operation_failed",
    message: string,
  ) {
    super(message);
  }
}
export function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
