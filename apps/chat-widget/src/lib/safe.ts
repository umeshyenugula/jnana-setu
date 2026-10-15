// Safe rendering helpers. Answers and excerpts are untrusted text: they are
// rendered as plain text (React escapes by default) and never injected as
// HTML. Links open only for approved http(s) URLs from verified metadata.
export function escapeHtml(text: string): string {
  return text
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

export function isSafeHttpUrl(raw: unknown): raw is string {
  if (typeof raw !== "string") return false;
  try {
    const parsed = new URL(raw);
    return parsed.protocol === "http:" || parsed.protocol === "https:";
  } catch {
    return false;
  }
}

// sourceLink returns a safe outbound URL from verified source metadata, or
// null when the metadata carries no approved link. Only http(s) targets from
// the trusted chat API are ever used; the model cannot invent destinations.
export function sourceLink(sourceMeta: Record<string, unknown> | undefined): string | null {
  if (!sourceMeta) return null;
  const candidate = sourceMeta.url ?? sourceMeta.link;
  if (isSafeHttpUrl(candidate)) return candidate;
  return null;
}

export function sourceLabel(sourceMeta: Record<string, unknown> | undefined, fallback: string): string {
  if (!sourceMeta) return fallback;
  const title = sourceMeta.title ?? sourceMeta.section;
  if (typeof title === "string" && title.trim()) return title.trim().slice(0, 80);
  return fallback;
}
