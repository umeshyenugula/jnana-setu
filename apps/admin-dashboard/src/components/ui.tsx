import type { ReactNode } from "react";
import { useAuth } from "../auth/AuthContext";

export function Loading({ label = "Loading…" }: { label?: string }) {
  return (
    <p role="status" aria-live="polite" className="muted">
      {label}
    </p>
  );
}

export function Empty({ message }: { message: string }) {
  return (
    <p role="status" className="muted">
      {message}
    </p>
  );
}

export function ErrorState({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div role="alert" className="error-box">
      <p>{message}</p>
      {onRetry && (
        <button type="button" onClick={onRetry}>
          Retry
        </button>
      )}
    </div>
  );
}

export function StatusBadge({ status }: { status: string }) {
  return (
    <span className={`badge badge-${status}`} data-testid={`status-${status}`}>
      {status}
    </span>
  );
}

export function Layout({ children }: { children: ReactNode }) {
  const { session, signOut } = useAuth();
  return (
    <div className="app">
      <header className="topbar">
        <nav aria-label="Primary">
          <a href="#/">Organizations</a>
        </nav>
        <div className="topbar-right">
          <span className="muted">{session?.email}</span>
          <button type="button" onClick={() => void signOut()}>
            Sign out
          </button>
        </div>
      </header>
      <main className="content">{children}</main>
    </div>
  );
}
