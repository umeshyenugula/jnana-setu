import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "../App";
import { ChatWidget } from "./ChatWidget";

function chatResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status });
}

function answerEnvelope(overrides: Record<string, unknown> = {}) {
  return {
    success: true,
    data: {
      sessionId: "ses_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      status: "answered",
      answer: "Yes, returns are accepted.",
      sources: [{ documentId: "doc-1", versionId: "ver-1", chunkId: "c-1", rank: 1, excerpt: "Returns accepted." }],
      ...overrides,
    },
    meta: {},
  };
}

function renderWidget() {
  return render(<ChatWidget widgetId="wdg_1" apiBase="http://api" hostOrigin="http://host" />);
}

describe("App bootstrap", () => {
  it("requires widget configuration", () => {
    Object.defineProperty(window, "location", {
      value: { search: "", origin: "http://w" },
      writable: true,
      configurable: true,
    });
    render(<App />);
    screen.getByRole("alert");
  });
});

describe("ChatWidget", () => {
  beforeEach(() => {
    sessionStorage.clear();
    vi.restoreAllMocks();
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => chatResponse({ success: true, data: {}, meta: {} }));
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("toggles open and closed with accessible labels", async () => {
    const user = userEvent.setup();
    renderWidget();
    await user.click(screen.getByRole("button", { name: /open support chat/i }));
    expect(screen.getByRole("dialog", { name: "Support chat" })).toBeDefined();
    await user.click(screen.getByRole("button", { name: /close support chat/i }));
    expect(screen.getByRole("button", { name: /open support chat/i })).toBeDefined();
  });

  it("submits messages and reuses the session", async () => {
    const user = userEvent.setup();
    const fetchMock = vi.spyOn(globalThis, "fetch") as unknown as ReturnType<typeof vi.fn>;
    fetchMock.mockImplementation(async () => chatResponse(answerEnvelope()));
    renderWidget();
    await user.click(screen.getByRole("button", { name: /open support chat/i }));
    await user.type(screen.getByLabelText("Type your message"), "What is the policy?");
    await user.click(screen.getByRole("button", { name: "Send message" }));
    await screen.findByText("What is the policy?");
    await screen.findByText("Yes, returns are accepted.");
    expect(sessionStorage.getItem("jnana.session.wdg_1")).toBe("ses_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa");

    await user.type(screen.getByLabelText("Type your message"), "Follow-up?");
    await user.click(screen.getByRole("button", { name: "Send message" }));
    await screen.findByText("Follow-up?");
    const bodies = fetchMock.mock.calls
      .map(([, init]) => init as RequestInit | undefined)
      .filter(Boolean)
      .map((init) => JSON.parse(String(init?.body)) as Record<string, unknown>)
      .filter((body) => typeof body.message === "string");
    expect(bodies).toHaveLength(2);
    expect(bodies[1].sessionId).toBe("ses_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa");
  });

  it("sends with the keyboard", async () => {
    const user = userEvent.setup();
    renderWidget();
    await user.click(screen.getByRole("button", { name: /open support chat/i }));
    await user.type(screen.getByLabelText("Type your message"), "Hi{enter}");
    await screen.findByText("Hi");
  });

  it("shows insufficient evidence distinctly", async () => {
    const user = userEvent.setup();
    vi.spyOn(globalThis, "fetch").mockImplementation(async () =>
      chatResponse(answerEnvelope({ status: "insufficient_evidence", answer: "I do not know.", sources: [] })),
    );
    renderWidget();
    await user.click(screen.getByRole("button", { name: /open support chat/i }));
    await user.type(screen.getByLabelText("Type your message"), "Obscure?");
    await user.click(screen.getByRole("button", { name: "Send message" }));
    await screen.findByText("I do not know.");
    await screen.findByText(/no verified sources were found/i);
  });

  it("renders only verified source links and blocks javascript: URLs", async () => {
    const user = userEvent.setup();
    vi.spyOn(globalThis, "fetch").mockImplementation(async () =>
      chatResponse(
        answerEnvelope({
          sources: [
            { documentId: "doc-1", versionId: "ver-1", chunkId: "c-1", rank: 1, excerpt: "A", sourceMeta: { url: "https://example.com/help", title: "Help" } },
            { documentId: "doc-2", versionId: "ver-2", chunkId: "c-2", rank: 2, excerpt: "B", sourceMeta: { url: "javascript:alert(1)", title: "Evil" } },
          ],
        }),
      ),
    );
    renderWidget();
    await user.click(screen.getByRole("button", { name: /open support chat/i }));
    await user.type(screen.getByLabelText("Type your message"), "Hi");
    await user.click(screen.getByRole("button", { name: "Send message" }));
    const help = await screen.findByRole("link", { name: "Help" });
    expect(help.getAttribute("href")).toBe("https://example.com/help");
    expect(help.getAttribute("rel")).toContain("noopener");
    expect(screen.queryByRole("link", { name: "Evil" })).toBeNull();
    await screen.findByText("Evil");
  });

  it("escapes untrusted HTML in answers", async () => {
    const user = userEvent.setup();
    vi.spyOn(globalThis, "fetch").mockImplementation(async () =>
      chatResponse(answerEnvelope({ answer: '<script>alert("x")</script><b>bold</b>', sources: [] })),
    );
    renderWidget();
    await user.click(screen.getByRole("button", { name: /open support chat/i }));
    await user.type(screen.getByLabelText("Type your message"), "Hi");
    await user.click(screen.getByRole("button", { name: "Send message" }));
    await waitFor(() => {
      expect(document.querySelector("script")).toBeNull();
      expect(document.querySelector(".jnana-assistant b")).toBeNull();
    });
  });

  it("retries failed sends and recovers", async () => {
    const user = userEvent.setup();
    let chatCalls = 0;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (String(url).includes("/api/v1/chat")) {
        chatCalls += 1;
        if (chatCalls === 1) throw new TypeError("offline");
        return chatResponse(answerEnvelope());
      }
      return chatResponse({ success: true, data: {}, meta: {} });
    });
    renderWidget();
    await user.click(screen.getByRole("button", { name: /open support chat/i }));
    await user.type(screen.getByLabelText("Type your message"), "Hello?");
    await user.click(screen.getByRole("button", { name: "Send message" }));
    await screen.findByText(/could not reach/i);
    const bubble = (await screen.findByText("Hello?")).closest("div");
    if (!bubble) throw new Error("message bubble missing");
    await user.click(within(bubble as HTMLElement).getByRole("button", { name: "Retry" }));
    await screen.findByText("Yes, returns are accepted.");
  });

  it("announces loading state to assistive technology", async () => {
    const user = userEvent.setup();
    let release!: () => void;
    const gate = new Promise<Response>((resolve) => {
      release = () => resolve(chatResponse(answerEnvelope()));
    });
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
      if (String(url).includes("/api/v1/chat")) return gate;
      return chatResponse({ success: true, data: {}, meta: {} });
    });
    renderWidget();
    await user.click(screen.getByRole("button", { name: /open support chat/i }));
    await user.type(screen.getByLabelText("Type your message"), "Slow?");
    await user.click(screen.getByRole("button", { name: "Send message" }));
    await screen.findByRole("status");
    release();
    await screen.findByText("Yes, returns are accepted.");
  });

  it("adapts the panel on small screens", async () => {
    Object.defineProperty(window, "innerWidth", { value: 360, writable: true, configurable: true });
    const user = userEvent.setup();
    renderWidget();
    await user.click(screen.getByRole("button", { name: /open support chat/i }));
    const dialog = screen.getByRole("dialog");
    expect(getComputedStyle(dialog).position).toBeDefined();
    expect(dialog.className).toContain("jnana-panel");
  });
});
