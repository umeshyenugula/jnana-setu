// Supabase Auth integration for administrator sign-in (Module 1 design).
// Uses the public GoTrue REST API directly — no extra dependency. Only the
// publishable anon key is used; service-role keys must never appear here.
export interface AuthSession {
  accessToken: string;
  refreshToken: string;
  userId: string;
  email: string;
  expiresAt: number;
}

const STORAGE_KEY = "jnana.admin.session.v1";

function supabaseBase(): string {
  const base = import.meta.env.VITE_SUPABASE_URL as string | undefined;
  if (!base) throw new Error("VITE_SUPABASE_URL is not configured.");
  return base.replace(/\/+$/, "");
}

function anonKey(): string {
  const key = import.meta.env.VITE_SUPABASE_ANON_KEY as string | undefined;
  if (!key) throw new Error("VITE_SUPABASE_ANON_KEY is not configured.");
  return key;
}

async function readError(res: Response): Promise<Error> {
  try {
    const body = (await res.json()) as { msg?: string; message?: string; error_description?: string };
    return new Error(body.msg || body.message || body.error_description || `Sign-in failed (HTTP ${res.status}).`);
  } catch {
    return new Error(`Sign-in failed (HTTP ${res.status}).`);
  }
}

export async function signIn(email: string, password: string, fetchImpl: typeof fetch = fetch): Promise<AuthSession> {
  const res = await fetchImpl(`${supabaseBase()}/auth/v1/token?grant_type=password`, {
    method: "POST",
    headers: { "Content-Type": "application/json", apikey: anonKey() },
    body: JSON.stringify({ email: email.trim(), password }),
  });
  if (!res.ok) throw await readError(res);
  const body = (await res.json()) as {
    access_token: string;
    refresh_token: string;
    user: { id: string; email?: string };
    expires_in: number;
  };
  if (!body.access_token || !body.user?.id) {
    throw new Error("Sign-in returned an incomplete session.");
  }
  return {
    accessToken: body.access_token,
    refreshToken: body.refresh_token || "",
    userId: body.user.id,
    email: body.user.email || email.trim(),
    expiresAt: Date.now() + (body.expires_in || 3600) * 1000,
  };
}

export async function signOut(
  session: AuthSession | null,
  fetchImpl: typeof fetch = fetch,
  storage: Storage = sessionStorage,
): Promise<void> {
  if (!session) return;
  try {
    await fetchImpl(`${supabaseBase()}/auth/v1/logout`, {
      method: "POST",
      headers: { "Content-Type": "application/json", apikey: anonKey(), Authorization: `Bearer ${session.accessToken}` },
    });
  } catch {
    // Logout is best-effort; the local session is cleared regardless.
  } finally {
    clearSession(storage);
  }
}

export function loadSession(storage: Storage = sessionStorage): AuthSession | null {
  try {
    const raw = storage.getItem(STORAGE_KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as AuthSession;
    if (!parsed.accessToken || !parsed.userId) return null;
    return parsed;
  } catch {
    return null;
  }
}

export function saveSession(session: AuthSession, storage: Storage = sessionStorage): void {
  storage.setItem(STORAGE_KEY, JSON.stringify(session));
}

export function clearSession(storage: Storage = sessionStorage): void {
  storage.removeItem(STORAGE_KEY);
}

export function isExpired(session: AuthSession, now = Date.now()): boolean {
  return session.expiresAt <= now;
}
