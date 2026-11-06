import { useEffect, useRef } from "react";
import * as monaco from "monaco-editor";
import EditorWorker from "monaco-editor/editor/editor.worker?worker";
import JsonWorker from "monaco-editor/language/json/json.worker?worker";
import CssWorker from "monaco-editor/language/css/css.worker?worker";
import HtmlWorker from "monaco-editor/language/html/html.worker?worker";
import TsWorker from "monaco-editor/language/typescript/ts.worker?worker";

// Bundled workers: the editor must load without fetching anything from a CDN.
self.MonacoEnvironment = {
  getWorker(_workerId: string, label: string) {
    switch (label) {
      case "json":
        return new JsonWorker();
      case "css":
      case "scss":
      case "less":
        return new CssWorker();
      case "html":
      case "handlebars":
      case "razor":
        return new HtmlWorker();
      case "typescript":
      case "javascript":
        return new TsWorker();
      default:
        return new EditorWorker();
    }
  },
};

type MonacoEditorProps = {
  value: string;
  language: string;
  readOnly?: boolean;
  onChange?: (value: string) => void;
};

// Mounted once per document: remount with a key to load different content,
// which keeps the buffer owned by the editor instead of syncing every keystroke
// back through React.
export default function MonacoEditor({
  value,
  language,
  readOnly = false,
  onChange,
}: MonacoEditorProps) {
  const host = useRef<HTMLDivElement | null>(null);
  const instance = useRef<monaco.editor.IStandaloneCodeEditor | null>(null);
  const listener = useRef(onChange);
  listener.current = onChange;

  useEffect(() => {
    if (!host.current) return;
    const editor = monaco.editor.create(host.current, {
      value,
      language,
      readOnly,
      theme: "vs",
      automaticLayout: true,
      minimap: { enabled: false },
      fontSize: 13,
      lineHeight: 20,
      scrollBeyondLastLine: false,
      wordWrap: "on",
      renderLineHighlight: "none",
      padding: { top: 10, bottom: 10 },
      scrollbar: { verticalScrollbarSize: 10, horizontalScrollbarSize: 10 },
    });
    const subscription = editor.onDidChangeModelContent(() =>
      listener.current?.(editor.getValue()),
    );
    instance.current = editor;
    return () => {
      subscription.dispose();
      instance.current = null;
      editor.dispose();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    instance.current?.updateOptions({ readOnly });
  }, [readOnly]);

  return <div className="library-editor" ref={host} />;
}
