import { describe, expect, it, vi, beforeEach } from "vitest";
import { clearSession, isExpired, loadSession, saveSession, signIn, signOut } from "./supabaseAuth";
import type { AuthSession } from "./supabaseAuth";

const session: AuthSession = {
  accessToken: "access",
  refreshToken: "refresh",
  userId: "user-1",
  email: "admin@example.com",
  expiresAt: Date.now() + 3600_000,
};

function okSessionResponse() {
  return new Response(
    JSON.stringify({
      access_token: "access",
      refresh_token: "refresh",
      user: { id: "user-1", email: "admin@example.com" },
      expires_in: 3600,
    }),
    { status: 200 },
  );
}

describe("supabase auth", () => {
  beforeEach(() => {
    sessionStorage.clear();
    vi.stubEnv("VITE_SUPABASE_URL", "https://project.supabase.co");
    vi.stubEnv("VITE_SUPABASE_ANON_KEY", "public-anon-key");
  });

  it("signs in through the public GoTrue endpoint with the anon key", async () => {
    const fetchImpl = vi.fn(async () => okSessionResponse());
    const next = await signIn("admin@example.com", "secret", fetchImpl as unknown as typeof fetch);
    expect(next.userId).toBe("user-1");
    const [url, init] = fetchImpl.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe("https://project.supabase.co/auth/v1/token?grant_type=password");
    expect((init.headers as Record<string, string>).apikey).toBe("public-anon-key");
    const body = JSON.parse(init.body as string) as Record<string, string>;
    expect(body.email).toBe("admin@example.com");
  });

  it("surfaces backend auth failures", async () => {
    const fetchImpl = vi.fn(
      async () => new Response(JSON.stringify({ msg: "Invalid login credentials" }), { status: 400 }),
    );
    await expect(signIn("a@b.c", "wrong", fetchImpl as unknown as typeof fetch)).rejects.toThrow(
      "Invalid login credentials",
    );
  });

  it("persists the session in sessionStorage and detects expiry", () => {
    saveSession(session);
    expect(loadSession()?.accessToken).toBe("access");
    expect(isExpired(session)).toBe(false);
    expect(isExpired({ ...session, expiresAt: Date.now() - 1 })).toBe(true);
    clearSession();
    expect(loadSession()).toBeNull();
  });

  it("sign-out is best-effort and always clears locally", async () => {
    saveSession(session);
    const failing = vi.fn(async () => {
      throw new Error("offline");
    });
    await signOut(session, failing as unknown as typeof fetch);
    expect(loadSession()).toBeNull();
  });
});
