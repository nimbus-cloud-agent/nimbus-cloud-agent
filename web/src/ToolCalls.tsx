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

function toolInfo(node: Pick<ToolNode, "name" | "args">) {
  const labels: Record<string, [string, typeof Cloud]> = {
    sandbox_start: ["Start sandbox", Cloud],
    sandbox_status: ["Sandbox status", Cloud],
    sandbox_exec: ["Run command", TerminalSquare],
    sandbox_read_file: ["Read file", FileText],
    sandbox_write_file: ["Write file", FilePenLine],
    skill_read: ["Read skill", BookOpen],
    run_code: ["Run code", Braces],
    exit_plan_mode: ["Plan review", FileText],
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

/** Argument deltas are text, so this counts characters rather than bytes. */
function sizeLabel(chars: number) {
  return chars < 1000 ? `${chars} chars` : `${(chars / 1000).toFixed(1)}k chars`;
}

/**
 * A call whose arguments are still streaming. Native tool/call events only
 * exist once the whole call is assembled, so this row carries the progress in
 * the meantime and is replaced by ToolCall when the call starts executing.
 */
export function PendingToolCall({ name, chars }: { name: string; chars: number }) {
  const { label, Icon } = toolInfo({ name, args: { formatted: "{}" } });
  return (
    <div className="tool-call" data-state="running">
      <div className="tool-row pending-tool">
        <Icon size={15} className="tool-row-icon" />
        <span className="tool-row-label">{label}</span>
        <span className="tool-row-summary">Receiving arguments</span>
        <span className="tool-row-state">
          <LoaderCircle size={13} className="spin" />
          <span>{sizeLabel(chars)}</span>
        </span>
      </div>
    </div>
  );
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
          aria-label={"Copy " + label}
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
          Copy failed. Select the text and copy it manually.
        </span>
      )}
    </section>
  );
}
function Output({ node }: { node: ToolNode }) {
  if (node.result === undefined)
    return node.output ? (
      <CodeSection label="Live output" value={node.output} />
    ) : null;
  const result = node.result;
  if (result.command) {
    const value = result.command;
    return (
      <>
        {value.stdout && <CodeSection label="Output" value={value.stdout} />}
          {value.stderr && (
          <CodeSection label="Standard error" value={value.stderr} error />
        )}
        <div className="tool-result-meta">
          {value.exitCode !== undefined && <span>Exit code {value.exitCode}</span>}
          {value.truncated && <span>Output truncated</span>}
        </div>
      </>
    );
  }
  if (result.fileContent !== undefined)
    return <CodeSection label="File contents" value={result.fileContent} />;
  return result.text ? (
    <CodeSection
      label={result.failed ? "Error" : "Result"}
      value={result.formatted}
      error={result.failed}
    />
  ) : (
    <p className="tool-empty-result">The tool completed without text output.</p>
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
    error: "Failed",
    done: "Completed",
    waiting: "Awaiting approval",
    stopped: "Interrupted",
    running: "Running",
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
                  ? `${node.children.length} child calls`
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
                <CodeSection label="Command" value={node.args.command} />
                {node.args.cwd && (
                  <p className="tool-result-meta">Directory: {node.args.cwd}</p>
                )}
              </>
            ) : node.name === "sandbox_write_file" ? (
              <CodeSection label="Written content" value={node.args.content ?? ""} />
            ) : node.name === "sandbox_read_file" ? null : (
              <CodeSection label="Arguments" value={node.args.formatted} />
            )}
            {node.children.length > 0 && (
              <div className="tool-children" aria-label="Child tool calls">
                <div className="tool-children-label">
                  Child calls <span>{node.children.length}</span>
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
                Execution interrupted before a tool result was received.
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
    ? "Waiting for plan approval"
    : !outcome
      ? "In progress"
      : outcome === "interrupted"
        ? "Execution interrupted"
        : "Execution details";
  return (
    <Fold
      className="execution-process"
      label={`${title}, ${tools.length} calls${failures ? `, ${failures} failed` : ""}`}
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
          <span className="process-count">{tools.length} calls</span>
          {failures > 0 && (
            <span className="process-errors">{failures} failed</span>
          )}
          {duration != null && outcome && (
            <span className="process-duration">
              {duration < 60
                ? `${duration.toFixed(1)}s`
                : `${Math.floor(seconds / 60)}m ${seconds % 60}s`}
            </span>
          )}
        </span>
      }
    >
      {children}
    </Fold>
  );
}
