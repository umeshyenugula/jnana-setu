import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App, { parseHash } from "./App";
import { TestChatPage } from "./pages/widgetsTeamChat";
import { AuthProvider } from "./auth/AuthContext";
import { saveSession } from "./auth/supabaseAuth";

const session = {
  accessToken: "tok",
  refreshToken: "ref",
  userId: "user-1",
  email: "admin@example.com",
  expiresAt: Date.now() + 3600_000,
};

function envelope(data: unknown) {
  return new Response(JSON.stringify({ success: true, data, meta: {} }), { status: 200 });
}

describe("routing", () => {
  it("parses hash routes", () => {
    expect(parseHash("#/")).toEqual({ name: "orgs" });
    expect(parseHash("#/signin")).toEqual({ name: "signin" });
    expect(parseHash("#/orgs/o1")).toEqual({ name: "org", orgId: "o1" });
    expect(parseHash("#/orgs/o1/docs/d1")).toEqual({ name: "doc", orgId: "o1", docId: "d1" });
    expect(parseHash("#/orgs/o1/widgets")).toEqual({ name: "widgets", orgId: "o1" });
    expect(parseHash("#/orgs/o1/widgets/w1")).toEqual({ name: "widget", orgId: "o1", widgetId: "w1" });
    expect(parseHash("#/orgs/o1/team")).toEqual({ name: "team", orgId: "o1" });
    expect(parseHash("#/orgs/o1/test-chat")).toEqual({ name: "testchat", orgId: "o1" });
    expect(parseHash("#/nope")).toEqual({ name: "notfound" });
  });
});

describe("protected routes", () => {
  beforeEach(() => {
    sessionStorage.clear();
    window.location.hash = "#/";
    vi.restoreAllMocks();
  });

  it("redirects anonymous visitors to sign-in", async () => {
    render(<App />);
    await waitFor(() => expect(window.location.hash).toBe("#/signin"));
    await screen.findByRole("heading", { name: /sign in/i });
  });

  it("shows organizations to signed-in admins", async () => {
    saveSession(session);
    vi.spyOn(globalThis, "fetch").mockImplementation(async () =>
      envelope({ organizations: [{ id: "o1", name: "Acme", slug: "acme", status: "active" }] }),
    );
    window.location.hash = "#/";
    render(<App />);
    await screen.findByText("Acme");
  });
});

describe("test chat", () => {
  beforeEach(() => {
    sessionStorage.clear();
    saveSession(session);
    vi.restoreAllMocks();
  });

  function renderTestChat(fetchImpl: (url: string, init?: RequestInit) => Promise<Response>) {
    vi.spyOn(globalThis, "fetch").mockImplementation(fetchImpl as typeof fetch);
    return render(
      <AuthProvider>
        <TestChatPage orgId="org-1" />
      </AuthProvider>,
    );
  }

  it("displays insufficient-evidence answers distinctly", async () => {
    const user = userEvent.setup();
    renderTestChat(async (url: string, init?: RequestInit) => {
      if (url.includes("/widgets") && (!init || init.method === undefined || init.method === "GET")) {
        return envelope({ widgets: [{ id: "w1", publicWidgetId: "wdg_1", displayName: "Shop", active: true }] });
      }
      return envelope({
        sessionId: "ses_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        status: "insufficient_evidence",
        answer: "I do not know.",
        sources: [],
      });
    });
    await user.type(screen.getByLabelText("Message"), "Do you sell boats?");
    await user.click(screen.getByRole("button", { name: "Ask" }));
    await screen.findByText("insufficient_evidence");
    await screen.findByText("I do not know.");
  });

  it("renders verified source identifiers from the trusted response", async () => {
    const user = userEvent.setup();
    renderTestChat(async (url: string, init?: RequestInit) => {
      if (url.includes("/widgets")) {
        return envelope({ widgets: [{ id: "w1", publicWidgetId: "wdg_1", displayName: "Shop", active: true }] });
      }
      return envelope({
        sessionId: "ses_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        status: "answered",
        answer: "Yes.",
        sources: [{ documentId: "doc-1", versionId: "ver-1", chunkId: "c-1", rank: 1, excerpt: "Yes indeed." }],
      });
    });
    await user.type(screen.getByLabelText("Message"), "Is it yes?");
    await user.click(screen.getByRole("button", { name: "Ask" }));
    await screen.findByText(/doc-1/);
  });

  it("surfaces API errors with backend codes", async () => {
    const user = userEvent.setup();
    renderTestChat(async (url: string) => {
      if (url.includes("/widgets")) {
        return envelope({ widgets: [{ id: "w1", publicWidgetId: "wdg_1", displayName: "Shop", active: true }] });
      }
      return new Response(
        JSON.stringify({ success: false, error: { code: "WIDGET_NOT_FOUND", message: "Gone." }, meta: {} }),
        { status: 404 },
      );
    });
    await user.type(screen.getByLabelText("Message"), "Hi");
    await user.click(screen.getByRole("button", { name: "Ask" }));
    await screen.findByText(/WIDGET_NOT_FOUND/);
  });
});
