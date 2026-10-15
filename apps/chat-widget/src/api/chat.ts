// Module 6 public chat client. This is the ONLY backend API the widget may
// call. Retrieval, processing, inference, and admin endpoints are never
// contacted from the browser.
export type ChatStatus = "answered" | "insufficient_evidence" | "temporarily_unavailable";

export interface ChatSource {
  documentId: string;
  versionId: string;
  chunkId: string;
  rank: number;
  excerpt: string;
  sourceMeta?: Record<string, unknown>;
}

export interface ChatAnswer {
  sessionId: string;
  status: ChatStatus;
  answer: string;
  sources: ChatSource[];
}

export class ChatApiError extends Error {
  code: string;
  status: number;

  constructor(code: string, message: string, status: number) {
    super(message);
    this.name = "ChatApiError";
    this.code = code;
    this.status = status;
  }
}

interface Envelope {
  success: boolean;
  data?: unknown;
  error?: { code?: string; message?: string };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function isValidSource(value: unknown): value is ChatSource {
  if (!isRecord(value)) return false;
  return (
    typeof value.documentId === "string" &&
    typeof value.versionId === "string" &&
    typeof value.chunkId === "string" &&
    typeof value.rank === "number" &&
    typeof value.excerpt === "string" &&
    (value.sourceMeta === undefined || isRecord(value.sourceMeta))
  );
}

// guardAnswer validates the trusted chat API response shape before anything
// renders. Malformed payloads are rejected, never rendered.
export function guardAnswer(data: unknown): ChatAnswer {
  if (!isRecord(data)) throw new ChatApiError("BAD_RESPONSE", "The chat service returned an unreadable response.", 0);
  const { sessionId, status, answer, sources } = data;
  if (typeof sessionId !== "string" || !/^ses_[0-9a-f]{32}$/.test(sessionId)) {
    throw new ChatApiError("BAD_RESPONSE", "The chat service returned an invalid session.", 0);
  }
  if (status !== "answered" && status !== "insufficient_evidence" && status !== "temporarily_unavailable") {
    throw new ChatApiError("BAD_RESPONSE", "The chat service returned an unknown status.", 0);
  }
  if (typeof answer !== "string") {
    throw new ChatApiError("BAD_RESPONSE", "The chat service returned an invalid answer.", 0);
  }
  if (!Array.isArray(sources) || !sources.every(isValidSource)) {
    throw new ChatApiError("BAD_RESPONSE", "The chat service returned invalid sources.", 0);
  }
  return { sessionId, status, answer, sources };
}

export async function postChat(
  apiBase: string,
  input: { widgetId: string; message: string; sessionId?: string; idempotencyKey?: string },
  fetchImpl: typeof fetch = fetch,
): Promise<ChatAnswer> {
  let res: Response;
  try {
    res = await fetchImpl(`${apiBase.replace(/\/+$/, "")}/api/v1/chat`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        widgetId: input.widgetId,
        ...(input.sessionId ? { sessionId: input.sessionId } : {}),
        message: input.message,
        ...(input.idempotencyKey ? { idempotencyKey: input.idempotencyKey } : {}),
      }),
    });
  } catch {
    throw new ChatApiError("NETWORK_ERROR", "Could not reach the chat service. Check your connection and retry.", 0);
  }
  let envelope: Envelope;
  try {
    envelope = (await res.json()) as Envelope;
  } catch {
    throw new ChatApiError("BAD_RESPONSE", "The chat service returned an unreadable response.", res.status);
  }
  if (!envelope.success) {
    throw new ChatApiError(
      envelope.error?.code || "UNKNOWN_ERROR",
      envelope.error?.message || "The chat request failed.",
      res.status,
    );
  }
  return guardAnswer(envelope.data);
}

export interface HistoryMessage {
  role: "user" | "assistant";
  content: string;
  status: string;
  sources: ChatSource[];
  createdAt: string;
}

export async function fetchHistory(
  apiBase: string,
  widgetId: string,
  sessionId: string,
  fetchImpl: typeof fetch = fetch,
): Promise<HistoryMessage[]> {
  const url = `${apiBase.replace(/\/+$/, "")}/api/v1/chat/sessions/${sessionId}?widgetId=${encodeURIComponent(widgetId)}`;
  let res: Response;
  try {
    res = await fetchImpl(url);
  } catch {
    throw new ChatApiError("NETWORK_ERROR", "Could not reach the chat service.", 0);
  }
  if (!res.ok) {
    throw new ChatApiError("HISTORY_FAILED", "Could not load conversation history.", res.status);
  }
  const envelope = (await res.json()) as Envelope;
  if (!envelope.success || !isRecord(envelope.data) || !Array.isArray(envelope.data.messages)) {
    throw new ChatApiError("BAD_RESPONSE", "The chat service returned invalid history.", res.status);
  }
  return (envelope.data.messages as unknown[]).filter(isValidHistoryMessage) as HistoryMessage[];
}

function isValidHistoryMessage(value: unknown): value is HistoryMessage {
  if (!isRecord(value)) return false;
  return (
    (value.role === "user" || value.role === "assistant") &&
    typeof value.content === "string" &&
    typeof value.status === "string" &&
    Array.isArray(value.sources) &&
    (value.sources as unknown[]).every(isValidSource) &&
    typeof value.createdAt === "string"
  );
}
