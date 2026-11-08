import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import { Button, TextArea, Input, Modal } from "@heroui/react";
import {
  Cloud,
  Plus,
  Settings as SettingsIcon,
  ArrowUp,
  Square,
  MessageSquare,
  PanelLeft,
  TerminalSquare,
  Maximize2,
  Minimize2,
  MoreHorizontal,
  Workflow,
  BrainCircuit,
  Braces,
  ImagePlus,
  ChevronRight,
  ChevronDown,
  Code2,
  FileText,
  Library as LibraryIcon,
  Compass,
  Search,
  Trash2,
  Pencil,
  Check,
  X,
} from "lucide-react";
import {
  api,
  history,
  decodeJSON,
  sandboxLabels,
  type Session,
  type TurnSummary,
  type Outbox,
  type Options,
  type MessageImage,
  type UserQuestion,
} from "./api";
import { Picker, Toggle } from "./components";
import TerminalPanel from "./Terminal";
import Settings from "./Settings";
import Conversation from "./Conversation";
import { TurnTraceProvider, TrajectoryTurn } from "./TurnTrace";
import Library from "./Library";
import LibraryPreview from "./LibraryPreview";
import PanelDivider from "./PanelDivider";
import ImageGallery from "./ImageGallery";
function mergeRows<T>(
  current: T[],
  incoming: T[],
  key: (row: T) => string,
): T[] {
  const rows = new Map(current.map((row) => [key(row), row]));
  for (const row of incoming) rows.set(key(row), row);
  return [...rows.values()];
}
export default function App() {
  const [sessions, setSessions] = useState<Session[]>([]),
    [id, setId] = useState(location.hash.slice(1)),
    [turns, setTurns] = useState<TurnSummary[]>([]),
    [outbox, setOutbox] = useState<Outbox[]>([]);
  const [questions, setQuestions] = useState<UserQuestion[]>([]);
  const [beforeTurn, setBeforeTurn] = useState<string | null>(null);
  const [historySession, setHistorySession] = useState("");
  const [historyLoading, setHistoryLoading] = useState(false);
  const [historyError, setHistoryError] = useState("");
  const [historyAttempt, setHistoryAttempt] = useState(0);
  const olderRequest = useRef<AbortController | null>(null);
  const olderCursor = useRef<string | null>(null);
  const upwardIntent = useRef(0);
  const touchY = useRef<number | null>(null);
  const prependAnchor = useRef<{ turn: string; top: number } | null>(null);
  const readingPosition = useRef(0);
  const [draft, setDraft] = useState(""),
    [error, setError] = useState(""),
    [settings, setSettings] = useState(location.search.includes("settings=")),
    [filter, setFilter] = useState(""),
    [sidebarDrawer, setSidebarDrawer] = useState(false),
    [view, setView] = useState("chat"),
    [panel, setPanel] = useState("chat"),
    [preview, setPreview] = useState<{ id: string; version?: number } | null>(
      null,
    ),
    [online, setOnline] = useState(true),
    [sending, setSending] = useState(false),
    [rename, setRename] = useState<string | null>(null),
    [deleteID, setDeleteID] = useState(""),
    [chatActions, setChatActions] = useState(false),
    [modelOptions, setModelOptions] = useState(false),
    [terminalOpen, setTerminalOpen] = useState(false),
    [focused, setFocused] = useState(false);
  const [viewport, setViewport] = useState(() => innerWidth);
  const [sidebarOverride, setSidebarOverride] = useState<boolean | null>(null);
  const desktop = viewport >= 1100;
  const sidebarExpanded = sidebarOverride ?? viewport >= 1440;
  const sidebarWidth = focused || !desktop ? 0 : sidebarExpanded ? 216 : 48;
  const terminalVisible = terminalOpen && !focused;
  const terminalSplit = desktop && viewport - sidebarWidth >= 1041;
  const historyVisible =
    panel === "chat" && !sidebarDrawer && (!terminalVisible || terminalSplit);
  const loadedHistory = useRef<{
    id: string;
    loaded: boolean;
    anchor?: string;
  }>({ id: "", loaded: false });
  useEffect(() => {
    const resize = () => setViewport(innerWidth);
    window.addEventListener("resize", resize);
    return () => window.removeEventListener("resize", resize);
  }, []);
  const [draftImages, setDraftImages] = useState<MessageImage[]>([]);
  const [uploadingImages, setUploadingImages] = useState(false);
  const [draggingImage, setDraggingImage] = useState(false);
  const imageInput = useRef<HTMLInputElement>(null);
  const composerInput = useRef<HTMLTextAreaElement>(null);
  const resizeComposer = useCallback(() => {
    const input = composerInput.current;
    if (!input) return;
    input.style.height = "0px";
    input.style.height = `${Math.min(180, input.scrollHeight)}px`;
  }, []);
  useLayoutEffect(resizeComposer, [
    draft,
    panel,
    historyVisible,
    resizeComposer,
  ]);
  useLayoutEffect(() => {
    const input = composerInput.current;
    if (!input) return;
    let width = input.clientWidth;
    const observer = new ResizeObserver(() => {
      if (input.clientWidth === width) return;
      width = input.clientWidth;
      resizeComposer();
    });
    observer.observe(input);
    return () => observer.disconnect();
  }, [panel, resizeComposer]);
  const uploadPending = useRef(false);
  const selectedID = useRef(id);
  selectedID.current = id;
  const sendAttempt = useRef<{
    sid: string;
    content: string;
    client: string;
    imagesKey: string;
  } | null>(null);
  const scroll = useRef<HTMLDivElement>(null),
    nearBottom = useRef(true);
  const current = sessions.find((x) => x.id === id);
  useEffect(() => {
    const source = new EventSource("/api/sessions/stream");
    source.onopen = () => setOnline(true);
    source.onerror = () => setOnline(false);
    source.addEventListener("sessions", (e) => {
      const all = decodeJSON<Session[]>(e.data);
      setSessions(all);
      setId((selected) =>
        all.some((s) => s.id === selected) ? selected : (all[0]?.id ?? ""),
      );
    });
    return () => source.close();
  }, []);
  useEffect(() => {
    location.hash = id;
    loadedHistory.current = { id, loaded: false };
    setTurns([]);
    setOutbox([]);
    setQuestions([]);
    setBeforeTurn(null);
    olderCursor.current = null;
    olderRequest.current?.abort();
    olderRequest.current = null;
    prependAnchor.current = null;
    readingPosition.current = 0;
    upwardIntent.current = 0;
    setHistoryError("");
    setHistoryLoading(false);
    setDraftImages([]);
  }, [id]);
  useEffect(() => {
    if (!id || !historyVisible) return;
    let active = true;
    const abort = new AbortController();
    let source: EventSource | undefined;
    if (!loadedHistory.current.loaded) nearBottom.current = true;
    void (async () => {
      try {
        if (!loadedHistory.current.loaded) {
          setHistoryLoading(true);
          const d = await history(id, undefined, abort.signal);
          if (!active) return;
          setHistorySession(id);
          setOutbox(d.outbox);
          setTurns(d.turns);
          setQuestions(d.questions);
          setBeforeTurn(d.before_turn);
          olderCursor.current = d.before_turn;
          loadedHistory.current = {
            id,
            loaded: true,
            anchor: d.outbox[0]?.turn_id,
          };
          setHistoryLoading(false);
        }
        const anchor = loadedHistory.current.anchor;
        source = new EventSource(
          `/api/sessions/${id}/stream?include_question_content=false${anchor ? `&since_turn=${encodeURIComponent(anchor)}` : ""}`,
        );
        source.onopen = () => {
          if (active) setOnline(true);
        };
        source.onerror = () => {
          if (active) setOnline(false);
        };
        source.addEventListener("state", (e) => {
          if (!active) return;
          const v = decodeJSON<Session>(e.data);
          setSessions((xs) => xs.map((x) => (x.id === v.id ? v : x)));
        });
        source.addEventListener("outbox", (e) => {
          if (active)
            setOutbox((xs) =>
              mergeRows(xs, decodeJSON<Outbox[]>(e.data), (x) => x.turn_id),
            );
        });
        source.addEventListener("turns", (e) => {
          if (active)
            setTurns((xs) =>
              mergeRows(
                xs,
                decodeJSON<TurnSummary[]>(e.data),
                (x) => x.turn_id,
              ),
            );
        });
        source.addEventListener("questions", (e) => {
          if (active)
            setQuestions((xs) =>
              mergeRows(xs, decodeJSON<UserQuestion[]>(e.data), (x) => x.id),
            );
        });
      } catch (e) {
        if (active) {
          setHistoryError(String(e));
          setHistoryLoading(false);
        }
      }
    })();
    return () => {
      active = false;
      abort.abort();
      olderRequest.current?.abort();
      source?.close();
    };
  }, [id, historyAttempt, historyVisible]);
  const followOutput = useCallback(() => {
    if (nearBottom.current && !prependAnchor.current && scroll.current)
      scroll.current.scrollTop = scroll.current.scrollHeight;
  }, []);
  useLayoutEffect(() => {
    if (!historyVisible || !loadedHistory.current.loaded || !scroll.current)
      return;
    if (nearBottom.current) followOutput();
    else scroll.current.scrollTop = readingPosition.current;
  }, [historyVisible, followOutput]);
  useLayoutEffect(() => {
    const anchor = prependAnchor.current;
    if (anchor && scroll.current) {
      const node = Array.from(
        scroll.current.querySelectorAll<HTMLElement>("[data-turn-id]"),
      ).find((el) => el.dataset.turnId === anchor.turn);
      if (node)
        scroll.current.scrollTop +=
          node.getBoundingClientRect().top - anchor.top;
      prependAnchor.current = null;
    } else followOutput();
  }, [outbox, turns, questions, followOutput]);
  async function loadOlder() {
    const before = olderCursor.current;
    if (!id || !before || olderRequest.current) return;
    const sid = id;
    const abort = new AbortController();
    olderRequest.current = abort;
    upwardIntent.current = 0;
    setHistoryLoading(true);
    setHistoryError("");
    try {
      const page = await history(sid, before, abort.signal);
      if (abort.signal.aborted || selectedID.current !== sid) return;
      const container = scroll.current;
      const top = container?.getBoundingClientRect().top ?? 0;
      const visible =
        container &&
        Array.from(
          container.querySelectorAll<HTMLElement>("[data-turn-id]"),
        ).find((el) => el.getBoundingClientRect().bottom > top);
      if (visible)
        prependAnchor.current = {
          turn: visible.dataset.turnId!,
          top: visible.getBoundingClientRect().top,
        };
      nearBottom.current = false;
      // Older snapshots cannot overwrite a newer live upsert arriving in flight.
      setOutbox((xs) => mergeRows(page.outbox, xs, (x) => x.turn_id));
      setTurns((xs) => mergeRows(page.turns, xs, (x) => x.turn_id));
      setQuestions((xs) => mergeRows(page.questions, xs, (x) => x.id));
      olderCursor.current = page.before_turn;
      setBeforeTurn(page.before_turn);
    } catch (e) {
      if (!abort.signal.aborted && selectedID.current === sid)
        setHistoryError(String(e));
    } finally {
      if (olderRequest.current === abort) {
        olderRequest.current = null;
        setHistoryLoading(false);
      }
    }
  }
  function userScrollUp() {
    if (olderRequest.current) {
      upwardIntent.current = 0;
      return;
    }
    upwardIntent.current = Date.now();
    if (scroll.current && scroll.current.scrollTop < 160) void loadOlder();
  }
  async function newSession() {
    try {
      const s = await api<Session>("/sessions", "POST");
      setSessions((xs) => [s, ...xs.filter((x) => x.id !== s.id)]);
      setId(s.id);
      setSidebarDrawer(false);
      setPanel("chat");
      return s;
    } catch (e) {
      setError(String(e));
      return undefined;
    }
  }
  async function send() {
    if ((!draft.trim() && !draftImages.length) || sending || uploadingImages)
      return;
    const content = draft;
    const images = draftImages;
    const imagesKey = images.map((image) => image.id).join(",");
    setSending(true);
    try {
      const s = current ?? (await newSession());
      if (!s) return;
      const client =
        sendAttempt.current?.sid === s.id &&
        sendAttempt.current.content === content &&
        sendAttempt.current.imagesKey === imagesKey
          ? sendAttempt.current.client
          : crypto.randomUUID();
      sendAttempt.current = { sid: s.id, content, client, imagesKey };
      const optimistic = {
        id: client,
        turn_id: client,
        client_msg_id: client,
        content,
        images,
        status: "queued",
        error: "",
        outcome: "",
      };
      setOutbox((xs) => [...xs, optimistic]);
      setDraft("");
      setDraftImages([]);
      nearBottom.current = true;
      try {
        const result = await api<Outbox>(`/sessions/${s.id}/messages`, "POST", {
          message: content,
          client_msg_id: client,
          image_ids: images.map((image) => image.id),
          ...s.options,
        });
        setOutbox((xs) => [
          ...xs.filter((x) => x.client_msg_id !== client),
          result,
        ]);
        sendAttempt.current = null;
      } catch (e) {
        setDraft(content);
        if (selectedID.current === s.id) setDraftImages(images);
        setOutbox((xs) => xs.filter((x) => x.client_msg_id !== client));
        throw e;
      }
    } catch (e) {
      setError(String(e));
    } finally {
      setSending(false);
    }
  }
  async function addImages(files: File[]) {
    if (!files.length) return;
    if (uploadPending.current || sending) {
      setError("Wait for the current image upload or message to finish.");
      return;
    }
    if (draftImages.length + files.length > 4) {
      setError("You can attach up to 4 images to each message.");
      return;
    }
    if (
      files.some(
        (file) =>
          !["image/png", "image/jpeg", "image/webp", "image/gif"].includes(
            file.type,
          ),
      )
    ) {
      setError("Supported image formats are PNG, JPEG, WebP, and GIF.");
      return;
    }
    if (files.some((file) => file.size > 5 * 1024 * 1024)) {
      setError("Each image must be 5 MB or smaller.");
      return;
    }
    if (
      draftImages.reduce((n, image) => n + image.attachment.bytes, 0) +
        files.reduce((n, file) => n + file.size, 0) >
      10 * 1024 * 1024
    ) {
      setError("The total image size per message cannot exceed 10 MB.");
      return;
    }
    uploadPending.current = true;
    setUploadingImages(true);
    try {
      const session = current ?? (await newSession());
      if (!session) return;
      for (const file of files) {
        const data = await new Promise<string>((resolve, reject) => {
          const reader = new FileReader();
          reader.onload = () => resolve(String(reader.result).split(",")[1]);
          reader.onerror = () => reject(Error("Failed to read the image."));
          reader.readAsDataURL(file);
        });
        const image = await api<MessageImage>(
          `/sessions/${session.id}/images`,
          "POST",
          { name: file.name, media_type: file.type, data },
        );
        if (selectedID.current !== session.id) break;
        setDraftImages((images) => [...images, image]);
      }
    } catch (e) {
      setError(String(e));
    } finally {
      uploadPending.current = false;
      setUploadingImages(false);
    }
  }
  async function options(p: Partial<Options>) {
    if (!current) return;
    try {
      const s = await api<Session>(`/sessions/${id}`, "PATCH", {
        options: { ...current.options, ...p },
      });
      setSessions((xs) => xs.map((x) => (x.id === id ? s : x)));
    } catch (e) {
      setError(String(e));
    }
  }
  const suggestions = [
    {
      icon: <Code2 size={20} />,
      title: "Write some code",
      desc: "Create a script and run it in the sandbox",
      prompt:
        "Write a Python data analysis example in the sandbox, generate sample data, run the script, and share the results and file path.",
    },
    {
      icon: <FileText size={20} />,
      title: "Analyze a dataset",
      desc: "Clean data and generate insights",
      prompt:
        "Create a sample sales CSV in the sandbox, remove duplicates and handle missing values, summarize sales by month, and save the results.",
    },
    {
      icon: <Compass size={20} />,
      title: "Explore the cloud environment",
      desc: "Inspect the environment and try a command",
      prompt:
        "Check the Linux, Python, and Node versions in the current sandbox, show the working directory, and briefly explain what you can help me do.",
    },
  ];
  const sidebar = (
    <aside className="sidebar">
      <Button
        className="new-session"
        variant="primary"
        aria-label="New chat"
        onPress={newSession}
      >
        <Plus size={18} />
        <span className="nav-label">New chat</span>
      </Button>
      <div className="session-search">
        <Search size={17} />
        <Input
          aria-label="Search chats"
          placeholder="Search chats…"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        />
      </div>
      <Button
        variant={panel === "library" ? "secondary" : "ghost"}
        className={
          "session-item library-nav" +
          (panel === "library" ? " is-selected" : "")
        }
        aria-label="Library"
        aria-current={panel === "library" ? "page" : undefined}
        onPress={() => {
          setPanel("library");
          setSidebarDrawer(false);
        }}
      >
        <LibraryIcon size={18} />
        <span className="session-details">
          <strong>Library</strong>
        </span>
      </Button>
      <div className="sidebar-label">
        <span>Recent chats</span>
        <span>{sessions.length}</span>
      </div>
      <div className="session-list">
        {sessions
          .filter((s) => s.title.toLowerCase().includes(filter.toLowerCase()))
          .map((s) => (
            <Button
              key={s.id}
              variant={id === s.id ? "secondary" : "ghost"}
              className={"session-item" + (id === s.id ? " is-selected" : "")}
              aria-current={id === s.id ? "page" : undefined}
              onPress={() => {
                setId(s.id);
                setPanel("chat");
                setSidebarDrawer(false);
              }}
            >
              <MessageSquare size={18} />
              <span className="session-details">
                <strong title={s.title}>{s.title}</strong>
                <span>
                  <i
                    className={
                      "dot " +
                      (s.sandbox.state === "running"
                        ? "online"
                        : s.sandbox.state === "paused"
                          ? "paused"
                          : "")
                    }
                  />
                  {sandboxLabels[s.sandbox.state]}
                  <time>
                    {new Date(s.last_active_at).toLocaleDateString("en-US", {
                      month: "numeric",
                      day: "numeric",
                    })}
                  </time>
                </span>
              </span>
              {s.agent_state !== "idle" && <span className="pulse" />}
            </Button>
          ))}
        {sessions.length === 0 && (
          <p className="empty-list">
            Every new task
            <br />
            starts with a new conversation.
          </p>
        )}
      </div>
      <div className="sidebar-footer">
        <Button
          variant="ghost"
          aria-label="Settings"
          onPress={() => {
            setSidebarDrawer(false);
            setSettings(true);
          }}
        >
          <SettingsIcon size={18} />
          <span className="nav-label">Settings</span>
        </Button>
      </div>
    </aside>
  );
  const modelControls = current && (
    <div className="model-bar">
      <span
        className="model-label"
        title="deepseek-flash"
        aria-label="Model: DeepSeek Flash"
      >
        <Cloud size={16} />
        DeepSeek Flash
      </span>
      <span className="toolbar-divider" />
      <Toggle
        label="Plan"
        value={current.options.plan}
        onChange={(v) => options({ plan: v })}
      />
      <div
        className="thinking-control"
        title="Reasoning effort affects depth and response time. Changes apply to the next turn."
      >
        <BrainCircuit size={16} />
        <span>Reasoning</span>
        <Picker
          label="Reasoning effort"
          value={current.options.thinking}
          values={["off", "low", "high", "max"].map((x) => [x, x])}
          onChange={(v) => options({ thinking: v })}
        />
      </div>
    </div>
  );
  return (
    <TurnTraceProvider key={id}>
      <div
        className={`app ${sidebarExpanded ? "sidebar-expanded" : "sidebar-collapsed"}${focused ? " is-focused" : ""}`}
      >
        <header className="topbar">
          <div className="brand">
            <span className="brand-mark">
              <Cloud size={19} />
            </span>
            <strong>Nimbus</strong>
          </div>
          <Button
            isIconOnly
            size="sm"
            variant="ghost"
            aria-label={
              !desktop
                ? "Show sessions"
                : sidebarExpanded && !focused
                  ? "Collapse sidebar"
                  : "Expand sidebar"
            }
            aria-expanded={
              desktop ? sidebarExpanded && !focused : sidebarDrawer
            }
            onPress={() => {
              if (!desktop) setSidebarDrawer(true);
              else {
                setFocused(false);
                setSidebarOverride(focused || !sidebarExpanded);
              }
            }}
          >
            <PanelLeft size={18} />
          </Button>
          <div className="workspace-heading">
            {rename !== null && panel === "chat" ? (
              <div className="row">
                <Input
                  aria-label="Chat title"
                  value={rename}
                  onChange={(e) => setRename(e.target.value)}
                />
                <Button
                  isIconOnly
                  size="sm"
                  aria-label="Save title"
                  onPress={() =>
                    api(`/sessions/${id}`, "PATCH", { title: rename })
                      .then(() => setRename(null))
                      .catch((e) => setError(e.message))
                  }
                >
                  <Check size={15} />
                </Button>
                <Button
                  isIconOnly
                  size="sm"
                  variant="ghost"
                  aria-label="Cancel rename"
                  onPress={() => setRename(null)}
                >
                  <X size={15} />
                </Button>
              </div>
            ) : (
              <h1
                className="conversation-title"
                title={panel === "library" ? "Library" : current?.title}
              >
                {panel === "library"
                  ? "Library"
                  : (current?.title ?? "New conversation")}
              </h1>
            )}
          </div>
          <div className="top-actions">
            <span
              className="workspace-status"
              title={
                online
                  ? `Sandbox: ${sandboxLabels[current?.sandbox.state ?? "not_started"]}`
                  : "Reconnecting to service"
              }
            >
              <span
                className={
                  "dot " +
                  (online
                    ? current?.sandbox.state === "running"
                      ? "online"
                      : current?.sandbox.state === "paused"
                        ? "paused"
                        : ""
                    : "")
                }
              />
              <span className="toolbar-label">
                {online
                  ? sandboxLabels[current?.sandbox.state ?? "not_started"]
                  : "Reconnecting…"}
              </span>
            </span>
            {panel === "chat" && current && (
              <Button
                size="sm"
                variant="ghost"
                className={
                  "trajectory-toggle" +
                  (view === "trajectory" ? " is-active" : "")
                }
                aria-label={view === "chat" ? "Activity" : "Back to chat"}
                onPress={() => setView(view === "chat" ? "trajectory" : "chat")}
              >
                <Workflow size={17} />
                <span className="toolbar-label">
                  {view === "chat" ? "Activity" : "Chat"}
                </span>
              </Button>
            )}
            <Button
              size="sm"
              variant={terminalVisible ? "secondary" : "ghost"}
              aria-label="Terminal"
              aria-expanded={terminalVisible}
              onPress={() => {
                if (focused) {
                  setFocused(false);
                  setTerminalOpen(true);
                } else setTerminalOpen(!terminalOpen);
              }}
            >
              <TerminalSquare size={17} />
              <span className="toolbar-label">Terminal</span>
            </Button>
            <Button
              isIconOnly
              size="sm"
              variant="ghost"
              className="focus-toggle"
              aria-label={focused ? "Exit focus mode" : "Focus mode"}
              onPress={() => setFocused(!focused)}
            >
              {focused ? <Minimize2 size={17} /> : <Maximize2 size={17} />}
            </Button>
            <Button
              isIconOnly
              size="sm"
              variant="ghost"
              aria-label="Chat actions"
              onPress={() => setChatActions(true)}
            >
              <MoreHorizontal size={19} />
            </Button>
          </div>
        </header>
        {error && (
          <div className="error-banner" role="alert">
            <span>{error}</span>
            <Button
              isIconOnly
              size="sm"
              variant="ghost"
              aria-label="Dismiss alert"
              onPress={() => setError("")}
            >
              <X size={14} />
            </Button>
          </div>
        )}
        <main
          className={
            "workspace" +
            (terminalVisible && terminalSplit ? " terminal-split" : "")
          }
        >
          {desktop && !focused && sidebar}
          <section className="chat-panel">
            {panel === "library" ? (
              <Library
                onOpenFile={(id) => setPreview({ id })}
                onError={setError}
              />
            ) : (
              <>
                <div
                  ref={scroll}
                  className="conversation"
                  tabIndex={0}
                  onWheel={(e) => {
                    if (e.deltaY < 0) userScrollUp();
                  }}
                  onTouchStart={(e) => {
                    touchY.current = e.touches[0]?.clientY ?? null;
                  }}
                  onTouchMove={(e) => {
                    const next = e.touches[0]?.clientY;
                    if (
                      next != null &&
                      touchY.current != null &&
                      next > touchY.current
                    )
                      userScrollUp();
                    touchY.current = next ?? null;
                  }}
                  onKeyDown={(e) => {
                    if (["ArrowUp", "PageUp", "Home"].includes(e.key))
                      userScrollUp();
                  }}
                  onScroll={() => {
                    if (!historyVisible) return;
                    const e = scroll.current!;
                    readingPosition.current = e.scrollTop;
                    nearBottom.current =
                      e.scrollHeight - e.scrollTop - e.clientHeight < 120;
                    if (
                      e.scrollTop < 160 &&
                      Date.now() - upwardIntent.current < 500
                    )
                      void loadOlder();
                  }}
                >
                  {historyLoading && <p role="status">Loading conversation…</p>}
                  {historyError && (
                    <div role="alert">
                      {historyError}{" "}
                      <button
                        type="button"
                        onClick={() =>
                          beforeTurn
                            ? void loadOlder()
                            : setHistoryAttempt((n) => n + 1)
                        }
                      >
                        Retry conversation history
                      </button>
                    </div>
                  )}
                  {beforeTurn && !historyLoading && (
                    <p className="history-hint">
                      Scroll up to load older turns
                    </p>
                  )}
                  {!historyVisible ||
                  (historySession !== id && id) ? null : outbox.length === 0 ? (
                    <div className="welcome">
                      <div className="welcome-symbol">
                        <Cloud size={38} strokeWidth={1.5} />
                      </div>
                      <h2>Give Nimbus your next task.</h2>
                      <p>
                        Describe what you need, and we’ll work through it
                        together.
                        <br />
                        Write code, analyze data, and run commands in a
                        dedicated cloud environment.
                      </p>
                      <div className="suggestions">
                        {suggestions.map((s) => (
                          <Button
                            key={s.title}
                            variant="outline"
                            className="suggestion"
                            onPress={() => setDraft(s.prompt)}
                          >
                            <span className="suggestion-icon">{s.icon}</span>
                            <span className="suggestion-copy">
                              <strong>{s.title}</strong>
                              <span>{s.desc}</span>
                            </span>
                            <ChevronRight
                              size={16}
                              className="suggestion-chevron"
                            />
                          </Button>
                        ))}
                      </div>
                      <span className="welcome-note">
                        <span className="dot online" />
                        Start with a conversation. The sandbox starts when a
                        task needs it.
                      </span>
                    </div>
                  ) : view === "trajectory" ? (
                    <div className="trajectory">
                      {outbox.map((o) => (
                        <TrajectoryTurn
                          key={o.turn_id}
                          sessionId={id}
                          turnId={o.turn_id}
                          title={o.content}
                          running={!o.outcome && o.status !== "failed"}
                        />
                      ))}
                    </div>
                  ) : (
                    <Conversation
                      sessionId={id}
                      turns={turns}
                      outbox={outbox}
                      questions={questions}
                      retryError={setError}
                      onOutput={followOutput}
                      onOpenLibraryFile={(fileId) => setPreview({ id: fileId })}
                    />
                  )}
                </div>
                <div className="composer-wrap">
                  <div
                    className={
                      "composer" + (draggingImage ? " is-dragging" : "")
                    }
                    onDragOver={(e) => {
                      if (e.dataTransfer.types.includes("Files")) {
                        e.preventDefault();
                        setDraggingImage(true);
                      }
                    }}
                    onDragLeave={(e) => {
                      if (!e.currentTarget.contains(e.relatedTarget as Node))
                        setDraggingImage(false);
                    }}
                    onDrop={(e) => {
                      e.preventDefault();
                      setDraggingImage(false);
                      void addImages(Array.from(e.dataTransfer.files));
                    }}
                  >
                    <ImageGallery
                      images={draftImages}
                      onRemove={(imageID) =>
                        setDraftImages((images) =>
                          images.filter((image) => image.id !== imageID),
                        )
                      }
                    />
                    <TextArea
                      ref={composerInput}
                      aria-label="Send a message"
                      placeholder={
                        viewport < 768
                          ? "Message Nimbus…"
                          : "Send a message, paste an image, or describe what you want to do…"
                      }
                      value={draft}
                      onChange={(e) => setDraft(e.target.value)}
                      onPaste={(e) => {
                        const images = Array.from(e.clipboardData.items)
                          .filter((item) => item.type.startsWith("image/"))
                          .map((item) => item.getAsFile())
                          .filter((file): file is File => !!file);
                        if (images.length) {
                          e.preventDefault();
                          void addImages(images);
                        }
                      }}
                      onKeyDown={(e) => {
                        if (
                          e.key === "Enter" &&
                          !e.shiftKey &&
                          !e.nativeEvent.isComposing
                        ) {
                          e.preventDefault();
                          send();
                        }
                      }}
                      rows={1}
                    />
                    <div className="composer-bottom">
                      <div className="composer-tools">
                        <input
                          ref={imageInput}
                          type="file"
                          accept="image/png,image/jpeg,image/webp,image/gif"
                          multiple
                          hidden
                          aria-label="Choose images"
                          onChange={(e) => {
                            void addImages(Array.from(e.target.files ?? []));
                            e.target.value = "";
                          }}
                        />
                        <span title="Attach images by selecting, pasting, or dropping them · Up to 4 images, 5 MB each">
                          <Button
                            isIconOnly
                            size="sm"
                            variant="ghost"
                            aria-label="Attach images"
                            isPending={uploadingImages}
                            isDisabled={sending}
                            onPress={() => imageInput.current?.click()}
                          >
                            <ImagePlus size={18} />
                          </Button>
                        </span>
                        {(draggingImage || uploadingImages) && (
                          <span className="composer-hint" role="status">
                            {draggingImage
                              ? "Release to attach images"
                              : "Uploading images…"}
                          </span>
                        )}
                        {viewport >= 768
                          ? modelControls
                          : current && (
                              <Button
                                size="sm"
                                variant="ghost"
                                className="compact-model-options"
                                aria-label="Model options"
                                onPress={() => setModelOptions(true)}
                              >
                                <Cloud size={15} />
                                <span>
                                  Flash
                                  {current.options.thinking !== "off"
                                    ? ` · ${current.options.thinking}`
                                    : ""}
                                  {current.options.plan ? " · Plan" : ""}
                                </span>
                                <ChevronDown size={13} />
                              </Button>
                            )}
                      </div>
                      <div className="row">
                        {current && current.agent_state !== "idle" && (
                          <Button
                            size="sm"
                            variant="secondary"
                            onPress={() =>
                              api(`/sessions/${id}/stop`, "POST").catch((e) =>
                                setError(e.message),
                              )
                            }
                          >
                            <Square size={12} />
                            Stop
                          </Button>
                        )}
                        <Button
                          isIconOnly
                          aria-label="Send"
                          variant="primary"
                          className="send-button"
                          isDisabled={
                            (!draft.trim() && !draftImages.length) ||
                            uploadingImages
                          }
                          isPending={sending}
                          onPress={send}
                        >
                          <ArrowUp size={20} />
                        </Button>
                      </div>
                    </div>
                  </div>
                </div>
              </>
            )}
            <Modal isOpen={modelOptions} onOpenChange={setModelOptions}>
              <Modal.Backdrop>
                <Modal.Container>
                  <Modal.Dialog className="model-options-dialog">
                    <Modal.CloseTrigger />
                    <Modal.Header>
                      <Modal.Heading>Model options</Modal.Heading>
                    </Modal.Header>
                    <Modal.Body>{modelControls}</Modal.Body>
                  </Modal.Dialog>
                </Modal.Container>
              </Modal.Backdrop>
            </Modal>
          </section>
          {terminalVisible && terminalSplit && (
            <>
              <PanelDivider />
              <div className="terminal-dock">
                <TerminalPanel
                  session={current}
                  onError={setError}
                  onClose={() => setTerminalOpen(false)}
                />
              </div>
            </>
          )}
        </main>
        {sidebarDrawer && (
          <Modal isOpen onOpenChange={setSidebarDrawer}>
            <Modal.Backdrop className="workspace-overlay-backdrop">
              <Modal.Container
                size="full"
                className="workspace-overlay-container"
              >
                <Modal.Dialog className="sidebar-drawer-dialog">
                  <Modal.CloseTrigger />
                  <Modal.Header>
                    <Modal.Heading>Sessions</Modal.Heading>
                  </Modal.Header>
                  {sidebar}
                </Modal.Dialog>
              </Modal.Container>
            </Modal.Backdrop>
          </Modal>
        )}
        {terminalVisible && !terminalSplit && (
          <Modal
            isOpen
            onOpenChange={(open) => {
              if (!open) setTerminalOpen(false);
            }}
          >
            <Modal.Backdrop className="workspace-overlay-backdrop">
              <Modal.Container
                size="full"
                className="workspace-overlay-container"
              >
                <Modal.Dialog className="terminal-overlay-dialog">
                  <Modal.CloseTrigger aria-label="Close terminal" />
                  <Modal.Header>
                    <Modal.Heading>Terminal</Modal.Heading>
                  </Modal.Header>
                  <TerminalPanel session={current} onError={setError} />
                </Modal.Dialog>
              </Modal.Container>
            </Modal.Backdrop>
          </Modal>
        )}
        <Modal isOpen={chatActions} onOpenChange={setChatActions}>
          <Modal.Backdrop>
            <Modal.Container>
              <Modal.Dialog className="chat-actions-dialog">
                <Modal.CloseTrigger />
                <Modal.Header>
                  <Modal.Heading>Chat actions</Modal.Heading>
                </Modal.Header>
                <Modal.Body>
                  {current && panel === "chat" && (
                    <>
                      <Button
                        variant="ghost"
                        onPress={() => {
                          setRename(current.title);
                          setChatActions(false);
                        }}
                      >
                        <Pencil size={17} />
                        Rename chat
                      </Button>
                      <Button
                        variant="ghost"
                        onPress={() => {
                          setChatActions(false);
                          setDeleteID(id);
                        }}
                      >
                        <Trash2 size={17} />
                        Delete chat
                      </Button>
                    </>
                  )}
                  <Button
                    variant="ghost"
                    onPress={() => {
                      setChatActions(false);
                      setSettings(true);
                    }}
                  >
                    <SettingsIcon size={17} />
                    Settings
                  </Button>
                  <p className="engine-note">
                    <Braces size={14} />
                    Tools run through Wasm PTC
                  </p>
                </Modal.Body>
              </Modal.Dialog>
            </Modal.Container>
          </Modal.Backdrop>
        </Modal>
        <Settings
          open={settings}
          onClose={() => setSettings(false)}
          onError={setError}
        />
        <LibraryPreview
          fileId={preview?.id ?? null}
          version={preview?.version}
          onClose={() => setPreview(null)}
          onError={setError}
          onOpenLibraryFile={(fileId) => setPreview({ id: fileId })}
        />
        <Modal
          isOpen={!!deleteID}
          onOpenChange={(v) => {
            if (!v) setDeleteID("");
          }}
        >
          <Modal.Backdrop>
            <Modal.Container>
              <Modal.Dialog>
                <Modal.CloseTrigger />
                <Modal.Header>
                  <Modal.Heading>Delete this chat?</Modal.Heading>
                </Modal.Header>
                <Modal.Body>
                  <p>
                    This permanently deletes the dedicated sandbox and the
                    entire chat history.
                  </p>
                </Modal.Body>
                <Modal.Footer>
                  <Button variant="secondary" onPress={() => setDeleteID("")}>
                    Cancel
                  </Button>
                  <Button
                    variant="danger"
                    onPress={() =>
                      api(`/sessions/${deleteID}`, "DELETE")
                        .then(() => {
                          const remaining = sessions.filter(
                            (s) => s.id !== deleteID,
                          );
                          setSessions(remaining);
                          setId((selected) =>
                            selected === deleteID
                              ? (remaining[0]?.id ?? "")
                              : selected,
                          );
                          setDeleteID("");
                        })
                        .catch((e) => setError(e.message))
                    }
                  >
                    Delete chat
                  </Button>
                </Modal.Footer>
              </Modal.Dialog>
            </Modal.Container>
          </Modal.Backdrop>
        </Modal>
      </div>
    </TurnTraceProvider>
  );
}
