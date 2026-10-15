import { useEffect, useRef, useState } from "react";
import { ChatApiError, fetchHistory, postChat } from "../api/chat";
import type { ChatAnswer, ChatSource } from "../api/chat";
import { clearSessionId, loadSessionId, saveSessionId } from "../lib/session";
import { outboundMessage, guardInbound } from "../lib/loaderMessages";
import { sourceLabel, sourceLink } from "../lib/safe";

interface Turn {
  id: number;
  role: "user" | "assistant";
  text: string;
  status?: ChatAnswer["status"];
  sources?: ChatSource[];
  failed?: boolean;
}

let turnCounter = 0;
function nextId(): number {
  turnCounter += 1;
  return turnCounter;
}

export function ChatWidget({ widgetId, apiBase, hostOrigin }: { widgetId: string; apiBase: string; hostOrigin: string }) {
  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState("Support chat");
  const [turns, setTurns] = useState<Turn[]>([]);
  const [input, setInput] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [unavailable, setUnavailable] = useState(false);
  const sessionRef = useRef<string | null>(null);
  const listRef = useRef<HTMLDivElement>(null);

  // Load public widget configuration (display name only; treated as non-secret).
  useEffect(() => {
    let cancelled = false;
    fetch(`${apiBase.replace(/\/+$/, "")}/api/v1/widget-config/${encodeURIComponent(widgetId)}`)
      .then((res) => (res.ok ? res.json() : null))
      .then((body: unknown) => {
        if (cancelled || !body || typeof body !== "object") return;
        const data = (body as { data?: { displayName?: unknown } }).data;
        if (typeof data?.displayName === "string" && data.displayName.trim()) {
          setTitle(data.displayName.trim().slice(0, 80));
        }
      })
      .catch(() => {
        // Config is cosmetic; the chat API remains the source of truth.
      });
    return () => {
      cancelled = true;
    };
  }, [apiBase, widgetId]);

  // Restore a previous session's visible history.
  useEffect(() => {
    const saved = loadSessionId(widgetId);
    if (!saved) return;
    sessionRef.current = saved;
    fetchHistory(apiBase, widgetId, saved)
      .then((messages) => {
        setTurns(
          messages.map((m) => ({
            id: nextId(),
            role: m.role,
            text: m.content,
            status: m.role === "assistant" ? (m.status === "fallback" ? "insufficient_evidence" : "answered") : undefined,
            sources: m.role === "assistant" ? m.sources : undefined,
          })),
        );
      })
      .catch(() => {
        clearSessionId(widgetId);
        sessionRef.current = null;
      });
  }, [apiBase, widgetId]);

  // Host-page commands (loader buttons) with exact-origin validation.
  useEffect(() => {
    function onMessage(event: MessageEvent) {
      const command = guardInbound(event, hostOrigin);
      if (!command) return;
      if (command.type === "jnana-open") setOpen(true);
      else if (command.type === "jnana-close") setOpen(false);
      else setOpen((v) => !v);
    }
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [hostOrigin]);

  useEffect(() => {
    window.parent.postMessage(outboundMessage(open ? { type: "jnana-opened" } : { type: "jnana-closed" }), hostOrigin);
    const list = listRef.current;
    if (list && typeof list.scrollTo === "function") {
      list.scrollTo({ top: list.scrollHeight });
    }
  }, [open, turns, hostOrigin]);

  async function send(messageText?: string, retryTurn?: Turn) {
    const text = (messageText ?? input).trim();
    if (!text || busy) return;
    if (text.length > 2000) {
      setError("Messages are limited to 2000 characters.");
      return;
    }
    setError(null);
    setUnavailable(false);
    const userTurn: Turn = retryTurn && retryTurn.role === "user" ? retryTurn : { id: nextId(), role: "user", text };
    if (!retryTurn) {
      setTurns((prev) => [...prev, userTurn]);
      setInput("");
    } else {
      setTurns((prev) => prev.map((t) => (t.id === retryTurn.id ? { ...t, failed: false } : t)));
    }
    setBusy(true);
    try {
      const answer = await postChat(apiBase, {
        widgetId,
        message: text,
        sessionId: sessionRef.current ?? undefined,
        idempotencyKey: `web-${userTurn.id}-${text.length}`,
      });
      sessionRef.current = answer.sessionId;
      saveSessionId(widgetId, answer.sessionId);
      setTurns((prev) => [
        ...prev,
        { id: nextId(), role: "assistant", text: answer.answer, status: answer.status, sources: answer.sources },
      ]);
      if (answer.status === "temporarily_unavailable") setUnavailable(true);
    } catch (err) {
      if (err instanceof ChatApiError && (err.code === "SESSION_NOT_FOUND" || err.status === 404)) {
        clearSessionId(widgetId);
        sessionRef.current = null;
      }
      setTurns((prev) => prev.map((t) => (t.id === userTurn.id ? { ...t, failed: true } : t)));
      setError(err instanceof Error ? err.message : "Sending failed.");
    } finally {
      setBusy(false);
    }
  }

  function retry(turn: Turn) {
    void send(turn.text, turn);
  }

  return (
    <div className="jnana-root">
      {!open && (
        <button
          type="button"
          className="jnana-launcher"
          aria-label="Open support chat"
          onClick={() => setOpen(true)}
        >
          <span aria-hidden="true">💬</span>
        </button>
      )}
      {open && (
        <section className="jnana-panel" role="dialog" aria-modal="false" aria-label={title}>
          <header className="jnana-header">
            <h1>{title}</h1>
            <button type="button" aria-label="Close support chat" onClick={() => setOpen(false)}>
              ✕
            </button>
          </header>
          <div className="jnana-messages" ref={listRef} aria-live="polite">
            {turns.length === 0 && <p className="jnana-hint">Ask a question about our products or policies.</p>}
            {turns.map((turn) =>
              turn.role === "user" ? (
                <div key={turn.id} className="jnana-bubble jnana-user">
                  <p>{turn.text}</p>
                  {turn.failed && (
                    <button type="button" onClick={() => retry(turn)}>
                      Retry
                    </button>
                  )}
                </div>
              ) : (
                <div key={turn.id} className="jnana-bubble jnana-assistant">
                  <p>{turn.text}</p>
                  {turn.status === "insufficient_evidence" && (
                    <p className="jnana-fallback-note">This answer uses our general fallback — no verified sources were found.</p>
                  )}
                  {turn.sources && turn.sources.length > 0 && (
                    <ul className="jnana-sources" aria-label="Verified sources">
                      {turn.sources.map((s) => {
                        const link = sourceLink(s.sourceMeta);
                        const label = sourceLabel(s.sourceMeta, `Source ${s.rank}`);
                        return (
                          <li key={s.chunkId}>
                            {link ? (
                              <a href={link} target="_blank" rel="noopener noreferrer">
                                {label}
                              </a>
                            ) : (
                              <span>{label}</span>
                            )}
                          </li>
                        );
                      })}
                    </ul>
                  )}
                </div>
              ),
            )}
            {busy && (
              <p className="jnana-hint" role="status">
                Thinking…
              </p>
            )}
          </div>
          {error && (
            <p role="alert" className="jnana-error">
              {error}
            </p>
          )}
          {unavailable && <p className="jnana-hint">The service is temporarily unavailable — please try again shortly.</p>}
          <form
            className="jnana-input"
            onSubmit={(e) => {
              e.preventDefault();
              void send();
            }}
          >
            <label htmlFor="jnana-message" className="jnana-sr">
              Type your message
            </label>
            <input
              id="jnana-message"
              value={input}
              onChange={(e) => setInput(e.target.value)}
              maxLength={2000}
              placeholder="Type your message…"
              autoComplete="off"
              disabled={busy}
            />
            <button type="submit" disabled={busy || !input.trim()} aria-label="Send message">
              ➤
            </button>
          </form>
        </section>
      )}
    </div>
  );
}
