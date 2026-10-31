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
import { api } from "./api";
import { Toggle } from "./components";
type Skill = {
  id?: string;
  name: string;
  description: string;
  enabled: boolean;
  files: { path: string; content: string }[];
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
  const [tab, setTab] = useState("model"),
    [key, setKey] = useState(""),
    [configured, setConfigured] = useState(false),
    [template, setTemplate] = useState("base"),
    [skills, setSkills] = useState<Skill[]>([]),
    [mcps, setMcps] = useState<any[]>([]),
    [edit, setEdit] = useState<Skill | null>(null),
    [file, setFile] = useState(0),
    [mcpEdit, setMcpEdit] = useState<any>(null),
    [busy, setBusy] = useState(false),
    [notice, setNotice] = useState(""),
    [failure, setFailure] = useState("");
  async function load() {
    try {
      const [s, k, m] = await Promise.all([
        api("/settings"),
        api("/skills"),
        api("/mcp/servers"),
      ]);
      setTemplate(s.e2b_template);
      setConfigured(!!s.deepseek_api_key);
      setSkills(k);
      setMcps(m);
    } catch (e) {
      setFailure(String(e));
    }
  }
  useEffect(() => {
    if (open) {
      load();
      setFailure("");
      setNotice("");
    }
  }, [open]);
  async function run(fn: () => Promise<any>, message = "已保存，下轮对话生效") {
    setBusy(true);
    setFailure("");
    setNotice("");
    try {
      await fn();
      await load();
      setNotice(message);
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
        if (f.size > 5242880) throw Error(`${f.name} 超过 5 MB`);
        const bytes = await f.arrayBuffer();
        const content = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
        if (content.includes("\0")) throw Error(`${f.name} 不是文本文件`);
        files.push({ path: f.webkitRelativePath || f.name, content });
      }
      const merged = [
        ...edit.files.filter((f) => f.content || f.path !== "SKILL.md"),
        ...files,
      ];
      if (merged.length > 100) throw Error("每个 Skill 最多 100 个文件");
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
              <Modal.Heading>全局设置</Modal.Heading>
              <p className="muted">
                配置你的云端工作空间。修改会从下一轮开始生效。
              </p>
            </Modal.Header>
            <Modal.Body>
              <div className="settings-tabs">
                {[
                  ["model", "模型与沙箱", <KeyRound size={15} />],
                  ["skills", "Skills", <BookOpen size={15} />],
                  ["mcp", "MCP 服务", <Plug size={15} />],
                ].map(([id, name, icon]) => (
                  <Button
                    key={String(id)}
                    variant={tab === id ? "secondary" : "ghost"}
                    onPress={() => {
                      setTab(String(id));
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
              {notice && (
                <div className="form-success" role="status">
                  <Check size={15} />
                  {notice}
                </div>
              )}
              {tab === "model" && (
                <div className="settings-section">
                  <h3>
                    <KeyRound size={17} />
                    DeepSeek 模型
                  </h3>
                  <Label>
                    API Key{" "}
                    {configured && (
                      <Chip size="sm" variant="soft">
                        已配置
                      </Chip>
                    )}
                  </Label>
                  <Input
                    aria-label="DeepSeek API Key"
                    type="password"
                    value={key}
                    placeholder={
                      configured ? "已安全保存 · 输入新的 key 以替换" : "sk-…"
                    }
                    onChange={(e) => setKey(e.target.value)}
                  />
                  <p className="muted">
                    密钥只保存在服务端，页面不会返回完整内容。
                  </p>
                  <h3>
                    <Cloud size={17} />
                    E2B 云端沙箱
                  </h3>
                  <Label>沙箱模板</Label>
                  <Input
                    aria-label="E2B 模板"
                    value={template}
                    onChange={(e) => setTemplate(e.target.value)}
                  />
                  <p className="muted">
                    仅影响后续创建的沙箱。已有沙箱保持原有环境。
                  </p>
                  <div className="settings-facts">
                    <span>
                      E2B API Key{" "}
                      <Chip size="sm" variant="soft">
                        启动时配置
                      </Chip>
                    </span>
                    <span>
                      闲置自动暂停 <strong>15 分钟</strong>
                    </span>
                    <span>
                      单次运行窗口 <strong>1 小时</strong>
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
                    保存设置
                  </Button>
                </div>
              )}
              {tab === "skills" && (
                <div className="settings-section">
                  <div className="row between">
                    <h3>你的技能库</h3>
                    <Button
                      size="sm"
                      variant="secondary"
                      onPress={() => {
                        setEdit(blank());
                        setFile(0);
                      }}
                    >
                      <Plus size={14} />
                      安装 Skill
                    </Button>
                  </div>
                  <p className="muted">
                    内容保存在数据库，Agent 按需读取。每个 Skill 最多 100
                    个文本文件，单文件 ≤ 5 MB。
                  </p>
                  {skills.length === 0 && !edit && (
                    <p className="settings-empty">
                      还没有 Skill。添加常用的工作方法，让 Agent 更懂你的任务。
                    </p>
                  )}
                  {skills.map((s) => (
                    <div className="setting-item" key={s.id}>
                      <div>
                        <strong>{s.name}</strong>
                        <small>
                          {s.files.length} 个文件 · {s.description || "无描述"}
                        </small>
                      </div>
                      <div className="row">
                        <Toggle
                          label="启用"
                          value={s.enabled}
                          onChange={(v) =>
                            run(() =>
                              api("/skills/" + s.id, "PUT", {
                                ...s,
                                enabled: v,
                              }),
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
                          编辑
                        </Button>
                        <Button
                          isIconOnly
                          size="sm"
                          variant="ghost"
                          aria-label={"删除 Skill " + s.name}
                          onPress={() =>
                            run(
                              () => api("/skills/" + s.id, "DELETE"),
                              "Skill 已删除",
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
                      <Label>技能名称</Label>
                      <Input
                        aria-label="Skill 名称"
                        value={edit.name}
                        onChange={(e) =>
                          setEdit({ ...edit, name: e.target.value })
                        }
                        placeholder="例如 data-cleaning"
                      />
                      <Label>描述</Label>
                      <Input
                        aria-label="Skill 描述"
                        value={edit.description}
                        onChange={(e) =>
                          setEdit({ ...edit, description: e.target.value })
                        }
                        placeholder="适用场景与用途"
                      />
                      <div className="row between">
                        <Label>文件 ({edit.files.length}/100)</Label>
                        <Button
                          size="sm"
                          variant="ghost"
                          onPress={() => {
                            if (edit.files.length >= 100) {
                              setFailure("最多 100 个文件");
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
                          添加文件
                        </Button>
                      </div>
                      <Input
                        aria-label="上传 Skill 文本文件"
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
                            aria-label="文件路径"
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
                          <TextArea
                            aria-label="Skill 文件内容"
                            rows={10}
                            value={edit.files[file].content}
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
                                edit.id ? "PUT" : "POST",
                                edit,
                              );
                              setEdit(null);
                            })
                          }
                        >
                          保存 Skill
                        </Button>
                        <Button variant="ghost" onPress={() => setEdit(null)}>
                          取消
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
                            移除当前文件
                          </Button>
                        )}
                      </div>
                    </div>
                  )}
                </div>
              )}
              {tab === "mcp" && (
                <div className="settings-section">
                  <div className="row between">
                    <h3>连接外部工具</h3>
                    <Button
                      size="sm"
                      variant="secondary"
                      onPress={() =>
                        setMcpEdit({ name: "", url: "", enabled: true })
                      }
                    >
                      <Plus size={14} />
                      添加 MCP
                    </Button>
                  </div>
                  <p className="muted">
                    仅支持 Streamable HTTP。需要登录的服务通过 OAuth
                    授权，凭据由服务端保存和刷新。
                  </p>
                  {mcps.length === 0 && !mcpEdit && (
                    <p className="settings-empty">
                      连接你的文档、知识库或其他工具。
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
                                none: "未授权",
                                ok: "已授权",
                                pending: "等待授权",
                                expired: "需要重新授权",
                              }[m.oauth_state as string]
                            }
                          </Chip>
                        </strong>
                        <small>{m.url}</small>
                      </div>
                      <div className="row wrap">
                        <Toggle
                          label="启用"
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
                              const r = await api(
                                `/mcp/servers/${m.id}/test`,
                                "POST",
                              );
                              setNotice(
                                `连接成功 · ${r.tools?.length ?? 0} 个工具`,
                              );
                            }, "连通性检查成功")
                          }
                        >
                          测试
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          onPress={() =>
                            run(async () => {
                              const r = await api(
                                `/mcp/servers/${m.id}/oauth/start`,
                                "POST",
                              );
                              if (r.auth_url) location.assign(r.auth_url);
                            }, "授权已更新")
                          }
                        >
                          <ExternalLink size={13} />
                          授权
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          onPress={() => setMcpEdit({ ...m })}
                        >
                          编辑
                        </Button>
                        <Button
                          isIconOnly
                          size="sm"
                          variant="ghost"
                          aria-label={"删除 MCP " + m.name}
                          onPress={() =>
                            run(
                              () => api("/mcp/servers/" + m.id, "DELETE"),
                              "MCP 已删除",
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
                      <Label>服务名称</Label>
                      <Input
                        aria-label="MCP 名称"
                        value={mcpEdit.name}
                        onChange={(e) =>
                          setMcpEdit({ ...mcpEdit, name: e.target.value })
                        }
                      />
                      <Label>HTTP 服务地址</Label>
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
                          保存 MCP
                        </Button>
                        <Button
                          variant="ghost"
                          onPress={() => setMcpEdit(null)}
                        >
                          取消
                        </Button>
                      </div>
                    </div>
                  )}
                </div>
              )}
            </Modal.Body>
            <Modal.Footer>
              <span className="muted">单用户工作空间 · 你就是管理员</span>
              <Button variant="secondary" onPress={onClose}>
                完成
              </Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  );
}
