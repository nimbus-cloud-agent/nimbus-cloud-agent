// Kept out of MonacoEditor so importing the language hint never pulls the
// editor bundle into the main chunk.
export function editorLanguage(mimeType: string, name: string): string {
  const ext = name.slice(name.lastIndexOf(".")).toLowerCase();
  if (mimeType === "text/html" || ext === ".html") return "html";
  if (
    mimeType === "text/markdown" ||
    mimeType === "text/x-markdown" ||
    ext === ".md" ||
    ext === ".markdown"
  )
    return "markdown";
  if (mimeType === "application/json" || ext === ".json") return "json";
  return "plaintext";
}
