import { useEffect, useState, type ReactNode } from "react";
import { Button } from "@heroui/react";
import {
  AlertCircle,
  BookOpen,
  Braces,
  Check,
  Cloud,
  Copy,
  FilePenLine,
  FileText,
  LoaderCircle,
  Pause,
  Plug,
  TerminalSquare,
  Workflow,
} from "lucide-react";
import { Fold } from "./components";
import { toolFailed, type ToolNode } from "./tool-events";

function toolInfo(node: ToolNode) {
  const labels: Record<string, [string, typeof Cloud]> = {
    sandbox_start: ["启动沙箱", Cloud],
    sandbox_status: ["沙箱状态", Cloud],
    sandbox_exec: ["运行命令", TerminalSquare],
    sandbox_read_file: ["读取文件", FileText],
    sandbox_write_file: ["写入文件", FilePenLine],
    skill_read: ["读取技能", BookOpen],
    run_code: ["运行代码", Braces],
    exit_plan_mode: ["计划评审", FileText],
  };
  const shortName = node.name.startsWith("mcp__")
    ? node.name.split("__").slice(2).join("__")
    : node.name;
  const [label, Icon] = labels[node.name] ?? [shortName, Plug];
  const a = node.args;
  const detail =
    a.description ??
    a.path ??
    a.command ??
    a.query ??
    a.name ??
    a.plan?.split("\n")[0]?.replace(/^#+\s*/, "") ??
    "";
  return { label, Icon, detail: String(detail) };
}
function CodeSection({
  label,
  value,
  error = false,
}: {
  label: string;
  value: string;
  error?: boolean;
}) {
  const [copied, setCopied] = useState(false);
  const [copyError, setCopyError] = useState(false);
  useEffect(() => {
    setCopied(false);
    setCopyError(false);
  }, [value]);
  return (
    <section className={"tool-section" + (error ? " is-error" : "")}>
      <div className="tool-section-heading">
        <span>{label}</span>
        <Button
          size="sm"
          variant="ghost"
          isIconOnly
          aria-label={"复制" + label}
          onPress={async () => {
            try {
              await navigator.clipboard.writeText(value);
              setCopied(true);
              setCopyError(false);
            } catch {
              setCopyError(true);
            }
          }}
        >
          {copied ? <Check size={13} /> : <Copy size={13} />}
        </Button>
      </div>
      <pre tabIndex={0}>{value}</pre>
      {copyError && (
        <span role="status" className="tool-copy-error">
          复制失败，请选择内容手动复制
        </span>
      )}
    </section>
  );
}
function Output({ node }: { node: ToolNode }) {
  if (node.result === undefined)
    return node.output ? (
      <CodeSection label="实时输出" value={node.output} />
    ) : null;
  const result = node.result;
  if (result.command) {
    const value = result.command;
    return (
      <>
        {value.stdout && <CodeSection label="输出" value={value.stdout} />}
        {value.stderr && (
          <CodeSection label="标准错误" value={value.stderr} error />
        )}
        <div className="tool-result-meta">
          {value.exitCode !== undefined && <span>退出码 {value.exitCode}</span>}
          {value.truncated && <span>输出已截断</span>}
        </div>
      </>
    );
  }
  if (result.fileContent !== undefined)
    return <CodeSection label="文件内容" value={result.fileContent} />;
  return result.text ? (
    <CodeSection
      label={result.failed ? "错误" : "结果"}
      value={result.formatted}
      error={result.failed}
    />
  ) : (
    <p className="tool-empty-result">工具执行完成，无文本输出。</p>
  );
}

export function ToolCall({
  node,
  settled,
  waiting,
}: {
  node: ToolNode;
  settled: boolean;
  waiting: Set<string>;
}) {
  const { label, Icon, detail } = toolInfo(node);
  const failed = toolFailed(node);
  const pending =
    waiting.has(node.id) || node.children.some((c) => waiting.has(c.id));
  const state = failed
    ? "error"
    : node.result !== undefined
      ? "done"
      : pending
        ? "waiting"
        : settled
          ? "stopped"
          : "running";
  const status = {
    error: "失败",
    done: "完成",
    waiting: "待确认",
    stopped: "已中断",
    running: "执行中",
  }[state];
  const duration =
    node.time != null && node.ended != null
      ? Math.max(0, node.ended - node.time) / 1000
      : null;
  return (
    <div className="tool-call" data-tool-id={node.id} data-state={state}>
      <Fold
        className="tool-disclosure"
        label={`${label}：${detail || node.name}，${status}`}
        title={
          <span className="tool-row">
            <Icon size={15} className="tool-row-icon" />
            <span className="tool-row-label">{label}</span>
            <span className="tool-row-summary" title={detail}>
              {detail ||
                (node.children.length
                  ? `${node.children.length} 个子调用`
                  : "")}
            </span>
            <span className="tool-row-state" title={status}>
              {state === "running" ? (
                <LoaderCircle size={13} className="spin" />
              ) : state === "done" ? (
                <Check size={13} />
              ) : state === "waiting" || state === "stopped" ? (
                <Pause size={12} />
              ) : (
                <AlertCircle size={13} />
              )}
              <span>
                {state === "done"
                  ? duration != null
                    ? `${duration < 1 ? duration.toFixed(2) : duration.toFixed(1)}s`
                    : ""
                  : status}
              </span>
            </span>
          </span>
        }
      >
        {() => (
          <div className="tool-detail">
            {node.name === "run_code" && node.args.code ? (
              <CodeSection label="TypeScript" value={node.args.code} />
            ) : node.name === "sandbox_exec" && node.args.command ? (
              <>
                <CodeSection label="命令" value={node.args.command} />
                {node.args.cwd && (
                  <p className="tool-result-meta">目录 {node.args.cwd}</p>
                )}
              </>
            ) : node.name === "sandbox_write_file" ? (
              <CodeSection label="写入内容" value={node.args.content ?? ""} />
            ) : node.name === "sandbox_read_file" ? null : (
              <CodeSection label="参数" value={node.args.formatted} />
            )}
            {node.children.length > 0 && (
              <div className="tool-children" aria-label="子工具调用">
                <div className="tool-children-label">
                  子调用 <span>{node.children.length}</span>
                </div>
                {node.children.map((child) => (
                  <ToolCall
                    key={child.id}
                    node={child}
                    settled={settled || node.result !== undefined}
                    waiting={waiting}
                  />
                ))}
              </div>
            )}
            <Output node={node} />
            {state === "stopped" && (
              <p className="tool-empty-result">
                执行已中断，没有收到工具结果。
              </p>
            )}
          </div>
        )}
      </Fold>
    </div>
  );
}

export function ExecutionProcess({
  tools,
  outcome,
  duration,
  waiting,
  children,
}: {
  tools: ToolNode[];
  outcome: string;
  duration?: number;
  waiting: boolean;
  children: ReactNode;
}) {
  const [manualOpen, setManualOpen] = useState<boolean>();
  const open = manualOpen ?? !outcome;
  const failures = tools.filter(toolFailed).length;
  const seconds = Math.round(duration ?? 0);
  const title = waiting
    ? "等待计划确认"
    : !outcome
      ? "正在执行"
      : outcome === "interrupted"
        ? "执行已中断"
        : "执行过程";
  return (
    <Fold
      className="execution-process"
      label={`${title}，${tools.length} 次调用${failures ? `，${failures} 项失败` : ""}`}
      open={open}
      onOpenChange={setManualOpen}
      title={
        <span className="process-summary">
          {!outcome && !waiting ? (
            <LoaderCircle size={15} className="spin" />
          ) : (
            <Workflow size={15} />
          )}
          <span>{title}</span>
          <span className="process-count">{tools.length} 次调用</span>
          {failures > 0 && (
            <span className="process-errors">{failures} 项失败</span>
          )}
          {duration != null && outcome && (
            <span className="process-duration">
              {duration < 60
                ? `${duration.toFixed(1)} 秒`
                : `${Math.floor(seconds / 60)} 分 ${seconds % 60} 秒`}
            </span>
          )}
        </span>
      }
    >
      {children}
    </Fold>
  );
}
