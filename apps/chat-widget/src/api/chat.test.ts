import { describe, expect, it, vi } from "vitest";
import { ChatApiError, fetchHistory, guardAnswer, postChat } from "./chat";

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status });
}

const good = {
  sessionId: "ses_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  status: "answered",
  answer: "Yes.",
  sources: [
    { documentId: "doc-1", versionId: "ver-1", chunkId: "c-1", rank: 1, excerpt: "Yes." },
  ],
};

describe("guardAnswer", () => {
  it("accepts valid answers", () => {
    expect(guardAnswer(good).sessionId).toBe(good.sessionId);
  });

  it("rejects malformed payloads", () => {
    const bad = [
      null,
      {},
      { ...good, sessionId: "nope" },
      { ...good, status: "maybe" },
      { ...good, answer: 42 },
      { ...good, sources: [{ documentId: "d" }] },
      { ...good, sources: "x" },
    ];
    for (const payload of bad) {
      expect(() => guardAnswer(payload), JSON.stringify(payload)).toThrow(ChatApiError);
    }
  });
});

describe("postChat", () => {
  it("posts to the Module 6 endpoint and validates the response", async () => {
    const fetchImpl = vi.fn(async () => jsonResponse({ success: true, data: good, meta: {} }));
    const answer = await postChat(
      "http://api/",
      { widgetId: "wdg_1", message: "Hi" },
      fetchImpl as unknown as typeof fetch,
    );
    expect(answer.status).toBe("answered");
    const [url, init] = fetchImpl.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe("http://api/api/v1/chat");
    expect(JSON.parse(init.body as string)).toMatchObject({ widgetId: "wdg_1", message: "Hi" });
  });

  it("surfaces backend error codes", async () => {
    const fetchImpl = vi.fn(async () =>
      jsonResponse({ success: false, error: { code: "RATE_LIMITED", message: "Slow down." }, meta: {} }, 429),
    );
    await expect(
      postChat("http://api", { widgetId: "w", message: "Hi" }, fetchImpl as unknown as typeof fetch),
    ).rejects.toMatchObject({ code: "RATE_LIMITED", status: 429 });
  });

  it("maps network failures to retryable errors", async () => {
    const fetchImpl = vi.fn(async () => {
      throw new TypeError("down");
    });
    await expect(
      postChat("http://api", { widgetId: "w", message: "Hi" }, fetchImpl as unknown as typeof fetch),
    ).rejects.toMatchObject({ code: "NETWORK_ERROR" });
  });

  it("rejects unreadable responses", async () => {
    const fetchImpl = vi.fn(async () => new Response("nope", { status: 200 }));
    await expect(
      postChat("http://api", { widgetId: "w", message: "Hi" }, fetchImpl as unknown as typeof fetch),
    ).rejects.toMatchObject({ code: "BAD_RESPONSE" });
  });
});

describe("fetchHistory", () => {
  it("returns validated turns", async () => {
    const fetchImpl = vi.fn(async () =>
      jsonResponse({
        success: true,
        data: {
          messages: [
            { role: "user", content: "Hi", status: "ok", sources: [], createdAt: "2026-01-01T00:00:00Z" },
            { role: "bot", content: "Nope", status: "ok", sources: [], createdAt: "" },
          ],
        },
        meta: {},
      }),
    );
    const messages = await fetchHistory(
      "http://api",
      "wdg_1",
      "ses_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      fetchImpl as unknown as typeof fetch,
    );
    expect(messages).toHaveLength(1);
    expect(messages[0].role).toBe("user");
  });
});
