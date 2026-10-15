import { describe, expect, it } from "vitest";
import { guardInbound, guardOutbound } from "./loaderMessages";

function inboundEvent(origin: string, data: unknown): MessageEvent {
  return { origin, data } as MessageEvent;
}

describe("cross-origin message validation", () => {
  it("accepts documented commands from the exact origin", () => {
    expect(
      guardInbound(inboundEvent("https://host.example.com", { namespace: "jnana-widget", type: "jnana-open" }), "https://host.example.com"),
    ).toEqual({ type: "jnana-open" });
    expect(
      guardInbound(inboundEvent("https://host.example.com", { namespace: "jnana-widget", type: "jnana-toggle" }), "https://host.example.com"),
    ).toEqual({ type: "jnana-toggle" });
  });

  it("rejects wrong origins, namespaces, payloads, and HTML", () => {
    const good = { namespace: "jnana-widget", type: "jnana-open" };
    expect(guardInbound(inboundEvent("https://evil.example.com", good), "https://host.example.com")).toBeNull();
    expect(guardInbound(inboundEvent("https://host.example.com", { namespace: "other", type: "jnana-open" }), "https://host.example.com")).toBeNull();
    expect(guardInbound(inboundEvent("https://host.example.com", { namespace: "jnana-widget", type: "delete-everything" }), "https://host.example.com")).toBeNull();
    expect(guardInbound(inboundEvent("https://host.example.com", "<img src=x onerror=alert(1)>"), "https://host.example.com")).toBeNull();
    expect(guardInbound(inboundEvent("https://host.example.com", null), "https://host.example.com")).toBeNull();
  });

  it("validates outbound iframe messages", () => {
    expect(
      guardOutbound(inboundEvent("https://w.example.com", { namespace: "jnana-widget", type: "jnana-opened" }), "https://w.example.com"),
    ).toEqual({ type: "jnana-opened" });
    expect(
      guardOutbound(inboundEvent("https://w.example.com", { namespace: "jnana-widget", type: "jnana-resize", height: 400 }), "https://w.example.com"),
    ).toEqual({ type: "jnana-resize", height: 400 });
    expect(
      guardOutbound(inboundEvent("https://evil.example.com", { namespace: "jnana-widget", type: "jnana-opened" }), "https://w.example.com"),
    ).toBeNull();
    expect(
      guardOutbound(inboundEvent("https://w.example.com", { namespace: "jnana-widget", type: "jnana-resize", height: 99999 }), "https://w.example.com"),
    ).toBeNull();
  });
});
