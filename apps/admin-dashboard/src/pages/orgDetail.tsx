import { useEffect, useState } from "react";
import { ApiError } from "../api/client";
import { knowledge, widgets } from "../api/resources";
import type { KnowledgeDocument, Widget } from "../api/types";
import { validateTextInput, validateUploadFile } from "../lib/validation";
import { Empty, ErrorState, Layout, Loading, StatusBadge } from "../components/ui";
import { useClient } from "./organizations";

export function OrgDetailPage({ orgId }: { orgId: string }) {
  const client = useClient();
  const [docs, setDocs] = useState<KnowledgeDocument[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  // Upload form state.
  const [title, setTitle] = useState("");
  const [text, setText] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const [formError, setFormError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function load() {
    setError(null);
    try {
      const data = await knowledge.list(client, orgId, true);
      setDocs(data.documents);
    } catch (err) {
      setError(err instanceof ApiError ? `${err.code}: ${err.message}` : "Could not load documents.");
    }
  }

  useEffect(() => {
    void load();
  }, [orgId]); // eslint-disable-line react-hooks/exhaustive-deps

  async function submitText(event: React.FormEvent) {
    event.preventDefault();
    const problem = validateTextInput(title, text);
    if (problem) {
      setFormError(problem);
      return;
    }
    setFormError(null);
    setBusy(true);
    try {
      await knowledge.createText(client, orgId, title.trim(), text);
      setTitle("");
      setText("");
      await load();
    } catch (err) {
      setFormError(err instanceof ApiError ? `${err.code}: ${err.message}` : "Text submission failed.");
    } finally {
      setBusy(false);
    }
  }

  async function submitFile(event: React.FormEvent) {
    event.preventDefault();
    if (!file) {
      setFormError("Choose a file to upload.");
      return;
    }
    const problem = validateUploadFile(file);
    if (problem) {
      setFormError(problem);
      return;
    }
    setFormError(null);
    setBusy(true);
    try {
      await knowledge.uploadFile(client, orgId, title, file);
      setTitle("");
      setFile(null);
      await load();
    } catch (err) {
      setFormError(err instanceof ApiError ? `${err.code}: ${err.message}` : "Upload failed.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Layout>
      <p>
        <a href="#/">← Organizations</a> · <a href={`#/orgs/${orgId}/widgets`}>Widget settings</a> ·{" "}
        <a href={`#/orgs/${orgId}/team`}>Team</a> · <a href={`#/orgs/${orgId}/test-chat`}>Test chat</a>
      </p>
      <h1>Knowledge base</h1>
      {error && <ErrorState message={error} onRetry={() => void load()} />}
      {docs === null ? (
        <Loading />
      ) : docs.length === 0 ? (
        <Empty message="No documents yet. Add text or upload a file below." />
      ) : (
        <ul className="list">
          {docs.map((doc) => (
            <li key={doc.id}>
              <a href={`#/orgs/${orgId}/docs/${doc.id}`}>{doc.title}</a>{" "}
              <StatusBadge status={doc.isActive ? doc.processingStatus : "inactive"} />{" "}
              <span className="muted">{doc.sourceType}</span>
            </li>
          ))}
        </ul>
      )}

      <h2>Add knowledge as text</h2>
      <form onSubmit={(e) => void submitText(e)} aria-label="Add text">
        <label>
          Title
          <input value={title} onChange={(e) => setTitle(e.target.value)} maxLength={200} />
        </label>
        <label>
          Content
          <textarea value={text} onChange={(e) => setText(e.target.value)} rows={6} />
        </label>
        <button type="submit" disabled={busy}>
          {busy ? "Saving…" : "Save text"}
        </button>
      </form>

      <h2>Upload a document</h2>
      <form onSubmit={(e) => void submitFile(e)} aria-label="Upload file">
        <p className="muted">Accepted: .txt, .md, .pdf, .docx — up to 10 MB. The title defaults to the file name.</p>
        <label>
          Title (optional)
          <input value={title} onChange={(e) => setTitle(e.target.value)} maxLength={200} />
        </label>
        <label>
          File
          <input
            type="file"
            accept=".txt,.md,.pdf,.docx"
            onChange={(e) => setFile(e.target.files?.[0] ?? null)}
          />
        </label>
        <button type="submit" disabled={busy}>
          {busy ? "Uploading…" : "Upload"}
        </button>
      </form>
      {formError && (
        <p role="alert" className="error-text">
          {formError}
        </p>
      )}
    </Layout>
  );
}

export function WidgetsPage({ orgId }: { orgId: string }) {
  const client = useClient();
  const [items, setItems] = useState<Widget[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  async function load() {
    try {
      setItems((await widgets.list(client, orgId)).widgets);
    } catch (err) {
      setError(err instanceof ApiError ? `${err.code}: ${err.message}` : "Could not load widgets.");
    }
  }

  useEffect(() => {
    void load();
  }, [orgId]); // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <Layout>
      <p>
        <a href={`#/orgs/${orgId}`}>← Knowledge base</a>
      </p>
      <h1>Widget settings</h1>
      {error && <ErrorState message={error} onRetry={() => void load()} />}
      {items === null ? (
        <Loading />
      ) : items.length === 0 ? (
        <Empty message="No widgets registered yet." />
      ) : (
        <ul className="list">
          {items.map((w) => (
            <li key={w.id}>
              <a href={`#/orgs/${orgId}/widgets/${w.id}`}>{w.displayName}</a>{" "}
              <span className="muted">
                {w.publicWidgetId} · {w.active ? "active" : "disabled"}
              </span>
            </li>
          ))}
        </ul>
      )}
    </Layout>
  );
}
