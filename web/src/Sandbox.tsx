import { useEffect, useRef, useState } from "react";
import { Button } from "@heroui/react";
import {
  Box,
  Clock3,
  FolderTree,
  MapPin,
  Pause,
  Play,
  TerminalSquare,
  X,
} from "lucide-react";
import { api, sandboxLabels, type Session } from "./api";
import TerminalView from "./Terminal";
import FilesView from "./Files";

// The panel is one sandbox seen two ways: a shell you type into, and the files
// that shell works on. Both tabs read the same running sandbox, so the tab is
// view state, not a second connection.
export default function SandboxPanel({
  session,
  onError,
  onClose,
  overlay = false,
}: {
  session?: Session;
  onError: (s: string) => void;
  onClose?: () => void;
  // The overlay dialog already carries the title, so the panel drops its own
  // heading and keeps only the state badge.
  overlay?: boolean;
}) {
  const [tab, setTab] = useState<"terminal" | "files">("terminal"),
    [busy, setBusy] = useState(false);
  const state = session?.sandbox.state ?? "not_started";
  const running = state === "running";
  // The PTY connects on mount, and xterm cannot measure a hidden element, so
  // mount the terminal the first time its tab is actually shown and keep it
  // mounted afterwards. Switching tabs then preserves scrollback and the shell
  // instead of reconnecting.
  const [terminalReady, setTerminalReady] = useState(false);
  const [filesReady, setFilesReady] = useState(false);
  // Pausing closes the PTY, which surfaces as a transport error. That close is
  // the pause the user asked for, so it must not become an error banner.
  const pausing = useRef(false);
  useEffect(() => {
    if (!running) return;
    pausing.current = false;
    if (tab === "terminal") setTerminalReady(true);
    else setFilesReady(true);
  }, [running, tab]);
  const terminalError = (message: string) => {
    if (!pausing.current) onError(message);
  };
  async function action(which: string) {
    if (!session) return;
    if (which === "pause") pausing.current = true;
    if (which === "start") pausing.current = false;
    setBusy(true);
    try {
      await api(`/sessions/${session.id}/sandbox/${which}`, "POST");
    } catch (e) {
      if (which === "pause") pausing.current = false;
      onError(String(e));
    } finally {
      setBusy(false);
    }
  }
  const tabs = [
    { id: "terminal" as const, label: "Terminal", icon: TerminalSquare },
    { id: "files" as const, label: "Files", icon: FolderTree },
  ];
  return (
    <aside className="terminal-panel sandbox-panel" aria-label="Sandbox workspace">
      <div className={"panel-heading" + (overlay ? " is-overlay" : "")}>
        {!overlay && (
          <span>
            <Box size={19} />
            Sandbox workspace
          </span>
        )}
        <div className="sandbox-heading-actions">
          <span className={"sandbox-badge state-" + state}>
            <span
              className={
                "dot " +
                (state === "running"
                  ? "online"
                  : state === "paused"
                    ? "paused"
                    : "")
              }
            />
            {sandboxLabels[state]}
          </span>
          {onClose && (
            <Button
              isIconOnly
              size="sm"
              variant="ghost"
              aria-label="Close sandbox workspace"
              onPress={onClose}
            >
              <X size={17} />
            </Button>
          )}
        </div>
      </div>
      <div className="terminal-meta">
        <span>Dedicated Linux environment</span>
        <span>
          <MapPin size={13} />
          {session?.sandbox.info?.region ??
            (session?.sandbox.e2b_sandbox_id ? "us-west-1" : "E2B Cloud")}
        </span>
      </div>
      <div className="sandbox-tabs" role="tablist" aria-label="Sandbox views">
        {tabs.map(({ id, label, icon: Icon }) => (
          <button
            key={id}
            type="button"
            role="tab"
            id={"sandbox-tab-" + id}
            aria-selected={tab === id}
            aria-controls={"sandbox-view-" + id}
            className={"sandbox-tab" + (tab === id ? " is-active" : "")}
            onClick={() => setTab(id)}
            onKeyDown={(e) => {
              if (e.key !== "ArrowRight" && e.key !== "ArrowLeft") return;
              e.preventDefault();
              setTab(id === "terminal" ? "files" : "terminal");
            }}
          >
            <Icon size={14} />
            {label}
          </button>
        ))}
      </div>
      <div
        className="sandbox-views"
        role="tabpanel"
        id={"sandbox-view-" + tab}
        aria-labelledby={"sandbox-tab-" + tab}
      >
        {running ? (
          <>
            <div className="sandbox-view" hidden={tab !== "terminal"}>
              {terminalReady && (
                <TerminalView
                  session={session}
                  onError={terminalError}
                  visible={tab === "terminal"}
                />
              )}
            </div>
            <div className="sandbox-view" hidden={tab !== "files"}>
              {filesReady && <FilesView session={session} onError={onError} />}
            </div>
          </>
        ) : (
          <SandboxOffline
            session={session}
            state={state}
            busy={busy}
            onStart={() => action("start")}
          />
        )}
      </div>
      <div className="terminal-bottom">
        {session?.sandbox.e2b_sandbox_id && (
          <div className="sandbox-id">
            <span>Sandbox ID</span>
            <code title={session.sandbox.e2b_sandbox_id}>
              {session.sandbox.e2b_sandbox_id}
            </code>
          </div>
        )}
        <div className="row">
          <span className="idle-hint">
            <Clock3 size={14} />
            Pauses after 15 minutes of inactivity
          </span>
          {running && (
            <Button
              size="sm"
              variant="ghost"
              isPending={busy}
              isDisabled={session?.agent_state !== "idle"}
              onPress={() => action("pause")}
            >
              <Pause size={15} />
              Pause
            </Button>
          )}
        </div>
      </div>
    </aside>
  );
}

// One resume surface for both tabs: a paused sandbox has no shell and no file
// listing, and looking at either is not a reason to spend a sandbox.
function SandboxOffline({
  session,
  state,
  busy,
  onStart,
}: {
  session?: Session;
  state: string;
  busy: boolean;
  onStart: () => void;
}) {
  return (
    <div className="terminal-empty">
      <div className="cloud-mark">
        <TerminalSquare size={32} strokeWidth={1.5} />
      </div>
      <h3>
        {state === "error"
          ? "Sandbox temporarily unavailable"
          : state === "killed"
            ? "Sandbox deleted"
            : state === "paused"
              ? "Your workspace is saved and ready to resume"
              : "Your cloud workspace"}
      </h3>
      <p>
        {state === "error"
          ? session?.sandbox.error
          : state === "killed"
            ? "Your chat history is still available. Files in the deleted sandbox cannot be recovered; starting again creates a new environment."
            : state === "paused"
              ? "Your files and memory are preserved in the cloud. Resume the sandbox to browse them and continue where you left off."
              : "The agent starts the sandbox when a task needs it. You can also start it now, then run commands and browse files."}
      </p>
      <Button
        variant="secondary"
        isDisabled={!session}
        isPending={busy || state === "provisioning"}
        onPress={onStart}
      >
        <Play size={16} />
        {state === "paused" ? "Resume sandbox" : "Start sandbox"}
      </Button>
      <small>Browsing your chats does not start the sandbox.</small>
    </div>
  );
}
