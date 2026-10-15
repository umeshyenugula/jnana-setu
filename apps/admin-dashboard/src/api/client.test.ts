import { describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "./client";

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("ApiClient envelopes", () => {
  it("returns data on success envelopes", async () => {
    const fetchImpl = vi.fn(async () =>
      jsonResponse({ success: true, data: { organizations: [] }, meta: { correlationId: "c" } }),
    );
    const client = new ApiClient({ baseUrl: "http://api", fetchImpl: fetchImpl as unknown as typeof fetch });
    await expect(client.get("/api/v1/organizations")).resolves.toEqual({ organizations: [] });
  });

  it("throws ApiError with backend code on failure envelopes", async () => {
    const fetchImpl = vi.fn(async () =>
      jsonResponse({ success: false, error: { code: "FORBIDDEN", message: "No." }, meta: { correlationId: "c1" } }, 403),
    );
    const client = new ApiClient({ baseUrl: "http://api", fetchImpl: fetchImpl as unknown as typeof fetch });
    try {
      await client.get("/x");
      expect.unreachable();
    } catch (err) {
      expect(err).toBeInstanceOf(ApiError);
      const apiErr = err as ApiError;
      expect(apiErr.code).toBe("FORBIDDEN");
      expect(apiErr.status).toBe(403);
      expect(apiErr.correlationId).toBe("c1");
    }
  });

  it("rejects unreadable responses", async () => {
    const fetchImpl = vi.fn(async () => new Response("not json", { status: 200 }));
    const client = new ApiClient({ baseUrl: "http://api", fetchImpl: fetchImpl as unknown as typeof fetch });
    await expect(client.get("/x")).rejects.toMatchObject({ code: "BAD_RESPONSE" });
  });

  it("sends the bearer token when a session exists", async () => {
    const fetchImpl = vi.fn(async () => jsonResponse({ success: true, data: {}, meta: {} }));
    const client = new ApiClient({
      baseUrl: "http://api",
      getToken: () => "tok123",
      fetchImpl: fetchImpl as unknown as typeof fetch,
    });
    await client.get("/x");
    expect(fetchImpl).toHaveBeenCalledOnce();
    const [, init] = fetchImpl.mock.calls[0] as unknown as [string, RequestInit];
    expect((init.headers as Record<string, string>)["Authorization"]).toBe("Bearer tok123");
  });

  it("sends no Authorization header without a session", async () => {
    const fetchImpl = vi.fn(async () => jsonResponse({ success: true, data: {}, meta: {} }));
    const client = new ApiClient({ baseUrl: "http://api", fetchImpl: fetchImpl as unknown as typeof fetch });
    await client.get("/x");
    const [, init] = fetchImpl.mock.calls[0] as unknown as [string, RequestInit];
    expect((init.headers as Record<string, string>)["Authorization"]).toBeUndefined();
  });
});
