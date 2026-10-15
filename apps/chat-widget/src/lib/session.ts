// Session persistence. The session ID is an opaque server-issued value stored
// per widget in sessionStorage so follow-up turns reuse the conversation.
// It is cleared when it stops working (404) and never shared across widgets.
export function sessionKey(widgetId: string): string {
  return `jnana.session.${widgetId}`;
}

export function loadSessionId(widgetId: string, storage: Storage = sessionStorage): string | null {
  try {
    const value = storage.getItem(sessionKey(widgetId));
    if (value && /^ses_[0-9a-f]{32}$/.test(value)) return value;
    return null;
  } catch {
    return null;
  }
}

export function saveSessionId(widgetId: string, sessionId: string, storage: Storage = sessionStorage): void {
  try {
    storage.setItem(sessionKey(widgetId), sessionId);
  } catch {
    // Private-mode storage failures must not break chatting.
  }
}

export function clearSessionId(widgetId: string, storage: Storage = sessionStorage): void {
  try {
    storage.removeItem(sessionKey(widgetId));
  } catch {
    // ignore
  }
}
