import { decodeJSON, type TerminalMessage } from "./api";
import { useEffect, useRef, useState } from "react";
import { Terminal as XTerm } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { Button } from "@heroui/react";
import { TerminalSquare, Minus, Plus, RotateCw } from "lucide-react";
import { type Session } from "./api";
import "@xterm/xterm/css/xterm.css";

// The terminal tab owns the PTY socket. It stays mounted while the Files tab
// is in front so switching back keeps scrollback and the live shell.
export default function TerminalView({
  session,
  onError,
  visible,
}: {
  session?: Session;
  onError: (s: string) => void;
  visible: boolean;
}) {
  const el = useRef<HTMLDivElement>(null);
  const [connection, setConnection] = useState("connecting"),
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
  // A hidden tab has no measurable size, so the fit has to wait until the
  // terminal is on screen again.
  useEffect(() => {
    if (!visible) return;
    const frame = requestAnimationFrame(() => {
      try {
        fitAddon.current?.fit();
      } catch {}
    });
    return () => cancelAnimationFrame(frame);
  }, [visible]);
  useEffect(() => {
    if (
      !session ||
      state !== "running" ||
      !el.current ||
      !el.current.getClientRects().length
    ) {
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
    let mounted = true;
    const resize = () => {
      if (mounted && el.current?.clientWidth) {
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
      if (!mounted) return;
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
        term.writeln(
          "\r\n\x1b[90mConnection closed. Select reconnect to try again.\x1b[0m",
        );
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
      ws.onmessage = null;
      ws.close();
      // xterm 5.5 queues viewport work in both a timer and an animation frame.
      // Drain those before releasing the renderer; the socket is already closed.
      requestAnimationFrame(() => setTimeout(() => term.dispose(), 0));
    };
  }, [session?.id, state, generation]);
  return (
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
  );
}
