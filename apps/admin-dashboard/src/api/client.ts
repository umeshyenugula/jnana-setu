// Typed API client. UI components never call fetch directly; they use the
// per-resource functions below. The client speaks the Module 0 envelope
// shapes and surfaces backend error codes unchanged.
import type { ApiEnvelope } from "./types";

export class ApiError extends Error {
  code: string;
  status: number;
  correlationId: string;

  constructor(code: string, message: string, status: number, correlationId = "") {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.status = status;
    this.correlationId = correlationId;
  }
}

export interface ClientOptions {
  baseUrl: string;
  getToken?: () => string | null;
  fetchImpl?: typeof fetch;
}

async function parseEnvelope<T>(res: Response): Promise<T> {
  let body: ApiEnvelope<T>;
  try {
    body = (await res.json()) as ApiEnvelope<T>;
  } catch {
    throw new ApiError("BAD_RESPONSE", "The server returned an unreadable response.", res.status);
  }
  if (body && body.success === true) {
    return (body as { data: T }).data;
  }
  const failure = body as { error?: { code?: string; message?: string }; meta?: { correlationId?: string } };
  throw new ApiError(
    failure?.error?.code || "UNKNOWN_ERROR",
    failure?.error?.message || "The request failed.",
    res.status,
    failure?.meta?.correlationId || "",
  );
}

export class ApiClient {
  private baseUrl: string;
  private getToken: () => string | null;
  private fetchImpl: typeof fetch;

  constructor(options: ClientOptions) {
    this.baseUrl = options.baseUrl.replace(/\/+$/, "");
    this.getToken = options.getToken || (() => null);
    this.fetchImpl = options.fetchImpl || fetch.bind(globalThis);
  }

  private headers(extra?: Record<string, string>): Record<string, string> {
    const headers: Record<string, string> = { ...(extra || {}) };
    const token = this.getToken();
    if (token) {
      headers["Authorization"] = `Bearer ${token}`;
    }
    return headers;
  }

  async get<T>(path: string): Promise<T> {
    const res = await this.fetchImpl(this.baseUrl + path, { headers: this.headers() });
    return parseEnvelope<T>(res);
  }

  async postJson<T>(path: string, body: unknown): Promise<T> {
    const res = await this.fetchImpl(this.baseUrl + path, {
      method: "POST",
      headers: this.headers({ "Content-Type": "application/json" }),
      body: JSON.stringify(body),
    });
    return parseEnvelope<T>(res);
  }

  async postForm<T>(path: string, form: FormData): Promise<T> {
    const res = await this.fetchImpl(this.baseUrl + path, {
      method: "POST",
      headers: this.headers(),
      body: form,
    });
    return parseEnvelope<T>(res);
  }

  async patchJson<T>(path: string, body: unknown): Promise<T> {
    const res = await this.fetchImpl(this.baseUrl + path, {
      method: "PATCH",
      headers: this.headers({ "Content-Type": "application/json" }),
      body: JSON.stringify(body),
    });
    return parseEnvelope<T>(res);
  }

  async del<T>(path: string): Promise<T> {
    const res = await this.fetchImpl(this.baseUrl + path, {
      method: "DELETE",
      headers: this.headers(),
    });
    return parseEnvelope<T>(res);
  }
}

export function apiBaseUrl(): string {
  const fromEnv = (import.meta as unknown as { env?: Record<string, string | undefined> }).env?.VITE_API_BASE_URL;
  return (fromEnv || "http://localhost:8080").replace(/\/+$/, "");
}
