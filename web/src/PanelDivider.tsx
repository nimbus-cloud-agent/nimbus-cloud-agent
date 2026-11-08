import { useEffect, useRef, useState } from "react";

// Keep the terminal useful at laptop and ultrawide sizes. Keyboard users can
// adjust the same separator with arrow keys; double click restores the layout.
export default function PanelDivider() {
  const ref = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(420);
  const [maximum, setMaximum] = useState(760);
  const saved = useRef<number | null>(null);
  function update(value: number | null, persist = true) {
    const workspace = ref.current?.parentElement;
    if (!workspace) return;
    const sidebar =
      workspace.querySelector(".sidebar")?.getBoundingClientRect().width ?? 0;
    const max = Math.max(
      320,
      Math.min(760, Math.floor(workspace.clientWidth - sidebar - 721)),
    );
    const preferred =
      value === null ? null : Math.round(Math.max(320, Math.min(760, value)));
    const next = Math.min(
      max,
      preferred ?? Math.max(320, Math.min(480, innerWidth * 0.3)),
    );
    if (persist) saved.current = preferred;
    workspace.style.setProperty("--terminal-width", `${next}px`);
    setMaximum(max);
    setWidth(Math.round(next));
    if (persist) {
      try {
        if (preferred === null)
          localStorage.removeItem("nimbus.terminal-width");
        else localStorage.setItem("nimbus.terminal-width", String(preferred));
      } catch {
        /* Layout still works when browser storage is unavailable. */
      }
    }
  }
  useEffect(() => {
    try {
      const stored = Number(localStorage.getItem("nimbus.terminal-width"));
      saved.current = stored > 0 && Number.isFinite(stored) ? stored : null;
    } catch {}
    const resize = () => update(saved.current, false);
    const workspace = ref.current?.parentElement;
    const observer = new ResizeObserver(resize);
    if (workspace) {
      observer.observe(workspace);
      const sidebar = workspace.querySelector(".sidebar");
      if (sidebar) observer.observe(sidebar);
    }
    resize();
    return () => {
      observer.disconnect();
      document.body.classList.remove("resizing-panels");
    };
  }, []);
  return (
    <div
      ref={ref}
      className="panel-divider"
      role="separator"
      aria-label="Terminal width"
      aria-orientation="vertical"
      aria-valuemin={320}
      aria-valuemax={maximum}
      aria-valuenow={width}
      tabIndex={0}
      title="Drag to resize the terminal. Double-click to restore the default width."
      onDoubleClick={() => update(null)}
      onKeyDown={(e) => {
        if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(e.key)) return;
        e.preventDefault();
        update(
          e.key === "Home"
            ? 320
            : e.key === "End"
              ? maximum
              : width + (e.key === "ArrowLeft" ? 24 : -24),
        );
      }}
      onPointerDown={(e) => {
        if (e.button !== 0) return;
        e.currentTarget.setPointerCapture(e.pointerId);
        document.body.classList.add("resizing-panels");
      }}
      onPointerMove={(e) => {
        if (e.currentTarget.hasPointerCapture(e.pointerId))
          update(
            e.currentTarget.parentElement!.getBoundingClientRect().right -
              e.clientX,
          );
      }}
      onPointerUp={(e) => {
        if (e.currentTarget.hasPointerCapture(e.pointerId))
          e.currentTarget.releasePointerCapture(e.pointerId);
      }}
      onLostPointerCapture={() =>
        document.body.classList.remove("resizing-panels")
      }
    >
      <span />
    </div>
  );
}
