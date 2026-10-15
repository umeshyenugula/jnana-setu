import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import { AuthProvider, useAuth } from "./auth/AuthContext";
import { SignInPage, OrganizationsPage } from "./pages/organizations";
import { OrgDetailPage, WidgetsPage } from "./pages/orgDetail";
import { DocumentDetailPage } from "./pages/documentDetail";
import { TeamPage, TestChatPage, WidgetDetailPage } from "./pages/widgetsTeamChat";

type Route =
  | { name: "signin" }
  | { name: "orgs" }
  | { name: "org"; orgId: string }
  | { name: "doc"; orgId: string; docId: string }
  | { name: "widgets"; orgId: string }
  | { name: "widget"; orgId: string; widgetId: string }
  | { name: "team"; orgId: string }
  | { name: "testchat"; orgId: string }
  | { name: "notfound" };

export function parseHash(hash: string): Route {
  const parts = hash.replace(/^#\/?/, "").split("/").filter(Boolean);
  if (parts.length === 0) return { name: "orgs" };
  if (parts[0] === "signin") return { name: "signin" };
  if (parts[0] === "orgs" && parts.length === 1) return { name: "orgs" };
  if (parts[0] === "orgs" && parts[1]) {
    const orgId = parts[1];
    if (parts.length === 2) return { name: "org", orgId };
    if (parts[2] === "docs" && parts[3]) return { name: "doc", orgId, docId: parts[3] };
    if (parts[2] === "widgets" && parts.length === 3) return { name: "widgets", orgId };
    if (parts[2] === "widgets" && parts[3]) return { name: "widget", orgId, widgetId: parts[3] };
    if (parts[2] === "team") return { name: "team", orgId };
    if (parts[2] === "test-chat") return { name: "testchat", orgId };
  }
  return { name: "notfound" };
}

function Guarded({ children }: { children: ReactNode }) {
  const { session } = useAuth();
  useEffect(() => {
    if (!session) window.location.hash = "#/signin";
  }, [session]);
  if (!session) return null;
  return children;
}

function Router() {
  const [route, setRoute] = useState<Route>(() => parseHash(window.location.hash));
  useEffect(() => {
    const onChange = () => setRoute(parseHash(window.location.hash));
    window.addEventListener("hashchange", onChange);
    return () => window.removeEventListener("hashchange", onChange);
  }, []);

  switch (route.name) {
    case "signin":
      return <SignInPage />;
    case "orgs":
      return (
        <Guarded>
          <OrganizationsPage />
        </Guarded>
      );
    case "org":
      return (
        <Guarded>
          <OrgDetailPage orgId={route.orgId} />
        </Guarded>
      );
    case "doc":
      return (
        <Guarded>
          <DocumentDetailPage orgId={route.orgId} docId={route.docId} />
        </Guarded>
      );
    case "widgets":
      return (
        <Guarded>
          <WidgetsPage orgId={route.orgId} />
        </Guarded>
      );
    case "widget":
      return (
        <Guarded>
          <WidgetDetailPage orgId={route.orgId} widgetId={route.widgetId} />
        </Guarded>
      );
    case "team":
      return (
        <Guarded>
          <TeamPage orgId={route.orgId} />
        </Guarded>
      );
    case "testchat":
      return (
        <Guarded>
          <TestChatPage orgId={route.orgId} />
        </Guarded>
      );
    default:
      return (
        <main className="content">
          <h1>Not found</h1>
          <p>
            <a href="#/">Back to organizations</a>
          </p>
        </main>
      );
  }
}

export default function App() {
  return (
    <AuthProvider>
      <Router />
    </AuthProvider>
  );
}
