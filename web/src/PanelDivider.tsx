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
      workspace.querySelector(".sidebar")?.getBoundingClientRect().width ?? 240;
    const max = Math.max(
      320,
      Math.min(
        workspace.clientWidth * 0.55,
        workspace.clientWidth - sidebar - 420,
      ),
    );
    const next =
      value === null ? null : Math.round(Math.max(320, Math.min(max, value)));
    saved.current = next;
    if (next === null) workspace.style.removeProperty("--terminal-width");
    else workspace.style.setProperty("--terminal-width", `${next}px`);
    setMaximum(Math.round(max));
    setWidth(
      next ??
        Math.round(
          workspace.querySelector(".terminal-panel")?.getBoundingClientRect()
            .width ?? 420,
        ),
    );
    if (persist) {
      try {
        if (next === null) localStorage.removeItem("nimbus.terminal-width");
        else localStorage.setItem("nimbus.terminal-width", String(next));
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
    const resize = () => {
      if (innerWidth > 1100) update(saved.current, false);
    };
    resize();
    window.addEventListener("resize", resize);
    return () => {
      window.removeEventListener("resize", resize);
      document.body.classList.remove("resizing-panels");
    };
  }, []);
  return (
    <div
      ref={ref}
      className="panel-divider"
      role="separator"
      aria-label="终端宽度"
      aria-orientation="vertical"
      aria-valuemin={320}
      aria-valuemax={maximum}
      aria-valuenow={width}
      tabIndex={0}
      title="拖动调整终端宽度，双击恢复默认"
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
