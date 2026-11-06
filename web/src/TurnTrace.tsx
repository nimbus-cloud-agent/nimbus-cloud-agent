import {
  createContext,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  api,
  decodeJSON,
  turnTrace,
  type TurnTrace,
  type TurnMetadata,
  type WireEvent,
  type StreamFrame,
  type TurnHeaders,
  type EventHeader,
  type FrameHeader,
} from "./api";
import { decodeEvent } from "./event-codec";
import { Fold } from "./components";

type Snapshot = TurnTrace | TurnMetadata;
type CachedQuery = { value: Snapshot; settled: boolean };
const TraceCache = createContext<Map<string, CachedQuery> | null>(null);
const DisclosureCache = createContext<Map<string, boolean> | null>(null);
type CachedHeaders = TurnHeaders & { settled: boolean };
const HeaderCache = createContext<Map<string, CachedHeaders> | null>(null);

// Session-keyed by the caller: records, projections and cursors never cross sessions.
export function TurnTraceProvider({ children }: { children: ReactNode }) {
  const cache = useRef(new Map<string, CachedQuery>());
  const choices = useRef(new Map<string, boolean>());
  const headers = useRef(new Map<string, CachedHeaders>());
  return (
    <TraceCache.Provider value={cache.current}>
      <DisclosureCache.Provider value={choices.current}>
        <HeaderCache.Provider value={headers.current}>
          {children}
        </HeaderCache.Provider>
      </DisclosureCache.Provider>
    </TraceCache.Provider>
  );
}

export function useTurnDisclosure(key: string, defaultOpen = false) {
  const choices = useContext(DisclosureCache)!;
  const [choice, setChoice] = useState<boolean | undefined>(() =>
    choices.get(key),
  );
  return [
    choice ?? defaultOpen,
    (open: boolean) => {
      choices.set(key, open);
      setChoice(open);
    },
  ] as const;
}

function useTurnQuery<T extends Snapshot>(
  sessionId: string,
  turnId: string,
  open: boolean,
  running: boolean,
  query: string,
  metadata: boolean,
) {
  const cache = useContext(TraceCache)!;
  const key = `${turnId}?${query}`;
  const [value, setValue] = useState<T | undefined>(
    () => cache.get(key)?.value as T | undefined,
  );
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (!open) return;
    let active = true;
    const abort = new AbortController();
    let source: EventSource | undefined;
    const publish = (next: T, settled: boolean) => {
      if (!active) return;
      cache.set(key, { value: next, settled });
      setValue(next);
    };
    const receive = (event: MessageEvent, update: (current: T) => T) => {
      if (!active) return;
      const current = cache.get(key);
      if (!current) return;
      const next = update(current.value as T);
      const [seq, frame] = event.lastEventId.split(":").map(Number);
      if (Number.isFinite(seq) && Number.isFinite(frame))
        next.cursor = {
          seq: Math.max(next.cursor.seq, seq),
          frame: Math.max(next.cursor.frame, frame),
        };
      publish(next, current.settled);
    };
    void (async () => {
      try {
        setError("");
        let cached = cache.get(key);
        // A completion notification can overtake the detail stream. A running
        // cache is never terminal until a final, consistent snapshot catches up.
        if (!cached || (!running && !cached.settled)) {
          setLoading(true);
          const snapshot = metadata
            ? await api<T>(
                `/sessions/${sessionId}/turns/${turnId}/events?${query}`,
                "GET",
                undefined,
                abort.signal,
              )
            : ((await turnTrace(sessionId, turnId, abort.signal, query)) as T);
          if (!active) return;
          publish(snapshot, !running);
          cached = cache.get(key)!;
        } else setValue(cached.value as T);
        setLoading(false);
        if (!running || cached.settled || !active) return;
        const cursor = cached.value.cursor;
        source = new EventSource(
          `/api/sessions/${sessionId}/turns/${turnId}/stream?${query ? `${query}&` : ""}after_seq=${cursor.seq}&after_frame=${cursor.frame}`,
        );
        source.onopen = () => {
          if (active) setError("");
        };
        source.onerror = () => {
          if (active) setError("Live execution disconnected. Reconnecting…");
        };
        if (metadata)
          source.addEventListener("metadata", (e) =>
            receive(e, () => decodeJSON<T>(e.data)),
          );
        else {
          source.addEventListener("session", (e) =>
            receive(e, (current) => {
              const trace = current as TurnTrace;
              const row = decodeEvent(decodeJSON<WireEvent>(e.data));
              return {
                ...trace,
                events: trace.events.some((x) => x.seq === row.seq)
                  ? trace.events
                  : [...trace.events, row].sort((a, b) => a.seq - b.seq),
                cursor: {
                  ...trace.cursor,
                  seq: Math.max(trace.cursor.seq, row.seq),
                },
              } as T;
            }),
          );
          source.addEventListener("frame", (e) =>
            receive(e, (current) => {
              const trace = current as TurnTrace;
              const frame = decodeJSON<StreamFrame>(e.data);
              return {
                ...trace,
                frames: trace.frames.some((x) => x.id === frame.id)
                  ? trace.frames
                  : [...trace.frames, frame].sort((a, b) => a.id - b.id),
                cursor: {
                  ...trace.cursor,
                  frame: Math.max(trace.cursor.frame, frame.id),
                },
              } as T;
            }),
          );
        }
      } catch (e) {
        if (active) {
          setLoading(false);
          setError(String(e));
        }
      }
    })();
    return () => {
      active = false;
      abort.abort();
      source?.close();
    };
  }, [sessionId, turnId, open, running, attempt, key, query, metadata, cache]);
  return {
    value,
    loading,
    error,
    retry: () => {
      cache.delete(key);
      setAttempt((n) => n + 1);
    },
  };
}

export function useTurnTrace(
  sessionId: string,
  turnId: string,
  open: boolean,
  running: boolean,
  query = "",
) {
  const { value, ...status } = useTurnQuery<TurnTrace>(
    sessionId,
    turnId,
    open,
    running,
    `${query ? `${query}&` : ""}include_question_content=false&include_questions=false&include_outbox=false`,
    false,
  );
  return { trace: value, ...status };
}
export function useTurnMetadata(
  sessionId: string,
  turnId: string,
  open: boolean,
  running: boolean,
  parent = "root",
) {
  const query = `projection=metadata&parent_call_id=${encodeURIComponent(parent)}&argument_keys=description,path,command,query,name&max_field_chars=256&include_question_content=false&include_questions=false&include_outbox=false`;
  const { value, ...status } = useTurnQuery<TurnMetadata>(
    sessionId,
    turnId,
    open,
    running,
    query,
    true,
  );
  return { metadata: value, ...status };
}
export function TraceStatus({
  loading,
  error,
  retry,
}: {
  loading: boolean;
  error: string;
  retry: () => void;
}) {
  return (
    <>
      {loading && <p role="status">Loading execution details…</p>}
      {error && (
        <div role="alert">
          {error}{" "}
          <button type="button" onClick={retry}>
            Retry execution details
          </button>
        </div>
      )}
    </>
  );
}
export function TrajectoryTurn({
  sessionId,
  turnId,
  title,
  running,
}: {
  sessionId: string;
  turnId: string;
  title: string;
  running: boolean;
}) {
  const [open, setOpen] = useTurnDisclosure(`trajectory:${turnId}`);
  return (
    <Fold
      className="trajectory-turn"
      title={title || "Turn activity"}
      open={open}
      onOpenChange={setOpen}
    >
      <TrajectoryHeaders
        sessionId={sessionId}
        turnId={turnId}
        kind="events"
        running={running}
      />
      <TrajectoryHeaders
        sessionId={sessionId}
        turnId={turnId}
        kind="frames"
        running={running}
      />
    </Fold>
  );
}

function mergeHeaderRows<T>(older: T[], newer: T[], key: (row: T) => number) {
  const rows = new Map(older.map((row) => [key(row), row]));
  for (const row of newer) rows.set(key(row), row);
  return [...rows.values()].sort((a, b) => key(a) - key(b));
}

function useTurnHeaders(
  sessionId: string,
  turnId: string,
  kind: "events" | "frames",
  open: boolean,
  running: boolean,
) {
  const cache = useContext(HeaderCache)!;
  const key = `${turnId}:${kind}`;
  const query = `projection=headers&kind=${kind}&limit=100&include_questions=false&include_outbox=false`;
  const [headers, setHeaders] = useState<CachedHeaders | undefined>(() =>
    cache.get(key),
  );
  const [loading, setLoading] = useState(false);
  const [loadingOlder, setLoadingOlder] = useState(false);
  const [error, setError] = useState("");
  const [olderError, setOlderError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const olderRequest = useRef<AbortController | null>(null);
  const needsRefresh = useRef(false);
  useEffect(() => {
    if (!open) return;
    setLoadingOlder(false);
    let active = true;
    const abort = new AbortController();
    let source: EventSource | undefined;
    void (async () => {
      try {
        setError("");
        let current = cache.get(key);
        if (
          !current ||
          (!running && !current.settled) ||
          needsRefresh.current
        ) {
          setLoading(true);
          const page = await api<TurnHeaders>(
            `/sessions/${sessionId}/turns/${turnId}/events?${query}`,
            "GET",
            undefined,
            abort.signal,
          );
          if (!active) return;
          const overlaps =
            !!current &&
            (kind === "events"
              ? page.events.some((row) =>
                  current!.events.some((old) => old.seq === row.seq),
                )
              : page.frames.some((row) =>
                  current!.frames.some((old) => old.id === row.id),
                ));
          const retainPagination = !!current && page.has_more && overlaps;
          current = {
            ...page,
            events: mergeHeaderRows(
              current?.events ?? [],
              page.events,
              (row) => row.seq,
            ),
            frames: mergeHeaderRows(
              current?.frames ?? [],
              page.frames,
              (row) => row.id,
            ),
            before: retainPagination ? current!.before : page.before,
            has_more: retainPagination ? current!.has_more : page.has_more,
            cursor: {
              seq: Math.max(current?.cursor.seq ?? -1, page.cursor.seq),
              frame: Math.max(current?.cursor.frame ?? 0, page.cursor.frame),
            },
            settled: !running,
          };
          cache.set(key, current);
          needsRefresh.current = false;
        }
        setHeaders(current);
        setLoading(false);
        if (!running || !active) return;
        source = new EventSource(
          `/api/sessions/${sessionId}/turns/${turnId}/stream?${query}&after_seq=${current.cursor.seq}&after_frame=${current.cursor.frame}`,
        );
        source.onopen = () => {
          if (active) setError("");
        };
        source.onerror = () => {
          if (active) setError("Live activity disconnected. Reconnecting…");
        };
        const receive = (event: MessageEvent) => {
          if (!active) return;
          const previous = cache.get(key)!;
          const next = { ...previous, cursor: { ...previous.cursor } };
          if (kind === "events") {
            const header = decodeJSON<EventHeader>(event.data);
            next.events = mergeHeaderRows(
              previous.events,
              [header],
              (row) => row.seq,
            );
            next.cursor.seq = Math.max(next.cursor.seq, header.seq);
          } else {
            const header = decodeJSON<FrameHeader>(event.data);
            next.frames = mergeHeaderRows(
              previous.frames,
              [header],
              (row) => row.id,
            );
            next.cursor.frame = Math.max(next.cursor.frame, header.id);
          }
          const [seq, frame] = event.lastEventId.split(":").map(Number);
          if (Number.isFinite(seq) && Number.isFinite(frame))
            next.cursor = {
              seq: Math.max(next.cursor.seq, seq),
              frame: Math.max(next.cursor.frame, frame),
            };
          cache.set(key, next);
          setHeaders(next);
        };
        source.addEventListener(
          kind === "events" ? "event_header" : "frame_header",
          receive,
        );
      } catch (e) {
        if (active) {
          setLoading(false);
          setError(String(e));
        }
      }
    })();
    return () => {
      active = false;
      abort.abort();
      olderRequest.current?.abort();
      olderRequest.current = null;
      source?.close();
    };
  }, [sessionId, turnId, kind, open, running, key, query, attempt, cache]);
  async function loadOlder() {
    const previous = cache.get(key);
    if (
      !open ||
      !previous?.has_more ||
      previous.before == null ||
      olderRequest.current
    )
      return;
    const abort = new AbortController();
    olderRequest.current = abort;
    setLoadingOlder(true);
    setOlderError("");
    try {
      const page = await api<TurnHeaders>(
        `/sessions/${sessionId}/turns/${turnId}/events?${query}&before=${previous.before}`,
        "GET",
        undefined,
        abort.signal,
      );
      if (abort.signal.aborted) return;
      const current = cache.get(key)!;
      const next = {
        ...current,
        events: mergeHeaderRows(page.events, current.events, (row) => row.seq),
        frames: mergeHeaderRows(page.frames, current.frames, (row) => row.id),
        before: page.before,
        has_more: page.has_more,
      };
      // The older page never rewinds the independent live reconnect cursor.
      cache.set(key, next);
      setHeaders(next);
    } catch (e) {
      if (!abort.signal.aborted) setOlderError(String(e));
    } finally {
      if (olderRequest.current === abort) {
        olderRequest.current = null;
        if (!abort.signal.aborted) setLoadingOlder(false);
      }
    }
  }
  return {
    headers,
    loading,
    error,
    retry: () => {
      needsRefresh.current = true;
      setAttempt((n) => n + 1);
    },
    loadingOlder,
    olderError,
    loadOlder,
  };
}

function TrajectoryHeaders({
  sessionId,
  turnId,
  kind,
  running,
}: {
  sessionId: string;
  turnId: string;
  kind: "events" | "frames";
  running: boolean;
}) {
  const [open, setOpen] = useTurnDisclosure(
    `trajectory:${turnId}:${kind}`,
    kind === "events",
  );
  const detail = useTurnHeaders(sessionId, turnId, kind, open, running);
  return (
    <Fold
      className={`trajectory-${kind}`}
      title={kind === "events" ? "Native events" : "Stream frames"}
      open={open}
      onOpenChange={setOpen}
    >
      <TraceStatus {...detail} />
      {detail.olderError && <p role="alert">{detail.olderError}</p>}
      {detail.headers?.has_more && (
        <button
          type="button"
          disabled={detail.loadingOlder}
          onClick={() => void detail.loadOlder()}
        >
          {detail.loadingOlder
            ? "Loading older activity…"
            : `Load older ${kind}`}
        </button>
      )}
      {kind === "events"
        ? detail.headers?.events.map((header) => (
            <TrajectoryRecord
              key={header.seq}
              sessionId={sessionId}
              turnId={turnId}
              title={`#${header.seq} ${header.type}`}
              query={`seq=${header.seq}`}
            />
          ))
        : detail.headers?.frames.map((header) => (
            <TrajectoryRecord
              key={header.id}
              sessionId={sessionId}
              turnId={turnId}
              title={`Frame #${header.id}: ${header.type}${header.chunk_type ? ` / ${header.chunk_type}` : ""}`}
              query={`frame_id=${header.id}`}
            />
          ))}
    </Fold>
  );
}

function TrajectoryRecord({
  sessionId,
  turnId,
  title,
  query,
}: {
  sessionId: string;
  turnId: string;
  title: string;
  query: string;
}) {
  const [open, setOpen] = useTurnDisclosure(`trajectory:${turnId}:${query}`);
  const detail = useTurnTrace(sessionId, turnId, open, false, query);
  return (
    <Fold
      className="trajectory-record"
      title={title}
      open={open}
      onOpenChange={setOpen}
    >
      <TraceStatus {...detail} />
      {detail.trace && (
        <pre>
          {JSON.stringify(
            detail.trace.events[0]?.payload ?? detail.trace.frames[0]?.payload,
            null,
            2,
          )}
        </pre>
      )}
    </Fold>
  );
}
