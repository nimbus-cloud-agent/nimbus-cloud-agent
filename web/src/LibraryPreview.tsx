import { useEffect, useState } from "react";
import { Button, Modal } from "@heroui/react";
import { Download } from "lucide-react";
import {
  getLibraryFile,
  libraryContentUrl,
  type LibraryDetail,
} from "./api";
import { Picker } from "./components";
import Rich from "./Rich";

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

export function formatLibraryDate(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime())
    ? iso
    : d.toLocaleString("en-US", {
        month: "short",
        day: "numeric",
        year: "numeric",
        hour: "numeric",
        minute: "2-digit",
      });
}

function isMarkdown(file: LibraryDetail): boolean {
  return (
    file.mime_type === "text/markdown" ||
    file.mime_type === "text/x-markdown" ||
    file.name.toLowerCase().endsWith(".md") ||
    file.name.toLowerCase().endsWith(".markdown")
  );
}

export default function LibraryPreview({
  fileId,
  version,
  onClose,
  onError,
  onOpenLibraryFile,
}: {
  fileId: string | null;
  version?: number;
  onClose: () => void;
  onError: (s: string) => void;
  onOpenLibraryFile?: (id: string, version?: number) => void;
}) {
  const [detail, setDetail] = useState<LibraryDetail | null>(null);
  const [selectedVersion, setSelectedVersion] = useState<number | null>(null);
  const [markdown, setMarkdown] = useState("");
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    setDetail(null);
    setSelectedVersion(null);
    setMarkdown("");
    if (!fileId) return;
    let active = true;
    setLoading(true);
    getLibraryFile(fileId)
      .then((d) => {
        if (active) setDetail(d);
      })
      .catch((e: Error) => {
        if (active) {
          onError(e.message);
          onClose();
        }
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
    // onClose/onError are stable App setters; only reload when the file changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fileId]);

  const effectiveVersion =
    selectedVersion ?? version ?? detail?.version ?? undefined;
  const url = fileId ? libraryContentUrl(fileId, effectiveVersion) : "";

  useEffect(() => {
    if (!detail || !fileId || !isMarkdown(detail) || !effectiveVersion) return;
    let active = true;
    fetch(libraryContentUrl(fileId, effectiveVersion))
      .then((r) => {
        if (!r.ok) throw Error(r.statusText || "Failed to load file");
        return r.text();
      })
      .then((t) => {
        if (active) setMarkdown(t);
      })
      .catch((e: Error) => {
        if (active) onError(e.message);
      });
    return () => {
      active = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [detail, fileId, effectiveVersion]);

  const isHtml = detail?.mime_type === "text/html";
  const isImage = detail?.mime_type.startsWith("image/") ?? false;

  return (
    <Modal
      isOpen={!!fileId}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <Modal.Backdrop>
        <Modal.Container>
          <Modal.Dialog className="library-preview-dialog">
            <Modal.CloseTrigger />
            <Modal.Header>
              <Modal.Heading>
                {detail?.name ?? (loading ? "Loading…" : "File")}
              </Modal.Heading>
            </Modal.Header>
            <Modal.Body>
              {detail && (
                <>
                  <div className="library-preview-meta">
                    {detail.versions.length > 1 ? (
                      <Picker
                        label="Version"
                        value={String(effectiveVersion ?? detail.version)}
                        values={[...detail.versions]
                          .sort((a, b) => b.version - a.version)
                          .map((v) => [
                            String(v.version),
                            `Version ${v.version} · ${formatLibraryDate(v.created_at)} · ${formatBytes(v.size_bytes)}`,
                          ])}
                        onChange={(v) => setSelectedVersion(Number(v))}
                      />
                    ) : (
                      <span className="muted">
                        Version {detail.version} ·{" "}
                        {formatLibraryDate(detail.updated_at)}
                      </span>
                    )}
                    <span className="muted">
                      {detail.mime_type} · {formatBytes(detail.size_bytes)}
                    </span>
                  </div>
                  {isHtml ? (
                    <iframe
                      className="library-preview-frame"
                      title={detail.name}
                      sandbox="allow-scripts"
                      src={url}
                    />
                  ) : isMarkdown(detail) ? (
                    <div className="library-preview-markdown">
                      <Rich onOpenLibraryFile={onOpenLibraryFile}>
                        {markdown}
                      </Rich>
                    </div>
                  ) : isImage ? (
                    <img
                      className="library-preview-image"
                      src={url}
                      alt={detail.name}
                    />
                  ) : (
                    <div className="library-preview-fallback">
                      <p className="muted">
                        No preview is available for this file type. Download
                        it to view it.
                      </p>
                      <dl className="library-meta">
                        <div>
                          <dt>Name</dt>
                          <dd>{detail.name}</dd>
                        </div>
                        <div>
                          <dt>Type</dt>
                          <dd>{detail.mime_type}</dd>
                        </div>
                        <div>
                          <dt>Size</dt>
                          <dd>{formatBytes(detail.size_bytes)}</dd>
                        </div>
                        <div>
                          <dt>Version</dt>
                          <dd>{effectiveVersion}</dd>
                        </div>
                        <div>
                          <dt>Updated</dt>
                          <dd>{formatLibraryDate(detail.updated_at)}</dd>
                        </div>
                      </dl>
                    </div>
                  )}
                </>
              )}
              {!detail && !loading && fileId && (
                <p className="muted">This file could not be loaded.</p>
              )}
            </Modal.Body>
            {detail && (
              <Modal.Footer>
                <a className="image-download" href={url} download={detail.name}>
                  <Download size={16} />
                  Download
                </a>
                <Button variant="secondary" onPress={onClose}>
                  Close
                </Button>
              </Modal.Footer>
            )}
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  );
}
