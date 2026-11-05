import { useEffect, useRef, useState } from "react";
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
  Layers3,
  Workflow,
  BrainCircuit,
  Braces,
  ImagePlus,
  ChevronRight,
  Code2,
  FileText,
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
  type WireEvent,
  type StreamFrame,
  sandboxLabels,
  type Session,
  type Event,
  type Outbox,
  type Options,
  type MessageImage,
  type UserQuestion,
} from "./api";
import { decodeEvent } from "./event-codec";
import { Picker, Toggle, Fold } from "./components";
import TerminalPanel from "./Terminal";
import Settings from "./Settings";
import Conversation from "./Conversation";
import PanelDivider from "./PanelDivider";
import ImageGallery from "./ImageGallery";
export default function App() {
  const [sessions, setSessions] = useState<Session[]>([]),
    [id, setId] = useState(location.hash.slice(1)),
    [events, setEvents] = useState<Event[]>([]),
    [outbox, setOutbox] = useState<Outbox[]>([]),
    [frames, setFrames] = useState<StreamFrame[]>([]);
  const [questions, setQuestions] = useState<UserQuestion[]>([]);
  const [draft, setDraft] = useState(""),
    [error, setError] = useState(""),
    [settings, setSettings] = useState(location.search.includes("settings=")),
    [filter, setFilter] = useState(""),
    [tab, setTab] = useState("chat"),
    [view, setView] = useState("chat"),
    [online, setOnline] = useState(true),
    [sending, setSending] = useState(false),
    [rename, setRename] = useState<string | null>(null),
    [deleteID, setDeleteID] = useState("");
  const [draftImages, setDraftImages] = useState<MessageImage[]>([]);
  const [uploadingImages, setUploadingImages] = useState(false);
  const [draggingImage, setDraggingImage] = useState(false);
  const imageInput = useRef<HTMLInputElement>(null);
  const uploadPending = useRef(false);
  const selectedID = useRef(id);
  selectedID.current = id;
  const sendAttempt = useRef<{
    sid: string;
    content: string;
    client: string;
    imagesKey: string;
  } | null>(null);
  const end = useRef<HTMLDivElement>(null),
    scroll = useRef<HTMLDivElement>(null),
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
    setEvents([]);
    setOutbox([]);
    setFrames([]);
    setQuestions([]);
    setDraftImages([]);
    if (!id) return;
    let active = true;
    let source: EventSource;
    nearBottom.current = true;
    (async () => {
      try {
        let after = -1,
          all: Event[] = [];
        while (true) {
          const d = await history(id, after);
          if (!active) return;
          all = [...all, ...d.events];
          setOutbox(d.outbox);
          setFrames(d.frames ?? []);
          setQuestions(d.questions ?? []);
          if (!d.has_more) break;
          after = d.events.at(-1)?.seq ?? after;
        }
        setEvents(all);
        after = all.at(-1)?.seq ?? -1;
        source = new EventSource(
          `/api/sessions/${id}/stream?after_seq=${after}`,
        );
        source.onopen = () => setOnline(true);
        source.onerror = () => setOnline(false);
        source.addEventListener("session", (e) => {
          const v = decodeEvent(decodeJSON<WireEvent>(e.data));
          setEvents((xs) =>
            xs.some((x) => x.seq === v.seq)
              ? xs
              : [...xs, v].sort((a, b) => a.seq - b.seq),
          );
        });
        source.addEventListener("state", (e) => {
          const v = decodeJSON<Session>(e.data);
          setSessions((xs) => xs.map((x) => (x.id === v.id ? v : x)));
        });
        source.addEventListener("outbox", (e) =>
          setOutbox(decodeJSON<Outbox[]>(e.data)),
        );
        source.addEventListener("questions", (e) =>
          setQuestions(decodeJSON<UserQuestion[]>(e.data)),
        );
        source.addEventListener("frame", (e) => {
          const v = decodeJSON<StreamFrame>(e.data);
          setFrames((xs) => (xs.some((x) => x.id === v.id) ? xs : [...xs, v]));
        });
      } catch (e) {
        setError(String(e));
      }
    })();
    return () => {
      active = false;
      source?.close();
    };
  }, [id]);
  useEffect(() => {
    if (nearBottom.current)
      end.current?.scrollIntoView({ behavior: "instant", block: "end" });
  }, [events, frames, outbox, questions]);
  async function newSession() {
    try {
      const s = await api<Session>("/sessions", "POST");
      setSessions((xs) => [s, ...xs.filter((x) => x.id !== s.id)]);
      setId(s.id);
      setTab("chat");
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
  return (
    <div className="app">
      <header className="topbar">
        <div className="brand">
          <span className="brand-mark">
            <Cloud size={24} />
          </span>
          <strong>Nimbus</strong>
          <span className="brand-divider" />
          <span>Cloud Agent workspace</span>
        </div>
        <div className="top-actions">
          <span className="connection">
            <span className={"dot " + (online ? "online" : "")} />
            {online ? "Connected to local service" : "Reconnecting…"}
          </span>
          <Button size="sm" variant="ghost" onPress={() => setSettings(true)}>
            <SettingsIcon size={18} />
            Settings
          </Button>
        </div>
      </header>
      <nav className="mobile-tabs">
        {[
          ["sessions", "Sessions", <PanelLeft size={16} />],
          ["chat", "Chat", <MessageSquare size={16} />],
          ["terminal", "Terminal", <TerminalSquare size={16} />],
        ].map(([k, t, icon]) => (
          <Button
            key={String(k)}
            variant={tab === k ? "secondary" : "ghost"}
            onPress={() => setTab(String(k))}
          >
            {icon}
            {t}
          </Button>
        ))}
      </nav>
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
      <main className={"workspace show-" + tab}>
        <aside className="sidebar">
          <div className="sidebar-intro">
            <span>My workspace</span>
            <Layers3 size={16} />
          </div>
          <Button
            className="new-session"
            variant="primary"
            onPress={newSession}
          >
            <Plus size={18} />
            New chat
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
          <div className="sidebar-label">
            <span>Recent chats</span>
            <span>{sessions.length}</span>
          </div>
          <div className="session-list">
            {sessions
              .filter((s) =>
                s.title.toLowerCase().includes(filter.toLowerCase()),
              )
              .map((s) => (
                <Button
                  key={s.id}
                  variant={id === s.id ? "secondary" : "ghost"}
                  className={
                    "session-item" + (id === s.id ? " is-selected" : "")
                  }
                  aria-current={id === s.id ? "page" : undefined}
                  onPress={() => {
                    setId(s.id);
                    setTab("chat");
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
                        {new Date(s.last_active_at).toLocaleDateString(
                          "en-US",
                          { month: "numeric", day: "numeric" },
                        )}
                      </time>
                    </span>
                    {s.sandbox.e2b_sandbox_id && (
                      <span
                        className="session-sandbox"
                        title={s.sandbox.e2b_sandbox_id}
                      >
                        <span>{s.sandbox.info?.region ?? "us-west-1"}</span>
                        <code>{s.sandbox.e2b_sandbox_id.slice(0, 8)}…</code>
                      </span>
                    )}
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
            <span className="avatar">
              <Cloud size={19} />
            </span>
            <div>
              <strong>Local workspace</strong>
              <small>DeepSeek + E2B Cloud</small>
            </div>
          </div>
        </aside>
        <section className="chat-panel">
          <div className="chat-heading">
            <div>
              {rename !== null ? (
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
                        .then(() => {
                          setRename(null);
                        })
                        .catch((e) => setError(e.message))
                    }
                  >
                    <Check size={15} />
                  </Button>
                </div>
              ) : (
                <h1>
                  <span className="conversation-title" title={current?.title}>
                    {current?.title ?? "Start something new"}
                  </span>
                  {current && (
                    <Button
                      isIconOnly
                      size="sm"
                      variant="ghost"
                      aria-label="Rename chat"
                      onPress={() => setRename(current.title)}
                    >
                      <Pencil size={16} />
                    </Button>
                  )}
                </h1>
              )}
              <p>
                <span className="dot online" />
                Chats are saved automatically. The sandbox runs on demand.
              </p>
            </div>
            <div className="row">
              <Button
                className={
                  "trajectory-toggle" +
                  (view === "trajectory" ? " is-active" : "")
                }
                size="sm"
                variant="ghost"
                onPress={() => setView(view === "chat" ? "trajectory" : "chat")}
              >
                <Workflow size={17} />
                {view === "chat" ? "Activity" : "Back to chat"}
              </Button>
              {current && (
                <Button
                  isIconOnly
                  size="sm"
                  variant="ghost"
                  aria-label="Delete chat"
                  onPress={() => setDeleteID(id)}
                >
                  <Trash2 size={17} />
                </Button>
              )}
            </div>
          </div>
          {current && (
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
              <span className="toolbar-divider" />
              <span className="model-label" title="Wasm PTC">
                <Braces size={16} />
                Wasm PTC
              </span>
            </div>
          )}
          <div
            ref={scroll}
            className="conversation"
            onScroll={() => {
              const e = scroll.current!;
              nearBottom.current =
                e.scrollHeight - e.scrollTop - e.clientHeight < 120;
            }}
          >
            {outbox.length === 0 ? (
              <div className="welcome">
                <div className="welcome-symbol">
                  <Cloud size={38} strokeWidth={1.5} />
                </div>
                <h2>Give Nimbus your next task.</h2>
                <p>
                  Describe what you need, and we’ll work through it together.
                  <br />
                  Write code, analyze data, and run commands in a dedicated cloud environment.
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
                      <ChevronRight size={16} className="suggestion-chevron" />
                    </Button>
                  ))}
                </div>
                <span className="welcome-note">
                  <span className="dot online" />
                  Start with a conversation. The sandbox starts when a task needs it.
                </span>
              </div>
            ) : view === "trajectory" ? (
              <div className="trajectory">
                {events.map((e) => (
                  <Fold
                    key={e.seq}
                    title={
                      <span>
                        #{e.seq} {e.type}
                      </span>
                    }
                  >
                    <pre>{JSON.stringify(e.payload, null, 2)}</pre>
                  </Fold>
                ))}
              </div>
            ) : (
              <Conversation
                sessionId={id}
                events={events}
                outbox={outbox}
                frames={frames}
                questions={questions}
                retryError={setError}
              />
            )}
            <div ref={end} />
          </div>
          <div className="composer-wrap">
            <div
              className={"composer" + (draggingImage ? " is-dragging" : "")}
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
                aria-label="Send a message"
                placeholder="Send a message, paste an image, or describe what you want to do…"
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
                rows={2}
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
                  <span>
                    <span
                      className={
                        "dot " +
                        (current?.sandbox.state === "running" ? "online" : "")
                      }
                    />
                    {draggingImage
                      ? "Release to attach images"
                      : uploadingImages
                        ? "Uploading images…"
                        : current?.sandbox.state === "running"
                          ? "Connected to your sandbox"
                          : "Sandbox starts on demand"}
                  </span>
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
                      (!draft.trim() && !draftImages.length) || uploadingImages
                    }
                    isPending={sending}
                    onPress={send}
                  >
                    <ArrowUp size={20} />
                  </Button>
                </div>
              </div>
            </div>
            <div className="composer-note">
              <span>
                <kbd>Enter</kbd> to send <kbd>Shift + Enter</kbd> for a new line
              </span>
              <span>
                {"Tools run through Wasm PTC"}
              </span>
            </div>
          </div>
        </section>
        <PanelDivider />
        <TerminalPanel session={current} onError={setError} />
      </main>
      <Settings
        open={settings}
        onClose={() => setSettings(false)}
        onError={setError}
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
                <p>This permanently deletes the dedicated sandbox and the entire chat history.</p>
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
                        setEvents([]);
                        setOutbox([]);
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
  );
}
