import { useEffect, useState } from "react";
import { ApiError } from "../api/client";
import { organizations, testChat, widgets } from "../api/resources";
import type { ChatAnswer, Membership, Widget } from "../api/types";
import { buildEmbedSnippet, widgetAppUrl } from "../lib/snippet";
import { validateWidgetForm } from "../lib/validation";
import { Empty, ErrorState, Layout, Loading } from "../components/ui";
import { useClient } from "./organizations";

export function WidgetDetailPage({ orgId, widgetId }: { orgId: string; widgetId: string }) {
  const client = useClient();
  const [widget, setWidget] = useState<Widget | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [displayName, setDisplayName] = useState("");
  const [originsText, setOriginsText] = useState("");
  const [active, setActive] = useState(true);
  const [busy, setBusy] = useState(false);
  const widgetHost = (import.meta.env.VITE_WIDGET_HOST as string | undefined) || "http://localhost:5174";
  const apiBase = (import.meta.env.VITE_API_BASE_URL as string | undefined) || "http://localhost:8080";

  async function load() {
    try {
      const list = await widgets.list(client, orgId);
      const found = list.widgets.find((w) => w.id === widgetId) || null;
      if (!found) {
        setError("Widget not found.");
        return;
      }
      setWidget(found);
      setDisplayName(found.displayName);
      setOriginsText(found.allowedOrigins.join("\n"));
      setActive(found.active);
    } catch (err) {
      setError(err instanceof ApiError ? `${err.code}: ${err.message}` : "Could not load the widget.");
    }
  }

  useEffect(() => {
    void load();
  }, [orgId, widgetId]); // eslint-disable-line react-hooks/exhaustive-deps

  async function save(event: React.FormEvent) {
    event.preventDefault();
    const origins = originsText.split("\n").map((s) => s.trim()).filter(Boolean);
    const problem = validateWidgetForm(displayName, origins);
    if (problem) {
      setError(problem);
      return;
    }
    setBusy(true);
    try {
      await widgets.update(client, orgId, widgetId, { displayName: displayName.trim(), allowedOrigins: origins, active });
      setNotice("Widget settings saved. Authorization is enforced by the backend.");
      await load();
    } catch (err) {
      setError(err instanceof ApiError ? `${err.code}: ${err.message}` : "Save failed.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Layout>
      <p>
        <a href={`#/orgs/${orgId}/widgets`}>← Widgets</a>
      </p>
      <h1>Widget settings</h1>
      {error && <ErrorState message={error} onRetry={() => void load()} />}
      {notice && (
        <p role="status" className="ok-text">
          {notice}
        </p>
      )}
      {widget === null ? (
        <Loading />
      ) : (
        <>
          <form onSubmit={(e) => void save(e)} aria-label="Widget settings">
            <label>
              Display name
              <input value={displayName} onChange={(e) => setDisplayName(e.target.value)} maxLength={120} />
            </label>
            <label>
              Allowed origins (one per line)
              <textarea
                value={originsText}
                onChange={(e) => setOriginsText(e.target.value)}
                rows={4}
                placeholder="https://example.com"
              />
            </label>
            <label className="checkbox">
              <input type="checkbox" checked={active} onChange={(e) => setActive(e.target.checked)} /> Active
            </label>
            <button type="submit" disabled={busy}>
              {busy ? "Saving…" : "Save"}
            </button>
          </form>

          <h2>Installation</h2>
          <p className="muted">
            Paste this snippet before <code>{`</body>`}</code> on every allowed website. The public widget ID is
            not a secret; the backend validates the website origin on every chat request.
          </p>
          <pre data-testid="embed-snippet">
            <code>{buildEmbedSnippet({ widgetHost, widgetId: widget.publicWidgetId })}</code>
          </pre>
          <button
            type="button"
            onClick={() => void navigator.clipboard?.writeText(buildEmbedSnippet({ widgetHost, widgetId: widget.publicWidgetId }))}
          >
            Copy snippet
          </button>
          <p className="muted">
            Direct link (used by the loader iframe):{" "}
            <code>{widgetAppUrl(widgetHost, widget.publicWidgetId, apiBase)}</code>
          </p>
        </>
      )}
    </Layout>
  );
}

export function TeamPage({ orgId }: { orgId: string }) {
  const client = useClient();
  const [members, setMembers] = useState<Membership[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    organizations
      .members(client, orgId)
      .then((d) => setMembers(d.members))
      .catch((err: unknown) =>
        setError(err instanceof ApiError ? `${err.code}: ${err.message}` : "Could not load the team."),
      );
  }, [orgId]); // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <Layout>
      <p>
        <a href={`#/orgs/${orgId}`}>← Knowledge base</a>
      </p>
      <h1>Team</h1>
      <p className="muted">Membership changes are authorized by the backend; roles shown here are informational.</p>
      {error && <ErrorState message={error} />}
      {members === null ? (
        <Loading />
      ) : members.length === 0 ? (
        <Empty message="No members found." />
      ) : (
        <ul className="list">
          {members.map((m) => (
            <li key={m.id}>
              <span className="mono">{m.userId}</span> · {m.role}
            </li>
          ))}
        </ul>
      )}
    </Layout>
  );
}

export function TestChatPage({ orgId }: { orgId: string }) {
  const client = useClient();
  const [list, setList] = useState<Widget[] | null>(null);
  const [widgetId, setWidgetId] = useState("");
  const [message, setMessage] = useState("");
  const [sessionId, setSessionId] = useState<string | undefined>(undefined);
  const [answer, setAnswer] = useState<ChatAnswer | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    widgets
      .list(client, orgId)
      .then((d) => {
        setList(d.widgets);
        const firstActive = d.widgets.find((w) => w.active);
        if (firstActive) setWidgetId(firstActive.publicWidgetId);
      })
      .catch(() => setError("Could not load widgets."));
  }, [orgId]); // eslint-disable-line react-hooks/exhaustive-deps

  async function send(event: React.FormEvent) {
    event.preventDefault();
    if (!widgetId || !message.trim()) {
      setError("Choose a widget and type a message.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      // Test chat uses the same public Module 6 endpoint as the widget.
      const res = await testChat.send(client, widgetId, message.trim(), sessionId);
      setAnswer(res);
      setSessionId(res.sessionId);
    } catch (err) {
      setError(err instanceof ApiError ? `${err.code}: ${err.message}` : "Test chat failed.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Layout>
      <p>
        <a href={`#/orgs/${orgId}`}>← Knowledge base</a>
      </p>
      <h1>Test chat</h1>
      <p className="muted">Preview answers exactly as customers will receive them, including sources and fallbacks.</p>
      <form onSubmit={(e) => void send(e)} aria-label="Test chat">
        <label>
          Widget
          <select value={widgetId} onChange={(e) => setWidgetId(e.target.value)} aria-label="Widget">
            {(list || []).map((w) => (
              <option key={w.id} value={w.publicWidgetId}>
                {w.displayName} ({w.active ? "active" : "disabled"})
              </option>
            ))}
          </select>
        </label>
        <label>
          Message
          <input value={message} onChange={(e) => setMessage(e.target.value)} maxLength={2000} />
        </label>
        <button type="submit" disabled={busy}>
          {busy ? "Asking…" : "Ask"}
        </button>
        <button type="button" onClick={() => setSessionId(undefined)} title="Start a new session">
          New session
        </button>
      </form>
      {error && (
        <p role="alert" className="error-text">
          {error}
        </p>
      )}
      {answer && (
        <section aria-live="polite" className="answer">
          <p>
            <strong>Status:</strong> {answer.status}
          </p>
          <p>{answer.answer}</p>
          {answer.sources.length > 0 && (
            <>
              <h3>Sources</h3>
              <ul>
                {answer.sources.map((s) => (
                  <li key={s.chunkId}>
                    document <span className="mono">{s.documentId}</span> · version{" "}
                    <span className="mono">{s.versionId}</span> · rank {s.rank}
                  </li>
                ))}
              </ul>
            </>
          )}
        </section>
      )}
    </Layout>
  );
}
