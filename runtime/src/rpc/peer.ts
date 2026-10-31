import type { Readable, Writable } from "node:stream";
import { Ajv } from "ajv";
import {
  createMessageConnection,
  StreamMessageReader,
  StreamMessageWriter,
  CancellationTokenSource,
  ResponseError,
  ErrorCodes,
  type CancellationToken,
  type MessageConnection,
} from "vscode-jsonrpc/node";
import type { RpcPort } from "../generated/sdk.js";
import schema from "../generated/schema.json" with { type: "json" };
import { RuntimeFault, errorMessage } from "../errors.js";

const ajv = new Ajv({ strict: false, allErrors: false });
ajv.addSchema(schema, "nimbus");
function validate(
  type: string,
  value: unknown,
  code: number = ErrorCodes.InvalidParams,
): void {
  const validator = ajv.getSchema("nimbus#/definitions/" + type);
  if (!validator || !validator(value))
    throw new ResponseError(code, `Invalid ${type} payload`);
}
// This adapter is the only application module aware of vscode-jsonrpc. The SDK
// owns Content-Length framing, IDs, response routing, writes and cancellation.
export class RpcPeer implements RpcPort {
  private readonly connection: MessageConnection;
  private readonly lifetime = new AbortController();
  private resolveDone!: () => void;
  readonly done: Promise<void>;
  constructor(input: Readable, output: Writable) {
    this.connection = createMessageConnection(
      new StreamMessageReader(input),
      new StreamMessageWriter(output),
    );
    this.done = new Promise((resolve) => {
      this.resolveDone = resolve;
    });
    this.connection.onClose(() => this.close());
    this.connection.onError(() => this.close());
  }
  listen(): void {
    this.connection.listen();
  }
  close(): void {
    if (this.lifetime.signal.aborted) return;
    this.lifetime.abort();
    this.connection.dispose();
    this.resolveDone();
  }
  async call<P, R>(
    method: string,
    input: P,
    requestType: string,
    resultType: string,
    signal?: AbortSignal,
  ): Promise<R> {
    validate(requestType, input);
    signal?.throwIfAborted();
    const token = new CancellationTokenSource();
    const abort = () => token.cancel();
    signal?.addEventListener("abort", abort, { once: true });
    try {
      const result = await this.connection.sendRequest<R>(
        method,
        { request: input },
        token.token,
      );
      validate(resultType, result, ErrorCodes.InternalError);
      return result;
    } catch (error) {
      if (error instanceof ResponseError)
        throw new RuntimeFault(
          error.code === -32800
            ? "cancelled"
            : error.code === -32002
              ? "worker_lost"
              : "operation_failed",
          error.message,
        );
      throw error;
    } finally {
      signal?.removeEventListener("abort", abort);
      token.dispose();
    }
  }
  handle<P, R>(
    method: string,
    requestType: string,
    resultType: string,
    handler: (input: P, signal: AbortSignal) => Promise<R>,
  ): void {
    this.connection.onRequest<unknown, unknown>(
      method,
      async (params: unknown, token: CancellationToken): Promise<unknown> => {
        const controller = new AbortController();
        const disposable = token.onCancellationRequested(() =>
          controller.abort(),
        );
        const signal = AbortSignal.any([
          controller.signal,
          this.lifetime.signal,
        ]);
        if (token.isCancellationRequested) controller.abort();
        try {
          if (!params || typeof params !== "object" || !("request" in params))
            throw new ResponseError(
              ErrorCodes.InvalidParams,
              "Named request parameters required",
            );
          validate(requestType, params.request);
          const result = await handler(params.request as P, signal);
          validate(resultType, result, ErrorCodes.InternalError);
          return result;
        } catch (error) {
          if (error instanceof ResponseError) throw error;
          const code =
            error instanceof RuntimeFault
              ? error.kind === "cancelled"
                ? -32800
                : error.kind === "worker_lost"
                  ? -32002
                  : -32000
              : signal.aborted
                ? -32800
                : -32000;
          throw new ResponseError(code, errorMessage(error));
        } finally {
          disposable.dispose();
        }
      },
    );
  }
}
