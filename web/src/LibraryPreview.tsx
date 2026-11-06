import { lazy, Suspense, useEffect, useState } from "react";
import { Button, Modal } from "@heroui/react";
import { Download, ExternalLink, Pencil } from "lucide-react";
import {
  ApiError,
  deleteLibraryDraft,
  getLibraryDraft,
  getLibraryFile,
  libraryContentUrl,
  libraryText,
  saveLibraryDraft,
  saveLibraryVersion,
  type LibraryDetail,
  type LibraryDraft,
  type LibraryVersion,
} from "./api";
import { Picker } from "./components";
import { editorLanguage } from "./editor-language";
import Rich from "./Rich";

const MonacoEditor = lazy(() => import("./MonacoEditor"));
// Mirrors the server's edit limit so the editor never asks to load more than
// the modal can carry.
const LIBRARY_EDIT_MAX_TEXT = 2 << 20;

function agentPrompt(draft: LibraryDraft): string {
  return (
    `The Library file "${draft.file_name}" (${draft.file_id}) changed while I ` +
    `was editing it: my edit is based on version ${draft.base_version}, but the ` +
    `current version is ${draft.current_version}. I saved my edit as draft ` +
    `${draft.id}. Please read the current version and that draft, merge both ` +
    `sides' changes, resolve any conflicts, and publish the result with ` +
    `library_write(name="${draft.file_name}", base_version=${draft.current_version}, ` +
    `draft_id=${draft.id}).`
  );
}

function isTextFile(mimeType: string): boolean {
  const value = mimeType.toLowerCase();
  return (
    value.startsWith("text/") ||
    value === "application/json" ||
    value.endsWith("+json")
  );
}

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

type LibraryPreviewProps = {
  fileId: string | null;
  version?: number;
  onClose: () => void;
  onError: (s: string) => void;
  onOpenLibraryFile?: (id: string, version?: number) => void;
};

export default function LibraryPreview(props: LibraryPreviewProps) {
  // Unmount hidden previews, including markdown and native iframe/image reads.
  if (!props.fileId) return null;
  return (
    <OpenLibraryPreview
      key={`${props.fileId}:${props.version ?? "current"}`}
      {...props}
      fileId={props.fileId}
    />
  );
}

function OpenLibraryPreview({
  fileId,
  version,
  onClose,
  onError,
  onOpenLibraryFile,
}: LibraryPreviewProps & { fileId: string }) {
  const [detail, setDetail] = useState<LibraryDetail | null>(null);
  const [selectedVersion, setSelectedVersion] = useState<number | null>(null);
  const [markdownContent, setMarkdownContent] = useState<{
    version: number;
    text: string;
  } | null>(null);
  const [loading, setLoading] = useState(false);
  const [versions, setVersions] = useState<LibraryVersion[] | null>(null);
  const [selectorOpen, setSelectorOpen] = useState(false);
  const [editing, setEditing] = useState(false);
  const [editText, setEditText] = useState("");
  const [editBase, setEditBase] = useState(0);
  const [saving, setSaving] = useState(false);
  const [conflict, setConflict] = useState<{
    base: number;
    current: number;
  } | null>(null);
  const [draft, setDraft] = useState<LibraryDraft | null>(null);
  const [notice, setNotice] = useState("");
  const [reloadKey, setReloadKey] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    let active = true;
    setLoading(true);
    getLibraryFile(fileId, { signal: controller.signal })
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
      controller.abort();
    };
    // onClose/onError are stable App setters; reload when the file changes or
    // an edit lands a new version.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fileId, reloadKey]);

  useEffect(() => {
    const controller = new AbortController();
    let active = true;
    getLibraryDraft(fileId, controller.signal)
      .then((d) => {
        if (active) setDraft(d);
      })
      .catch(() => {
        // A draft is optional; a failed read must not block the preview.
      });
    return () => {
      active = false;
      controller.abort();
    };
  }, [fileId, reloadKey]);

  const effectiveVersion =
    selectedVersion ?? version ?? detail?.version ?? undefined;
  const url = fileId ? libraryContentUrl(fileId, effectiveVersion) : "";
  const markdown =
    markdownContent && markdownContent.version === effectiveVersion
      ? markdownContent.text
      : "";

  useEffect(() => {
    if (!selectorOpen || !detail || versions !== null) return;
    const controller = new AbortController();
    let active = true;
    getLibraryFile(fileId, { includeVersions: true, signal: controller.signal })
      .then((file) => {
        if (active) setVersions(file.versions);
      })
      .catch((error: Error) => {
        if (active) onError(error.message);
      });
    return () => {
      active = false;
      controller.abort();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectorOpen, detail, fileId, versions]);

  useEffect(() => {
    if (!detail || !fileId || !isMarkdown(detail) || !effectiveVersion) return;
    const controller = new AbortController();
    let active = true;
    fetch(libraryContentUrl(fileId, effectiveVersion), {
      signal: controller.signal,
    })
      .then((r) => {
        if (!r.ok) throw Error(r.statusText || "Failed to load file");
        return r.text();
      })
      .then((t) => {
        if (active) setMarkdownContent({ version: effectiveVersion, text: t });
      })
      .catch((e: Error) => {
        if (active) onError(e.message);
      });
    return () => {
      active = false;
      controller.abort();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [detail, fileId, effectiveVersion]);

  const isHtml = detail?.mime_type === "text/html";
  const isImage = detail?.mime_type.startsWith("image/") ?? false;
  const canEdit =
    !!detail &&
    isTextFile(detail.mime_type) &&
    detail.size_bytes <= LIBRARY_EDIT_MAX_TEXT;

  const startEdit = async (source: "latest" | "draft") => {
    if (!detail) return;
    try {
      if (source === "draft" && draft) {
        setEditText(draft.content);
        setEditBase(draft.base_version);
      } else {
        setEditText(await libraryText(fileId, effectiveVersion));
        setEditBase(effectiveVersion ?? detail.version);
      }
      setNotice("");
      setConflict(null);
      setEditing(true);
    } catch (error) {
      onError((error as Error).message);
    }
  };

  const save = async () => {
    setSaving(true);
    setNotice("");
    try {
      const result = await saveLibraryVersion(fileId, editBase, editText);
      setEditing(false);
      setDraft(null);
      setConflict(null);
      setNotice(`Saved as version ${result.version}.`);
      setReloadKey((n) => n + 1);
    } catch (error) {
      if (error instanceof ApiError && error.status === 409) {
        const current = error.data;
        setConflict({
          base: editBase,
          current:
            typeof current === "object" &&
            current !== null &&
            "current_version" in current &&
            typeof current.current_version === "number"
              ? current.current_version
              : editBase + 1,
        });
      } else {
        onError((error as Error).message);
      }
    } finally {
      setSaving(false);
    }
  };

  const keepDraft = async (copyPrompt: boolean) => {
    if (!conflict) return;
    setSaving(true);
    try {
      const saved = await saveLibraryDraft(fileId, conflict.base, editText);
      setDraft(saved);
      setEditing(false);
      setConflict(null);
      if (copyPrompt) {
        try {
          await navigator.clipboard.writeText(agentPrompt(saved));
          setNotice("Draft saved and the agent prompt was copied.");
        } catch {
          setNotice("Draft saved. Use “Copy agent prompt” below to copy it.");
        }
      } else {
        setNotice("Draft saved.");
      }
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setSaving(false);
    }
  };

  const copyDraftPrompt = async () => {
    if (!draft) return;
    try {
      await navigator.clipboard.writeText(agentPrompt(draft));
      setNotice("Agent prompt copied.");
    } catch {
      onError("Could not copy the prompt.");
    }
  };

  const discardDraft = async () => {
    try {
      await deleteLibraryDraft(fileId);
      setDraft(null);
      setNotice("Draft discarded.");
    } catch (error) {
      onError((error as Error).message);
    }
  };

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
                    {detail.version > 1 ? (
                      <Picker
                        label="Version"
                        value={String(effectiveVersion ?? detail.version)}
                        onOpenChange={setSelectorOpen}
                        values={[
                          ...(versions ?? [
                            {
                              version: effectiveVersion ?? detail.version,
                              created_at: detail.updated_at,
                              size_bytes: detail.size_bytes,
                              sha256: "",
                            },
                          ]),
                        ]
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
                  {editing ? (
                    <Suspense
                      fallback={
                        <div className="library-editor library-editor-loading">
                          Loading editor…
                        </div>
                      }
                    >
                      <MonacoEditor
                        key={`${fileId}:${editBase}:${draft?.id ?? "new"}`}
                        value={editText}
                        language={editorLanguage(
                          detail.mime_type,
                          detail.name,
                        )}
                        onChange={setEditText}
                      />
                    </Suspense>
                  ) : isHtml ? (
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
                        No preview is available for this file type. Download it
                        to view it.
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
                  {notice && <p className="library-edit-notice">{notice}</p>}
                  {draft && !editing && (
                    <div className="library-draft-banner">
                      <span>
                        Draft based on version {draft.base_version} saved
                        {draft.current_version > draft.base_version
                          ? ` — the current version is ${draft.current_version}`
                          : ""}
                        .
                      </span>
                      <div className="library-draft-actions">
                        <Button
                          size="sm"
                          variant="secondary"
                          onPress={() => startEdit("draft")}
                        >
                          Continue draft
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          onPress={copyDraftPrompt}
                        >
                          Copy agent prompt
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          onPress={discardDraft}
                        >
                          Discard draft
                        </Button>
                      </div>
                    </div>
                  )}
                  {conflict && (
                    <div className="library-conflict">
                      <p>
                        <strong>Version conflict.</strong> This file is now
                        version {conflict.current}; your edit started from
                        version {conflict.base}.
                      </p>
                      <div className="library-conflict-actions">
                        <Button
                          variant="secondary"
                          onPress={() => {
                            setConflict(null);
                            setEditing(false);
                            setNotice("");
                            setReloadKey((n) => n + 1);
                          }}
                        >
                          Discard my changes
                        </Button>
                        <Button
                          variant="secondary"
                          isDisabled={saving}
                          onPress={() => keepDraft(false)}
                        >
                          Save as draft version
                        </Button>
                        <Button
                          isDisabled={saving}
                          onPress={() => keepDraft(true)}
                        >
                          Save as draft version and copy prompt
                        </Button>
                      </div>
                    </div>
                  )}
                </>
              )}
              {!detail && !loading && fileId && (
                <p className="muted">This file could not be loaded.</p>
              )}
            </Modal.Body>
            {detail && editing && (
              <Modal.Footer>
                <Button
                  variant="secondary"
                  onPress={() => {
                    setEditing(false);
                    setConflict(null);
                    setNotice("");
                  }}
                >
                  Cancel
                </Button>
                <Button isDisabled={saving} onPress={save}>
                  {saving ? "Saving…" : "Save version"}
                </Button>
              </Modal.Footer>
            )}
            {detail && !editing && (
              <Modal.Footer>
                <a
                  className="library-open-tab"
                  href={url}
                  target="_blank"
                  rel="noreferrer"
                  aria-label="Open in new tab"
                  title="Open in new tab"
                >
                  <ExternalLink size={15} strokeWidth={1.8} />
                </a>
                <a className="image-download" href={url} download={detail.name}>
                  <Download size={16} />
                  Download
                </a>
                {canEdit && (
                  <Button
                    variant="secondary"
                    onPress={() =>
                      startEdit(draft ? "draft" : "latest")
                    }
                  >
                    <Pencil size={15} />
                    Edit
                  </Button>
                )}
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
