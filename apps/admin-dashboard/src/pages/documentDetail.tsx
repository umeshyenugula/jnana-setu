import { useEffect, useState } from "react";
import { ApiError } from "../api/client";
import { knowledge } from "../api/resources";
import type { DocumentVersion, KnowledgeDocument } from "../api/types";
import { Empty, ErrorState, Layout, Loading, StatusBadge } from "../components/ui";
import { useClient } from "./organizations";

export function DocumentDetailPage({ orgId, docId }: { orgId: string; docId: string }) {
  const client = useClient();
  const [doc, setDoc] = useState<KnowledgeDocument | null>(null);
  const [versions, setVersions] = useState<DocumentVersion[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [newText, setNewText] = useState("");
  const [busy, setBusy] = useState(false);

  async function load() {
    setError(null);
    try {
      const detail = await knowledge.get(client, orgId, docId);
      setDoc(detail.document);
      setVersions((await knowledge.versions(client, orgId, docId)).versions);
    } catch (err) {
      setError(err instanceof ApiError ? `${err.code}: ${err.message}` : "Could not load the document.");
    }
  }

  useEffect(() => {
    void load();
  }, [orgId, docId]); // eslint-disable-line react-hooks/exhaustive-deps

  async function run(label: string, action: () => Promise<unknown>) {
    setNotice(null);
    setBusy(true);
    try {
      await action();
      await load();
      setNotice(`${label} succeeded.`);
    } catch (err) {
      setError(err instanceof ApiError ? `${err.code}: ${err.message}` : `${label} failed.`);
    } finally {
      setBusy(false);
    }
  }

  async function addVersion(event: React.FormEvent) {
    event.preventDefault();
    if (!newText.trim()) {
      setError("Version content must not be empty.");
      return;
    }
    await run("New version", async () => {
      await knowledge.addTextVersion(client, orgId, docId, newText);
      setNewText("");
    });
  }

  async function remove() {
    if (!window.confirm("Delete this document and queue its index cleanup?")) return;
    await run("Delete", () => knowledge.remove(client, orgId, docId));
  }

  return (
    <Layout>
      <p>
        <a href={`#/orgs/${orgId}`}>← Knowledge base</a>
      </p>
      {error && <ErrorState message={error} onRetry={() => void load()} />}
      {notice && (
        <p role="status" className="ok-text">
          {notice}
        </p>
      )}
      {doc === null ? (
        <Loading />
      ) : (
        <>
          <h1>{doc.title}</h1>
          <p>
            <StatusBadge status={doc.isActive ? doc.processingStatus : "inactive"} />{" "}
            <span className="muted">
              {doc.sourceType} · version {versions?.length ?? "…"}
            </span>
          </p>
          <div className="actions" role="group" aria-label="Lifecycle">
            <button type="button" disabled={busy || !doc.isActive} onClick={() => void run("Deactivate", () => knowledge.setActive(client, orgId, docId, false))}>
              Deactivate
            </button>
            <button type="button" disabled={busy || doc.isActive} onClick={() => void run("Publish", () => knowledge.setActive(client, orgId, docId, true))}>
              Publish
            </button>
            <button type="button" disabled={busy} onClick={() => void run("Reprocess", () => knowledge.reprocess(client, orgId, docId))}>
              Reprocess
            </button>
            <button type="button" disabled={busy} onClick={() => void remove()}>
              Delete
            </button>
          </div>

          <h2>Version history</h2>
          {versions === null ? (
            <Loading />
          ) : versions.length === 0 ? (
            <Empty message="No versions recorded." />
          ) : (
            <ol className="list">
              {versions.map((v) => (
                <li key={v.id}>
                  v{v.versionNumber} · {v.contentType} · {(v.contentSize / 1024).toFixed(1)} KB{" "}
                  {doc.currentVersionId === v.id && <strong>(current)</strong>}{" "}
                  {v.originalFilename && <span className="muted">{v.originalFilename}</span>}
                </li>
              ))}
            </ol>
          )}

          <h2>Add text version</h2>
          <form onSubmit={(e) => void addVersion(e)} aria-label="Add version">
            <label>
              Content
              <textarea value={newText} onChange={(e) => setNewText(e.target.value)} rows={5} />
            </label>
            <button type="submit" disabled={busy}>
              Save new version
            </button>
          </form>
        </>
      )}
    </Layout>
  );
}
