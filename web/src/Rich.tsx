import { useEffect, useState, type ReactNode } from "react";
import Markdown, { defaultUrlTransform } from "react-markdown";
import remarkGfm from "remark-gfm";
import { listLibrary } from "./api";

const LIBRARY_URL_RE =
  /nimbus:\/\/library\/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})/g;
const LIBRARY_HREF_RE =
  /^nimbus:\/\/library\/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})$/;

// Shared, lazily fetched id -> file name map, so many library links in one
// conversation trigger a single list request and never block rendering.
let nameCache: Map<string, string> | null = null;
let namePromise: Promise<Map<string, string>> | null = null;
function libraryNames(): Promise<Map<string, string>> {
  if (nameCache) return Promise.resolve(nameCache);
  if (!namePromise) {
    namePromise = listLibrary()
      .then((files) => {
        nameCache = new Map(files.map((f) => [f.id, f.name]));
        return nameCache;
      })
      .catch(() => {
        namePromise = null;
        return new Map<string, string>();
      });
  }
  return namePromise;
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
    if (!isBareUrl || !onOpenLibraryFile) return;
    let active = true;
    libraryNames().then((names) => {
      if (active) setName(names.get(id) ?? null);
    });
    return () => {
      active = false;
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
                <LibraryLink
                  id={m[1]}
                  onOpenLibraryFile={onOpenLibraryFile}
                >
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
