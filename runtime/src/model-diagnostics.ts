import type {
  AdapterRegistrationHandle,
  GenerateOptions,
  LlmAdapter,
  StreamChunk,
} from "@deepseek-ai/dsh-llm";
import type { ModelDiagnostic, ModelErrorDiagnostic } from "./generated/types.js";

// Harness normalizes a provider exception into a durable `LlmFailure` that keeps
// only `message` and `code`, so a transport outage reaches the control plane as
// "DeepSeek Messages transport failed" with no evidence. The useful facts live
// on the wrapped `cause` (`ECONNRESET`, `EAI_AGAIN`, `UND_ERR_*`, HTTP status),
// so they are captured here, before normalization, as a bounded allowlist.
const MAX_CHAIN = 8;
const MAX_MESSAGE = 160;
const MAX_FIELD = 64;

type Call = Pick<GenerateOptions, "provider" | "model" | "signal">;
type AdapterRegistry = {
  registerAdapter(
    providers: string[],
    adapter: LlmAdapter,
  ): AdapterRegistrationHandle;
};

export async function withModelDiagnostics(
  llm: AdapterRegistry,
  apiKey: string,
  emit: (diagnostic: ModelDiagnostic) => Promise<void>,
  mountProvider: () => Promise<unknown>,
) {
  const register = llm.registerAdapter.bind(llm);
  let attempts = 0;
  const report = async (
    call: Call,
    attempt: number,
    phase: ModelDiagnostic["phase"],
    duration: number,
    received: number,
    error: unknown,
  ) => {
    // A user stop is not a provider failure and must not be reported as one.
    if (call.signal?.aborted) return;
    const { entries, truncated } = errorChain(error, apiKey);
    try {
      await emit({
        provider: call.provider,
        model: call.model,
        phase,
        attempt,
        duration_ms: Math.round(duration),
        chunks_received: received,
        error_chain: entries,
        truncated,
      });
    } catch {
      // Diagnostics are best-effort and never change the turn's outcome.
    }
  };
  const track = async function* (
    chunks: AsyncIterable<StreamChunk>,
    call: Call,
    attempt: number,
  ): AsyncGenerator<StreamChunk> {
    const started = performance.now();
    let received = 0;
    const iterator = chunks[Symbol.asyncIterator]();
    let done = false;
    try {
      while (true) {
        const next = await iterator.next();
        if (next.done) {
          done = true;
          return;
        }
        received++;
        yield next.value;
      }
    } catch (error) {
      done = true;
      await report(
        call,
        attempt,
        received === 0 ? "request" : "stream",
        performance.now() - started,
        received,
        error,
      );
      throw error;
    } finally {
      if (!done) await iterator.return?.();
    }
  };
  // `prepareCall` is the only route the agent loop takes and its returned
  // `stream` closure bypasses `adapter.stream`, so both are instrumented.
  const instrument = (adapter: LlmAdapter): LlmAdapter => {
    const prepare = adapter.prepareCall.bind(adapter);
    const stream = adapter.stream.bind(adapter);
    adapter.prepareCall = async (provider, model, signal) => {
      const attempt = ++attempts;
      const started = performance.now();
      let prepared;
      try {
        prepared = await prepare(provider, model, signal);
      } catch (error) {
        await report(
          { provider, model, signal },
          attempt,
          "prepare",
          performance.now() - started,
          0,
          error,
        );
        throw error;
      }
      return {
        ...prepared,
        stream: (options) =>
          track(prepared.stream(options), options, attempt),
      };
    };
    adapter.stream = (options) => track(stream(options), options, ++attempts);
    return adapter;
  };
  llm.registerAdapter = (providers, adapter) =>
    register(providers, instrument(adapter));
  await mountProvider();
}

function errorChain(value: unknown, secret: string) {
  const entries: ModelErrorDiagnostic[] = [];
  const seen = new Set<unknown>();
  let truncated = false;
  let current = value;
  while (current !== undefined && current !== null) {
    if (seen.has(current) || entries.length === MAX_CHAIN) {
      truncated = true;
      break;
    }
    seen.add(current);
    const { entry, cut } = describe(current, secret);
    entries.push(entry);
    truncated ||= cut;
    current = causeOf(current);
  }
  return { entries, truncated };
}

function describe(error: unknown, secret: string): {
  entry: ModelErrorDiagnostic;
  cut: boolean;
} {
  const source = typeof error === "object" && error !== null ? error : {};
  // Harness errors keep their HTTP status and provider request id in a
  // serialized failure snapshot rather than as direct properties.
  const snapshot = field(source, "failure");
  const facts =
    typeof snapshot === "object" && snapshot !== null
      ? [source, snapshot]
      : [source];
  const raw = text(field(source, "message"), MAX_MESSAGE * 4) ?? String(error);
  const message = sanitize(raw, secret);
  const entry: ModelErrorDiagnostic = {
    name:
      text(field(source, "name"), MAX_FIELD) ??
      (error instanceof Error ? error.constructor.name : "ThrownValue"),
  };
  if (message.text) entry.message = message.text;
  const code = text(field(source, "code"), MAX_FIELD);
  if (code) entry.code = code;
  const errno = integer(field(source, "errno"), -(2 ** 31), 2 ** 31 - 1);
  if (errno !== undefined) entry.errno = errno;
  const syscall = text(field(source, "syscall"), 32);
  if (syscall) entry.syscall = syscall;
  const address = text(field(source, "address"), 45);
  if (address) entry.address = address;
  const port = integer(field(source, "port"), 0, 65535);
  if (port !== undefined) entry.port = port;
  const status = lookup(facts, "status", (value) => integer(value, 100, 599));
  if (status !== undefined) entry.status = status;
  const requestId =
    lookup(facts, "requestId", (value) => text(value, 128)) ??
    lookup(facts, "request_id", (value) => text(value, 128));
  if (requestId) entry.request_id = requestId;
  return { entry, cut: message.cut };
}

function causeOf(error: unknown): unknown {
  return typeof error === "object" && error !== null
    ? Reflect.get(error, "cause")
    : undefined;
}

// Errors from undici and the Node network stack expose their transport facts
// as ad-hoc own properties, so they are read reflectively and allowlisted.
function field(source: object, key: string): unknown {
  return Reflect.get(source, key);
}

function lookup<T>(
  sources: object[],
  key: string,
  read: (value: unknown) => T | undefined,
): T | undefined {
  for (const source of sources) {
    const value = read(field(source, key));
    if (value !== undefined) return value;
  }
  return undefined;
}

// One line per layer: transport messages can embed an HTML error page or a
// multi-line socket report, and the API key must never reach the log.
function sanitize(value: string, secret: string) {
  const collapsed = value.replace(/\s+/g, " ").trim();
  const redacted =
    secret.length > 0 ? collapsed.replaceAll(secret, "[REDACTED]") : collapsed;
  if (redacted.length <= MAX_MESSAGE) return { text: redacted, cut: false };
  return { text: `${redacted.slice(0, MAX_MESSAGE)}…`, cut: true };
}

function text(value: unknown, max: number): string | undefined {
  return typeof value === "string" && value.length > 0
    ? value.slice(0, max)
    : undefined;
}

function integer(value: unknown, min: number, max: number): number | undefined {
  return typeof value === "number" &&
    Number.isInteger(value) &&
    value >= min &&
    value <= max
    ? value
    : undefined;
}
