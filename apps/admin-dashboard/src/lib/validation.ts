// Client-side upload validation mirrors the backend's accepted types so users
// get immediate feedback. The backend remains authoritative: it re-checks
// extensions, sizes, and file signatures on every upload.
export const ACCEPTED_EXTENSIONS = [".txt", ".md", ".pdf", ".docx"] as const;

export const MAX_UPLOAD_BYTES = 10 * 1024 * 1024;

export function validateUploadFile(file: File): string | null {
  const name = file.name || "";
  const lower = name.toLowerCase();
  const supported = (ACCEPTED_EXTENSIONS as readonly string[]).some((ext) => lower.endsWith(ext));
  if (!supported) {
    return "Unsupported file type. Upload .txt, .md, .pdf, or .docx files.";
  }
  if (file.size === 0) {
    return "The file is empty.";
  }
  if (file.size > MAX_UPLOAD_BYTES) {
    return "The file exceeds the 10 MB upload limit.";
  }
  return null;
}

export function validateTextInput(title: string, content: string): string | null {
  if (!title.trim()) return "A title is required.";
  if (title.trim().length > 200) return "The title must be 200 characters or fewer.";
  if (!content.trim()) return "Content must not be empty.";
  if (content.length > 200_000) return "Content exceeds the maximum length.";
  return null;
}

// Allowed origins must be bare http(s) origins, matching Module 1 rules:
// scheme://host, no path, query, or fragment.
export function validateOriginEntry(raw: string): string | null {
  const value = raw.trim();
  let parsed: URL;
  try {
    parsed = new URL(value);
  } catch {
    return `"${raw.trim()}" is not a valid URL.`;
  }
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    return "Origins must use http or https.";
  }
  const withoutTrailing = value.replace(/\/+$/, "");
  if (withoutTrailing !== `${parsed.protocol}//${parsed.host}`) {
    return "Enter a bare origin such as https://example.com (no path, query, or fragment).";
  }
  return null;
}

export function validateWidgetForm(displayName: string, origins: string[]): string | null {
  if (!displayName.trim()) return "A display name is required.";
  if (origins.length === 0) return "At least one allowed origin is required.";
  if (origins.length > 20) return "Too many allowed origins (maximum 20).";
  const seen = new Set<string>();
  for (const entry of origins) {
    const problem = validateOriginEntry(entry);
    if (problem) return problem;
    const key = entry.trim().replace(/\/+$/, "").toLowerCase();
    if (seen.has(key)) return "Duplicate origin in the list.";
    seen.add(key);
  }
  return null;
}
