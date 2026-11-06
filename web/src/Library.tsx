import { useEffect, useState } from "react";
import { Button } from "@heroui/react";
import { FileText, LoaderCircle } from "lucide-react";
import { listLibrary, type LibraryFile } from "./api";
import { formatBytes, formatLibraryDate } from "./LibraryPreview";

export default function Library({
  onOpenFile,
  onError,
}: {
  onOpenFile: (id: string) => void;
  onError: (s: string) => void;
}) {
  const [files, setFiles] = useState<LibraryFile[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let active = true;
    const controller = new AbortController();
    listLibrary(controller.signal)
      .then((list) => {
        if (!active) return;
        setFiles(
          [...list].sort(
            (a, b) =>
              new Date(b.updated_at).getTime() -
              new Date(a.updated_at).getTime(),
          ),
        );
      })
      .catch((e: Error) => {
        if (active) onError(e.message);
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
      controller.abort();
    };
    // onError is App's stable setError; reload only on mount.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div className="library-page">
      <div className="library-heading">
        <div>
          <h1>Library</h1>
          <p>
            Files the agent saved for you — documents and images, with every
            version kept.
          </p>
        </div>
        <span className="muted">
          {loading
            ? ""
            : `${files.length} file${files.length === 1 ? "" : "s"}`}
        </span>
      </div>
      {loading ? (
        <p className="library-empty">
          <LoaderCircle size={16} className="spin" />
          Loading files…
        </p>
      ) : files.length === 0 ? (
        <p className="library-empty">
          No files yet. Ask the agent to save a document or image here and it
          will appear in this list.
        </p>
      ) : (
        <ul className="library-list">
          {files.map((f) => (
            <li key={f.id}>
              <Button
                variant="ghost"
                className="library-item"
                onPress={() => onOpenFile(f.id)}
              >
                <FileText size={18} />
                <span className="library-item-details">
                  <strong title={f.name}>{f.name}</strong>
                  <span>
                    {f.mime_type} · {formatBytes(f.size_bytes)}
                  </span>
                </span>
                <span className="library-item-meta">
                  <span>Version {f.version}</span>
                  <time>{formatLibraryDate(f.updated_at)}</time>
                </span>
              </Button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
