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
  LockKeyhole,
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
        setSessions((xs) =>
          xs.map((x) => (x.id === s.id ? { ...x, mode_locked: true } : x)),
        );
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
      setError("请等当前图片上传或消息提交完成");
      return;
    }
    if (draftImages.length + files.length > 4) {
      setError("每条消息最多 4 张图片");
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
      setError("支持 PNG、JPEG、WebP 和 GIF 图片");
      return;
    }
    if (files.some((file) => file.size > 5 * 1024 * 1024)) {
      setError("每张图片不能超过 5 MB");
      return;
    }
    if (
      draftImages.reduce((n, image) => n + image.attachment.bytes, 0) +
        files.reduce((n, file) => n + file.size, 0) >
      10 * 1024 * 1024
    ) {
      setError("每条消息图片总大小不能超过 10 MB");
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
          reader.onerror = () => reject(Error("图片读取失败"));
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
      title: "写一段代码",
      desc: "编写脚本，并在沙箱里运行",
      prompt:
        "请在沙箱里写一个 Python 数据分析示例，生成示例数据，运行分析脚本并告诉我结果和文件路径。",
    },
    {
      icon: <FileText size={20} />,
      title: "处理一份数据",
      desc: "清洗数据，生成分析结果",
      prompt:
        "在沙箱里生成一份示例销售 CSV，完成去重和缺失值处理，按月份统计销售额并保存结果。",
    },
    {
      icon: <Compass size={20} />,
      title: "探索云端环境",
      desc: "查看环境，试一条命令",
      prompt:
        "请检查当前沙箱的 Linux、Python 和 Node 版本，展示工作目录，并简要介绍我可以让你做什么。",
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
          <span>云端 Agent 工作台</span>
        </div>
        <div className="top-actions">
          <span className="connection">
            <span className={"dot " + (online ? "online" : "")} />
            {online ? "本地服务已连接" : "正在重新连接"}
          </span>
          <Button size="sm" variant="ghost" onPress={() => setSettings(true)}>
            <SettingsIcon size={18} />
            全局设置
          </Button>
        </div>
      </header>
      <nav className="mobile-tabs">
        {[
          ["sessions", "会话", <PanelLeft size={16} />],
          ["chat", "对话", <MessageSquare size={16} />],
          ["terminal", "终端", <TerminalSquare size={16} />],
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
            aria-label="关闭提示"
            onPress={() => setError("")}
          >
            <X size={14} />
          </Button>
        </div>
      )}
      <main className={"workspace show-" + tab}>
        <aside className="sidebar">
          <div className="sidebar-intro">
            <span>我的工作空间</span>
            <Layers3 size={16} />
          </div>
          <Button
            className="new-session"
            variant="primary"
            onPress={newSession}
          >
            <Plus size={18} />
            新建会话
          </Button>
          <div className="session-search">
            <Search size={17} />
            <Input
              aria-label="搜索会话"
              placeholder="搜索会话…"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
            />
          </div>
          <div className="sidebar-label">
            <span>最近会话</span>
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
                          "zh-CN",
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
                每一个新任务，
                <br />
                从一段新对话开始。
              </p>
            )}
          </div>
          <div className="sidebar-footer">
            <span className="avatar">
              <Cloud size={19} />
            </span>
            <div>
              <strong>本地工作空间</strong>
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
                    aria-label="会话标题"
                    value={rename}
                    onChange={(e) => setRename(e.target.value)}
                  />
                  <Button
                    isIconOnly
                    size="sm"
                    aria-label="保存标题"
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
                    {current?.title ?? "开始新的可能"}
                  </span>
                  {current && (
                    <Button
                      isIconOnly
                      size="sm"
                      variant="ghost"
                      aria-label="重命名会话"
                      onPress={() => setRename(current.title)}
                    >
                      <Pencil size={16} />
                    </Button>
                  )}
                </h1>
              )}
              <p>
                <span className="dot online" />
                对话自动保存，沙箱按需运行
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
                {view === "chat" ? "执行轨迹" : "返回对话"}
              </Button>
              {current && (
                <Button
                  isIconOnly
                  size="sm"
                  variant="ghost"
                  aria-label="删除会话"
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
                aria-label="模型：DeepSeek Flash"
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
                title="思考强度影响深度和耗时，下一轮生效"
              >
                <BrainCircuit size={16} />
                <span>思考</span>
                <Picker
                  label="思考强度"
                  value={current.options.thinking}
                  values={["off", "low", "high", "max"].map((x) => [x, x])}
                  onChange={(v) => options({ thinking: v })}
                />
              </div>
              <Picker
                label="工具模式"
                icon={
                  current.mode_locked || sending ? (
                    <LockKeyhole size={14} />
                  ) : (
                    <Braces size={16} />
                  )
                }
                disabled={current.mode_locked || sending}
                description={
                  current.mode_locked
                    ? "工具模式已固定，切换请新建会话"
                    : "首条消息发送后工具模式固定"
                }
                value={current.options.mode}
                values={[
                  ["standard", "标准工具"],
                  ["ptc", "Wasm PTC"],
                ]}
                onChange={(v) => options({ mode: v })}
              />
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
                <h2>把下一件事，交给 Nimbus。</h2>
                <p>
                  描述任务，一起把它完成。
                  <br />
                  写代码、分析数据、运行命令，都有独立的云端环境。
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
                  先聊聊也可以，需要执行时再启动沙箱
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
                aria-label="发送消息"
                placeholder="发消息、粘贴图片，或描述你想完成的任务…"
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
                    aria-label="选择图片"
                    onChange={(e) => {
                      void addImages(Array.from(e.target.files ?? []));
                      e.target.value = "";
                    }}
                  />
                  <span title="添加图片，也可粘贴或拖入 · 最多 4 张，每张 5 MB">
                    <Button
                      isIconOnly
                      size="sm"
                      variant="ghost"
                      aria-label="添加图片"
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
                      ? "松开添加图片"
                      : uploadingImages
                        ? "图片上传中…"
                        : current?.sandbox.state === "running"
                          ? "已连接专属沙箱"
                          : "沙箱按需启动"}
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
                      停止
                    </Button>
                  )}
                  <Button
                    isIconOnly
                    aria-label="发送"
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
                <kbd>Enter</kbd> 发送 <kbd>Shift + Enter</kbd> 换行
              </span>
              <span>
                {current?.mode_locked
                  ? "工具模式已固定 · 切换请新建会话"
                  : "首条消息发送后，工具模式固定"}
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
                <Modal.Heading>删除会话？</Modal.Heading>
              </Modal.Header>
              <Modal.Body>
                <p>这会销毁专属沙箱和全部对话历史，无法恢复。</p>
              </Modal.Body>
              <Modal.Footer>
                <Button variant="secondary" onPress={() => setDeleteID("")}>
                  取消
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
                  确认删除
                </Button>
              </Modal.Footer>
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal>
    </div>
  );
}
