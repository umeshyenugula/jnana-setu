import { useEffect, useState } from "react";
import { ApiClient, ApiError, apiBaseUrl } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { organizations } from "../api/resources";
import type { Organization } from "../api/types";
import { Empty, ErrorState, Layout, Loading } from "../components/ui";

export function useClient(): ApiClient {
  const { session } = useAuth();
  const token = session?.accessToken ?? null;
  return new ApiClient({ baseUrl: apiBaseUrl(), getToken: () => token });
}

export function SignInPage() {
  const { session, signIn } = useAuth();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  if (session) {
    window.location.hash = "#/";
    return null;
  }

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setError(null);
    if (!email.trim() || !password) {
      setError("Email and password are required.");
      return;
    }
    setBusy(true);
    try {
      await signIn(email, password);
      window.location.hash = "#/";
    } catch (err) {
      setError(err instanceof Error ? err.message : "Sign-in failed.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="content narrow">
      <h1>Sign in</h1>
      <p className="muted">Use your administrator account. Authentication is verified by the backend on every request.</p>
      <form onSubmit={(e) => void submit(e)} aria-label="Sign in">
        <label>
          Email
          <input type="email" autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
        </label>
        <label>
          Password
          <input type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required />
        </label>
        {error && <p role="alert" className="error-text">{error}</p>}
        <button type="submit" disabled={busy}>
          {busy ? "Signing in…" : "Sign in"}
        </button>
      </form>
    </main>
  );
}

export function OrganizationsPage() {
  const client = useClient();
  const [orgs, setOrgs] = useState<Organization[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");

  async function load() {
    setError(null);
    try {
      const data = await organizations.list(client);
      setOrgs(Array.isArray(data.organizations) ? data.organizations : []);
    } catch (err) {
      setError(err instanceof ApiError ? `${err.code}: ${err.message}` : "Could not load organizations.");
    }
  }

  useEffect(() => {
    void load();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  async function create(event: React.FormEvent) {
    event.preventDefault();
    try {
      await organizations.create(client, name.trim(), slug.trim());
      setName("");
      setSlug("");
      await load();
    } catch (err) {
      setError(err instanceof ApiError ? `${err.code}: ${err.message}` : "Could not create the organization.");
    }
  }

  return (
    <Layout>
      <h1>Organizations</h1>
      {error && <ErrorState message={error} onRetry={() => void load()} />}
      {orgs === null ? (
        <Loading />
      ) : orgs.length === 0 ? (
        <Empty message="No organizations yet. Create the first one below." />
      ) : (
        <ul className="list">
          {orgs.map((org) => (
            <li key={org.id}>
              <a href={`#/orgs/${org.id}`}>{org.name}</a>{" "}
              <span className="muted">
                {org.slug} · {org.status}
              </span>
            </li>
          ))}
        </ul>
      )}
      <h2>New organization</h2>
      <form onSubmit={(e) => void create(e)} aria-label="New organization">
        <label>
          Name
          <input value={name} onChange={(e) => setName(e.target.value)} required maxLength={120} />
        </label>
        <label>
          Slug
          <input value={slug} onChange={(e) => setSlug(e.target.value)} required maxLength={64} />
        </label>
        <button type="submit">Create</button>
      </form>
    </Layout>
  );
}
