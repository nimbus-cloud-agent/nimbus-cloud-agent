import { decodeJSON, type TerminalMessage } from "./api";
import { useEffect, useRef, useState } from "react";
import { Terminal as XTerm } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { Button } from "@heroui/react";
import {
  TerminalSquare,
  Play,
  Pause,
  RotateCw,
  Clock3,
  MapPin,
  Minus,
  Plus,
} from "lucide-react";
import { api, sandboxLabels, type Session } from "./api";
import "@xterm/xterm/css/xterm.css";
export default function TerminalPanel({
  session,
  onError,
}: {
  session?: Session;
  onError: (s: string) => void;
}) {
  const el = useRef<HTMLDivElement>(null);
  const [busy, setBusy] = useState(false),
    [connection, setConnection] = useState("connecting"),
    [generation, setGeneration] = useState(0);
  const pauseRequested = useRef(false);
  const connected = connection === "connected";
  const [fontSize, setFontSize] = useState(() => {
    try {
      return Math.min(
        20,
        Math.max(
          12,
          Number(localStorage.getItem("nimbus.terminal-font")) || 14,
        ),
      );
    } catch {
      return 14;
    }
  });
  const terminal = useRef<XTerm | null>(null),
    fitAddon = useRef<FitAddon | null>(null);
  const state = session?.sandbox.state ?? "not_started";
  useEffect(() => {
    if (terminal.current) {
      terminal.current.options.fontSize = fontSize;
      fitAddon.current?.fit();
    }
    try {
      localStorage.setItem("nimbus.terminal-font", String(fontSize));
    } catch {}
  }, [fontSize]);
  useEffect(() => {
    if (!session || state !== "running" || !el.current) {
      if (state !== "running") pauseRequested.current = false;
      return;
    }
    pauseRequested.current = false;
    setConnection("connecting");
    const term = new XTerm({
      fontSize,
      fontFamily: '"IBM Plex Mono", "SFMono-Regular", Menlo, monospace',
      lineHeight: 1.45,
      cursorStyle: "bar",
      cursorBlink: true,
      theme: {
        background: "#17231e",
        foreground: "#e0e9e4",
        cursor: "#a7dbc0",
        selectionBackground: "#3d5c4c",
      },
      convertEol: true,
      scrollback: 5000,
    });
    const fit = new FitAddon();
    terminal.current = term;
    fitAddon.current = fit;
    term.loadAddon(fit);
    term.open(el.current);
    fit.fit();
    const ws = new WebSocket(
      `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/api/sessions/${session.id}/terminal`,
    );
    const resize = () => {
      if (el.current?.clientWidth) {
        try {
          fit.fit();
          if (ws.readyState === 1)
            ws.send(
              JSON.stringify({
                type: "resize",
                cols: term.cols,
                rows: term.rows,
              }),
            );
        } catch {}
      }
    };
    const observer = new ResizeObserver(resize);
    observer.observe(el.current);
    ws.onmessage = (e) => {
      const msg = decodeJSON<TerminalMessage>(e.data);
      if (msg.data)
        term.write(
          Uint8Array.from(atob(msg.data), (c: string) => c.charCodeAt(0)),
        );
      if (msg.ready) {
        setConnection("connected");
        resize();
      }
      if (msg.error) {
        if (!pauseRequested.current) {
          term.writeln("\r\n" + msg.error);
          onError(msg.error);
        }
      }
    };
    ws.onclose = () => {
      setConnection("disconnected");
      if (!pauseRequested.current)
        term.writeln("\r\n\x1b[90mConnection closed. Select reconnect to try again.\x1b[0m");
    };
    const input = term.onData((data) => {
      if (ws.readyState === 1) {
        const bytes = new TextEncoder().encode(data);
        ws.send(
          JSON.stringify({
            type: "input",
            data: btoa(String.fromCharCode(...bytes)),
          }),
        );
      }
    });
    const terminalResize = term.onResize(({ cols, rows }) => {
      if (ws.readyState === 1)
        ws.send(JSON.stringify({ type: "resize", cols, rows }));
    });
    let mounted = true;
    document.fonts.ready.then(() => {
      if (mounted) resize();
    });
    return () => {
      mounted = false;
      observer.disconnect();
      input.dispose();
      terminalResize.dispose();
      terminal.current = null;
      fitAddon.current = null;
      ws.onclose = null;
      ws.close();
      term.dispose();
    };
  }, [session?.id, state, generation]);
  async function action(which: string) {
    if (!session) return;
    if (which === "pause") pauseRequested.current = true;
    setBusy(true);
    try {
      await api(`/sessions/${session.id}/sandbox/${which}`, "POST");
    } catch (e) {
      if (which === "pause") pauseRequested.current = false;
      onError(String(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <aside className="terminal-panel" aria-label="Cloud terminal">
      <div className="panel-heading">
        <span>
          <TerminalSquare size={19} />
          Cloud terminal
        </span>
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
      </div>
      <div className="terminal-meta">
        <span>Dedicated Linux environment</span>
        <span>
          <MapPin size={13} />
          {session?.sandbox.info?.region ??
            (session?.sandbox.e2b_sandbox_id ? "us-west-1" : "E2B Cloud")}
        </span>
      </div>
      {state === "running" ? (
        <div className="terminal-shell">
          <div className="terminal-bar">
            <TerminalSquare size={15} />
            <code>/workspace</code>
            <span className="terminal-controls">
              <Button
                isIconOnly
                size="sm"
                variant="ghost"
                aria-label="Decrease terminal font size"
                isDisabled={fontSize <= 12}
                onPress={() => setFontSize((s) => Math.max(12, s - 1))}
              >
                <Minus size={15} />
              </Button>
              <span className="terminal-font-size" aria-live="polite">
                {fontSize}
              </span>
              <Button
                isIconOnly
                size="sm"
                variant="ghost"
                aria-label="Increase terminal font size"
                isDisabled={fontSize >= 20}
                onPress={() => setFontSize((s) => Math.min(20, s + 1))}
              >
                <Plus size={15} />
              </Button>
              <Button
                isIconOnly
                size="sm"
                variant="ghost"
                aria-label="Reconnect terminal"
                onPress={() => setGeneration((x) => x + 1)}
              >
                <RotateCw size={15} />
              </Button>
            </span>
          </div>
          <div ref={el} className="xterm-host" />
          <div className="terminal-hint">
            <span className={connected ? "dot online" : "dot"} />
            {!connected
              ? connection === "connecting"
                ? "Connecting to the cloud terminal…"
                : "Connection closed. Select the reconnect button above."
              : session?.agent_state !== "idle"
                ? "The agent is using this sandbox too."
                : "Your commands run directly in the sandbox."}
          </div>
        </div>
      ) : (
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
                  ? "Your files and memory are preserved in the cloud. Resume the sandbox to continue where you left off."
                  : "The agent starts the sandbox when a task needs it. You can also start it now and run commands in the terminal."}
          </p>
          <Button
            variant="secondary"
            isDisabled={!session}
            isPending={busy || state === "provisioning"}
            onPress={() => action("start")}
          >
            <Play size={16} />
            {state === "paused" ? "Resume sandbox" : "Start sandbox"}
          </Button>
          <small>Browsing your chats does not start the sandbox.</small>
        </div>
      )}
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
          {state === "running" && (
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
