import { useEffect, useState, type ReactNode } from "react";
import Markdown, { defaultUrlTransform } from "react-markdown";
import remarkGfm from "remark-gfm";
import { getLibraryFile } from "./api";

const LIBRARY_URL_RE =
  /nimbus:\/\/library\/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})/g;
const LIBRARY_HREF_RE =
  /^nimbus:\/\/library\/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})$/;

// Only visible bare links resolve their own metadata. Concurrent instances of
// the same link share one request, cancelled when its last consumer unmounts.
type LibraryNameRequest = {
  name?: string;
  promise: Promise<string>;
  controller: AbortController;
  users: number;
};
const libraryNames = new Map<string, LibraryNameRequest>();
function acquireLibraryName(id: string) {
  let request = libraryNames.get(id);
  if (!request) {
    const controller = new AbortController();
    const entry: LibraryNameRequest = {
      controller,
      users: 0,
      promise: getLibraryFile(id, { signal: controller.signal })
        .then((file) => {
          entry.name = file.name;
          return file.name;
        })
        .catch((error: unknown) => {
          if (libraryNames.get(id) === entry) libraryNames.delete(id);
          throw error;
        }),
    };
    request = entry;
    libraryNames.set(id, request);
  }
  request.users++;
  const current = request;
  return {
    promise: current.promise,
    release: () => {
      current.users--;
      if (current.users === 0 && current.name === undefined) {
        if (libraryNames.get(id) === current) libraryNames.delete(id);
        current.controller.abort();
      }
    },
  };
}

function LibraryLink({
  id,
  children,
  onOpenLibraryFile,
}: {
  id: string;
  children: ReactNode;
  onOpenLibraryFile?: (id: string) => void;
}) {
  const [name, setName] = useState<string | null>(null);
  const text =
    typeof children === "string"
      ? children
      : Array.isArray(children)
        ? children.filter((c) => typeof c === "string").join("")
        : "";
  const isBareUrl = text === `nimbus://library/${id}` || text === "";
  useEffect(() => {
    setName(null);
    if (!isBareUrl || !onOpenLibraryFile) return;
    let active = true;
    const request = acquireLibraryName(id);
    request.promise
      .then((name) => {
        if (active) setName(name);
      })
      .catch(() => {
        // Keep the safe original link label when metadata is unavailable.
      });
    return () => {
      active = false;
      request.release();
    };
  }, [id, isBareUrl, onOpenLibraryFile]);
  const label = isBareUrl ? (name ?? `nimbus://library/${id}`) : text;
  return (
    <a
      href={`nimbus://library/${id}`}
      onClick={(e) => {
        e.preventDefault();
        onOpenLibraryFile?.(id);
      }}
    >
      {label}
    </a>
  );
}

// Autolink bare nimbus://library/<id> URLs: react-markdown does not autolink
// custom schemes. Only text outside fenced/inline code is rewritten, and a
// URL already used as a markdown link destination (preceded by `(`) or as
// link text (preceded by `[`) is left alone.
function autolinkLibraryUrls(text: string): string {
  return text
    .split(/(```[\s\S]*?```|`[^`\n]*`)/g)
    .map((part, i) => {
      if (i % 2 === 1) return part;
      return part.replace(LIBRARY_URL_RE, (match, _id, offset, whole) => {
        const prev = offset > 0 ? whole[offset - 1] : "";
        if (prev === "(" || prev === "[" || prev === "<") return match;
        return `[${match}](${match})`;
      });
    })
    .join("");
}

export default function Rich({
  children,
  onOpenLibraryFile,
}: {
  children: string;
  onOpenLibraryFile?: (id: string) => void;
}) {
  return (
    <div className="markdown">
      <Markdown
        remarkPlugins={[remarkGfm]}
        urlTransform={(url, key) =>
          key === "href" && LIBRARY_HREF_RE.test(url)
            ? url
            : defaultUrlTransform(url)
        }
        components={{
          a: ({ href, children: linkChildren }) => {
            const m = href ? LIBRARY_HREF_RE.exec(href) : null;
            if (m) {
              return (
                <LibraryLink id={m[1]} onOpenLibraryFile={onOpenLibraryFile}>
                  {linkChildren}
                </LibraryLink>
              );
            }
            return <a href={href}>{linkChildren}</a>;
          },
        }}
      >
        {autolinkLibraryUrls(children)}
      </Markdown>
    </div>
  );
}
