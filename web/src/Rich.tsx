import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";

export default function Rich({ children }: { children: string }) {
  return (
    <div className="markdown">
      <Markdown remarkPlugins={[remarkGfm]}>{children}</Markdown>
    </div>
  );
}
