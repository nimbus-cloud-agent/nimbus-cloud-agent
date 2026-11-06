import { useEffect, useState } from "react";
import { Modal, Button, Input, TextArea, Label, Chip } from "@heroui/react";
import {
  KeyRound,
  Cloud,
  BookOpen,
  Plug,
  Plus,
  Trash2,
  ExternalLink,
  Check,
  Upload,
} from "lucide-react";
import {
  api,
  type SettingsData,
  type MCPServer,
  type MCPDraft,
  type MCPResult,
} from "./api";
import { Toggle } from "./components";
type Skill = {
  id?: string;
  name: string;
  description: string;
  enabled: boolean;
  files: { path: string; content?: string }[];
};
const blank = (): Skill => ({
  name: "",
  description: "",
  enabled: true,
  files: [{ path: "SKILL.md", content: "" }],
});
export default function Settings({
  open,
  onClose,
  onError,
}: {
  open: boolean;
  onClose: () => void;
  onError: (s: string) => void;
}) {
  const [tab, setTab] = useState(() =>
      new URLSearchParams(location.search).get("settings") === "mcp"
        ? "mcp"
        : "model",
    ),
    [key, setKey] = useState(""),
    [configured, setConfigured] = useState(false),
    [template, setTemplate] = useState("base"),
    [skills, setSkills] = useState<Skill[]>([]),
    [mcps, setMcps] = useState<MCPServer[]>([]),
    [edit, setEdit] = useState<Skill | null>(null),
    [file, setFile] = useState(0),
    [mcpEdit, setMcpEdit] = useState<MCPDraft | null>(null),
    [busy, setBusy] = useState(false),
    [notice, setNotice] = useState(""),
    [failure, setFailure] = useState("");
  const [revision, setRevision] = useState(0);
  const [loading, setLoading] = useState(true);
  const [fileAttempt, setFileAttempt] = useState(0);
  const [fileError, setFileError] = useState("");
  useEffect(() => {
    setNotice("");
  }, [open, tab]);
  useEffect(() => {
    if (!open) return;
    const controller = new AbortController();
    setFailure("");
    setLoading(true);
    async function load() {
      try {
        if (tab === "model") {
          const data = await api<SettingsData>(
            "/settings",
            "GET",
            undefined,
            controller.signal,
          );
          setTemplate(data.e2b_template);
          setConfigured(!!data.deepseek_api_key);
        } else if (tab === "skills") {
          setSkills(
            await api<Skill[]>(
              "/skills?include_content=false",
              "GET",
              undefined,
              controller.signal,
            ),
          );
        } else {
          setMcps(
            await api<MCPServer[]>(
              "/mcp/servers",
              "GET",
              undefined,
              controller.signal,
            ),
          );
        }
      } catch (e) {
        if (!controller.signal.aborted) setFailure(String(e));
      } finally {
        if (!controller.signal.aborted) setLoading(false);
      }
    }
    void load();
    return () => controller.abort();
  }, [open, tab, revision]);
  const selectedFile = edit?.files[file];
  useEffect(() => {
    setFileError("");
    if (
      !open ||
      loading ||
      tab !== "skills" ||
      !edit?.id ||
      !selectedFile ||
      selectedFile.content !== undefined
    )
      return;
    const controller = new AbortController();
    const skillID = edit.id,
      path = selectedFile.path;
    api<{ path: string; content: string }>(
      `/skills/${skillID}/files?path=${encodeURIComponent(path)}`,
      "GET",
      undefined,
      controller.signal,
    )
      .then((data) => {
        if (controller.signal.aborted) return;
        setEdit((current) =>
          current?.id === skillID
            ? {
                ...current,
                files: current.files.map((entry) =>
                  entry.path === path && entry.content === undefined
                    ? { ...entry, content: data.content }
                    : entry,
                ),
              }
            : current,
        );
      })
      .catch((e: Error) => {
        if (!controller.signal.aborted) setFileError(e.message);
      });
    return () => controller.abort();
  }, [
    open,
    loading,
    tab,
    edit?.id,
    selectedFile?.path,
    selectedFile?.content,
    fileAttempt,
  ]);
  async function run(
    fn: () => Promise<unknown>,
    message = "Saved. Changes take effect in the next conversation.",
  ) {
    setBusy(true);
    setFailure("");
    setNotice("");
    try {
      const result = await fn();
      setRevision((value) => value + 1);
      setNotice(typeof result === "string" ? result : message);
    } catch (e) {
      setFailure(String(e));
    } finally {
      setBusy(false);
    }
  }
  async function upload(list: FileList | null) {
    if (!list || !edit) return;
    const files = [];
    try {
      for (const f of Array.from(list)) {
        if (f.size > 5242880) throw Error(`${f.name} exceeds 5 MB.`);
        const bytes = await f.arrayBuffer();
        const content = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
        if (content.includes("\0"))
          throw Error(`${f.name} is not a text file.`);
        files.push({ path: f.webkitRelativePath || f.name, content });
      }
      const merged = [
        ...edit.files.filter((f) => f.content !== "" || f.path !== "SKILL.md"),
        ...files,
      ];
      if (merged.length > 100)
        throw Error("Each skill can contain up to 100 files.");
      setEdit({ ...edit, files: merged });
      setFile(0);
    } catch (e) {
      setFailure(String(e));
    }
  }
  return (
    <Modal
      isOpen={open}
      onOpenChange={(v) => {
        if (!v) onClose();
      }}
    >
      <Modal.Backdrop>
        <Modal.Container size="lg">
          <Modal.Dialog>
            <Modal.CloseTrigger />
            <Modal.Header>
              <Modal.Heading>Settings</Modal.Heading>
              <p className="muted">
                Configure your cloud workspace. Changes take effect in the next
                turn.
              </p>
            </Modal.Header>
            <Modal.Body>
              <div className="settings-tabs">
                {[
                  ["model", "Model & Sandbox", <KeyRound size={15} />],
                  ["skills", "Skills", <BookOpen size={15} />],
                  ["mcp", "MCP Servers", <Plug size={15} />],
                ].map(([id, name, icon]) => (
                  <Button
                    key={String(id)}
                    variant={tab === id ? "secondary" : "ghost"}
                    onPress={() => {
                      if (tab !== String(id)) {
                        setLoading(true);
                        setTab(String(id));
                      }
                      setNotice("");
                      setFailure("");
                    }}
                  >
                    {icon}
                    {name}
                  </Button>
                ))}
              </div>
              {failure && (
                <div className="form-error" role="alert">
                  {failure}
                </div>
              )}
              {failure && (
                <Button
                  size="sm"
                  variant="ghost"
                  onPress={() => setRevision((value) => value + 1)}
                >
                  Retry loading settings
                </Button>
              )}
              {loading && <p role="status">Loading settings…</p>}
              {notice && (
                <div className="form-success" role="status">
                  <Check size={15} />
                  {notice}
                </div>
              )}
              {tab === "model" && !loading && (
                <div className="settings-section">
                  <h3>
                    <KeyRound size={17} />
                    DeepSeek model
                  </h3>
                  <Label>
                    API Key{" "}
                    {configured && (
                      <Chip size="sm" variant="soft">
                        Configured
                      </Chip>
                    )}
                  </Label>
                  <Input
                    aria-label="DeepSeek API Key"
                    type="password"
                    value={key}
                    placeholder={
                      configured
                        ? "Saved securely · Enter a new key to replace it"
                        : "sk-…"
                    }
                    onChange={(e) => setKey(e.target.value)}
                  />
                  <p className="muted">
                    Keys are stored on the server. Their full values are never
                    sent to this page.
                  </p>
                  <h3>
                    <Cloud size={17} />
                    E2B cloud sandbox
                  </h3>
                  <Label>Sandbox template</Label>
                  <Input
                    aria-label="E2B template"
                    value={template}
                    onChange={(e) => setTemplate(e.target.value)}
                  />
                  <p className="muted">
                    Applies only to new sandboxes. Existing sandboxes keep their
                    current environment.
                  </p>
                  <div className="settings-facts">
                    <span>
                      E2B API Key{" "}
                      <Chip size="sm" variant="soft">
                        Configure at startup
                      </Chip>
                    </span>
                    <span>
                      Auto-pause after <strong>15 minutes</strong> of inactivity
                    </span>
                    <span>
                      Maximum runtime <strong>1 hour</strong>
                    </span>
                  </div>
                  <Button
                    variant="primary"
                    isPending={busy}
                    onPress={() =>
                      run(async () => {
                        await api("/settings", "PUT", {
                          e2b_template: template,
                          ...(key ? { deepseek_api_key: key } : {}),
                        });
                        setKey("");
                      })
                    }
                  >
                    Save settings
                  </Button>
                </div>
              )}
              {tab === "skills" && !loading && (
                <div className="settings-section">
                  <div className="row between">
                    <h3>Your skills</h3>
                    <Button
                      size="sm"
                      variant="secondary"
                      onPress={() => {
                        setEdit(blank());
                        setFile(0);
                      }}
                    >
                      <Plus size={14} />
                      Add skill
                    </Button>
                  </div>
                  <p className="muted">
                    Skills are stored in the database and loaded by the agent
                    when needed. Each skill can contain up to 100 text files,
                    with a 5 MB limit per file.
                  </p>
                  {!loading && skills.length === 0 && !edit && (
                    <p className="settings-empty">
                      No skills yet. Add reusable workflows to help the agent
                      with your tasks.
                    </p>
                  )}
                  {skills.map((s) => (
                    <div className="setting-item" key={s.id}>
                      <div>
                        <strong>{s.name}</strong>
                        <small>
                          {s.files.length} files ·{" "}
                          {s.description || "No description"}
                        </small>
                      </div>
                      <div className="row">
                        <Toggle
                          label="Enabled"
                          value={s.enabled}
                          onChange={(v) =>
                            run(() =>
                              api("/skills/" + s.id, "PATCH", { enabled: v }),
                            )
                          }
                        />
                        <Button
                          size="sm"
                          variant="ghost"
                          onPress={() => {
                            setEdit(structuredClone(s));
                            setFile(0);
                          }}
                        >
                          Edit
                        </Button>
                        <Button
                          isIconOnly
                          size="sm"
                          variant="ghost"
                          aria-label={"Delete skill " + s.name}
                          onPress={() =>
                            run(
                              () => api("/skills/" + s.id, "DELETE"),
                              "Skill deleted",
                            )
                          }
                        >
                          <Trash2 size={14} />
                        </Button>
                      </div>
                    </div>
                  ))}
                  {edit && (
                    <div className="skill-editor">
                      <Label>Skill name</Label>
                      <Input
                        aria-label="Skill name"
                        value={edit.name}
                        onChange={(e) =>
                          setEdit({ ...edit, name: e.target.value })
                        }
                        placeholder="e.g. data-cleaning"
                      />
                      <Label>Description</Label>
                      <Input
                        aria-label="Skill description"
                        value={edit.description}
                        onChange={(e) =>
                          setEdit({ ...edit, description: e.target.value })
                        }
                        placeholder="When to use this skill and what it does"
                      />
                      <div className="row between">
                        <Label>Files ({edit.files.length}/100)</Label>
                        <Button
                          size="sm"
                          variant="ghost"
                          onPress={() => {
                            if (edit.files.length >= 100) {
                              setFailure(
                                "A skill can contain up to 100 files.",
                              );
                              return;
                            }
                            setEdit({
                              ...edit,
                              files: [
                                ...edit.files,
                                {
                                  path: `reference-${edit.files.length}.md`,
                                  content: "",
                                },
                              ],
                            });
                            setFile(edit.files.length);
                          }}
                        >
                          <Plus size={13} />
                          Add file
                        </Button>
                      </div>
                      <Input
                        aria-label="Upload skill text files"
                        type="file"
                        multiple
                        onChange={(e) => upload(e.target.files)}
                      />
                      <div className="file-tabs">
                        {edit.files.map((f, i) => (
                          <Button
                            key={i}
                            size="sm"
                            variant={i === file ? "secondary" : "ghost"}
                            onPress={() => setFile(i)}
                          >
                            {f.path}
                          </Button>
                        ))}
                      </div>
                      {edit.files[file] && (
                        <>
                          <Input
                            aria-label="File path"
                            disabled={edit.files[file].content === undefined}
                            value={edit.files[file].path}
                            onChange={(e) =>
                              setEdit({
                                ...edit,
                                files: edit.files.map((f, i) =>
                                  i === file
                                    ? { ...f, path: e.target.value }
                                    : f,
                                ),
                              })
                            }
                          />
                          {edit.files[file].content === undefined && (
                            <div role="status">
                              {fileError || "Loading file…"}
                              {fileError && (
                                <Button
                                  size="sm"
                                  variant="ghost"
                                  onPress={() =>
                                    setFileAttempt((value) => value + 1)
                                  }
                                >
                                  Retry loading file
                                </Button>
                              )}
                            </div>
                          )}
                          <TextArea
                            aria-label="Skill file contents"
                            rows={10}
                            value={edit.files[file].content ?? ""}
                            disabled={edit.files[file].content === undefined}
                            onChange={(e) =>
                              setEdit({
                                ...edit,
                                files: edit.files.map((f, i) =>
                                  i === file
                                    ? { ...f, content: e.target.value }
                                    : f,
                                ),
                              })
                            }
                          />
                        </>
                      )}
                      <div className="row">
                        <Button
                          variant="primary"
                          isPending={busy}
                          onPress={() =>
                            run(async () => {
                              await api(
                                "/skills" + (edit.id ? "/" + edit.id : ""),
                                edit.id ? "PATCH" : "POST",
                                edit,
                              );
                              setEdit(null);
                            })
                          }
                        >
                          Save skill
                        </Button>
                        <Button variant="ghost" onPress={() => setEdit(null)}>
                          Cancel
                        </Button>
                        {edit.files.length > 1 && (
                          <Button
                            size="sm"
                            variant="ghost"
                            onPress={() => {
                              setEdit({
                                ...edit,
                                files: edit.files.filter((_, i) => i !== file),
                              });
                              setFile(0);
                            }}
                          >
                            Remove current file
                          </Button>
                        )}
                      </div>
                    </div>
                  )}
                </div>
              )}
              {tab === "mcp" && !loading && (
                <div className="settings-section">
                  <div className="row between">
                    <h3>Connect external tools</h3>
                    <Button
                      size="sm"
                      variant="secondary"
                      onPress={() =>
                        setMcpEdit({ name: "", url: "", enabled: true })
                      }
                    >
                      <Plus size={14} />
                      Add MCP server
                    </Button>
                  </div>
                  <p className="muted">
                    Streamable HTTP only. Services that require sign-in use
                    OAuth; credentials are stored and refreshed by the server.
                    “Authorized” means sign-in credentials are saved. Select
                    “Test” to check whether the tools are available. Connection
                    changes take effect in the next conversation.
                  </p>
                  {mcps.length === 0 && !mcpEdit && (
                    <p className="settings-empty">
                      Connect your documents, knowledge bases, and other tools.
                    </p>
                  )}
                  {mcps.map((m) => (
                    <div className="setting-item mcp-item" key={m.id}>
                      <div>
                        <strong>
                          {m.name}{" "}
                          <Chip size="sm" variant="soft">
                            {
                              {
                                none: "Not authorized",
                                ok: "Authorized",
                                pending: "Authorization pending",
                                expired: "Reauthorization required",
                              }[m.oauth_state]
                            }
                          </Chip>
                        </strong>
                        <small>{m.url}</small>
                      </div>
                      <div className="row wrap">
                        <Toggle
                          label="Enabled"
                          value={m.enabled}
                          onChange={(v) =>
                            run(() =>
                              api("/mcp/servers/" + m.id, "PUT", {
                                ...m,
                                enabled: v,
                              }),
                            )
                          }
                        />
                        <Button
                          size="sm"
                          variant="ghost"
                          onPress={() =>
                            run(async () => {
                              const r = await api<MCPResult>(
                                `/mcp/servers/${m.id}/test`,
                                "POST",
                              );
                              return `Connected · ${r.tools?.length ?? 0} tools`;
                            }, "Connection test passed")
                          }
                        >
                          Test
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          onPress={() =>
                            run(async () => {
                              const r = await api<MCPResult>(
                                `/mcp/servers/${m.id}/oauth/start`,
                                "POST",
                              );
                              if (r.auth_url) location.assign(r.auth_url);
                            }, "Authorization updated")
                          }
                        >
                          <ExternalLink size={13} />
                          Authorize
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          onPress={() => setMcpEdit({ ...m })}
                        >
                          Edit
                        </Button>
                        <Button
                          isIconOnly
                          size="sm"
                          variant="ghost"
                          aria-label={"Delete MCP server " + m.name}
                          onPress={() =>
                            run(
                              () => api("/mcp/servers/" + m.id, "DELETE"),
                              "MCP server deleted",
                            )
                          }
                        >
                          <Trash2 size={14} />
                        </Button>
                      </div>
                    </div>
                  ))}
                  {mcpEdit && (
                    <div className="skill-editor">
                      <Label>Server name</Label>
                      <Input
                        aria-label="MCP server name"
                        value={mcpEdit.name}
                        onChange={(e) =>
                          setMcpEdit({ ...mcpEdit, name: e.target.value })
                        }
                      />
                      <Label>HTTP server URL</Label>
                      <Input
                        aria-label="MCP URL"
                        value={mcpEdit.url}
                        placeholder="https://mcp.notion.com/mcp"
                        onChange={(e) =>
                          setMcpEdit({ ...mcpEdit, url: e.target.value })
                        }
                      />
                      <div className="row">
                        <Button
                          variant="primary"
                          isPending={busy}
                          onPress={() =>
                            run(async () => {
                              await api(
                                "/mcp/servers" +
                                  (mcpEdit.id ? "/" + mcpEdit.id : ""),
                                mcpEdit.id ? "PUT" : "POST",
                                mcpEdit,
                              );
                              setMcpEdit(null);
                            })
                          }
                        >
                          Save MCP server
                        </Button>
                        <Button
                          variant="ghost"
                          onPress={() => setMcpEdit(null)}
                        >
                          Cancel
                        </Button>
                      </div>
                    </div>
                  )}
                </div>
              )}
            </Modal.Body>
            <Modal.Footer>
              <span className="muted">
                Single-user workspace · You are the admin
              </span>
              <Button variant="secondary" onPress={onClose}>
                Done
              </Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  );
}
