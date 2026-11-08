import { useCallback, useEffect, useRef, useState } from "react";
import { Button } from "@heroui/react";
import {
  ArrowLeft,
  ChevronRight,
  ExternalLink,
  File as FileIcon,
  FileCode2,
  FileImage,
  FileText,
  Folder,
  FolderOpen,
  RefreshCw,
} from "lucide-react";
import {
  sandboxFiles,
  sandboxRawURL,
  type FileEntry,
  type Session,
} from "./api";

const ROOT = "/workspace";
const IMAGE = new Set(["png", "jpg", "jpeg", "gif", "webp", "avif", "bmp", "ico", "svg"]);
const HTML = new Set(["html", "htm"]);
const TEXT = new Set([
  "txt", "md", "markdown", "json", "jsonc", "js", "jsx", "mjs", "cjs", "ts",
  "tsx", "css", "scss", "less", "go", "py", "rb", "rs", "java", "kt", "c", "h",
  "cpp", "hpp", "sh", "bash", "zsh", "fish", "yml", "yaml", "toml", "ini",
  "cfg", "conf", "env", "sql", "xml", "csv", "tsv", "log", "lock", "sum",
  "mod", "gitignore", "dockerfile", "makefile", "proto", "graphql", "vue",
]);

type Preview = { path: string; name: string; size: number };
type DirNode = FileEntry[] | "loading" | { error: string };

function extension(name: string) {
  const dot = name.lastIndexOf(".");
  return dot <= 0 ? "" : name.slice(dot + 1).toLowerCase();
}

// Preview is deliberately narrow: text, HTML and images only. Anything else
// reports its type instead of guessing at a renderer.
function kindOf(name: string): "image" | "html" | "text" | "other" {
  const ext = extension(name);
  if (IMAGE.has(ext)) return "image";
  if (HTML.has(ext)) return "html";
  if (ext === "" || TEXT.has(ext)) return "text";
  return "other";
}

function sizeOf(bytes?: number) {
  if (!bytes) return "";
  if (bytes >= 1 << 20) return `${(bytes / (1 << 20)).toFixed(1)} MB`;
  if (bytes >= 1 << 10) return `${Math.round(bytes / (1 << 10))} KB`;
  return `${bytes} B`;
}

function FileGlyph({ name, size = 15 }: { name: string; size?: number }) {
  const kind = kindOf(name);
  if (kind === "image") return <FileImage size={size} />;
  if (kind === "html") return <FileCode2 size={size} />;
  if (kind === "text") return <FileText size={size} />;
  return <FileIcon size={size} />;
}

export default function FilesView({
  session,
  onError,
}: {
  session?: Session;
  onError: (s: string) => void;
}) {
  const [dirs, setDirs] = useState<Record<string, DirNode>>({});
  const [open, setOpen] = useState<Record<string, boolean>>({ [ROOT]: true });
  const [preview, setPreview] = useState<Preview | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const alive = useRef(true);

  const load = useCallback(
    async (path: string) => {
      if (!session) return;
      setDirs((d) => ({ ...d, [path]: "loading" }));
      try {
        const listing = await sandboxFiles(session.id, path);
        if (!alive.current) return;
        const sorted = [...(listing.entries ?? [])].sort((a, b) =>
          a.type === b.type
            ? a.name.localeCompare(b.name, undefined, { sensitivity: "base" })
            : a.type === "directory"
              ? -1
              : 1,
        );
        setDirs((d) => ({ ...d, [path]: sorted }));
      } catch (e) {
        if (!alive.current) return;
        setDirs((d) => ({ ...d, [path]: { error: String(e) } }));
        onError(String(e));
      }
    },
    [session?.id, onError],
  );

  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  // A different session is a different sandbox: drop the whole tree.
  useEffect(() => {
    setDirs({});
    setOpen({ [ROOT]: true });
    setPreview(null);
    if (session) void load(ROOT);
  }, [session?.id]);

  const refresh = useCallback(async () => {
    const paths = Object.entries(open)
      .filter(([, isOpen]) => isOpen)
      .map(([path]) => path);
    setRefreshing(true);
    await Promise.all(paths.map((path) => load(path)));
    setRefreshing(false);
  }, [open, load]);

  function toggle(entry: FileEntry) {
    const next = !open[entry.path];
    setOpen((o) => ({ ...o, [entry.path]: next }));
    if (next && !dirs[entry.path]) void load(entry.path);
  }

  function renderDir(path: string, depth: number) {
    const node = dirs[path];
    if (node === "loading")
      return (
        <div className="file-note" style={{ paddingLeft: 10 + depth * 14 }}>
          Loading…
        </div>
      );
    if (node && !Array.isArray(node))
      return (
        <div className="file-note is-error" style={{ paddingLeft: 10 + depth * 14 }}>
          {node.error}
        </div>
      );
    if (!node) return null;
    if (node.length === 0)
      return (
        <div className="file-note" style={{ paddingLeft: 10 + depth * 14 }}>
          Empty folder
        </div>
      );
    return node.map((entry) => {
      const indent = 8 + depth * 14;
      if (entry.type === "directory") {
        const expanded = !!open[entry.path];
        return (
          <div key={entry.path}>
            <button
              type="button"
              className="file-row is-dir"
              style={{ paddingLeft: indent }}
              aria-expanded={expanded}
              onClick={() => toggle(entry)}
            >
              <ChevronRight size={13} className={"file-chevron" + (expanded ? " is-open" : "")} />
              {expanded ? <FolderOpen size={15} /> : <Folder size={15} />}
              <span className="file-name">{entry.name}</span>
            </button>
            {expanded && renderDir(entry.path, depth + 1)}
          </div>
        );
      }
      return (
        <button
          key={entry.path}
          type="button"
          className={
            "file-row" + (preview?.path === entry.path ? " is-selected" : "")
          }
          style={{ paddingLeft: indent + 13 }}
          onClick={() => setPreview({ path: entry.path, name: entry.name, size: entry.size ?? 0 })}
        >
          <FileGlyph name={entry.name} />
          <span className="file-name">{entry.name}</span>
          <span className="file-size">{sizeOf(entry.size)}</span>
        </button>
      );
    });
  }

  if (preview && session) {
    return (
      <FilePreview
        session={session}
        file={preview}
        onClose={() => setPreview(null)}
        onError={onError}
      />
    );
  }

  return (
    <div className="files-view">
      <div className="files-toolbar">
        <code className="files-root">{ROOT}</code>
        <Button
          isIconOnly
          size="sm"
          variant="ghost"
          aria-label="Refresh file list"
          isPending={refreshing}
          onPress={() => void refresh()}
        >
          <RefreshCw size={14} />
        </Button>
      </div>
      <div className="files-tree">{renderDir(ROOT, 0)}</div>
    </div>
  );
}

function FilePreview({
  session,
  file,
  onClose,
  onError,
}: {
  session: Session;
  file: Preview;
  onClose: () => void;
  onError: (s: string) => void;
}) {
  const kind = kindOf(file.name);
  const url = sandboxRawURL(session.id, file.path);
  const [text, setText] = useState<string | null>(null);
  const [failure, setFailure] = useState("");

  useEffect(() => {
    if (kind !== "text" && kind !== "other") return;
    const controller = new AbortController();
    setText(null);
    setFailure("");
    (async () => {
      try {
        const response = await fetch(url, { signal: controller.signal });
        if (!response.ok) {
          let message = response.statusText || "Preview failed";
          try {
            const body = (await response.json()) as { error?: string };
            if (body?.error) message = body.error;
          } catch {}
          throw new Error(message);
        }
        const type = response.headers.get("Content-Type") ?? "";
        if (kind === "other" && !/^text\/|json|xml|javascript/.test(type)) {
          setText(null);
          setFailure("No preview for this file type.");
          return;
        }
        const body = await response.text();
        // A NUL byte in the head means binary content even when the extension
        // looks like text.
        if (body.slice(0, 4096).includes("\u0000")) {
          setText(null);
          setFailure("No preview for this file type.");
          return;
        }
        setText(body);
      } catch (e) {
        if (controller.signal.aborted) return;
        setFailure(String(e));
        onError(String(e));
      }
    })();
    return () => controller.abort();
  }, [url, kind]);

  return (
    <div className="file-preview">
      <div className="file-preview-bar">
        <Button
          isIconOnly
          size="sm"
          variant="ghost"
          aria-label="Back to files"
          onPress={onClose}
        >
          <ArrowLeft size={15} />
        </Button>
        <span className="file-preview-name">
          <FileGlyph name={file.name} size={14} />
          <span title={file.path}>{file.name}</span>
        </span>
        <span className="file-size">{sizeOf(file.size)}</span>
        <a
          className="file-preview-open"
          href={url}
          target="_blank"
          rel="noreferrer"
          aria-label="Open file in a new tab"
          title="Open in new tab"
        >
          <ExternalLink size={14} />
        </a>
      </div>
      <div className={"file-preview-body kind-" + kind}>
        {kind === "image" && <img src={url} alt={file.name} />}
        {kind === "html" && (
          <iframe
            className="file-html"
            src={url}
            title={file.name}
            sandbox=""
          />
        )}
        {(kind === "text" || kind === "other") && failure ? (
          <p className="file-preview-note">{failure}</p>
        ) : kind === "text" || kind === "other" ? (
          text === null ? (
            <p className="file-preview-note">Loading…</p>
          ) : (
            <pre className="file-text" tabIndex={0}>
              {text}
            </pre>
          )
        ) : null}
      </div>
    </div>
  );
}
